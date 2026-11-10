package llm

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"time"

	"ai-adv-agent/internal/port"
)

// RetryPolicy is the retry part of the llm config. Every value comes from
// llm.retry; there are no hidden constants.
type RetryPolicy struct {
	// MaxAttempts is the number of retries after the first call (0 = none).
	MaxAttempts int
	// BaseDelay is the first back-off step; it doubles on every retry.
	BaseDelay time.Duration
	// MaxDelay caps a single back-off step and the Retry-After the client is
	// willing to honour: a longer Retry-After ends the retries at once.
	MaxDelay time.Duration
	// Jitter in [0,1] shortens each step by a random fraction up to Jitter,
	// so parallel clients do not retry in lockstep.
	Jitter float64
}

// RetryingClient wraps a port.LLMClient with rate limiting and retries.
//
// Policy: 429 waits for Retry-After (or backs off when absent); 500/502/503/
// 504 and transport errors back off; everything else — 400/401/403/404/413,
// guard_input_blocked, upstream_not_configured, upstream_model_not_found — is
// returned at once, because repeating the same request cannot change the
// answer. Cancelling ctx interrupts both the limiter and the back-off sleep.
type RetryingClient struct {
	inner   port.LLMClient
	policy  RetryPolicy
	limiter *TokenBucket // nil = client-side limiter disabled
	clock   port.Clock
	rand    func() float64
}

// NewRetryingClient wires the decorator. limiter may be nil; clock is required.
func NewRetryingClient(inner port.LLMClient, policy RetryPolicy, limiter *TokenBucket, clock port.Clock) (*RetryingClient, error) {
	if inner == nil || clock == nil {
		return nil, fmt.Errorf("retrying client: inner client and clock are required")
	}
	if policy.MaxAttempts < 0 || policy.BaseDelay <= 0 || policy.MaxDelay < policy.BaseDelay ||
		policy.Jitter < 0 || policy.Jitter > 1 {
		return nil, fmt.Errorf("retrying client: invalid policy %+v", policy)
	}
	return &RetryingClient{inner: inner, policy: policy, limiter: limiter, clock: clock, rand: rand.Float64}, nil
}

// Chat implements port.LLMClient.
func (r *RetryingClient) Chat(ctx context.Context, req port.LLMRequest) (port.LLMResponse, error) {
	for attempt := 0; ; attempt++ {
		if err := r.limiter.Wait(ctx); err != nil {
			return port.LLMResponse{}, fmt.Errorf("llm rate limiter: %w", err)
		}
		resp, err := r.inner.Chat(ctx, req)
		if err == nil {
			return resp, nil
		}
		if !r.retryable(ctx, err) {
			return port.LLMResponse{}, err
		}
		if attempt >= r.policy.MaxAttempts {
			return port.LLMResponse{}, fmt.Errorf("LLM: исчерпаны повторы (%d из llm.retry.max_attempts): %w", attempt, err)
		}
		delay, ok := r.delay(attempt, err)
		if !ok {
			return port.LLMResponse{}, fmt.Errorf("LLM: Retry-After больше llm.retry.max_delay (%s), не жду: %w", r.policy.MaxDelay, err)
		}
		if serr := r.clock.Sleep(ctx, delay); serr != nil {
			return port.LLMResponse{}, fmt.Errorf("LLM: ожидание повтора прервано: %w (последняя ошибка: %v)", serr, err)
		}
	}
}

// retryable reports whether err is worth another attempt.
func (r *RetryingClient) retryable(ctx context.Context, err error) bool {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var le *port.LLMError
	if errors.As(err, &le) {
		return le.Retryable()
	}
	var te *transientError
	if errors.As(err, &te) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne)
}

// delay picks the wait before retry number attempt+1. ok is false when the
// server demands a longer pause than MaxDelay.
func (r *RetryingClient) delay(attempt int, err error) (time.Duration, bool) {
	var le *port.LLMError
	if errors.As(err, &le) && le.RetryAfter > 0 {
		if le.RetryAfter > r.policy.MaxDelay {
			return 0, false
		}
		return le.RetryAfter, true
	}
	d := r.policy.BaseDelay
	for i := 0; i < attempt && d < r.policy.MaxDelay; i++ {
		d *= 2
	}
	if d > r.policy.MaxDelay {
		d = r.policy.MaxDelay
	}
	if r.policy.Jitter > 0 {
		d -= time.Duration(float64(d) * r.policy.Jitter * r.rand())
	}
	return d, true
}

// transientError marks a failure of the transport (connection refused, reset,
// timeout while reading) as opposed to a well-formed but unwelcome answer.
type transientError struct{ err error }

func (e *transientError) Error() string { return e.err.Error() }
func (e *transientError) Unwrap() error { return e.err }
