package telegram

import (
	"sync"
	"testing"
	"time"
)

// fakeClock — управляемые часы: время двигается только через Advance.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, time.January, 1, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func TestChatLimiterAllow(t *testing.T) {
	c := newFakeClock()
	l := newChatLimiter(perChatInterval, maxTrackedChats, c.Now)
	var elapsed time.Duration
	steps := []struct {
		at   time.Duration
		want bool
	}{{0, true}, {time.Second, false}, {1999 * time.Millisecond, false}, {2 * time.Second, true}, {3 * time.Second, false}, {4 * time.Second, true}}
	for _, st := range steps {
		c.Advance(st.at - elapsed)
		elapsed = st.at
		if got := l.Allow(1); got != st.want {
			t.Errorf("Allow(+%v) = %v, want %v", st.at, got, st.want)
		}
	}
}

func TestChatLimiterNilClockUsesTimeNow(t *testing.T) {
	l := newChatLimiter(perChatInterval, maxTrackedChats, nil)
	if !l.Allow(1) || l.Allow(1) {
		t.Error("Allow with real clock: want first true, second false")
	}
}

func TestChatLimiterEvictsStale(t *testing.T) {
	c := newFakeClock()
	l := newChatLimiter(perChatInterval, maxTrackedChats, c.Now)
	for id := range int64(maxTrackedChats) {
		l.Allow(id)
	}
	c.Advance(time.Minute)
	l.Allow(-1)
	if len(l.last) != 1 {
		t.Errorf("tracked chats = %d, want 1 after eviction", len(l.last))
	}
}

func TestChatLimiterKeepsFreshEntries(t *testing.T) {
	const maxChats = 3
	c := newFakeClock()
	l := newChatLimiter(perChatInterval, maxChats, c.Now)
	for id := range int64(maxChats) {
		l.Allow(id)
	}
	c.Advance(perChatInterval - time.Millisecond)
	if !l.Allow(-1) {
		t.Fatal("Allow(new chat) = false, want true")
	}
	if len(l.last) != maxChats+1 {
		t.Errorf("tracked chats = %d, want %d: fresh entries must not be evicted", len(l.last), maxChats+1)
	}
	if l.Allow(0) {
		t.Error("Allow(0) = true, want false: its window has not expired")
	}
}

func TestChatLimiterConcurrent(t *testing.T) {
	c := newFakeClock()
	l := newChatLimiter(perChatInterval, maxTrackedChats, c.Now)
	const goroutines = 50
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		allowed int
	)
	for i := range goroutines {
		wg.Go(func() {
			ok := l.Allow(1) // все бьют в один чат: пройти должен ровно один
			l.Allow(int64(i + 100))
			if ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if allowed != 1 {
		t.Errorf("allowed = %d, want 1", allowed)
	}
}
