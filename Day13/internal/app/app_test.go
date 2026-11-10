package app

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"agile-consultant/internal/config"
)

func TestBuildWiring(t *testing.T) {
	var mu sync.Mutex
	var bodies, auths []string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`)
	}))
	defer up.Close()

	_, f, _, _ := runtime.Caller(0)
	auditPath := filepath.Join(t.TempDir(), "audit", "a.jsonl")
	env := map[string]string{"PROXY_UPSTREAM_MLX_BASE_URL": up.URL + "/v1", "PROXY_AUDIT_PATH": auditPath, "AUDIT_PEPPER": "0123456789abcdef!",
		"PROXY_API_KEYS": testProxyKey}
	getenv := func(k string) string { return env[k] }
	cfg, err := config.LoadProxyEnv(filepath.Join(filepath.Dir(f), "..", "..", "configs", "proxy.yaml"), getenv)
	if err != nil {
		t.Fatal(err)
	}
	a, err := Build(cfg, Options{Getenv: getenv, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	srv := httptest.NewServer(a.Handler)
	defer srv.Close()

	tests := []struct {
		name, model, content string
		status               int
		guardIn              string
		upstreamCalls        int
	}{
		{"masked", "qwen2.5-7b-local", "ключ sk-proj-abc123 и a@b.io", 200, "masked", 1},
		{"no key", "gpt-4.1-mini", "hi", 503, "", 1},
		{"blocked pem", "qwen2.5-7b-local", "-----BEGIN PRIVATE KEY-----\nAAAA\n-----END PRIVATE KEY-----", 400, "blocked", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"model": tt.model, "messages": []map[string]string{{"role": "user", "content": tt.content}}})
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions", strings.NewReader(string(body)))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+testProxyKey)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != tt.status || resp.Header.Get("X-Guard-Input") != tt.guardIn {
				t.Fatalf("status %d input %q", resp.StatusCode, resp.Header.Get("X-Guard-Input"))
			}
			mu.Lock()
			n := len(bodies)
			mu.Unlock()
			if n != tt.upstreamCalls {
				t.Fatalf("upstream calls = %d, want %d", n, tt.upstreamCalls)
			}
		})
	}
	if !strings.Contains(bodies[0], "[REDACTED_API_KEY]") || strings.Contains(bodies[0], "sk-proj-abc123") {
		t.Fatalf("upstream body %s", bodies[0])
	}
	for _, a := range auths {
		if strings.Contains(a, testProxyKey) {
			t.Fatal("proxy key forwarded upstream")
		}
	}
	data, _ := os.ReadFile(auditPath)
	if strings.Count(string(data), "\n") != 3 || strings.Contains(string(data), "sk-proj-abc123") || strings.Contains(string(data), testProxyKey) {
		t.Fatalf("audit:\n%s", data)
	}
	if home, _ := os.UserHomeDir(); home != "" && strings.Contains(string(data), home) {
		t.Fatalf("audit contains an absolute home path:\n%s", data)
	}
	if !strings.Contains(string(data), `"upstream_model":"models/baseline-qwen2.5-7b-4bit"`) {
		t.Fatalf("upstream_model must be relative:\n%s", data)
	}
	if !strings.Contains(string(data), `"key_id":"`) || !strings.Contains(string(data), `"v":3`) {
		t.Fatalf("audit v3 with key_id expected:\n%s", data)
	}
}

// Synthetic proxy key for app tests.
const testProxyKey = "app-test-proxy-key-0123456789AbCdEfGhIj"

