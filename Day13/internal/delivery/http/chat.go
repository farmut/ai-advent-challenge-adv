package httpdelivery

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"

	"github.com/gin-gonic/gin"

	"agile-consultant/internal/ports"
	"agile-consultant/internal/usecase/proxy"
)

// chatRequest is the accepted subset of the OpenAI request. Unknown fields
// are ignored and never forwarded.
type chatRequest struct {
	Model               string          `json:"model"`
	Messages            []messageDTO    `json:"messages"`
	Stream              bool            `json:"stream"`
	N                   *int            `json:"n"`
	MaxTokens           *int            `json:"max_tokens"`
	MaxCompletionTokens *int            `json:"max_completion_tokens"`
	Temperature         *float64        `json:"temperature"`
	TopP                *float64        `json:"top_p"`
	Stop                json.RawMessage `json:"stop"`
	Tools               json.RawMessage `json:"tools"`
	ToolChoice          json.RawMessage `json:"tool_choice"`
	Functions           json.RawMessage `json:"functions"`
	FunctionCall        json.RawMessage `json:"function_call"`
}

type messageDTO struct {
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	ToolCalls json.RawMessage `json:"tool_calls"`
}

type contentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func present(r json.RawMessage) bool {
	t := bytes.TrimSpace(r)
	return len(t) > 0 && !bytes.Equal(t, []byte("null"))
}

func (h *handlers) chat(c *gin.Context) {
	call := proxy.Call{RequestID: reqID(c), ClientIP: c.RemoteIP(), KeyID: c.GetString(ctxKeyID)}
	body, err := io.ReadAll(c.Request.Body)
	switch {
	case err != nil && isTooLarge(err):
		call.Reject = &proxy.Reject{Kind: proxy.KindTooLarge, Code: proxy.CodeTooLarge, Message: "Request body is too large."}
	case err != nil:
		call.Reject = &proxy.Reject{Kind: proxy.KindInvalid, Code: proxy.CodeInvalidJSON, Message: "Could not read the request body."}
	default:
		var req chatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			call.Reject = &proxy.Reject{Kind: proxy.KindInvalid, Code: proxy.CodeInvalidJSON, Message: "The request body is not valid JSON or has wrong field types."}
		} else if rej := fill(&call, req); rej != nil {
			call.Reject = rej
		}
	}
	writeOutcome(c, h.svc.Chat(c.Request.Context(), call))
}

// fill converts the DTO into a Call.
func fill(call *proxy.Call, req chatRequest) *proxy.Reject {
	call.Model, call.Stream = req.Model, req.Stream
	call.Params = ports.ChatParams{MaxTokens: req.MaxTokens, Temperature: req.Temperature, TopP: req.TopP}
	if call.Params.MaxTokens == nil {
		call.Params.MaxTokens = req.MaxCompletionTokens
	}
	if present(req.Stop) {
		var one string
		var many []string
		switch {
		case json.Unmarshal(req.Stop, &one) == nil:
			call.Params.Stop = []string{one}
		case json.Unmarshal(req.Stop, &many) == nil:
			call.Params.Stop = many
		default:
			return &proxy.Reject{Kind: proxy.KindInvalid, Code: proxy.CodeInvalidRequest, Param: "stop", Message: "stop must be a string or an array of strings."}
		}
	}
	switch {
	case req.N != nil && *req.N != 1:
		call.Unsupported = "n"
	case present(req.Tools) || present(req.ToolChoice):
		call.Unsupported = "tools"
	case present(req.Functions) || present(req.FunctionCall):
		call.Unsupported = "functions"
	}
	for _, m := range req.Messages {
		text, ok := contentText(m.Content)
		if !ok && call.Unsupported == "" {
			call.Unsupported = "messages.content"
		}
		if present(m.ToolCalls) && call.Unsupported == "" {
			call.Unsupported = "tools"
		}
		call.Messages = append(call.Messages, ports.ChatMessage{Role: m.Role, Content: text})
	}
	return nil
}

// contentText accepts a string, null or an array of text parts. Any other
// part type (image_url, input_audio, file) is unsupported.
func contentText(raw json.RawMessage) (string, bool) {
	if !present(raw) {
		return "", true
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	var parts []contentPart
	if json.Unmarshal(raw, &parts) != nil {
		return "", false
	}
	texts := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.Type != "text" {
			return "", false
		}
		texts = append(texts, p.Text)
	}
	return strings.Join(texts, "\n"), true
}
