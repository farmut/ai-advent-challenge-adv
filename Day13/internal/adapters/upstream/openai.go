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
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content *string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Chat sends one request. The upstream body is never included in errors.
func (o *OpenAI) Chat(ctx context.Context, r ports.UpstreamRequest) (ports.UpstreamResult, error) {
	body, err := json.Marshal(chatRequest{
		Model: r.Model, Messages: r.Messages, Stream: false,
		MaxTokens: r.Params.MaxTokens, Temperature: r.Params.Temperature, TopP: r.Params.TopP, Stop: r.Params.Stop,
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
	if len(cr.Choices) == 0 || cr.Choices[0].Message.Content == nil {
		return ports.UpstreamResult{}, fmt.Errorf("%w: no choices", ports.ErrUpstreamBadResponse)
	}
	res := ports.UpstreamResult{Content: *cr.Choices[0].Message.Content, FinishReason: cr.Choices[0].FinishReason}
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
