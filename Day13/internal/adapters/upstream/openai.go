// Package upstream implements ports.Upstream for OpenAI-compatible chat
// completion APIs (mlx_lm.server, Ollama, LM Studio, OpenAI and compatible
// clouds).
package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"agile-consultant/internal/ports"
)

// MaxResponseBytes caps the upstream response body.
const MaxResponseBytes = 4 << 20

// OpenAI is a non-streaming chat completions client.
type OpenAI struct {
	endpoint string
	apiKey   string
	timeout  time.Duration
	hc       *http.Client
}

var _ ports.Upstream = (*OpenAI)(nil)

// NewOpenAI returns a client for baseURL (".../v1"). apiKey may be empty.
// Redirects are never followed: a 3xx is a bad response. hc may be nil.
func NewOpenAI(baseURL, apiKey string, timeout time.Duration, hc *http.Client) *OpenAI {
	c := &http.Client{}
	if hc != nil {
		cp := *hc
		c = &cp
	}
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &OpenAI{endpoint: strings.TrimRight(baseURL, "/") + "/chat/completions", apiKey: apiKey, timeout: timeout, hc: c}
}

// chatRequest lists the only fields forwarded upstream.
type chatRequest struct {
	Model       string              `json:"model"`
	Messages    []ports.ChatMessage `json:"messages"`
	Stream      bool                `json:"stream"`
	MaxTokens   *int                `json:"max_tokens,omitempty"`
	Temperature *float64            `json:"temperature,omitempty"`
	TopP        *float64            `json:"top_p,omitempty"`
	Stop        []string            `json:"stop,omitempty"`
	// Tools and ToolChoice are validated and re-encoded by the use case.
	Tools      json.RawMessage `json:"tools,omitempty"`
	ToolChoice json.RawMessage `json:"tool_choice,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content   *string        `json:"content"`
			ToolCalls []respToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// respToolCall is a tool call of the upstream answer. arguments is a JSON
// string in the OpenAI API; some compatible servers send a JSON object,
// which is accepted as its compact text.
type respToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

func (t respToolCall) toPort() (ports.ToolCall, error) {
	c := ports.ToolCall{ID: t.ID, Type: t.Type, Function: ports.ToolFunction{Name: t.Function.Name}}
	if c.Type == "" {
		c.Type = "function" // some OpenAI-compatible servers omit it
	}
	raw := bytes.TrimSpace(t.Function.Arguments)
	switch {
	case len(raw) == 0 || bytes.Equal(raw, []byte("null")):
	case raw[0] == '"':
		if err := json.Unmarshal(raw, &c.Function.Arguments); err != nil {
			return c, errors.New("tool call arguments: invalid string")
		}
	case raw[0] == '{':
		var b bytes.Buffer
		if err := json.Compact(&b, raw); err != nil {
			return c, errors.New("tool call arguments: invalid object")
		}
		c.Function.Arguments = b.String()
	default:
		return c, errors.New("tool call arguments must be a string or an object")
	}
	return c, nil
}

// Chat sends one request. The upstream body is never included in errors.
func (o *OpenAI) Chat(ctx context.Context, r ports.UpstreamRequest) (ports.UpstreamResult, error) {
	body, err := json.Marshal(chatRequest{
		Model: r.Model, Messages: r.Messages, Stream: false,
		MaxTokens: r.Params.MaxTokens, Temperature: r.Params.Temperature, TopP: r.Params.TopP, Stop: r.Params.Stop,
		Tools: r.Tools, ToolChoice: r.ToolChoice,
	})
	if err != nil {
		return ports.UpstreamResult{}, fmt.Errorf("marshal request: %w", err)
	}
	if o.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.endpoint, bytes.NewReader(body))
	if err != nil {
		return ports.UpstreamResult{}, fmt.Errorf("%w: build request: %v", ports.ErrUpstreamUnavailable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	resp, err := o.hc.Do(req)
	if err != nil {
		return ports.UpstreamResult{}, classify(ctx, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return ports.UpstreamResult{}, classify(ctx, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		// Ollama/LM Studio answer 404 for a model that is not pulled/loaded.
		return ports.UpstreamResult{}, fmt.Errorf("%w: status %d", ports.ErrUpstreamModelNotFound, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return ports.UpstreamResult{}, fmt.Errorf("%w: status %d", ports.ErrUpstreamBadResponse, resp.StatusCode)
	}
	if len(data) > MaxResponseBytes {
		return ports.UpstreamResult{}, fmt.Errorf("%w: response larger than %d bytes", ports.ErrUpstreamBadResponse, MaxResponseBytes)
	}
	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return ports.UpstreamResult{}, fmt.Errorf("%w: invalid JSON", ports.ErrUpstreamBadResponse)
	}
	if len(cr.Choices) == 0 {
		return ports.UpstreamResult{}, fmt.Errorf("%w: no choices", ports.ErrUpstreamBadResponse)
	}
	msg := cr.Choices[0].Message
	// content may be null only when the model called tools.
	if msg.Content == nil && len(msg.ToolCalls) == 0 {
		return ports.UpstreamResult{}, fmt.Errorf("%w: no content", ports.ErrUpstreamBadResponse)
	}
	res := ports.UpstreamResult{FinishReason: cr.Choices[0].FinishReason}
	if msg.Content != nil {
		res.Content = *msg.Content
	}
	for _, tc := range msg.ToolCalls {
		call, err := tc.toPort()
		if err != nil {
			return ports.UpstreamResult{}, fmt.Errorf("%w: %v", ports.ErrUpstreamBadResponse, err)
		}
		res.ToolCalls = append(res.ToolCalls, call)
	}
	if cr.Usage != nil {
		res.HasUsage = true
		res.Usage = ports.Usage{PromptTokens: cr.Usage.PromptTokens, CompletionTokens: cr.Usage.CompletionTokens, Source: ports.UsageSourceUpstream}
	}
	return res, nil
}

func classify(ctx context.Context, err error) error {
	var ne net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("%w: %v", ports.ErrUpstreamTimeout, errors.Unwrap(err))
	case errors.As(err, &ne) && ne.Timeout():
		return fmt.Errorf("%w: network timeout", ports.ErrUpstreamTimeout)
	}
	return fmt.Errorf("%w: %v", ports.ErrUpstreamUnavailable, err)
}
