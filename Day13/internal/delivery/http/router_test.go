package httpdelivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agile-consultant/internal/ports"
	"agile-consultant/internal/usecase/proxy"
)

type stubService struct {
	last    proxy.Call
	out     proxy.Outcome
	panic   bool
	denials []proxy.Denial
	chats   int
}

func (s *stubService) Reject(_ context.Context, d proxy.Denial) proxy.Outcome {
	s.denials = append(s.denials, d)
	o := proxy.Outcome{Kind: d.Kind, Code: proxy.CodeInvalidAPIKey, Message: proxy.MessageUnauthorized}
	if d.Kind == proxy.KindRateLimited {
		o.Code, o.Message = proxy.CodeRateLimited, "Too many failed authentication attempts."
	}
	return o
}

func (s *stubService) Chat(_ context.Context, c proxy.Call) proxy.Outcome {
	if s.panic {
		panic("boom with user data")
	}
	s.last = c
	s.chats++
	o := s.out
	if c.Reject != nil {
		o.Kind, o.Code, o.Message = c.Reject.Kind, c.Reject.Code, c.Reject.Message
	}
	return o
}
func (s *stubService) Aliases() []string { return []string{"a", "b"} }

// Synthetic proxy keys for router tests.
const (
	testKey1 = "router-test-key-1-AbCdEfGhIjKlMnOpQrStUv"
	testKey2 = "router-test-key-2-ZyXwVuTsRqPoNmLkJiHgFe"
)

type stubAuth map[string]string

func (a stubAuth) Authenticate(tok string) (string, bool) { id, ok := a[tok]; return id, ok }

var testAuth = stubAuth{testKey1: "kid-one", testKey2: "kid-two"}

// countLimiter is a FailureLimiter with a fixed limit and no window.
type countLimiter struct {
	limit int
	hits  map[string]int
}

func newCountLimiter(n int) *countLimiter { return &countLimiter{limit: n, hits: map[string]int{}} }

func (l *countLimiter) Peek(k string) ports.RateDecision {
	if l.hits[k] >= l.limit {
		return ports.RateDecision{Limit: l.limit, RetryAfter: 299500 * time.Millisecond}
	}
	return ports.RateDecision{Allowed: true, Limit: l.limit, Remaining: l.limit - l.hits[k]}
}

func (l *countLimiter) Allow(k string) ports.RateDecision {
	l.hits[k]++
	return l.Peek(k)
}

func newTestRouter(t *testing.T, svc *stubService, ready bool) http.Handler {
	t.Helper()
	return newAuthRouter(t, svc, ready, newCountLimiter(10))
}

