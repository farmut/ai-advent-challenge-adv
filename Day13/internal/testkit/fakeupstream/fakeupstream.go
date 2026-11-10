// Package fakeupstream implements a deterministic OpenAI-compatible fake
// upstream: it serves scripted replies from per-role queues (a JSON script)
// or from the legacy Script/Enqueue API used by the proxy e2e tests. The role
// is detected from a `# profile: <name>` / `# role: <name>` marker in the
// first lines of the system prompt.
package fakeupstream

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"agile-consultant/internal/ports"
)

// DefaultReply is the fake upstream answer when a script/case defines none.
const DefaultReply = "Рекомендую провести ретроспективу в формате Start/Stop/Continue и ограничить её 60 минутами."

// markerScanLines is how many leading lines of the system prompt are
// inspected for a role marker.
const markerScanLines = 5

// Usage is a scripted upstream usage.
type Usage struct {
	Prompt     int `json:"prompt_tokens"`
	Completion int `json:"completion_tokens"`
}

// Reply is one scripted answer. With ToolCalls the message content is null
// unless Content is set; Finish defaults to "tool_calls" with tool calls
// and "stop" otherwise. Usage, when set, overrides the default usage for
// this reply only.
type Reply struct {
	Content   string           `json:"content,omitempty"`
	ToolCalls []ports.ToolCall `json:"tool_calls,omitempty"`
	Finish    string           `json:"finish_reason,omitempty"`
	Usage     *Usage           `json:"usage,omitempty"`
}

// Valid finish reasons for a scripted reply (empty means "derive").
var validFinishReasons = map[string]bool{
	"": true, "stop": true, "length": true, "tool_calls": true, "content_filter": true,
}

// scriptFile is the on-disk JSON script format:
//
//	{"roles": {"planner": [reply, ...], "coder": [reply, ...]},
//	 "default": {"content": "ok"}}
type scriptFile struct {
	Roles   map[string][]Reply `json:"roles"`
	Default *Reply             `json:"default"`
}

// Handler is a scripted OpenAI-compatible /v1/chat/completions handler.
// It is shared by the test FakeUpstream (httptest) and the standalone
// cmd/fake-upstream server. Safe for concurrent use.
type Handler struct {
	mu     sync.Mutex
	reply  string
	usage  *Usage
	queue  []Reply
	roles  map[string][]Reply
	served map[string]int

	// OnRole, if set, is called with the detected role for every request
	// ("" when the request carries no marker). Set it before serving.
	OnRole func(role string)
}

// NewHandler returns a Handler serving DefaultReply with a small default
// usage, mirroring the historical FakeUpstream behaviour.
func NewHandler() *Handler {
	return &Handler{
		reply:  DefaultReply,
		usage:  &Usage{Prompt: 40, Completion: 20},
		served: map[string]int{},
	}
}

// Script sets the default reply; an empty reply uses DefaultReply and a nil
// usage omits the usage object. It clears the legacy queue, keeps role queues.
func (h *Handler) Script(reply string, usage *Usage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if reply == "" {
		reply = DefaultReply
	}
	h.reply, h.usage = reply, usage
	h.queue = nil
}

// Enqueue appends replies served once each (for requests without a known
// role) before the default reply.
func (h *Handler) Enqueue(rs ...Reply) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.queue = append(h.queue, rs...)
}

// SetRole sets the reply queue for a role: replies are served once each in
// order, then the last one is repeated. An empty queue removes the role.
func (h *Handler) SetRole(role string, rs ...Reply) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(rs) == 0 {
		delete(h.roles, role)
		delete(h.served, role)
		return
	}
	if h.roles == nil {
		h.roles = map[string][]Reply{}
	}
	h.roles[role] = append([]Reply(nil), rs...)
	delete(h.served, role)
}

