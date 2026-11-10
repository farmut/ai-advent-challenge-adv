package llm_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ai-adv-agent/internal/adapter/clock"
	"ai-adv-agent/internal/adapter/llm"
	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

var hiReq = port.LLMRequest{Model: "m", Messages: []domain.Message{{Role: domain.RoleUser, Content: "hi"}}}

func writeOK(w http.ResponseWriter, content, finish string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": content}, "finish_reason": finish}},
		"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2},
	})
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{
		"type": "invalid_request_error", "code": code, "param": "messages[0]", "message": msg,
	}})
}

// scripted serves the given handlers in order (the last one repeats) and
// counts calls.
func scripted(t *testing.T, hs ...http.HandlerFunc) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(n.Add(1)) - 1
		if i >= len(hs) {
			i = len(hs) - 1
		}
		hs[i](w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

func retrying(t *testing.T, url string, policy llm.RetryPolicy, clk port.Clock) port.LLMClient {
	t.Helper()
	base := mustClient(t, llm.Config{Provider: llm.ProviderOpenAI, APIKey: "sk-secret-key-123456", BaseURL: url})
	rc, err := llm.NewRetryingClient(base, policy, nil, clk)
	if err != nil {
		t.Fatal(err)
	}
	return rc
}

var defaultPolicy = llm.RetryPolicy{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: 30 * time.Second}

func TestChat_ErrorEnvelopeDecoded(t *testing.T) {
	srv, _ := scripted(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Request-ID", "req-42")
		w.Header().Set("Retry-After", "7")
		writeErr(w, http.StatusTooManyRequests, "rate_limit_exceeded", "slow down")
	})
	_, err := mustClient(t, llm.Config{Provider: llm.ProviderOpenAI, APIKey: "k", BaseURL: srv.URL}).Chat(context.Background(), hiReq)
	var le *port.LLMError
	if !errors.As(err, &le) {
		t.Fatalf("want *port.LLMError, got %T %v", err, err)
	}
	if le.Status != 429 || le.Code != "rate_limit_exceeded" || le.Param != "messages[0]" ||
		le.Message != "slow down" || le.RequestID != "req-42" || le.RetryAfter != 7*time.Second {
		t.Errorf("decoded %+v", le)
	}
	if !le.Retryable() {
		t.Error("429 must be retryable")
	}
}

func TestChat_GuardHeadersParsed(t *testing.T) {
	srv, _ := scripted(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Guard-Input", "mask")
		w.Header().Set("X-Guard-Input-Findings", "email:1")
		w.Header().Set("X-Guard-Output", "pass")
		w.Header().Set("X-Guard-Cost-USD", "0.0012")
		w.Header().Set("X-Guard-Usage-Source", "upstream")
		w.Header().Set("X-Upstream-Model", "qwen3.6-35b-a3b")
		writeOK(w, "ok", "stop")
	})
	resp, err := mustClient(t, llm.Config{Provider: llm.ProviderOpenAI, APIKey: "k", BaseURL: srv.URL}).Chat(context.Background(), hiReq)
	if err != nil {
		t.Fatal(err)
	}
	g := resp.Guard
	if g.Input != "mask" || g.InputFindings != "email:1" || g.Output != "pass" || g.UsageSource != "upstream" {
		t.Errorf("guard = %+v", g)
	}
	if !g.HasCost || g.CostUSD != 0.0012 {
		t.Errorf("cost = %v (has=%v), want 0.0012", g.CostUSD, g.HasCost)
	}
	if g.Blocked() {
		t.Error("pass must not be blocked")
	}
	if resp.UpstreamModel != "qwen3.6-35b-a3b" {
		t.Errorf("upstream model = %q, want qwen3.6-35b-a3b", resp.UpstreamModel)
	}
}

func TestChat_GuardBlockedClassification(t *testing.T) {
	const refusal = "Ответ модели скрыт политикой безопасности прокси."
	tests := []struct {
		name string
		h    http.HandlerFunc
	}{
		{"content_filter", func(w http.ResponseWriter, _ *http.Request) { writeOK(w, "", "content_filter") }},
		{"header", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("X-Guard-Output", "blocked")
			writeOK(w, "x", "stop")
		}},
		{"refusal text", func(w http.ResponseWriter, _ *http.Request) { writeOK(w, refusal, "stop") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := scripted(t, tt.h)
			c := mustClient(t, llm.Config{Provider: llm.ProviderOpenAI, APIKey: "k", BaseURL: srv.URL, RefusalText: refusal})
			resp, err := c.Chat(context.Background(), hiReq)
			if err != nil {
				t.Fatalf("a guard block is not a client error: %v", err)
			}
			if resp.Guard.Output != port.GuardBlocked {
				t.Errorf("Guard.Output = %q, want blocked", resp.Guard.Output)
			}
		})
	}
}

