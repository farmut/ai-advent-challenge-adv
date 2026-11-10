package llm_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"ai-adv-agent/internal/adapter/clock"
	"ai-adv-agent/internal/adapter/llm"
	"ai-adv-agent/internal/port"
)

// All retry tests run against httptest with a virtual clock: no real sleeps.

func TestRetry_429HonoursRetryAfter(t *testing.T) {
	srv, calls := scripted(t,
		func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Retry-After", "2")
			writeErr(w, http.StatusTooManyRequests, "rate_limit_exceeded", "slow")
		},
		func(w http.ResponseWriter, _ *http.Request) { writeOK(w, "ok", "stop") },
	)
	clk := clock.NewFake(time.Time{})
	resp, err := retrying(t, srv.URL, defaultPolicy, clk).Chat(context.Background(), hiReq)
	if err != nil || resp.Content != "ok" {
		t.Fatalf("got (%q, %v)", resp.Content, err)
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2", calls.Load())
	}
	if s := clk.Sleeps(); len(s) != 1 || s[0] != 2*time.Second {
		t.Errorf("sleeps = %v, want [2s]", s)
	}
}

func TestRetry_RetryAfterAboveMaxDelayGivesUp(t *testing.T) {
	srv, calls := scripted(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "120")
		writeErr(w, http.StatusTooManyRequests, "rate_limit_exceeded", "slow")
	})
	clk := clock.NewFake(time.Time{})
	_, err := retrying(t, srv.URL, defaultPolicy, clk).Chat(context.Background(), hiReq)
	if err == nil || calls.Load() != 1 || len(clk.Sleeps()) != 0 {
		t.Fatalf("want immediate failure, got err=%v calls=%d sleeps=%v", err, calls.Load(), clk.Sleeps())
	}
}

