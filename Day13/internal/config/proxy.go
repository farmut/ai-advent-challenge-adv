package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"agile-consultant/internal/usecase/secrets"
)

// Proxy is the guard-proxy configuration (configs/proxy.yaml).
type Proxy struct {
	Listen           string                `yaml:"-"`
	MaxBodyBytes     int64                 `yaml:"-"`
	AllowNonLoopback bool                  `yaml:"-"`
	Server           ProxyServer           `yaml:"proxy"`
	Upstreams        map[string]Upstream   `yaml:"upstreams"`
	Models           map[string]ModelRoute `yaml:"models"`
	RateLimit        RateLimit             `yaml:"rate_limit"`
	Guard            ProxyGuard            `yaml:"guard"`
	Audit            Audit                 `yaml:"audit"`
	Log              ProxyLog              `yaml:"log"`

	// Root is the directory relative paths are resolved against (the module
	// root: the nearest parent of the config file containing go.mod).
	Root string `yaml:"-"`
}

// ProxyServer is the "proxy" section. Proxy API keys are deliberately not
// part of the file: they come only from the env variable PROXY_API_KEYS
// (an "api_keys" field is rejected as unknown).
type ProxyServer struct {
	Listen       string `yaml:"listen"`
	MaxBodyBytes int64  `yaml:"max_body_bytes"`
	// AllowNonLoopback permits a non-loopback listen address. The proxy
	// has API-key auth but no TLS: use it only behind a TLS terminator.
	AllowNonLoopback bool `yaml:"allow_non_loopback"`
}

// Upstream is an OpenAI-compatible API. The API key is read from the env
// variable APIKeyEnv ("" = no key needed).
type Upstream struct {
	BaseURL   string        `yaml:"base_url"`
	APIKeyEnv string        `yaml:"api_key_env"`
	Timeout   time.Duration `yaml:"timeout"`
	// AllowRemote permits a keyless upstream on a non-loopback host (e.g.
	// Ollama on another machine). Such an API has no auth, so the proxy
	// logs a WARN at start; without the flag the config is rejected.
	AllowRemote bool `yaml:"allow_remote"`
}

// ModelRoute maps a public alias to an upstream model. With LocalPath the
// upstream model is a path resolved against Root (mlx_lm.server reloads the
// model when the path differs from --model, so it must be absolute).
type ModelRoute struct {
	Upstream      string `yaml:"upstream"`
	UpstreamModel string `yaml:"upstream_model"`
	LocalPath     bool   `yaml:"local_path"`
	Price         Price  `yaml:"price"`
	// AuditModel is the model name for the audit: for LocalPath routes the
	// path relative to Root (UpstreamModel holds the absolute path that is
	// sent to mlx_lm.server).
	AuditModel string `yaml:"-"`
}