func TestChat_ResponseBodyLimit(t *testing.T) {
	srv, _ := scripted(t, func(w http.ResponseWriter, _ *http.Request) { writeOK(w, strings.Repeat("x", 4096), "stop") })
	c := mustClient(t, llm.Config{Provider: llm.ProviderOpenAI, APIKey: "k", BaseURL: srv.URL, ResponseMaxBytes: 1024})
	if _, err := c.Chat(context.Background(), hiReq); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized body must fail, got %v", err)
	}
}

func TestChat_DebugDumpHasNoSecrets(t *testing.T) {
	const key = "sk-very-secret-api-key-0001"
	srv, _ := scripted(t, func(w http.ResponseWriter, _ *http.Request) { writeOK(w, "ok", "stop") })
	var log bytes.Buffer
	c, err := llm.NewClient(llm.Config{Provider: llm.ProviderOpenAI, APIKey: key, BaseURL: srv.URL, Log: &log})
	if err != nil {
		t.Fatal(err)
	}
	req := hiReq
	req.Debug = true
	req.Messages = []domain.Message{{Role: domain.RoleUser, Content: "мой ключ " + key + " и Bearer abc.def.ghi"}}
	if _, err := c.Chat(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	out := log.String()
	if !strings.Contains(out, "[debug]") {
		t.Fatalf("debug dump missing: %q", out)
	}
	for _, leak := range []string{key, "abc.def.ghi", "Authorization"} {
		if strings.Contains(out, leak) {
			t.Errorf("debug output leaks %q:\n%s", leak, out)
		}
	}
}

func TestChat_ErrorMessageRedacted(t *testing.T) {
	const key = "sk-very-secret-api-key-0002"
	srv, _ := scripted(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream said: invalid key " + key))
	})
	_, err := mustClient(t, llm.Config{Provider: llm.ProviderOpenAI, APIKey: key, BaseURL: srv.URL}).Chat(context.Background(), hiReq)
	if err == nil || strings.Contains(err.Error(), key) {
		t.Fatalf("error must exist and not contain the key: %v", err)
	}
}

func TestChat_RequestTrimmedToMaxBytes(t *testing.T) {
	var gotLen int
	var got map[string]any
	srv, _ := scripted(t, func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r.Body)
		gotLen = buf.Len()
		_ = json.Unmarshal(buf.Bytes(), &got)
		writeOK(w, "ok", "stop")
	})
	c := mustClient(t, llm.Config{Provider: llm.ProviderOpenAI, APIKey: "k", BaseURL: srv.URL, RequestMaxBytes: 2000})
	req := hiReq
	req.Messages = []domain.Message{
		{Role: domain.RoleSystem, Content: "sys"},
		{Role: domain.RoleUser, Content: strings.Repeat("a", 5000)},
		{Role: domain.RoleAssistant, Content: "ok"},
		{Role: domain.RoleUser, Content: "last"},
	}
	if _, err := c.Chat(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if gotLen > 2000 {
		t.Errorf("body %d bytes exceeds cap 2000", gotLen)
	}
	msgs := got["messages"].([]any)
	if last := msgs[len(msgs)-1].(map[string]any)["content"]; last != "last" {
		t.Errorf("last message changed: %v", last)
	}

	// Impossible to fit: refused locally with a non-retryable error.
	c = mustClient(t, llm.Config{Provider: llm.ProviderOpenAI, APIKey: "k", BaseURL: srv.URL, RequestMaxBytes: 100})
	req.Messages = []domain.Message{{Role: domain.RoleUser, Content: strings.Repeat("b", 500)}}
	_, err := c.Chat(context.Background(), req)
	var le *port.LLMError
	if !errors.As(err, &le) || le.Code != port.CodeRequestTooLargeClient || !errors.Is(err, port.ErrFatalLLM) {
		t.Fatalf("want request_too_large_client fatal error, got %v", err)
	}
}

func TestNewClient_CAFileErrors(t *testing.T) {
	if _, err := llm.NewClient(llm.Config{Provider: llm.ProviderOpenAI, CACertFile: "/nonexistent/ca.pem"}); err == nil {
		t.Error("unreadable CA file must fail NewClient")
	}
	bad := t.TempDir() + "/bad.pem"
	if err := writeFile(bad, "not a cert"); err != nil {
		t.Fatal(err)
	}
	if _, err := llm.NewClient(llm.Config{Provider: llm.ProviderGigaChat, CACertFile: bad}); err == nil {
		t.Error("CA file without PEM certificates must fail NewClient")
	}
}

func TestNewClient_GigaChatWithoutCAWarns(t *testing.T) {
	var log bytes.Buffer
	if _, err := llm.NewClient(llm.Config{Provider: llm.ProviderGigaChat, Log: &log}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log.String(), "Russian Trusted Root CA") {
		t.Errorf("missing CA warning, got %q", log.String())
	}
}

// Ensures clock.Fake satisfies the port at compile time for these tests.
var _ port.Clock = (*clock.Fake)(nil)