// LoadScript loads the JSON script format (roles + default). It is
// fail-closed: invalid JSON, unknown fields, empty queues, unknown finish
// reasons, or a script without any replies are all errors.
func (h *Handler) LoadScript(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var s scriptFile
	if err := dec.Decode(&s); err != nil {
		return fmt.Errorf("invalid script: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("invalid script: trailing data after JSON document")
	}
	if len(s.Roles) == 0 && s.Default == nil {
		return errors.New("invalid script: no roles and no default reply")
	}
	for role, rs := range s.Roles {
		if len(rs) == 0 {
			return fmt.Errorf("invalid script: role %q has an empty queue", role)
		}
		for i, r := range rs {
			if !validFinishReasons[r.Finish] {
				return fmt.Errorf("invalid script: role %q reply %d has unknown finish_reason %q", role, i, r.Finish)
			}
		}
	}
	if s.Default != nil && !validFinishReasons[s.Default.Finish] {
		return fmt.Errorf("invalid script: default has unknown finish_reason %q", s.Default.Finish)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.roles = make(map[string][]Reply, len(s.Roles))
	for role, rs := range s.Roles {
		h.roles[role] = append([]Reply(nil), rs...)
	}
	h.served = map[string]int{}
	h.queue = nil
	if s.Default != nil {
		h.reply = s.Default.Content
		if h.reply == "" && len(s.Default.ToolCalls) == 0 {
			h.reply = DefaultReply
		}
		h.usage = s.Default.Usage
	} else {
		h.reply = DefaultReply
		h.usage = nil
	}
	return nil
}

// next returns the reply for the request: the role queue when the role is
// known (repeat last after exhaustion), else the legacy queue, else the
// default reply.
func (h *Handler) next(role string) (Reply, *Usage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if q, ok := h.roles[role]; ok && len(q) > 0 {
		i := h.served[role]
		h.served[role]++
		if i >= len(q) {
			i = len(q) - 1
		}
		r := q[i]
		if r.Usage != nil {
			return r, r.Usage
		}
		return r, h.usage
	}
	if len(h.queue) > 0 {
		r := h.queue[0]
		h.queue = h.queue[1:]
		if r.Usage != nil {
			return r, r.Usage
		}
		return r, h.usage
	}
	return Reply{Content: h.reply}, h.usage
}

// DetectRole scans the first messages of the request for a system prompt
// whose leading lines contain a `# profile: <name>` or `# role: <name>`
// marker and returns the name ("" when absent).
func DetectRole(messages []struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}) string {
	for _, m := range messages {
		if m.Role != "system" {
			continue
		}
		lines := strings.Split(m.Content, "\n")
		if len(lines) > markerScanLines {
			lines = lines[:markerScanLines]
		}
		for _, line := range lines {
			line = strings.TrimSpace(line)
			for _, marker := range []string{"# profile:", "# role:"} {
				if rest, ok := strings.CutPrefix(line, marker); ok {
					if role := strings.TrimSpace(rest); role != "" {
						return role
					}
				}
			}
		}
	}
	return ""
}

// chatRequest is the minimal request shape the fake needs: roles and
// contents of the messages.
type chatRequest struct {
	Messages []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
}

// ServeHTTP implements http.Handler for POST /v1/chat/completions.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		http.Error(w, `{"error":{"type":"invalid_request","message":"unreadable body"}}`, http.StatusBadRequest)
		return
	}
	var req chatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, `{"error":{"type":"invalid_request","message":"invalid JSON body"}}`, http.StatusBadRequest)
		return
	}
	msgs := make([]struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}, 0, len(req.Messages))
	for _, m := range req.Messages {
		var content string
		_ = json.Unmarshal(m.Content, &content) // non-string contents carry no marker
		msgs = append(msgs, struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{m.Role, content})
	}
	role := DetectRole(msgs)
	if h.OnRole != nil {
		h.OnRole(role)
	}

	reply, usage := h.next(role)
	msg := map[string]any{"role": "assistant", "content": reply.Content}
	finish := reply.Finish
	if len(reply.ToolCalls) > 0 {
		msg["tool_calls"] = reply.ToolCalls
		if reply.Content == "" {
			msg["content"] = nil
		}
		if finish == "" {
			finish = "tool_calls"
		}
	}
	if finish == "" {
		finish = "stop"
	}
	resp := map[string]any{
		"id": "fake", "object": "chat.completion",
		"choices": []map[string]any{{"index": 0, "message": msg, "finish_reason": finish}},
	}
	if usage != nil {
		resp["usage"] = map[string]int{"prompt_tokens": usage.Prompt, "completion_tokens": usage.Completion, "total_tokens": usage.Prompt + usage.Completion}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// FakeUpstream is a scripted OpenAI-compatible server for tests. Queued
// replies (Enqueue) are served first, in order; then the Script reply is
// repeated. Role queues (SetRole/LoadScript) take precedence per request.
type FakeUpstream struct {
	*httptest.Server
	h *Handler
}

// NewFakeUpstream starts the server.
func NewFakeUpstream() *FakeUpstream {
	f := &FakeUpstream{h: NewHandler()}
	f.Server = httptest.NewServer(f.h)
	return f
}

// Script delegates to the embedded Handler; see Handler.Script.
func (f *FakeUpstream) Script(reply string, usage *Usage) { f.h.Script(reply, usage) }

// Enqueue delegates to the embedded Handler; see Handler.Enqueue.
func (f *FakeUpstream) Enqueue(rs ...Reply) { f.h.Enqueue(rs...) }

// SetRole delegates to the embedded Handler; see Handler.SetRole.
func (f *FakeUpstream) SetRole(role string, rs ...Reply) { f.h.SetRole(role, rs...) }

// LoadScript delegates to the embedded Handler; see Handler.LoadScript.
func (f *FakeUpstream) LoadScript(data []byte) error { return f.h.LoadScript(data) }