func TestRetry_503ExhaustsMaxAttempts(t *testing.T) {
	srv, calls := scripted(t, func(w http.ResponseWriter, _ *http.Request) {
		writeErr(w, http.StatusServiceUnavailable, "upstream_unavailable", "down")
	})
	clk := clock.NewFake(time.Time{})
	_, err := retrying(t, srv.URL, defaultPolicy, clk).Chat(context.Background(), hiReq)
	var le *port.LLMError
	if !errors.As(err, &le) || le.Status != 503 {
		t.Fatalf("want the last 503 wrapped, got %v", err)
	}
	if calls.Load() != 4 {
		t.Errorf("calls = %d, want 1 + 3 retries", calls.Load())
	}
	// Exponential back-off without jitter: 1s, 2s, 4s.
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	got := clk.Sleeps()
	if len(got) != len(want) {
		t.Fatalf("sleeps = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("sleep[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestRetry_NoRetryOnFatal(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		code      string
		wantGuard bool
	}{
		{"401", http.StatusUnauthorized, "invalid_api_key", false},
		{"403", http.StatusForbidden, "forbidden", false},
		{"400 unsupported", http.StatusBadRequest, "unsupported_parameter", false},
		{"400 invalid_request", http.StatusBadRequest, "invalid_request", false},
		{"404 model", http.StatusNotFound, "model_not_found", false},
		{"413", http.StatusRequestEntityTooLarge, "request_too_large", false},
		{"503 not configured", http.StatusServiceUnavailable, "upstream_not_configured", false},
		{"503 upstream model", http.StatusServiceUnavailable, "upstream_model_not_found", false},
		{"400 guard", http.StatusBadRequest, "guard_input_blocked", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, calls := scripted(t, func(w http.ResponseWriter, _ *http.Request) { writeErr(w, tt.status, tt.code, "no") })
			clk := clock.NewFake(time.Time{})
			_, err := retrying(t, srv.URL, defaultPolicy, clk).Chat(context.Background(), hiReq)
			if calls.Load() != 1 || len(clk.Sleeps()) != 0 {
				t.Errorf("calls=%d sleeps=%v, want exactly one call and no wait", calls.Load(), clk.Sleeps())
			}
			if !errors.Is(err, port.ErrFatalLLM) {
				t.Errorf("want ErrFatalLLM, got %v", err)
			}
			if got := errors.Is(err, port.ErrGuardBlocked); got != tt.wantGuard {
				t.Errorf("errors.Is(ErrGuardBlocked) = %v, want %v", got, tt.wantGuard)
			}
		})
	}
}

func TestRetry_TransportErrorIsRetried(t *testing.T) {
	srv, _ := scripted(t, func(w http.ResponseWriter, _ *http.Request) { writeOK(w, "ok", "stop") })
	url := srv.URL
	srv.Close() // connection refused from now on
	clk := clock.NewFake(time.Time{})
	policy := llm.RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}
	_, err := retrying(t, url, policy, clk).Chat(context.Background(), hiReq)
	if err == nil || len(clk.Sleeps()) != 2 {
		t.Fatalf("want 2 retries on connection refused, got err=%v sleeps=%v", err, clk.Sleeps())
	}
}

func TestRetry_JitterStaysWithinMaxDelay(t *testing.T) {
	srv, _ := scripted(t, func(w http.ResponseWriter, _ *http.Request) {
		writeErr(w, http.StatusBadGateway, "upstream_error", "bad")
	})
	clk := clock.NewFake(time.Time{})
	policy := llm.RetryPolicy{MaxAttempts: 6, BaseDelay: time.Second, MaxDelay: 5 * time.Second, Jitter: 0.5}
	_, _ = retrying(t, srv.URL, policy, clk).Chat(context.Background(), hiReq)
	sleeps := clk.Sleeps()
	if len(sleeps) != 6 {
		t.Fatalf("sleeps = %v", sleeps)
	}
	for i, d := range sleeps {
		step := time.Second << i
		if step > 5*time.Second {
			step = 5 * time.Second
		}
		if d > step || d < step/2 {
			t.Errorf("sleep[%d] = %v, want in [%v, %v]", i, d, step/2, step)
		}
	}
}

func TestRetry_ContextCancelStopsWaiting(t *testing.T) {
	srv, calls := scripted(t, func(w http.ResponseWriter, _ *http.Request) {
		writeErr(w, http.StatusServiceUnavailable, "upstream_unavailable", "down")
	})
	ctx, cancel := context.WithCancel(context.Background())
	clk := &cancellingClock{Fake: clock.NewFake(time.Time{}), cancel: cancel}
	_, err := retrying(t, srv.URL, defaultPolicy, clk).Chat(ctx, hiReq)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

// cancellingClock cancels the context the moment a back-off starts.
type cancellingClock struct {
	*clock.Fake
	cancel context.CancelFunc
}

func (c *cancellingClock) Sleep(ctx context.Context, d time.Duration) error {
	c.cancel()
	return c.Fake.Sleep(ctx, d)
}

func TestNewRetryingClient_RejectsInvalidPolicy(t *testing.T) {
	inner := llmFunc(func(context.Context, port.LLMRequest) (port.LLMResponse, error) { return port.LLMResponse{}, nil })
	clk := clock.NewFake(time.Time{})
	for _, p := range []llm.RetryPolicy{
		{MaxAttempts: -1, BaseDelay: time.Second, MaxDelay: time.Second},
		{BaseDelay: 0, MaxDelay: time.Second},
		{BaseDelay: 2 * time.Second, MaxDelay: time.Second},
		{BaseDelay: time.Second, MaxDelay: time.Second, Jitter: 2},
	} {
		if _, err := llm.NewRetryingClient(inner, p, nil, clk); err == nil {
			t.Errorf("policy %+v must be rejected", p)
		}
	}
	if _, err := llm.NewRetryingClient(inner, defaultPolicy, nil, nil); err == nil {
		t.Error("nil clock must be rejected")
	}
}

type llmFunc func(context.Context, port.LLMRequest) (port.LLMResponse, error)

func (f llmFunc) Chat(ctx context.Context, r port.LLMRequest) (port.LLMResponse, error) {
	return f(ctx, r)
}
