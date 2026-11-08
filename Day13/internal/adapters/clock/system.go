// Package clock provides ports.Clock implementations.
package clock

import (
	"sync"
	"time"

	"agile-consultant/internal/ports"
)

// System is the wall clock.
type System struct{}

var _ ports.Clock = System{}

// Now returns time.Now().
func (System) Now() time.Time { return time.Now() }

// Fake is a manually advanced clock for tests.
type Fake struct {
	mu  sync.Mutex
	now time.Time
}

// NewFake returns a Fake set to t.
func NewFake(t time.Time) *Fake { return &Fake{now: t} }

// Now returns the fake time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance moves the clock forward by d.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	f.mu.Unlock()
}