func newAuthRouter(t *testing.T, svc *stubService, ready bool, fl FailureLimiter) http.Handler {
	t.Helper()
	h, err := NewRouter(Deps{Service: svc, Ready: func() bool { return ready }, MaxBodyBytes: 256, Auth: testAuth, AuthFailures: fl})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// do sends a request with "Bearer testKey1" unless hdr sets Authorization
// (an empty value sends no Authorization header).
func do(h http.Handler, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+testKey1)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Authorization") == "" {
		req.Header.Del("Authorization")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

var now = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

func decision() ports.RateDecision {
	return ports.RateDecision{Allowed: true, Limit: 20, Remaining: 19, ResetAt: now.Add(60 * time.Second)}
}

func TestContract(t *testing.T) {
	findings := []ports.AuditFinding{{Type: "aws_access_key", Count: 1, View: "raw"}}
	tests := []struct {
		name    string
		out     proxy.Outcome
		status  int
		code    string
		headers map[string]string
	}{
		{"ok", proxy.Outcome{Kind: proxy.KindOK, InputAction: "pass", OutputAction: "pass", Content: "hi", FinishReason: "stop", CostMicroUSD: 120, Usage: ports.Usage{PromptTokens: 100, CompletionTokens: 50, Source: "upstream"}},
			200, "", map[string]string{"X-Guard-Input": "pass", "X-Guard-Output": "pass", "X-Guard-Cost-USD": "0.000120", "X-Guard-Usage-Source": "upstream", "X-RateLimit-Limit": "20", "X-RateLimit-Remaining": "19", "X-RateLimit-Reset": "60"}},
		{"masked", proxy.Outcome{Kind: proxy.KindOK, InputAction: "masked", InputFindings: []ports.AuditFinding{{Type: "email", Count: 1}, {Type: "api_key_openai", Count: 1}}, OutputAction: "pass"},
			200, "", map[string]string{"X-Guard-Input": "masked", "X-Guard-Input-Findings": "api_key_openai=1,email=1"}},
		{"output redacted", proxy.Outcome{Kind: proxy.KindOK, OutputAction: "redacted", OutputFindings: []ports.AuditFinding{{Type: "suspicious_url", Count: 2}}},
			200, "", map[string]string{"X-Guard-Output": "redacted", "X-Guard-Output-Findings": "suspicious_url=2"}},
		{"blocked", proxy.Outcome{Kind: proxy.KindInputBlocked, Code: "guard_input_blocked", Param: "messages", Warning: "secrets_detected", InputFindings: findings, Message: "Request blocked"},
			400, "guard_input_blocked", map[string]string{"X-Guard-Input": "blocked", "X-Guard-Input-Findings": "aws_access_key=1", "X-Guard-Warning": "secrets_detected"}},
		{"rate", proxy.Outcome{Kind: proxy.KindRateLimited, Code: "rate_limit_exceeded", Decision: ports.RateDecision{Limit: 20, RetryAfter: 1500 * time.Millisecond, ResetAt: now.Add(2 * time.Second)}},
			429, "rate_limit_exceeded", map[string]string{"Retry-After": "2", "X-RateLimit-Remaining": "0"}},
		{"not found", proxy.Outcome{Kind: proxy.KindModelNotFound, Code: "model_not_found", Param: "model"}, 404, "model_not_found", nil},
		{"not configured", proxy.Outcome{Kind: proxy.KindUpstreamNotConfigured, Code: "upstream_not_configured"}, 503, "upstream_not_configured", nil},
		{"timeout", proxy.Outcome{Kind: proxy.KindUpstreamTimeout, Code: "upstream_timeout"}, 504, "upstream_timeout", nil},
		{"bad", proxy.Outcome{Kind: proxy.KindUpstreamBad, Code: "upstream_error"}, 502, "upstream_error", nil},
		{"down", proxy.Outcome{Kind: proxy.KindUpstreamUnavailable, Code: "upstream_unavailable"}, 503, "upstream_unavailable", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.out.Created = now
			if tt.out.Decision.Limit == 0 {
				tt.out.Decision = decision()
			}
			svc := &stubService{out: tt.out}
			w := do(newTestRouter(t, svc, true), "POST", "/v1/chat/completions", `{"model":"a","messages":[{"role":"user","content":"x"}]}`, map[string]string{"X-Request-ID": "abc-1"})
			if w.Code != tt.status {
				t.Fatalf("status %d, want %d: %s", w.Code, tt.status, w.Body)
			}
			if w.Header().Get("X-Request-ID") != "abc-1" {
				t.Fatalf("request id %q", w.Header().Get("X-Request-ID"))
			}
			for k, v := range tt.headers {
				if got := w.Header().Get(k); got != v {
					t.Fatalf("%s = %q, want %q", k, got, v)
				}
			}
			var body map[string]map[string]any
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			if tt.code != "" && body["error"]["code"] != tt.code {
				t.Fatalf("code %v", body["error"])
			}
			if tt.name == "blocked" {
				if body["error"]["warning"] != "secrets_detected" || body["error"]["param"] != "messages" || body["error"]["findings"] == nil {
					t.Fatalf("blocked body %s", w.Body)
				}
			}
			if tt.status == 200 {
				var cc chatCompletion
				_ = json.Unmarshal(w.Body.Bytes(), &cc)
				if cc.ID != "chatcmpl-abc-1" || cc.Object != "chat.completion" || len(cc.Choices) != 1 {
					t.Fatalf("completion %s", w.Body)
				}
			}
		})
	}
}

