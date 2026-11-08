package httpdelivery

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agile-consultant/internal/ports"
	"agile-consultant/internal/usecase/proxy"
)

const want401 = `{"error":{"type":"invalid_request_error","code":"invalid_api_key","param":null,"message":"Invalid or missing API key."}}`

func TestParseBearer(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		token  string
		reason string
	}{
		{"ok", []string{"Bearer abc"}, "abc", ""},
		{"lower scheme", []string{"bearer abc"}, "abc", ""},
		{"upper scheme", []string{"BEARER abc"}, "abc", ""},
		{"none", nil, "", ports.AuthReasonMissing},
		{"empty", []string{""}, "", ports.AuthReasonMissing},
		{"two headers", []string{"Bearer a", "Bearer b"}, "", ports.AuthReasonScheme},
		{"basic", []string{"Basic dXNlcjpwYXNz"}, "", ports.AuthReasonScheme},
		{"token only", []string{"abc"}, "", ports.AuthReasonScheme},
		{"no token", []string{"Bearer "}, "", ports.AuthReasonScheme},
		{"scheme only", []string{"Bearer"}, "", ports.AuthReasonScheme},
		{"two spaces", []string{"Bearer  abc"}, "", ports.AuthReasonScheme},
		{"space in token", []string{"Bearer ab c"}, "", ports.AuthReasonScheme},
		{"tab", []string{"Bearer\tabc"}, "", ports.AuthReasonScheme},
		{"tab in token", []string{"Bearer ab\tc"}, "", ports.AuthReasonScheme},
		{"bearerx", []string{"Bearerx abc"}, "", ports.AuthReasonScheme},
		{"max length", []string{"Bearer " + strings.Repeat("a", MaxTokenLength)}, strings.Repeat("a", MaxTokenLength), ""},
		{"too long", []string{"Bearer " + strings.Repeat("a", MaxTokenLength+1)}, "", ports.AuthReasonTooLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			for _, v := range tt.values {
				h.Add("Authorization", v)
			}
			tok, reason := ParseBearer(h)
			if tok != tt.token || reason != tt.reason {
				t.Fatalf("got (%q, %q), want (%q, %q)", tok, reason, tt.token, tt.reason)
			}
		})
	}
}

func TestAuthMiddleware(t *testing.T) {
	body := `{"model":"a","messages":[{"role":"user","content":"x"}]}`
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		hdr    map[string]string
		multi  []string
		status int
		reason string
		keyID  string
	}{
		{"valid key 1", "POST", "/v1/chat/completions", body, nil, nil, 200, "", "kid-one"},
		{"valid key 2 lower scheme", "POST", "/v1/chat/completions", body, map[string]string{"Authorization": "bearer " + testKey2}, nil, 200, "", "kid-two"},
		{"missing", "POST", "/v1/chat/completions", body, map[string]string{"Authorization": ""}, nil, 401, ports.AuthReasonMissing, ""},
		{"wrong key", "POST", "/v1/chat/completions", body, map[string]string{"Authorization": "Bearer " + testKey1 + "x"}, nil, 401, ports.AuthReasonInvalid, ""},
		{"basic", "POST", "/v1/chat/completions", body, map[string]string{"Authorization": "Basic " + testKey1}, nil, 401, ports.AuthReasonScheme, ""},
		{"duplicate header", "POST", "/v1/chat/completions", body, nil, []string{"Bearer " + testKey1, "Bearer " + testKey1}, 401, ports.AuthReasonScheme, ""},
		{"query token with valid header", "POST", "/v1/chat/completions?api_key=" + testKey1, body, nil, nil, 401, ports.AuthReasonQuery, ""},
		{"query access_token", "GET", "/v1/models?access_token=x", "", nil, nil, 401, ports.AuthReasonQuery, ""},
		{"query Token upper", "GET", "/v1/models?Token=x", "", nil, nil, 401, ports.AuthReasonQuery, ""},
		{"query key", "GET", "/v1/models?key=x", "", nil, nil, 401, ports.AuthReasonQuery, ""},
		{"too long", "POST", "/v1/chat/completions", body, map[string]string{"Authorization": "Bearer " + strings.Repeat("k", 600)}, nil, 401, ports.AuthReasonTooLong, ""},
		{"invalid json without key", "POST", "/v1/chat/completions", `{"model":`, map[string]string{"Authorization": ""}, nil, 401, ports.AuthReasonMissing, ""},
		{"oversized body without key", "POST", "/v1/chat/completions", strings.Repeat("x", 1000), map[string]string{"Authorization": ""}, nil, 401, ports.AuthReasonMissing, ""},
		{"models without key", "GET", "/v1/models", "", map[string]string{"Authorization": ""}, nil, 401, ports.AuthReasonMissing, ""},
		{"unknown route without key", "GET", "/admin", "", map[string]string{"Authorization": ""}, nil, 401, ports.AuthReasonMissing, ""},
		{"wrong method without key", "GET", "/v1/chat/completions", "", map[string]string{"Authorization": ""}, nil, 401, ports.AuthReasonMissing, ""},
		{"unknown route with key", "GET", "/admin", "", nil, nil, 404, "", ""},
		{"healthz without key", "GET", "/healthz", "", map[string]string{"Authorization": ""}, nil, 200, "", ""},
		{"readyz without key", "GET", "/readyz", "", map[string]string{"Authorization": ""}, nil, 200, "", ""},
		{"healthz with bad key", "GET", "/healthz", "", map[string]string{"Authorization": "Basic x"}, nil, 200, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &stubService{out: proxy.Outcome{Kind: proxy.KindOK, InputAction: "pass", OutputAction: "pass", Created: now, Decision: decision()}}
			h := newTestRouter(t, svc, true)
			var w *httptest.ResponseRecorder
			if tt.multi != nil {
				req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
				for _, v := range tt.multi {
					req.Header.Add("Authorization", v)
				}
				w = httptest.NewRecorder()
				h.ServeHTTP(w, req)
			} else {
				w = do(h, tt.method, tt.path, tt.body, tt.hdr)
			}
			if w.Code != tt.status {
				t.Fatalf("status %d, want %d: %s", w.Code, tt.status, w.Body)
			}
			if w.Header().Get("X-Request-ID") == "" {
				t.Fatal("X-Request-ID must be set")
			}
			if tt.status == 401 {
				if got := strings.TrimSpace(w.Body.String()); got != want401 {
					t.Fatalf("401 body %s", got)
				}
				if w.Header().Get("WWW-Authenticate") != WWWAuthenticate {
					t.Fatalf("WWW-Authenticate %q", w.Header().Get("WWW-Authenticate"))
				}
				for k := range w.Header() {
					if strings.HasPrefix(k, "X-Ratelimit") || strings.HasPrefix(k, "X-Guard") {
						t.Fatalf("401 must not carry %s", k)
					}
				}
				if svc.chats != 0 {
					t.Fatal("handler must not run on 401")
				}
				if len(svc.denials) != 1 || svc.denials[0].Reason != tt.reason || svc.denials[0].Kind != proxy.KindUnauthorized ||
					svc.denials[0].ClientIP != "192.0.2.1" || svc.denials[0].RequestID == "" {
					t.Fatalf("denials %+v", svc.denials)
				}
				return
			}
			if len(svc.denials) != 0 {
				t.Fatalf("unexpected denial %+v", svc.denials)
			}
			if tt.keyID != "" && svc.last.KeyID != tt.keyID {
				t.Fatalf("key id %q, want %q", svc.last.KeyID, tt.keyID)
			}
			if strings.HasPrefix(tt.path, "/healthz") || strings.HasPrefix(tt.path, "/readyz") {
				if b := w.Body.String(); !strings.HasPrefix(b, `{"status":`) || strings.Count(b, ":") != 1 {
					t.Fatalf("public body must contain only status: %s", b)
				}
			}
		})
	}
}

