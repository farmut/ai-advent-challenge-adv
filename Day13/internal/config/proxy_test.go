package config

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func repoConfig(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "configs", "proxy.yaml")
}

func noEnv(string) string { return "" }

func TestCheckLoopback(t *testing.T) {
	tests := []struct {
		addr string
		ok   bool
	}{
		{"127.0.0.1:8090", true},
		{"[::1]:8090", true},
		{"localhost:8090", true},
		{"127.0.0.2:8090", true},
		{"0.0.0.0:8090", false},
		{":8090", false},
		{"192.168.1.5:8090", false},
		{"[::]:8090", false},
		{"example.com:8090", false},
		{"127.0.0.1", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			err := CheckLoopback(tt.addr)
			if (err == nil) != tt.ok {
				t.Fatalf("CheckLoopback(%q) = %v, want ok=%v", tt.addr, err, tt.ok)
			}
			if err != nil && !errors.Is(err, ErrNotLoopback) {
				t.Fatalf("error must wrap ErrNotLoopback: %v", err)
			}
		})
	}
}

func TestLoadProxyRepoConfig(t *testing.T) {
	cfg, err := LoadProxyEnv(repoConfig(t), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:8090" || cfg.RateLimit.PerMinute != 20 || cfg.RateLimit.Window != time.Minute {
		t.Fatalf("unexpected cfg: %+v", cfg)
	}
	if cfg.Guard.Input.Mode != "mask" || cfg.Guard.Input.Overrides["private_key"] != "block" {
		t.Fatalf("guard: %+v", cfg.Guard.Input)
	}
	m := cfg.Models["qwen2.5-7b-local"]
	if !filepath.IsAbs(m.UpstreamModel) || !strings.HasSuffix(m.UpstreamModel, "models/baseline-qwen2.5-7b-4bit") {
		t.Fatalf("local model path not resolved: %s", m.UpstreamModel)
	}
	if m.AuditModel != "models/baseline-qwen2.5-7b-4bit" {
		t.Fatalf("audit model must be relative to the module root: %q", m.AuditModel)
	}
	if RelToRoot("/a/b", "/c/d/m") != "m" || RelToRoot("/a/b", "/a/b/x/y") != "x/y" || RelToRoot("/a/b", "rel/m") != "rel/m" {
		t.Fatal("RelToRoot")
	}
	if !filepath.IsAbs(cfg.Audit.Path) {
		t.Fatalf("audit path not absolute: %s", cfg.Audit.Path)
	}
	if _, err := os.Stat(filepath.Join(cfg.Root, "go.mod")); err != nil {
		t.Fatalf("root %s has no go.mod", cfg.Root)
	}
	if cfg.MaxUpstreamTimeout() != 180*time.Second {
		t.Fatalf("max timeout = %v", cfg.MaxUpstreamTimeout())
	}
}

// TestLimitsProxyConfigs: every limit comes from the config; the e2e
// config is fast (high rate limit), the limits config is tiny, and both
// keep the production pipeline (guard modes, routes).
func TestLimitsProxyConfigs(t *testing.T) {
	dir := filepath.Dir(repoConfig(t))
	tests := []struct {
		file                string
		perMinute, failures int
		authWindow          time.Duration
	}{
		{"proxy.yaml", 20, 10, 5 * time.Minute},
		{"proxy.e2e.yaml", 100000, 10, 5 * time.Minute},
		{"proxy.limits.yaml", 3, 3, time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			cfg, err := LoadProxyEnv(filepath.Join(dir, tt.file), noEnv)
			if err != nil {
				t.Fatal(err)
			}
			if cfg.RateLimit.PerMinute != tt.perMinute || cfg.RateLimit.Window != time.Minute ||
				cfg.Auth.Bruteforce.MaxFailures != tt.failures || cfg.Auth.Bruteforce.Window != tt.authWindow {
				t.Fatalf("limits rate=%+v auth=%+v", cfg.RateLimit, cfg.Auth)
			}
			if cfg.Tools.MaxTools != 64 || cfg.Tools.MaxDefinitionsBytes != 256<<10 || cfg.Tools.MaxArgumentsBytes != 64<<10 {
				t.Fatalf("tools %+v", cfg.Tools)
			}
			if cfg.Guard.Input.Mode != "mask" || cfg.Guard.Input.Overrides["private_key"] != "block" {
				t.Fatalf("guard %+v", cfg.Guard.Input)
			}
			if _, ok := cfg.Models["qwen2.5-7b-local"]; !ok {
				t.Fatal("qwen2.5-7b-local route is required by the e2e harness")
			}
			if tt.file != "proxy.yaml" {
				if m, ok := cfg.Models["fake-coder"]; !ok || m.Upstream != "fake" || m.Price.InputMicroUSDPer1M <= 0 {
					t.Fatalf("fake-coder route %+v", m)
				}
			}
		})
	}
}

