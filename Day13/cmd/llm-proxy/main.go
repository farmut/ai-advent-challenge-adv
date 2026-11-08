// Command llm-proxy is an OpenAI-compatible guard proxy in front of LLM
// APIs: API-key auth (PROXY_API_KEYS, Authorization: Bearer), input guard
// (secrets/PII mask or block), output guard, per-IP rate limit, cost
// tracking and a JSONL audit log. It does not start without API keys and
// listens only on loopback unless proxy.allow_non_loopback is true (there
// is no TLS: put a TLS terminator in front).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"agile-consultant/internal/app"
	"agile-consultant/internal/config"
)

const defaultConfig = "configs/proxy.yaml"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stderr, nil); err != nil {
		fmt.Fprintf(os.Stderr, "llm-proxy: %v\n", err)
		os.Exit(1)
	}
}

// run starts the proxy and blocks until ctx is done. onListen, if set, gets
// the bound address.
func run(ctx context.Context, args []string, getenv func(string) string, logOut io.Writer, onListen func(string)) error {
	fs := flag.NewFlagSet("llm-proxy", flag.ContinueOnError)
	def := defaultConfig
	if v := getenv(config.EnvProxyConfig); v != "" {
		def = v
	}
	cfgPath := fs.String("config", def, "path to proxy config (env "+config.EnvProxyConfig+")")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.LoadProxyEnv(*cfgPath, getenv)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(logOut, &slog.HandlerOptions{Level: parseLevel(cfg.Log.Level)}))

	a, err := app.Build(cfg, app.Options{Logger: logger, Getenv: getenv})
	if err != nil {
		return err
	}
	defer func() {
		if err := a.Close(); err != nil {
			logger.Error("close", slog.String("error", err.Error()))
		}
	}()

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen %s: %w", cfg.Listen, err)
	}
	// Re-check the bound address: "localhost" must not resolve to a
	// non-loopback interface unless that is explicitly allowed.
	if err := cfg.CheckListen(ln.Addr().String()); err != nil {
		_ = ln.Close()
		return err
	}
	srv := &http.Server{
		Handler:           a.Handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      cfg.MaxUpstreamTimeout() + 10*time.Second,
		IdleTimeout:       60 * time.Second,
	}
	logger.Info("llm-proxy listening", slog.String("addr", ln.Addr().String()), slog.String("audit", config.RelToRoot(cfg.Root, cfg.Audit.Path)),
		slog.String("mode", cfg.Guard.Input.Mode), slog.Int("rate_per_min", cfg.RateLimit.PerMinute), slog.Bool("auth", true),
		slog.Any("models", a.Router.Aliases()))
	if onListen != nil {
		onListen(ln.Addr().String())
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	sctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	return slog.LevelInfo
}