func TestAuthorizationRemovedAfterAuth(t *testing.T) {
	var seen []string
	svc := &stubService{out: proxy.Outcome{Kind: proxy.KindOK, Created: now, Decision: decision()}}
	h := newTestRouter(t, svc, true)
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r)
		seen = append(seen, r.Header.Get("Authorization"))
	})
	w := do(wrapped, "POST", "/v1/chat/completions", `{"model":"a","messages":[{"role":"user","content":"x"}]}`, nil)
	if w.Code != 200 || len(seen) != 1 || seen[0] != "" {
		t.Fatalf("status %d, Authorization after auth %q", w.Code, seen)
	}
}

func TestAuthFailLimit(t *testing.T) {
	svc := &stubService{out: proxy.Outcome{Kind: proxy.KindOK, Created: now, Decision: decision()}}
	fl := newCountLimiter(10)
	h := newAuthRouter(t, svc, true, fl)
	chat := `{"model":"a","messages":[{"role":"user","content":"x"}]}`
	if w := do(h, "POST", "/v1/chat/completions", chat, nil); w.Code != 200 {
		t.Fatalf("valid key: %d", w.Code)
	}
	for i := 1; i <= 10; i++ {
		if w := do(h, "POST", "/v1/chat/completions", chat, map[string]string{"Authorization": "Bearer wrong-key"}); w.Code != 401 {
			t.Fatalf("failure %d: %d", i, w.Code)
		}
	}
	for _, hdr := range []map[string]string{{"Authorization": "Bearer wrong-key"}, nil} {
		w := do(h, "POST", "/v1/chat/completions", chat, hdr)
		if w.Code != 429 || w.Header().Get("Retry-After") != "300" || !strings.Contains(w.Body.String(), `"code":"rate_limit_exceeded"`) {
			t.Fatalf("after 10 failures: %d Retry-After %q %s", w.Code, w.Header().Get("Retry-After"), w.Body)
		}
	}
	if fl.hits[authFailPrefix+"192.0.2.1"] != 10 {
		t.Fatalf("429 must not count as failure: %d", fl.hits[authFailPrefix+"192.0.2.1"])
	}
	last := svc.denials[len(svc.denials)-1]
	if last.Kind != proxy.KindRateLimited || last.Reason != ports.AuthReasonFailLimit {
		t.Fatalf("denial %+v", last)
	}
	if w := do(h, "GET", "/healthz", "", map[string]string{"Authorization": ""}); w.Code != 200 {
		t.Fatalf("healthz while locked: %d", w.Code)
	}
	if svc.chats != 1 {
		t.Fatalf("handler ran %d times", svc.chats)
	}
}

func TestNewRouterRequiresAuth(t *testing.T) {
	if _, err := NewRouter(Deps{Service: &stubService{}, AuthFailures: newCountLimiter(1)}); err == nil {
		t.Fatal("router without authenticator must fail")
	}
	if _, err := NewRouter(Deps{Service: &stubService{}, Auth: testAuth}); err == nil {
		t.Fatal("router without failure limiter must fail")
	}
}
