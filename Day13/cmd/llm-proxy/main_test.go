package main

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func configPath() string {
	_, f, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(f), "..", "..", "configs", "proxy.yaml")
}

// Synthetic proxy key for cmd tests.
const testKey = "cmd-test-proxy-key-0123456789AbCdEfGhIjK"

func TestRunRejectsNonLoopback(t *testing.T) {
	tests := []string{"0.0.0.0:8090", ":8090", "192.168.1.5:8090"}
	for _, addr := range tests {
		t.Run(addr, func(t *testing.T) {
			env := map[string]string{"PROXY_LISTEN": addr, "PROXY_AUDIT_PATH": filepath.Join(t.TempDir(), "a.jsonl"), "PROXY_API_KEYS": testKey}
			err := run(context.Background(), []string{"--config", configPath()}, func(k string) string { return env[k] }, io.Discard, nil)
			if err == nil || !strings.Contains(err.Error(), "listen must be loopback") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestRunStartupErrors(t *testing.T) {
	tests := []struct {
		name, keys, want string
	}{
		{"no keys", "", "PROXY_API_KEYS is required"},
		{"short key", "short-key-123", "PROXY_API_KEYS[0]: too short"},
		{"low entropy", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "PROXY_API_KEYS[0]: low entropy"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := map[string]string{"PROXY_LISTEN": "127.0.0.1:0", "PROXY_AUDIT_PATH": filepath.Join(t.TempDir(), "a.jsonl"), "PROXY_API_KEYS": tt.keys}
			err := run(context.Background(), []string{"--config", configPath()}, func(k string) string { return env[k] }, io.Discard, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if tt.keys != "" && strings.Contains(err.Error(), tt.keys) {
				t.Fatal("error leaks the key value")
			}
		})
	}
}

func TestRunServesAndShutsDown(t *testing.T) {
	env := map[string]string{
		"PROXY_LISTEN":     "127.0.0.1:0",
		"PROXY_AUDIT_PATH": filepath.Join(t.TempDir(), "audit", "a.jsonl"),
		"AUDIT_PEPPER":     "test-pepper-0123456789",
		"PROXY_API_KEYS":   testKey,
	}
	ctx, cancel := context.WithCancel(context.Background())
	addrc := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"--config", configPath()}, func(k string) string { return env[k] }, io.Discard, func(a string) { addrc <- a })
	}()
	var addr string
	select {
	case addr = <-addrc:
	case err := <-done:
		t.Fatalf("run exited: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("proxy did not start")
	}
	tests := []struct {
		path   string
		bearer string
		status int
	}{{"/healthz", "", 200}, {"/readyz", "", 200}, {"/v1/models", testKey, 200}, {"/v1/models", "", 401}}
	for _, tt := range tests {
		req, _ := http.NewRequest(http.MethodGet, "http://"+addr+tt.path, nil)
		if tt.bearer != "" {
			req.Header.Set("Authorization", "Bearer "+tt.bearer)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tt.status {
			t.Fatalf("%s = %d", tt.path, resp.StatusCode)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("shutdown: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("graceful shutdown hung")
	}
}
