// Package clock provides port.Clock implementations: the wall clock for
// production and a virtual clock for tests that must not really sleep.
package clock

import (
	"context"
	"sync"
	"time"
)

// System is the wall clock.
type System struct{}

// Now returns time.Now().
func (System) Now() time.Time { return time.Now() }

// Sleep blocks for d or until ctx is done.
func (System) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Fake is a virtual clock: Sleep returns immediately and moves Now forward by
// d, recording every requested duration. It is safe for concurrent use.
type Fake struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

// NewFake returns a Fake starting at start (a fixed instant when zero).
func NewFake(start time.Time) *Fake {
	if start.IsZero() {
		start = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return &Fake{now: start}
}

// Now returns the virtual time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Sleep records d and advances virtual time, unless ctx is already done.
func (f *Fake) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sleeps = append(f.sleeps, d)
	if d > 0 {
		f.now = f.now.Add(d)
	}
	return nil
}

// Advance moves virtual time forward without recording a sleep.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// Sleeps returns a copy of every duration passed to Sleep.
func (f *Fake) Sleeps() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.sleeps...)
}

// Total is the sum of all recorded sleeps.
func (f *Fake) Total() time.Duration {
	var t time.Duration
	for _, d := range f.Sleeps() {
		t += d
	}
	return t
}
