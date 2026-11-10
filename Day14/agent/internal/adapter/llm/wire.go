package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ai-adv-agent/internal/port"
)

// marshalRequest encodes body and, when llm.request_max_bytes is set and the
// payload exceeds it, trims the message history with fitBody and encodes again.
func (c *Client) marshalRequest(body *apiRequest) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	limit := c.cfg.RequestMaxBytes
	if limit <= 0 || len(payload) <= limit {
		return payload, nil
	}
	overhead := len(payload) - messagesSize(body.Messages)
	fitted, err := fitBody(body.Messages, limit-overhead)
	if err != nil {
		return nil, err
	}
	body.Messages = fitted
	if payload, err = json.Marshal(body); err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	if len(payload) > limit {
		return nil, &port.LLMError{
			Status: http.StatusRequestEntityTooLarge, Code: port.CodeRequestTooLargeClient, Type: "invalid_request_error",
			Message: fmt.Sprintf("тело запроса %d байт больше llm.request_max_bytes=%d", len(payload), limit),
		}
	}
	return payload, nil
}

// readBody reads at most ResponseMaxBytes of the response. A larger body is an
// error rather than a silently truncated JSON document.
func (c *Client) readBody(resp *http.Response) ([]byte, error) {
	limit := c.cfg.ResponseMaxBytes
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, &transientError{fmt.Errorf("read response: %w", err)}
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("read response: body exceeds %d bytes (llm.response_max_bytes)", limit)
	}
	return b, nil
}

// decodeError turns a non-200 response into *port.LLMError. The OpenAI error
// envelope is decoded when present; otherwise a redacted snippet of the body
// becomes the message.
func (c *Client) decodeError(resp *http.Response, body []byte) error {
	e := &port.LLMError{
		Status:     resp.StatusCode,
		RequestID:  resp.Header.Get("X-Request-ID"),
		RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
	}
	var env apiError
	if json.Unmarshal(body, &env) == nil && (env.Error.Message != "" || len(env.Error.Code) > 0) {
		e.Message = c.redact(env.Error.Message)
		e.Type = env.Error.Type
		e.Code = rawCode(env.Error.Code)
		if env.Error.Param != nil {
			e.Param = *env.Error.Param
		}
	} else {
		e.Message = c.snippet(body)
	}
	return e
}

// rawCode accepts error.code as a JSON string or number.
func rawCode(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

// parseRetryAfter accepts delta-seconds or an HTTP-date (RFC 9110). Invalid or
// past values yield 0.
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil {
		if n < 0 {
			return 0
		}
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

// parseGuardHeaders reads the guard proxy's X-Guard-* headers.
func parseGuardHeaders(h http.Header) port.GuardMeta {
	g := port.GuardMeta{
		Input:          h.Get("X-Guard-Input"),
		Output:         h.Get("X-Guard-Output"),
		InputFindings:  h.Get("X-Guard-Input-Findings"),
		OutputFindings: h.Get("X-Guard-Output-Findings"),
		UsageSource:    h.Get("X-Guard-Usage-Source"),
	}
	if v := strings.TrimSpace(h.Get("X-Guard-Cost-USD")); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			g.CostUSD, g.HasCost = f, true
		}
	}
	return g
}

// classifyGuard marks the response as blocked when the model's answer did not
// get through: the proxy says so, the model stopped on its content filter, or
// the content is the proxy's refusal text. It is not an error — the caller
// decides that the step failed.
func (c *Client) classifyGuard(r *port.LLMResponse) {
	if r.Guard.Output == port.GuardBlocked {
		return
	}
	if r.FinishReason == "content_filter" ||
		(c.cfg.RefusalText != "" && strings.TrimSpace(r.Content) == strings.TrimSpace(c.cfg.RefusalText)) {
		r.Guard.Output = port.GuardBlocked
	}
}

// ---------------------------------------------------------------------------
// Secret hygiene for logs and error messages
// ---------------------------------------------------------------------------

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._~+/=-]+`),
	regexp.MustCompile(`(?i)basic\s+[A-Za-z0-9+/=]{8,}`),
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{8,}`),
}

const redacted = "[REDACTED]"

// redact removes the configured API key and anything that looks like a bearer
// token or an sk- key from s.
func (c *Client) redact(s string) string {
	if k := c.cfg.APIKey; len(k) >= 4 {
		s = strings.ReplaceAll(s, k, redacted)
	}
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, redacted)
	}
	return s
}

// snippet renders at most 512 bytes of a body for an error message, redacted.
func (c *Client) snippet(b []byte) string {
	const limit = 512
	s := string(b)
	if len(s) > limit {
		s = s[:limit] + "…"
	}
	return c.redact(s)
}

// debugDump prints the request payload, pretty-printed and redacted. Headers
// are never printed: they carry the Authorization token.
func (c *Client) debugDump(title string, payload []byte) {
	var pretty bytes.Buffer
	text := string(payload)
	if json.Indent(&pretty, payload, "", "  ") == nil {
		text = pretty.String()
	}
	fmt.Fprintf(c.log, "[debug] %s\n%s\n\n", title, c.redact(text))
}