// TestLimitsProxyDefaults: absent limit keys get safe defaults.
func TestLimitsProxyDefaults(t *testing.T) {
	src, err := os.ReadFile(repoConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	i, j := strings.Index(s, "auth:\n"), strings.Index(s, "guard:\n")
	if i < 0 || j < i {
		t.Fatal("auth/tools sections not found")
	}
	path := filepath.Join(t.TempDir(), "proxy.yaml")
	if err := os.WriteFile(path, []byte(s[:i]+s[j:]), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadProxyEnv(path, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Bruteforce.MaxFailures != DefaultAuthMaxFailures || cfg.Auth.Bruteforce.Window != DefaultAuthWindow ||
		cfg.Tools.MaxTools != DefaultMaxTools || cfg.Tools.MaxArgumentsBytes != DefaultMaxToolArgumentBytes {
		t.Fatalf("defaults auth=%+v tools=%+v", cfg.Auth, cfg.Tools)
	}
}

func TestLoadProxyEnvOverrides(t *testing.T) {
	env := map[string]string{
		EnvProxyListen:                "[::1]:9999",
		EnvProxyRate:                  "7",
		EnvProxyGuardMode:             "block",
		EnvProxyAuditPath:             "/tmp/x/audit.jsonl",
		EnvProxyLogLevel:              "debug",
		"PROXY_UPSTREAM_MLX_BASE_URL": "http://127.0.0.1:1/v1",
	}
	cfg, err := LoadProxyEnv(repoConfig(t), func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "[::1]:9999" || cfg.RateLimit.PerMinute != 7 || cfg.Guard.Input.Mode != "block" ||
		cfg.Audit.Path != "/tmp/x/audit.jsonl" || cfg.Log.Level != "debug" || cfg.Upstreams["mlx"].BaseURL != "http://127.0.0.1:1/v1" {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestLoadProxyInvalid(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		edit func(string) string
		want string
	}{
		{"public listen", map[string]string{EnvProxyListen: "0.0.0.0:8090"}, nil, "listen must be loopback"},
		{"bad mode", map[string]string{EnvProxyGuardMode: "drop"}, nil, "guard.input.mode"},
		{"bad rate", map[string]string{EnvProxyRate: "0"}, nil, "per_minute"},
		{"unknown upstream", nil, func(s string) string { return strings.Replace(s, "upstream: openai", "upstream: nope", 1) }, "unknown upstream"},
		{"bad override type", nil, func(s string) string { return strings.Replace(s, "private_key: block", "passport: block", 1) }, "unknown type"},
		{"bad override mode", nil, func(s string) string { return strings.Replace(s, "private_key: block", "private_key: drop", 1) }, "overrides.private_key"},
		{"bad scheme", nil, func(s string) string { return strings.Replace(s, "https://api.openai.com/v1", "file:///etc/passwd", 1) }, "base_url"},
		{"api_keys in yaml", nil, func(s string) string {
			return strings.Replace(s, "  max_body_bytes: 1048576", "  max_body_bytes: 1048576\n  api_keys: [x]", 1)
		}, "field api_keys not found"},
		{"bad system_content", nil, func(s string) string {
			return strings.Replace(s, "system_content: hash", "system_content: plain", 1)
		}, "audit.system_content"},
		{"negative bruteforce failures", nil, func(s string) string { return strings.Replace(s, "max_failures: 10", "max_failures: -1", 1) }, "auth.bruteforce"},
		{"negative bruteforce window", nil, func(s string) string { return strings.Replace(s, "window: 5m", "window: -1s", 1) }, "auth.bruteforce"},
		{"negative max_tools", nil, func(s string) string { return strings.Replace(s, "max_tools: 64", "max_tools: -1", 1) }, "tools.max_tools"},
		{"arguments over body", nil, func(s string) string {
			return strings.Replace(s, "max_arguments_bytes: 65536", "max_arguments_bytes: 2097152", 1)
		}, "proxy.max_body_bytes"},
		{"unknown auth field", nil, func(s string) string {
			return strings.Replace(s, "    max_failures: 10", "    max_failures: 10\n    api_keys: [x]", 1)
		}, "field api_keys not found"},
		{"negative price", nil, func(s string) string {
			return strings.Replace(s, "input_micro_usd_per_1m: 400000", "input_micro_usd_per_1m: -1", 1)
		}, "price"},
	}
	src, err := os.ReadFile(repoConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := repoConfig(t)
			if tt.edit != nil {
				path = filepath.Join(t.TempDir(), "proxy.yaml")
				if err := os.WriteFile(path, []byte(tt.edit(string(src))), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := LoadProxyEnv(path, func(k string) string { return tt.env[k] })
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestNonLoopbackNeedsFlag(t *testing.T) {
	src, err := os.ReadFile(repoConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	env := func(k string) string {
		if k == EnvProxyListen {
			return "0.0.0.0:8090"
		}
		return ""
	}
	if _, err := LoadProxyEnv(repoConfig(t), env); !errors.Is(err, ErrNotLoopback) {
		t.Fatalf("without flag: %v", err)
	}
	path := filepath.Join(t.TempDir(), "configs", "proxy.yaml")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	edited := strings.Replace(string(src), "allow_non_loopback: false", "allow_non_loopback: true", 1)
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadProxyEnv(path, env)
	if err != nil || !cfg.AllowNonLoopback || cfg.Listen != "0.0.0.0:8090" {
		t.Fatalf("with flag: %v %+v", err, cfg)
	}
	if IsLoopback(cfg.Listen) || !IsLoopback("127.0.0.1:1") {
		t.Fatal("IsLoopback")
	}
	if cfg.Audit.SystemContent != SystemContentHash {
		t.Fatalf("system_content default = %q", cfg.Audit.SystemContent)
	}
}

func TestRepoConfigLocalBackends(t *testing.T) {
	cfg, err := LoadProxyEnv(repoConfig(t), noEnv)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		alias, upstream, baseURL, model string
	}{
		{"qwen2.5-7b-ollama", "ollama", "http://127.0.0.1:11434/v1", "qwen2.5:7b-instruct"},
		{"qwen2.5-7b-lmstudio", "lmstudio", "http://127.0.0.1:1234/v1", "qwen2.5-7b-instruct"},
	}
	for _, tt := range tests {
		t.Run(tt.alias, func(t *testing.T) {
			m, ok := cfg.Models[tt.alias]
			if !ok || m.Upstream != tt.upstream || m.UpstreamModel != tt.model || m.LocalPath || m.Price.Ref != "local" ||
				m.Price.InputMicroUSDPer1M != 0 || m.Price.OutputMicroUSDPer1M != 0 {
				t.Fatalf("route = %+v", m)
			}
			u := cfg.Upstreams[tt.upstream]
			if u.BaseURL != tt.baseURL || u.APIKeyEnv != "" || u.Timeout != 180*time.Second || u.AllowRemote {
				t.Fatalf("upstream = %+v", u)
			}
		})
	}
}

func TestModelUpstreamModelEnv(t *testing.T) {
	tests := []struct {
		name, env, value, alias, want string
	}{
		{"ollama alias dots and dashes", "PROXY_MODEL_QWEN2_5_7B_OLLAMA_UPSTREAM_MODEL", "gemma4:12b", "qwen2.5-7b-ollama", "gemma4:12b"},
		{"lmstudio", "PROXY_MODEL_QWEN2_5_7B_LMSTUDIO_UPSTREAM_MODEL", "qwen2.5-7b-instruct-mlx", "qwen2.5-7b-lmstudio", "qwen2.5-7b-instruct-mlx"},
		{"cloud", "PROXY_MODEL_GPT_4_1_MINI_UPSTREAM_MODEL", "gpt-4.1-nano", "gpt-4.1-mini", "gpt-4.1-nano"},
		{"empty keeps yaml", "PROXY_MODEL_QWEN2_5_7B_OLLAMA_UPSTREAM_MODEL", "", "qwen2.5-7b-ollama", "qwen2.5:7b-instruct"},
		{"wrong case ignored", "proxy_model_qwen2_5_7b_ollama_upstream_model", "x", "qwen2.5-7b-ollama", "qwen2.5:7b-instruct"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := LoadProxyEnv(repoConfig(t), func(k string) string {
				if k == tt.env {
					return tt.value
				}
				return ""
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := cfg.Models[tt.alias].UpstreamModel; got != tt.want {
				t.Fatalf("upstream_model = %q, want %q", got, tt.want)
			}
		})
	}
	t.Run("local path resolved after env", func(t *testing.T) {
		cfg, err := LoadProxyEnv(repoConfig(t), func(k string) string {
			if k == "PROXY_MODEL_QWEN2_5_7B_LOCAL_UPSTREAM_MODEL" {
				return "models/other-model"
			}
			return ""
		})
		if err != nil {
			t.Fatal(err)
		}
		m := cfg.Models["qwen2.5-7b-local"]
		if m.UpstreamModel != filepath.Join(cfg.Root, "models", "other-model") || m.AuditModel != "models/other-model" {
			t.Fatalf("local route = %+v", m)
		}
	})
	if got := EnvName(EnvProxyModelUpstreamFmt, "qwen2.5-7b-ollama"); got != "PROXY_MODEL_QWEN2_5_7B_OLLAMA_UPSTREAM_MODEL" {
		t.Fatalf("EnvName = %q", got)
	}
}

func TestBaseURLValidation(t *testing.T) {
	const ollamaYAML = "  ollama:\n    base_url: http://127.0.0.1:11434/v1\n    api_key_env: \"\"\n    timeout: 180s\n"
	src, err := os.ReadFile(repoConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), ollamaYAML) {
		t.Fatal("repo config changed: ollama upstream block not found")
	}
	tests := []struct {
		name    string
		yaml    string            // replacement for the ollama upstream block ("" = unchanged)
		env     map[string]string // env overrides
		wantErr string            // "" = valid
	}{
		{"loopback keyless ok", "", nil, ""},
		{"localhost keyless ok", "", map[string]string{"PROXY_UPSTREAM_OLLAMA_BASE_URL": "http://localhost:11434/v1"}, ""},
		{"ipv6 loopback ok", "", map[string]string{"PROXY_UPSTREAM_OLLAMA_BASE_URL": "http://[::1]:11434/v1"}, ""},
		{"userinfo yaml", "  ollama:\n    base_url: http://u:p@127.0.0.1:11434/v1\n    api_key_env: \"\"\n    timeout: 1s\n", nil, "userinfo"},
		{"userinfo env", "", map[string]string{"PROXY_UPSTREAM_OLLAMA_BASE_URL": "http://user@127.0.0.1:11434/v1"}, "userinfo"},
		{"query env", "", map[string]string{"PROXY_UPSTREAM_OLLAMA_BASE_URL": "http://127.0.0.1:11434/v1?key=x"}, "query"},
		{"empty query env", "", map[string]string{"PROXY_UPSTREAM_OLLAMA_BASE_URL": "http://127.0.0.1:11434/v1?"}, "query"},
		{"fragment env", "", map[string]string{"PROXY_UPSTREAM_OLLAMA_BASE_URL": "http://127.0.0.1:11434/v1#x"}, "fragment"},
		{"query yaml", "  ollama:\n    base_url: http://127.0.0.1:11434/v1?a=1\n    api_key_env: \"\"\n    timeout: 1s\n", nil, "query"},
		{"keyless remote rejected", "", map[string]string{"PROXY_UPSTREAM_OLLAMA_BASE_URL": "http://192.168.1.10:11434/v1"}, "allow_remote"},
		{"keyless remote https rejected", "", map[string]string{"PROXY_UPSTREAM_OLLAMA_BASE_URL": "https://ollama.example.com/v1"}, "allow_remote"},
		{"keyless remote allowed", "  ollama:\n    base_url: http://192.168.1.10:11434/v1\n    api_key_env: \"\"\n    timeout: 1s\n    allow_remote: true\n", nil, ""},
		{"keyless remote env allowed by flag", "  ollama:\n    base_url: http://127.0.0.1:11434/v1\n    api_key_env: \"\"\n    timeout: 1s\n    allow_remote: true\n",
			map[string]string{"PROXY_UPSTREAM_OLLAMA_BASE_URL": "http://10.0.0.5:11434/v1"}, ""},
		{"key over http remote rejected", "  ollama:\n    base_url: http://10.0.0.5:11434/v1\n    api_key_env: OLLAMA_KEY\n    timeout: 1s\n", nil, "https or a loopback"},
		{"key over http remote rejected even with allow_remote", "  ollama:\n    base_url: http://10.0.0.5:11434/v1\n    api_key_env: OLLAMA_KEY\n    timeout: 1s\n    allow_remote: true\n", nil, "https or a loopback"},
		{"key over http loopback ok", "  ollama:\n    base_url: http://127.0.0.1:11434/v1\n    api_key_env: OLLAMA_KEY\n    timeout: 1s\n", nil, ""},
		{"key over https remote ok", "  ollama:\n    base_url: https://llm.example.com/v1\n    api_key_env: OLLAMA_KEY\n    timeout: 1s\n", nil, ""},
		{"cloud env downgraded to http", "", map[string]string{"PROXY_UPSTREAM_OPENAI_BASE_URL": "http://api.openai.com/v1"}, "https or a loopback"},
		{"bad scheme env", "", map[string]string{"PROXY_UPSTREAM_OLLAMA_BASE_URL": "ftp://127.0.0.1/v1"}, "http(s) URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := repoConfig(t)
			if tt.yaml != "" {
				path = filepath.Join(t.TempDir(), "configs", "proxy.yaml")
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(strings.Replace(string(src), ollamaYAML, tt.yaml, 1)), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			_, err := LoadProxyEnv(path, func(k string) string { return tt.env[k] })
			switch {
			case tt.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)):
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestAllowRemoteLogsWarn(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	src, err := os.ReadFile(repoConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "configs", "proxy.yaml")
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	edited := strings.Replace(string(src), "  ollama:\n    base_url: http://127.0.0.1:11434/v1\n", "  ollama:\n    allow_remote: true\n    base_url: http://192.168.1.10:11434/v1\n", 1)
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProxyEnv(path, noEnv); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); !strings.Contains(out, "level=WARN") || !strings.Contains(out, "upstream=ollama") || !strings.Contains(out, "allow_remote") {
		t.Fatalf("WARN expected, got %q", out)
	}
	buf.Reset()
	if _, err := LoadProxyEnv(repoConfig(t), noEnv); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "allow_remote") {
		t.Fatalf("no WARN expected for loopback upstreams: %q", buf.String())
	}
}