func TestBuildAuthRequirements(t *testing.T) {
	_, f, _, _ := runtime.Caller(0)
	cfgPath := filepath.Join(filepath.Dir(f), "..", "..", "configs", "proxy.yaml")
	tests := []struct {
		name string
		env  map[string]string
		edit func(*config.Proxy)
		want string
	}{
		{"no keys", map[string]string{}, nil, "PROXY_API_KEYS is required"},
		{"short key", map[string]string{"PROXY_API_KEYS": "short"}, nil, "PROXY_API_KEYS[0]: too short"},
		{"bad second key", map[string]string{"PROXY_API_KEYS": testProxyKey + ", "}, nil, "PROXY_API_KEYS[1]: empty"},
		{"equals upstream key", map[string]string{"PROXY_API_KEYS": testProxyKey, "OPENAI_API_KEY": testProxyKey}, nil, "equals the API key of upstream"},
		{"non-loopback without pepper", map[string]string{"PROXY_API_KEYS": testProxyKey}, func(c *config.Proxy) {
			c.Listen, c.AllowNonLoopback = "0.0.0.0:8090", true
		}, "AUDIT_PEPPER is required"},
		{"non-loopback without flag", map[string]string{"PROXY_API_KEYS": testProxyKey, "AUDIT_PEPPER": "0123456789abcdef!"}, func(c *config.Proxy) {
			c.Listen = "0.0.0.0:8090"
		}, "listen must be loopback"},
		{"ok non-loopback", map[string]string{"PROXY_API_KEYS": testProxyKey, "AUDIT_PEPPER": "0123456789abcdef!"}, func(c *config.Proxy) {
			c.Listen, c.AllowNonLoopback = "0.0.0.0:8090", true
		}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.env["PROXY_AUDIT_PATH"] = filepath.Join(t.TempDir(), "a.jsonl")
			getenv := func(k string) string { return tt.env[k] }
			cfg, err := config.LoadProxyEnv(cfgPath, getenv)
			if err != nil {
				t.Fatal(err)
			}
			if tt.edit != nil {
				tt.edit(cfg)
			}
			var logs strings.Builder
			a, err := Build(cfg, Options{Getenv: getenv, Logger: slog.New(slog.NewTextHandler(&logs, nil))})
			if tt.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				_ = a.Close()
				if !strings.Contains(logs.String(), "without TLS") {
					t.Fatalf("missing TLS warning: %s", logs.String())
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if strings.Contains(err.Error(), testProxyKey) {
				t.Fatal("error leaks the key")
			}
		})
	}
}

// An upstream 404 (Ollama/LM Studio: model not pulled/loaded) is a 502
// upstream_model_not_found; the upstream body never reaches the client.
func TestUpstreamModelNotFoundHTTP(t *testing.T) {
	const upstreamBody = `{"error":{"message":"model 'qwen2.5:7b-instruct' not found, try pulling it first","secret":"upstream-internal-detail"}}`
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, upstreamBody)
	}))
	defer up.Close()

	_, f, _, _ := runtime.Caller(0)
	env := map[string]string{
		"PROXY_UPSTREAM_OLLAMA_BASE_URL": up.URL + "/v1",
		"PROXY_AUDIT_PATH":               filepath.Join(t.TempDir(), "audit", "a.jsonl"),
		"AUDIT_PEPPER":                   "0123456789abcdef!",
		"PROXY_API_KEYS":                 testProxyKey,
	}
	getenv := func(k string) string { return env[k] }
	cfg, err := config.LoadProxyEnv(filepath.Join(filepath.Dir(f), "..", "..", "configs", "proxy.yaml"), getenv)
	if err != nil {
		t.Fatal(err)
	}
	a, err := Build(cfg, Options{Getenv: getenv, Logger: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	srv := httptest.NewServer(a.Handler)
	defer srv.Close()

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/chat/completions",
		strings.NewReader(`{"model":"qwen2.5-7b-ollama","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+testProxyKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502: %s", resp.StatusCode, b)
	}
	var e struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatalf("body %s: %v", b, err)
	}
	if e.Error.Code != "upstream_model_not_found" || !strings.Contains(e.Error.Message, "Upstream model not found (pull/load the model)") {
		t.Fatalf("error = %+v", e.Error)
	}
	if strings.Contains(string(b), "upstream-internal-detail") || strings.Contains(string(b), "try pulling") {
		t.Fatalf("upstream body leaked to client: %s", b)
	}
}