// RelToRoot returns p relative to root when p is inside root, else the base
// name of p, so that absolute local paths never reach audits or reports.
func RelToRoot(root, p string) string {
	if !filepath.IsAbs(p) {
		return filepath.ToSlash(p)
	}
	if rel, err := filepath.Rel(root, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(p)
}

// Price is in micro-USD per one million tokens.
type Price struct {
	InputMicroUSDPer1M  int64  `yaml:"input_micro_usd_per_1m"`
	OutputMicroUSDPer1M int64  `yaml:"output_micro_usd_per_1m"`
	Ref                 string `yaml:"ref"`
}

// RateLimit is a per-client-IP sliding window.
type RateLimit struct {
	PerMinute int           `yaml:"per_minute"`
	Window    time.Duration `yaml:"window"`
	MaxKeys   int           `yaml:"max_keys"`
}

// ProxyGuard configures the input and output guards.
type ProxyGuard struct {
	Input     GuardInput  `yaml:"input"`
	Output    GuardOutput `yaml:"output"`
	Allowlist []string    `yaml:"allowlist"`
}

// GuardInput configures the input guard.
type GuardInput struct {
	Mode           string            `yaml:"mode"`
	Overrides      map[string]string `yaml:"overrides"`
	MaxDecodeDepth int               `yaml:"max_decode_depth"`
	ScanRoles      []string          `yaml:"scan_roles"`
}

// GuardOutput configures the output guard.
type GuardOutput struct {
	SystemLeakNGram int      `yaml:"system_leak_ngram"`
	URLAllowlist    []string `yaml:"url_allowlist"`
	RefusalText     string   `yaml:"refusal_text"`
}

// Audit configures the JSONL audit log.
type Audit struct {
	Path         string `yaml:"path"`
	MaxTextRunes int    `yaml:"max_text_runes"`
	// SystemContent is how system prompts are stored: "hash" (default,
	// "[SYSTEM sha=<hmac12> runes=N]") or "full" (masked text).
	SystemContent string `yaml:"system_content"`
}

// System prompt audit modes.
const (
	SystemContentHash = "hash"
	SystemContentFull = "full"
)

// ProxyLog configures slog.
type ProxyLog struct {
	Level string `yaml:"level"`
}

// Env overrides.
const (
	EnvProxyConfig    = "PROXY_CONFIG"
	EnvProxyListen    = "PROXY_LISTEN"
	EnvProxyRate      = "PROXY_RATE_PER_MIN"
	EnvProxyGuardMode = "PROXY_GUARD_MODE"
	EnvProxyAuditPath = "PROXY_AUDIT_PATH"
	EnvProxyLogLevel  = "PROXY_LOG_LEVEL"
	// EnvProxyUpstreamFmt overrides upstreams.<name>.base_url; the name is
	// upper-cased with '-' and '.' replaced by '_'.
	EnvProxyUpstreamFmt = "PROXY_UPSTREAM_%s_BASE_URL"
	// EnvProxyModelUpstreamFmt overrides models.<alias>.upstream_model; the
	// alias is upper-cased with '-' and '.' replaced by '_', e.g.
	// PROXY_MODEL_QWEN2_5_7B_OLLAMA_UPSTREAM_MODEL.
	EnvProxyModelUpstreamFmt = "PROXY_MODEL_%s_UPSTREAM_MODEL"
)

var envNameReplacer = strings.NewReplacer("-", "_", ".", "_")

// EnvName returns the env variable for a config name (upstream or model
// alias) and a format such as EnvProxyModelUpstreamFmt.
func EnvName(format, name string) string {
	return fmt.Sprintf(format, strings.ToUpper(envNameReplacer.Replace(name)))
}

// ErrNotLoopback is returned when the listen address is not loopback and
// proxy.allow_non_loopback is not set. The proxy has no TLS, so API keys
// and prompts would travel in clear text over the network.
var ErrNotLoopback = errors.New("proxy has no TLS; listen must be loopback unless proxy.allow_non_loopback is true")

// LoadProxy reads, applies env overrides from the process environment and
// validates the proxy config.
func LoadProxy(path string) (*Proxy, error) { return LoadProxyEnv(path, os.Getenv) }

// LoadProxyEnv is LoadProxy with an injectable getenv.
func LoadProxyEnv(path string, getenv func(string) string) (*Proxy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read proxy config %s: %w", path, err)
	}
	cfg := &Proxy{}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse proxy config %s: %w", path, err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}
	cfg.Root = ModuleRoot(filepath.Dir(abs))
	cfg.applyDefaults()
	if err := cfg.applyEnv(getenv); err != nil {
		return nil, err
	}
	cfg.resolvePaths()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// ModuleRoot returns the nearest directory at or above dir that contains
// go.mod, or the parent of dir when there is none (configs/ layout).
func ModuleRoot(dir string) string {
	for d := dir; ; {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		p := filepath.Dir(d)
		if p == d {
			return filepath.Dir(dir)
		}
		d = p
	}
}

func (c *Proxy) applyDefaults() {
	c.Listen, c.MaxBodyBytes, c.AllowNonLoopback = c.Server.Listen, c.Server.MaxBodyBytes, c.Server.AllowNonLoopback
	if c.MaxBodyBytes == 0 {
		c.MaxBodyBytes = 1 << 20
	}
	if c.RateLimit.Window == 0 {
		c.RateLimit.Window = time.Minute
	}
	if c.RateLimit.MaxKeys == 0 {
		c.RateLimit.MaxKeys = 10000
	}
	if c.Guard.Input.Mode == "" {
		c.Guard.Input.Mode = string(secrets.ModeMask)
	}
	if c.Guard.Output.SystemLeakNGram == 0 {
		c.Guard.Output.SystemLeakNGram = 8
	}
	if c.Audit.Path == "" {
		c.Audit.Path = "logs/audit/proxy-audit.jsonl"
	}
	if c.Audit.SystemContent == "" {
		c.Audit.SystemContent = SystemContentHash
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	for name, u := range c.Upstreams {
		if u.Timeout == 0 {
			u.Timeout = 60 * time.Second
			c.Upstreams[name] = u
		}
	}
}

func (c *Proxy) applyEnv(getenv func(string) string) error {
	if v := getenv(EnvProxyListen); v != "" {
		c.Listen = v
	}
	if v := getenv(EnvProxyRate); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("%s: %w", EnvProxyRate, err)
		}
		c.RateLimit.PerMinute = n
	}
	if v := getenv(EnvProxyGuardMode); v != "" {
		c.Guard.Input.Mode = v
	}
	if v := getenv(EnvProxyAuditPath); v != "" {
		c.Audit.Path = v
	}
	if v := getenv(EnvProxyLogLevel); v != "" {
		c.Log.Level = v
	}
	for name, u := range c.Upstreams {
		if v := getenv(EnvName(EnvProxyUpstreamFmt, name)); v != "" {
			u.BaseURL = v
			c.Upstreams[name] = u
		}
	}
	for alias, m := range c.Models {
		if v := getenv(EnvName(EnvProxyModelUpstreamFmt, alias)); v != "" {
			m.UpstreamModel = v
			c.Models[alias] = m
		}
	}
	return nil
}

