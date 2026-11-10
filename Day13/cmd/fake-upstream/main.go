// Command fake-upstream runs the scripted OpenAI-compatible fake upstream
// from a JSON role script, for the Day14 e2e scenario (make e2e-loop-fake).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"agile-consultant/internal/testkit/fakeupstream"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:18099", "listen address")
	script := flag.String("script", "", "path to the JSON role script (required)")
	flag.Parse()

	if *script == "" {
		fmt.Fprintln(os.Stderr, "fake-upstream: -script is required")
		os.Exit(2)
	}
	data, err := os.ReadFile(*script)
	if err != nil {
		log.Fatalf("fake-upstream: read script %s: %v", *script, err)
	}
	h := fakeupstream.NewHandler()
	if err := h.LoadScript(data); err != nil {
		log.Fatalf("fake-upstream: %v", err)
	}
	h.OnRole = func(role string) { log.Printf("request role=%q", role) }

	srv := &http.Server{
		Addr:              *listen,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("fake-upstream listening on %s, script %s", *listen, *script)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("fake-upstream: serve: %v", err)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Fatalf("fake-upstream: graceful shutdown: %v", err)
		}
		log.Printf("fake-upstream stopped")
	}
}
