// Package ratelimit implements an in-memory sliding-window-log limiter.
package ratelimit

import (
	"context"
	"sync"
	"time"

	"agile-consultant/internal/ports"
)

// Memory limits requests per key to limit per window. When the number of
// tracked keys reaches maxKeys, expired keys are evicted synchronously; if
// the table is still full, new keys are denied (fail closed).
type Memory struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	maxKeys int
	clk     ports.Clock
	hits    map[string][]time.Time
}

var _ ports.RateLimiter = (*Memory)(nil)

// New returns a limiter.
func New(limit int, window time.Duration, maxKeys int, clk ports.Clock) *Memory {
	return &Memory{limit: limit, window: window, maxKeys: maxKeys, clk: clk, hits: map[string][]time.Time{}}
}

// Allow records a request for key if it is within the limit.
func (m *Memory) Allow(key string) ports.RateDecision {
	now := m.clk.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	ts, known := m.hits[key]
	if !known && len(m.hits) >= m.maxKeys {
		m.sweepLocked(now)
		if len(m.hits) >= m.maxKeys {
			return ports.RateDecision{Limit: m.limit, RetryAfter: m.window, ResetAt: now.Add(m.window)}
		}
	}
	ts = prune(ts, now.Add(-m.window))
	d := ports.RateDecision{Limit: m.limit}
	if len(ts) >= m.limit {
		d.ResetAt = ts[0].Add(m.window)
		d.RetryAfter = d.ResetAt.Sub(now)
		m.hits[key] = ts
		return d
	}
	ts = append(ts, now)
	m.hits[key] = ts
	d.Allowed = true
	d.Remaining = m.limit - len(ts)
	d.ResetAt = ts[0].Add(m.window)
	return d
}

// Peek reports whether key would be allowed now without recording a hit.
// It is used to count events (e.g. authentication failures) that are
// recorded with Allow only after the outcome is known.
func (m *Memory) Peek(key string) ports.RateDecision {
	now := m.clk.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	ts := prune(m.hits[key], now.Add(-m.window))
	if len(ts) == 0 {
		delete(m.hits, key)
		return ports.RateDecision{Allowed: true, Limit: m.limit, Remaining: m.limit}
	}
	m.hits[key] = ts
	d := ports.RateDecision{Limit: m.limit, ResetAt: ts[0].Add(m.window)}
	if len(ts) >= m.limit {
		d.RetryAfter = d.ResetAt.Sub(now)
		return d
	}
	d.Allowed = true
	d.Remaining = m.limit - len(ts)
	return d
}

// prune drops timestamps not after cutoff; ts is sorted.
func prune(ts []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(ts) && !ts[i].After(cutoff) {
		i++
	}
	if i == 0 {
		return ts
	}
	return append(ts[:0], ts[i:]...)
}

func (m *Memory) sweepLocked(now time.Time) {
	cutoff := now.Add(-m.window)
	for k, ts := range m.hits {
		if ts = prune(ts, cutoff); len(ts) == 0 {
			delete(m.hits, k)
		} else {
			m.hits[k] = ts
		}
	}
}

// Sweep evicts keys without requests in the current window.
func (m *Memory) Sweep() {
	now := m.clk.Now()
	m.mu.Lock()
	m.sweepLocked(now)
	m.mu.Unlock()
}

// Len returns the number of tracked keys.
func (m *Memory) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.hits)
}

// Start runs a janitor that sweeps every window until ctx is done.
func (m *Memory) Start(ctx context.Context) {
	go func() {
		t := time.NewTicker(m.window)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.Sweep()
			}
		}
	}()
}