func (c *Proxy) resolvePaths() {
	if c.Audit.Path != "" && !filepath.IsAbs(c.Audit.Path) {
		c.Audit.Path = filepath.Join(c.Root, c.Audit.Path)
	}
	for alias, m := range c.Models {
		if m.LocalPath && m.UpstreamModel != "" {
			if !filepath.IsAbs(m.UpstreamModel) {
				m.UpstreamModel = filepath.Join(c.Root, m.UpstreamModel)
			}
			m.AuditModel = RelToRoot(c.Root, m.UpstreamModel)
			c.Models[alias] = m
		}
	}
}

// Validate checks the config. It is called by LoadProxy and must be called
// again after programmatic changes. A non-loopback listen address needs
// AllowNonLoopback; the presence of API keys is checked by app.Build.
func (c *Proxy) Validate() error {
	if err := c.CheckListen(c.Listen); err != nil {
		return err
	}
	if c.MaxBodyBytes <= 0 {
		return errors.New("proxy.max_body_bytes must be > 0")
	}
	if c.RateLimit.PerMinute <= 0 {
		return errors.New("rate_limit.per_minute must be > 0")
	}
	if c.RateLimit.Window <= 0 || c.RateLimit.MaxKeys <= 0 {
		return errors.New("rate_limit.window and rate_limit.max_keys must be > 0")
	}
	if _, err := secrets.ParseMode(c.Guard.Input.Mode); err != nil {
		return fmt.Errorf("guard.input.mode: %w", err)
	}
	for t, m := range c.Guard.Input.Overrides {
		if !secrets.ValidType(t) {
			return fmt.Errorf("guard.input.overrides: unknown type %q", t)
		}
		if _, err := secrets.ParseMode(m); err != nil {
			return fmt.Errorf("guard.input.overrides.%s: %w", t, err)
		}
	}
	if c.Guard.Input.MaxDecodeDepth < 0 || c.Guard.Input.MaxDecodeDepth > 4 {
		return errors.New("guard.input.max_decode_depth must be in 0..4")
	}
	if len(c.Upstreams) == 0 || len(c.Models) == 0 {
		return errors.New("at least one upstream and one model are required")
	}
	for name, u := range c.Upstreams {
		if err := checkBaseURL(name, u); err != nil {
			return err
		}
		if u.Timeout <= 0 {
			return fmt.Errorf("upstreams.%s.timeout must be > 0", name)
		}
	}
	for alias, m := range c.Models {
		if _, ok := c.Upstreams[m.Upstream]; !ok {
			return fmt.Errorf("models.%s: unknown upstream %q", alias, m.Upstream)
		}
		if m.UpstreamModel == "" {
			return fmt.Errorf("models.%s.upstream_model is required", alias)
		}
		if m.Price.InputMicroUSDPer1M < 0 || m.Price.OutputMicroUSDPer1M < 0 {
			return fmt.Errorf("models.%s.price must be >= 0", alias)
		}
	}
	if c.Audit.Path == "" {
		return errors.New("audit.path is required")
	}
	if c.Audit.SystemContent != SystemContentHash && c.Audit.SystemContent != SystemContentFull {
		return fmt.Errorf("audit.system_content must be %q or %q", SystemContentHash, SystemContentFull)
	}
	return nil
}

