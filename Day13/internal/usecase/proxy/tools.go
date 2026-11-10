package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"agile-consultant/internal/ports"
)

// ToolLimits bound tool definitions and tool call arguments. Zero values
// are replaced by the defaults in NewService; the config layer validates
// the configured values.
type ToolLimits struct {
	// MaxTools is the maximum number of entries in "tools".
	MaxTools int
	// MaxDefinitionsBytes caps the size of the "tools" array (JSON bytes).
	MaxDefinitionsBytes int
	// MaxArgumentsBytes caps tool_calls[].function.arguments of one call.
	MaxArgumentsBytes int
}

// Default tool limits.
const (
	DefaultMaxTools            = 64
	DefaultMaxDefinitionsBytes = 256 << 10
	DefaultMaxArgumentsBytes   = 64 << 10
)

func (l ToolLimits) withDefaults() ToolLimits {
	if l.MaxTools <= 0 {
		l.MaxTools = DefaultMaxTools
	}
	if l.MaxDefinitionsBytes <= 0 {
		l.MaxDefinitionsBytes = DefaultMaxDefinitionsBytes
	}
	if l.MaxArgumentsBytes <= 0 {
		l.MaxArgumentsBytes = DefaultMaxArgumentsBytes
	}
	return l
}

var (
	toolNameRe   = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	toolCallIDRe = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)
)

// toolDef is the accepted subset of an OpenAI tool definition. Unknown
// fields are dropped: the upstream receives a re-encoded definition, never
// the raw client body.
type toolDef struct {
	Type     string      `json:"type"`
	Function toolDefFunc `json:"function"`
}

type toolDefFunc struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

type toolChoiceObj struct {
	Type     string `json:"type"`
	Function struct {
		Name string `json:"name"`
	} `json:"function"`
}

// invalidParam is a validation failure rendered as 400 invalid_request.
type invalidParam struct {
	param, msg string
}

func badParam(param, format string, a ...any) *invalidParam {
	return &invalidParam{param: param, msg: fmt.Sprintf(format, a...)}
}

// validTools holds the validated tool definitions and the re-encoded
// tool_choice (nil = absent).
type validTools struct {
	defs   []toolDef
	choice json.RawMessage
}

// maskToolDefs runs mask over the free text of tool definitions sent
// upstream: function.description and every string value of parameters
// (descriptions, enum, default, ...). Names and JSON structure are kept, so
// the schema stays valid. It returns the re-encoded "tools" (nil without
// tools).
func maskToolDefs(defs []toolDef, mask func(string) string) (json.RawMessage, error) {
	if defs == nil {
		return nil, nil
	}
	out := make([]toolDef, len(defs))
	for i, d := range defs {
		d.Function.Description = mask(d.Function.Description)
		if len(d.Function.Parameters) > 0 {
			if p, changed := mapStringLeaves(string(d.Function.Parameters), mask); changed {
				d.Function.Parameters = json.RawMessage(p)
			}
		}
		out[i] = d
	}
	return marshalNoEscape(out)
}

// validateTools checks "tools" and "tool_choice" (fail closed) and returns
// their re-encoded forms.
func validateTools(c Call, lim ToolLimits) (validTools, *invalidParam) {
	var out validTools
	names := map[string]bool{}
	if c.Tools != nil {
		if len(c.Tools) > lim.MaxDefinitionsBytes {
			return out, badParam("tools", "tools must not exceed %d bytes.", lim.MaxDefinitionsBytes)
		}
		var raws []json.RawMessage
		if err := json.Unmarshal(c.Tools, &raws); err != nil {
			return out, badParam("tools", "tools must be an array of tool definitions.")
		}
		if len(raws) == 0 || len(raws) > lim.MaxTools {
			return out, badParam("tools", "tools must contain 1..%d entries.", lim.MaxTools)
		}
		defs := make([]toolDef, 0, len(raws))
		for i, raw := range raws {
			p := fmt.Sprintf("tools[%d]", i)
			var d toolDef
			if !isJSONObject(raw) || json.Unmarshal(raw, &d) != nil {
				return out, badParam(p, "Each tool must be an object {type, function}.")
			}
			if d.Type != "function" {
				return out, badParam(p+".type", "Only tools of type \"function\" are supported.")
			}
			if !toolNameRe.MatchString(d.Function.Name) {
				return out, badParam(p+".function.name", "Tool name must match ^[A-Za-z0-9_-]{1,64}$.")
			}
			if names[d.Function.Name] {
				return out, badParam(p+".function.name", "Duplicate tool name %q.", d.Function.Name)
			}
			names[d.Function.Name] = true
			if len(d.Function.Parameters) > 0 && !isJSONObject(d.Function.Parameters) {
				return out, badParam(p+".function.parameters", "parameters must be a JSON object.")
			}
			defs = append(defs, d)
		}
		out.defs = defs
	}
	if c.ToolChoice != nil {
		if c.Tools == nil {
			return out, badParam("tool_choice", "tool_choice requires tools.")
		}
		var s string
		if json.Unmarshal(c.ToolChoice, &s) == nil {
			switch s {
			case "auto", "none", "required":
				out.choice, _ = json.Marshal(s)
				return out, nil
			}
			return out, badParam("tool_choice", "tool_choice must be \"auto\", \"none\", \"required\" or {type:function,function:{name}}.")
		}
		var o toolChoiceObj
		if !isJSONObject(c.ToolChoice) || json.Unmarshal(c.ToolChoice, &o) != nil || o.Type != "function" {
			return out, badParam("tool_choice", "tool_choice must be \"auto\", \"none\", \"required\" or {type:function,function:{name}}.")
		}
		if !names[o.Function.Name] {
			return out, badParam("tool_choice.function.name", "tool_choice names a tool that is not in tools.")
		}
		out.choice, _ = json.Marshal(o)
	}
	return out, nil
}