func TestParsing(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		unsupported string
		reject      string
		check       func(t *testing.T, c proxy.Call)
	}{
		{"string content", `{"model":"a","messages":[{"role":"user","content":"hi"}],"max_tokens":5,"stop":"x"}`, "", "", func(t *testing.T, c proxy.Call) {
			if c.Messages[0].Content != "hi" || *c.Params.MaxTokens != 5 || c.Params.Stop[0] != "x" {
				t.Fatalf("call %+v", c)
			}
		}},
		{"text parts", `{"model":"a","messages":[{"role":"user","content":[{"type":"text","text":"a"},{"type":"text","text":"b"}]}]}`, "", "", func(t *testing.T, c proxy.Call) {
			if c.Messages[0].Content != "a\nb" {
				t.Fatalf("content %q", c.Messages[0].Content)
			}
		}},
		{"stream", `{"model":"a","stream":true,"messages":[]}`, "", "", func(t *testing.T, c proxy.Call) {
			if !c.Stream {
				t.Fatal("stream not set")
			}
		}},
		{"n", `{"model":"a","n":2,"messages":[]}`, "n", "", nil},
		{"n=1 ok", `{"model":"a","n":1,"messages":[]}`, "", "", nil},
		{"tools", `{"model":"a","tools":[{}],"messages":[]}`, "tools", "", nil},
		{"functions", `{"model":"a","functions":[{}],"messages":[]}`, "functions", "", nil},
		{"image", `{"model":"a","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"x"}}]}]}`, "messages.content", "", nil},
		{"invalid json", `{"model":`, "", "invalid_json", nil},
		{"wrong type", `{"model":1}`, "", "invalid_json", nil},
		{"too large", `{"model":"a","messages":[{"role":"user","content":"` + strings.Repeat("x", 400) + `"}]}`, "", "request_too_large", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &stubService{out: proxy.Outcome{Kind: proxy.KindOK, Created: now, Decision: decision()}}
			w := do(newTestRouter(t, svc, true), "POST", "/v1/chat/completions", tt.body, map[string]string{"X-Forwarded-For": "203.0.113.9", "X-Real-IP": "203.0.113.9"})
			c := svc.last
			if c.Unsupported != tt.unsupported {
				t.Fatalf("unsupported = %q, want %q", c.Unsupported, tt.unsupported)
			}
			got := ""
			if c.Reject != nil {
				got = c.Reject.Code
			}
			if got != tt.reject {
				t.Fatalf("reject = %q, want %q", got, tt.reject)
			}
			if tt.reject == "request_too_large" && w.Code != 413 {
				t.Fatalf("status %d", w.Code)
			}
			if c.ClientIP != "192.0.2.1" {
				t.Fatalf("client ip %q: X-Forwarded-For must be ignored", c.ClientIP)
			}
			if tt.check != nil {
				tt.check(t, c)
			}
		})
	}
}

func TestRequestIDValidation(t *testing.T) {
	svc := &stubService{out: proxy.Outcome{Kind: proxy.KindOK, Created: now}}
	h := newTestRouter(t, svc, true)
	tests := []struct {
		in   string
		keep bool
	}{{"abc.DEF_1-2", true}, {"bad id with spaces", false}, {strings.Repeat("a", 65), false}, {"", false}}
	for _, tt := range tests {
		w := do(h, "GET", "/healthz", "", map[string]string{"X-Request-ID": tt.in})
		got := w.Header().Get("X-Request-ID")
		if (got == tt.in) != tt.keep || got == "" {
			t.Fatalf("in %q -> %q", tt.in, got)
		}
	}
}

func TestEndpoints(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		ready  bool
		panic  bool
		status int
	}{
		{"healthz", "GET", "/healthz", false, false, 200},
		{"readyz ok", "GET", "/readyz", true, false, 200},
		{"readyz not", "GET", "/readyz", false, false, 503},
		{"models", "GET", "/v1/models", true, false, 200},
		{"unknown", "GET", "/nope", true, false, 404},
		{"method", "GET", "/v1/chat/completions", true, false, 405},
		{"panic", "POST", "/v1/chat/completions", true, true, 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := do(newTestRouter(t, &stubService{panic: tt.panic}, tt.ready), tt.method, tt.path, `{}`, nil)
			if w.Code != tt.status {
				t.Fatalf("status %d, want %d", w.Code, tt.status)
			}
			if tt.panic && (strings.Contains(w.Body.String(), "boom") || !strings.Contains(w.Body.String(), "internal_error")) {
				t.Fatalf("panic body %s", w.Body)
			}
			if tt.name == "models" && !strings.Contains(w.Body.String(), `"id":"a"`) {
				t.Fatalf("models %s", w.Body)
			}
		})
	}
}
