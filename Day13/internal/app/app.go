// Package app wires the guard proxy. It is shared by cmd/llm-proxy and the
// e2e suite so both run exactly the same pipeline.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"agile-consultant/internal/adapters/apikey"
	"agile-consultant/internal/adapters/audit"
	"agile-consultant/internal/adapters/clock"
	"agile-consultant/internal/adapters/ratelimit"
	"agile-consultant/internal/adapters/textnorm"
	"agile-consultant/internal/adapters/upstream"
	"agile-consultant/internal/config"
	httpdelivery "agile-consultant/internal/delivery/http"
	"agile-consultant/internal/ports"
	"agile-consultant/internal/usecase/proxy"
	"agile-consultant/internal/usecase/secrets"
)

// Options override infrastructure for tests. Zero values use the real
// implementations.
type Options struct {
	Clock      ports.Clock
	Logger     *slog.Logger
	HTTPClient *http.Client
	// Getenv reads PROXY_API_KEYS, upstream API keys and AUDIT_PEPPER
	// (default os.Getenv).
	Getenv func(string) string
}

// App is the wired proxy.
type App struct {
	Handler http.Handler
	Service *proxy.Service
	Audit   *audit.JSONL
	Limiter *ratelimit.Memory
	// AuthFailures counts 401s per client IP.
	AuthFailures *ratelimit.Memory
	Router       *upstream.Router
	// Close stops the janitor and closes the audit log.
	Close func() error
}

// Build wires all dependencies from cfg. It fails when PROXY_API_KEYS is
// missing or invalid, when a proxy key equals an upstream key, and when a
// non-loopback listen address is used without AUDIT_PEPPER.
func Build(cfg *config.Proxy, opt Options) (*App, error) {
	if opt.Clock == nil {
		opt.Clock = clock.System{}
	}
	if opt.Logger == nil {
		opt.Logger = slog.New(slog.NewJSONHandler(os.Stderr, nil))
	}
	if opt.Getenv == nil {
		opt.Getenv = os.Getenv
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	policy, err := buildPolicy(cfg.Guard.Input)
	if err != nil {
		return nil, err
	}
	nonLoopback := !config.IsLoopback(cfg.Listen)
	if nonLoopback && opt.Getenv(audit.PepperEnv) == "" {
		return nil, fmt.Errorf("listen %s is not loopback: %s is required (stable fingerprints and key ids)", cfg.Listen, audit.PepperEnv)
	}
	fp, err := audit.NewHMAC(opt.Getenv(audit.PepperEnv), opt.Logger)
	if err != nil {
		return nil, err
	}
	authn, err := apikey.New(opt.Getenv(apikey.Env), fp)
	if err != nil {
		return nil, err
	}
	if err := checkUpstreamKeys(cfg, authn, opt.Getenv); err != nil {
		return nil, err
	}
	if nonLoopback {
		opt.Logger.Warn("listening on a non-loopback address without TLS: API keys and prompts travel in clear text; put a TLS terminator in front",
			slog.String("addr", cfg.Listen))
	}
	auditLog, err := audit.Open(cfg.Audit.Path)
	if err != nil {
		return nil, err
	}
	limiter := ratelimit.New(cfg.RateLimit.PerMinute, cfg.RateLimit.Window, cfg.RateLimit.MaxKeys, opt.Clock)
	ctx, cancel := context.WithCancel(context.Background())
	limiter.Start(ctx)
	authFails := ratelimit.New(cfg.Auth.Bruteforce.MaxFailures, cfg.Auth.Bruteforce.Window, cfg.RateLimit.MaxKeys, opt.Clock)
	authFails.Start(ctx)
	router := upstream.NewRouter(cfg, opt.Getenv, opt.HTTPClient, opt.Logger)

	svc, err := proxy.NewService(proxy.Deps{
		Scanner: secrets.New(textnorm.New(), cfg.Guard.Allowlist, cfg.Guard.Input.MaxDecodeDepth),
		Policy:  policy, Router: router, Audit: auditLog, Limiter: limiter, Clock: opt.Clock, FP: fp,
		Out: proxy.OutputConfig{
			NGram: cfg.Guard.Output.SystemLeakNGram, URLAllowlist: cfg.Guard.Output.URLAllowlist,
			RefusalText: cfg.Guard.Output.RefusalText,
		},
		MaxAuditRunes: cfg.Audit.MaxTextRunes,
		ScanRoles:     cfg.Guard.Input.ScanRoles,
		SystemContent: cfg.Audit.SystemContent,
		Tools: proxy.ToolLimits{
			MaxTools: cfg.Tools.MaxTools, MaxDefinitionsBytes: cfg.Tools.MaxDefinitionsBytes,
			MaxArgumentsBytes: cfg.Tools.MaxArgumentsBytes,
		},
		Logger: opt.Logger,
	})
	if err != nil {
		cancel()
		_ = auditLog.Close()
		return nil, err
	}
	handler, err := httpdelivery.NewRouter(httpdelivery.Deps{
		Service:      svc,
		Ready:        func() bool { return auditLog.Ready() && router.AnyReady() },
		Logger:       opt.Logger,
		MaxBodyBytes: cfg.MaxBodyBytes,
		Auth:         authn,
		AuthFailures: authFails,
	})
	if err != nil {
		cancel()
		_ = auditLog.Close()
		return nil, fmt.Errorf("http router: %w", err)
	}
	return &App{
		Handler: handler, Service: svc, Audit: auditLog, Limiter: limiter, AuthFailures: authFails, Router: router,
		Close: func() error {
			cancel()
			return errors.Join(auditLog.Close())
		},
	}, nil
}

// checkUpstreamKeys refuses a proxy key that equals an upstream API key:
// a client would otherwise hold the paid upstream key itself.
func checkUpstreamKeys(cfg *config.Proxy, a *apikey.Static, getenv func(string) string) error {
	for name, u := range cfg.Upstreams {
		if u.APIKeyEnv == "" {
			continue
		}
		if v := strings.TrimSpace(getenv(u.APIKeyEnv)); v != "" {
			if _, same := a.Authenticate(v); same {
				return fmt.Errorf("%s: a proxy key equals the API key of upstream %q (%s)", apikey.Env, name, u.APIKeyEnv)
			}
		}
	}
	return nil
}

func buildPolicy(in config.GuardInput) (secrets.Policy, error) {
	def, err := secrets.ParseMode(in.Mode)
	if err != nil {
		return secrets.Policy{}, err
	}
	p := secrets.Policy{Default: def, Overrides: map[secrets.Type]secrets.Mode{}}
	for t, m := range in.Overrides {
		mode, err := secrets.ParseMode(m)
		if err != nil {
			return secrets.Policy{}, fmt.Errorf("override %s: %w", t, err)
		}
		p.Overrides[secrets.Type(t)] = mode
	}
	return p, nil
}
