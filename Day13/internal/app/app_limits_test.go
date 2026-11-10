package app

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"agile-consultant/internal/adapters/clock"
	"agile-consultant/internal/config"
)

// Limit checks: the request rate limit and the authentication failure limit
// come from configs/proxy.limits.yaml (per_minute 3, max_failures 3) and
// are verified with a fake clock, so the test never sleeps.

func buildLimits(t *testing.T) (*httptest.Server, *clock.Fake, *config.Proxy) {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(up.Close)
	_, f, _, _ := runtime.Caller(0)
	env := map[string]string{"PROXY_UPSTREAM_MLX_BASE_URL": up.URL + "/v1", "PROXY_AUDIT_PATH": filepath.Join(t.TempDir(), "a.jsonl"),
		"AUDIT_PEPPER": "0123456789abcdef!", "PROXY_API_KEYS": testProxyKey}
	getenv := func(k string) string { return env[k] }
	cfg, err := config.LoadProxyEnv(filepath.Join(filepath.Dir(f), "..", "..", "configs", "proxy.limits.yaml"), getenv)
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC))
	a, err := Build(cfg, Options{Getenv: getenv, Logger: slog.New(slog.DiscardHandler), Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)
	return srv, clk, cfg
}

func postChat(t *testing.T, srv *httptest.Server, key string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"qwen2.5-7b-local","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func TestLimitsRateLimitFromConfig(t *testing.T) {
	srv, clk, cfg := buildLimits(t)
	n := cfg.RateLimit.PerMinute
	for i := 1; i <= n; i++ {
		if r := postChat(t, srv, testProxyKey); r.StatusCode != 200 {
			t.Fatalf("request %d: %d", i, r.StatusCode)
		}
	}
	r := postChat(t, srv, testProxyKey)
	ra, _ := strconv.Atoi(r.Header.Get("Retry-After"))
	if r.StatusCode != 429 || ra < 1 || ra > int(cfg.RateLimit.Window.Seconds()) || r.Header.Get("X-RateLimit-Limit") != strconv.Itoa(n) {
		t.Fatalf("request %d: status %d Retry-After %q limit %q", n+1, r.StatusCode, r.Header.Get("Retry-After"), r.Header.Get("X-RateLimit-Limit"))
	}
	clk.Advance(cfg.RateLimit.Window + time.Second)
	if r := postChat(t, srv, testProxyKey); r.StatusCode != 200 {
		t.Fatalf("after window: %d", r.StatusCode)
	}
}

func TestLimitsAuthBruteforceFromConfig(t *testing.T) {
	srv, clk, cfg := buildLimits(t)
	bad := strings.Repeat("x", 20) + "-wrong-key-0123456789"
	n := cfg.Auth.Bruteforce.MaxFailures
	for i := 1; i <= n; i++ {
		if r := postChat(t, srv, bad); r.StatusCode != 401 {
			t.Fatalf("failure %d: %d", i, r.StatusCode)
		}
	}
	r := postChat(t, srv, testProxyKey)
	ra, _ := strconv.Atoi(r.Header.Get("Retry-After"))
	if r.StatusCode != 429 || ra < 1 || ra > int(cfg.Auth.Bruteforce.Window.Seconds()) {
		t.Fatalf("valid key while blocked: status %d Retry-After %q", r.StatusCode, r.Header.Get("Retry-After"))
	}
	clk.Advance(cfg.Auth.Bruteforce.Window + time.Second)
	if r := postChat(t, srv, testProxyKey); r.StatusCode != 200 {
		t.Fatalf("after window: %d", r.StatusCode)
	}
}
