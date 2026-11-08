package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agile-consultant/internal/config"
	"agile-consultant/internal/ports"
)

func okHandler(t *testing.T, check func(*http.Request, map[string]any)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		if check != nil {
			check(r, body)
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1}}`)
	}
}

func TestOpenAIChat(t *testing.T) {
	maxT := 16
	srv := httptest.NewServer(okHandler(t, func(r *http.Request, body map[string]any) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		if body["stream"] != false || body["max_tokens"] != float64(16) {
			t.Errorf("body %v", body)
		}
		for k := range body {
			switch k {
			case "model", "messages", "stream", "max_tokens":
			default:
				t.Errorf("unexpected field %s", k)
			}
		}
	}))
	defer srv.Close()
	c := NewOpenAI(srv.URL+"/v1/", "test-key", time.Second, nil)
	res, err := c.Chat(context.Background(), ports.UpstreamRequest{Model: "m", Messages: []ports.ChatMessage{{Role: "user", Content: "x"}}, Params: ports.ChatParams{MaxTokens: &maxT}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "hi" || !res.HasUsage || res.Usage.PromptTokens != 3 {
		t.Fatalf("res %+v", res)
	}
}

func TestOpenAINoKeyNoAuthHeader(t *testing.T) {
	srv := httptest.NewServer(okHandler(t, func(r *http.Request, _ map[string]any) {
		if _, ok := r.Header["Authorization"]; ok {
			t.Error("Authorization must not be sent without a key")
		}
	}))
	defer srv.Close()
	if _, err := NewOpenAI(srv.URL, "", time.Second, nil).Chat(context.Background(), ports.UpstreamRequest{}); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAIErrors(t *testing.T) {
	closedURL := func() string {
		l, _ := net.Listen("tcp", "127.0.0.1:0")
		u := "http://" + l.Addr().String()
		l.Close()
		return u
	}()
	tests := []struct {
		name    string
		handler http.HandlerFunc
		url     string
		timeout time.Duration
		want    error
	}{
		{"500", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "secret upstream body", 500) }, "", time.Second, ports.ErrUpstreamBadResponse},
		{"404 model not found", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"error":"secret upstream body: model 'qwen' not found, try pulling it first"}`, http.StatusNotFound)
		}, "", time.Second, ports.ErrUpstreamModelNotFound},
		{"redirect", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://169.254.169.254/", http.StatusFound)
		}, "", time.Second, ports.ErrUpstreamBadResponse},
		{"bad json", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "{not json") }, "", time.Second, ports.ErrUpstreamBadResponse},
		{"no choices", func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `{"choices":[]}`) }, "", time.Second, ports.ErrUpstreamBadResponse},
		{"too large", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"`+strings.Repeat("a", MaxResponseBytes)+`"}}]}`)
		}, "", time.Second, ports.ErrUpstreamBadResponse},
		{"timeout", func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
		}, "", 50 * time.Millisecond, ports.ErrUpstreamTimeout},
		{"refused", nil, closedURL, time.Second, ports.ErrUpstreamUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := tt.url
			if tt.handler != nil {
				srv := httptest.NewServer(tt.handler)
				defer srv.Close()
				u = srv.URL
			}
			_, err := NewOpenAI(u, "", tt.timeout, nil).Chat(context.Background(), ports.UpstreamRequest{})
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), "secret upstream body") {
				t.Fatal("upstream body leaked into error")
			}
		})
	}
}

func TestRouter(t *testing.T) {
	cfg := &config.Proxy{
		Upstreams: map[string]config.Upstream{
			"local": {BaseURL: "http://127.0.0.1:1/v1", Timeout: time.Second},
			"cloud": {BaseURL: "https://api.example.com/v1", APIKeyEnv: "CLOUD_KEY", Timeout: time.Second},
		},
		Models: map[string]config.ModelRoute{
			"a": {Upstream: "local", UpstreamModel: "m1"},
			"b": {Upstream: "cloud", UpstreamModel: "m2", Price: config.Price{InputMicroUSDPer1M: 5, Ref: "r"}},
		},
	}
	tests := []struct {
		name   string
		env    map[string]string
		readyB bool
	}{{"no key", nil, false}, {"key", map[string]string{"CLOUD_KEY": "k"}, true}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRouter(cfg, func(k string) string { return tt.env[k] }, nil, nil)
			a, okA := r.Resolve("a")
			b, okB := r.Resolve("b")
			if !okA || !okB || !a.Ready || b.Ready != tt.readyB || b.Price.Ref != "r" || b.UpstreamModel != "m2" {
				t.Fatalf("routes a=%+v b=%+v", a, b)
			}
			if _, ok := r.Resolve("c"); ok {
				t.Fatal("unknown alias resolved")
			}
			if got := strings.Join(r.Aliases(), ","); got != "a,b" || !r.AnyReady() {
				t.Fatalf("aliases %s", got)
			}
		})
	}
}
