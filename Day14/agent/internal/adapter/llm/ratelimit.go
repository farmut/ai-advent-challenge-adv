package llm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"ai-adv-agent/internal/port"
)

// TokenBucket is a client-side rate limiter: perMin tokens per minute, at most
// burst saved up. It keeps the agent under the guard proxy's per-IP limit so
// that most calls never see a 429 in the first place.
//
// Wait reserves a token under the lock and sleeps outside it, so a waiter
// whose context is cancelled leaves promptly and hands its reservation back
// instead of blocking the others.
type TokenBucket struct {
	clock port.Clock
	rate  float64 // tokens per second
	burst float64

	mu     sync.Mutex
	tokens float64
	last   time.Time
}

// NewTokenBucket returns a bucket that starts full. perMin and burst must be
// positive (config validation guarantees it; the check here guards hand-built
// wiring).
func NewTokenBucket(perMin, burst int, clock port.Clock) (*TokenBucket, error) {
	if perMin <= 0 || burst <= 0 {
		return nil, fmt.Errorf("token bucket: per_minute=%d and burst=%d must be positive", perMin, burst)
	}
	if clock == nil {
		return nil, fmt.Errorf("token bucket: clock is required")
	}
	return &TokenBucket{
		clock:  clock,
		rate:   float64(perMin) / 60,
		burst:  float64(burst),
		tokens: float64(burst),
		last:   clock.Now(),
	}, nil
}

// Wait blocks until a token is available or ctx is done. A nil bucket (the
// limiter disabled in config) never waits.
func (b *TokenBucket) Wait(ctx context.Context) error {
	if b == nil {
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	b.refill(b.clock.Now())
	b.tokens--
	var wait time.Duration
	if b.tokens < 0 {
		wait = time.Duration(-b.tokens / b.rate * float64(time.Second))
	}
	b.mu.Unlock()

	if wait <= 0 {
		return nil
	}
	if err := b.clock.Sleep(ctx, wait); err != nil {
		b.mu.Lock()
		b.tokens++ // give the reservation back
		b.mu.Unlock()
		return err
	}
	return nil
}

// refill adds the tokens accrued since the last refill, capped at burst.
// Callers hold b.mu.
func (b *TokenBucket) refill(now time.Time) {
	elapsed := now.Sub(b.last)
	if elapsed <= 0 {
		return
	}
	b.tokens += elapsed.Seconds() * b.rate
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	b.last = now
}