// validateToolMessages checks tool calls in the conversation: assistant
// tool_calls (id, type, name, arguments as a JSON object) and role=tool
// messages, which must answer a call of the preceding assistant message.
func validateToolMessages(ms []ports.ChatMessage, lim ToolLimits) *invalidParam {
	var pending map[string]bool // ids of the latest assistant tool_calls not answered yet
	for i, m := range ms {
		p := fmt.Sprintf("messages[%d]", i)
		if m.Name != "" && !toolNameRe.MatchString(m.Name) {
			return badParam(p+".name", "name must match ^[A-Za-z0-9_-]{1,64}$.")
		}
		if m.Role != "tool" {
			pending = nil
			if m.ToolCallID != "" {
				return badParam(p+".tool_call_id", "tool_call_id is allowed only in role=tool messages.")
			}
		}
		if len(m.ToolCalls) > 0 {
			if m.Role != "assistant" {
				return badParam(p+".tool_calls", "tool_calls are allowed only in assistant messages.")
			}
			pending = make(map[string]bool, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				q := fmt.Sprintf("%s.tool_calls[%d]", p, j)
				switch {
				case !toolCallIDRe.MatchString(tc.ID):
					return badParam(q+".id", "Tool call id must be 1..128 characters of [A-Za-z0-9_.:-].")
				case pending[tc.ID]:
					return badParam(q+".id", "Duplicate tool call id.")
				case tc.Type != "function":
					return badParam(q+".type", "Only tool calls of type \"function\" are supported.")
				case !toolNameRe.MatchString(tc.Function.Name):
					return badParam(q+".function.name", "Tool name must match ^[A-Za-z0-9_-]{1,64}$.")
				case len(tc.Function.Arguments) > lim.MaxArgumentsBytes:
					return badParam(q+".function.arguments", "arguments must not exceed %d bytes.", lim.MaxArgumentsBytes)
				case !isJSONObject(json.RawMessage(tc.Function.Arguments)):
					return badParam(q+".function.arguments", "arguments must be a JSON object encoded as a string.")
				}
				pending[tc.ID] = true
			}
		}
		if m.Role == "tool" {
			switch {
			case m.ToolCallID == "":
				return badParam(p+".tool_call_id", "tool_call_id is required for role=tool messages.")
			case !pending[m.ToolCallID]:
				return badParam(p+".tool_call_id", "tool_call_id does not match an unanswered tool call of the preceding assistant message.")
			}
			delete(pending, m.ToolCallID)
		}
	}
	return nil
}

// isJSONObject reports whether raw is one valid JSON object.
func isJSONObject(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || t[0] != '{' || !json.Valid(t) {
		return false
	}
	return true
}

func marshalNoEscape(v any) (json.RawMessage, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return json.RawMessage(bytes.TrimRight(b.Bytes(), "\n")), nil
}

// mapStringLeaves applies fn to every string value of the JSON document
// args (object keys are kept) and re-encodes it. When args is not valid
// JSON, the whole text is treated as one string leaf, so the guards still
// see it. changed reports whether any leaf was modified; when false the
// caller should keep args unchanged (byte-identical passthrough).
func mapStringLeaves(args string, fn func(string) string) (out string, changed bool) {
	dec := json.NewDecoder(strings.NewReader(args))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil || dec.More() {
		r := fn(args)
		return r, r != args
	}
	doc = walkLeaves(doc, fn, &changed)
	if !changed {
		return args, false
	}
	enc, err := marshalNoEscape(doc)
	if err != nil {
		// Cannot happen for a decoded document; fail closed with the
		// leaf-mapped text of the whole input.
		return fn(args), true
	}
	return string(enc), true
}

func walkLeaves(v any, fn func(string) string, changed *bool) any {
	switch t := v.(type) {
	case string:
		r := fn(t)
		if r != t {
			*changed = true
		}
		return r
	case map[string]any:
		for k, x := range t {
			t[k] = walkLeaves(x, fn, changed)
		}
		return t
	case []any:
		for i, x := range t {
			t[i] = walkLeaves(x, fn, changed)
		}
		return t
	}
	return v
}

// estimateToolCalls is the coarse token estimate of tool calls.
func estimateToolCalls(calls []ports.ToolCall) int {
	t := 0
	for _, c := range calls {
		t += estimateTokens(c.Function.Name) + estimateTokens(c.Function.Arguments)
	}
	return t
}