// checkBaseURL validates upstreams.<name>.base_url (from yaml or env):
//   - http(s) with a host; no userinfo, query or fragment (credentials and
//     parameters belong in api_key_env, not in a URL that lands in logs);
//   - with an API key: https or a loopback host (no key in clear text over
//     the network);
//   - without a key: a loopback host, or allow_remote (logged as WARN: the
//     remote API has no auth).
func checkBaseURL(name string, u Upstream) error {
	field := "upstreams." + name + ".base_url"
	pu, err := url.Parse(u.BaseURL)
	if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
		return fmt.Errorf("%s must be an http(s) URL", field)
	}
	if pu.User != nil {
		return fmt.Errorf("%s must not contain userinfo (use api_key_env)", field)
	}
	if pu.RawQuery != "" || pu.ForceQuery {
		return fmt.Errorf("%s must not contain a query", field)
	}
	if pu.Fragment != "" || strings.Contains(u.BaseURL, "#") {
		return fmt.Errorf("%s must not contain a fragment", field)
	}
	loopback := isLoopbackHost(pu.Hostname())
	if u.APIKeyEnv != "" {
		if pu.Scheme != "https" && !loopback {
			return fmt.Errorf("%s: an upstream with api_key_env needs https or a loopback host", field)
		}
		return nil
	}
	if !loopback {
		if !u.AllowRemote {
			return fmt.Errorf("%s: a keyless upstream must be on a loopback host unless upstreams.%s.allow_remote is true", field, name)
		}
		slog.Warn("keyless upstream on a non-loopback host (allow_remote): its API has no auth",
			slog.String("upstream", name), slog.String("host", pu.Host))
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// CheckListen validates a listen (or bound) address: loopback always,
// anything else only with AllowNonLoopback.
func (c *Proxy) CheckListen(addr string) error {
	if !c.AllowNonLoopback {
		return CheckLoopback(addr)
	}
	if _, port, err := net.SplitHostPort(addr); err != nil || port == "" {
		return fmt.Errorf("invalid listen address %q", addr)
	}
	return nil
}

// IsLoopback reports whether addr is a loopback host:port.
func IsLoopback(addr string) bool { return CheckLoopback(addr) == nil }

// MaxUpstreamTimeout returns the largest upstream timeout.
func (c *Proxy) MaxUpstreamTimeout() time.Duration {
	var m time.Duration
	for _, u := range c.Upstreams {
		if u.Timeout > m {
			m = u.Timeout
		}
	}
	return m
}

// CheckLoopback accepts only host:port addresses whose host is a loopback IP
// literal or "localhost". The proxy has no TLS, so by default binding to any
// other interface is refused (see Proxy.CheckListen).
func CheckLoopback(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return fmt.Errorf("%w: invalid listen address %q", ErrNotLoopback, addr)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%w: got %q", ErrNotLoopback, addr)
	}
	return nil
}
