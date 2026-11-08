package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"agile-consultant/internal/adapters/clock"
)

func TestSlidingWindow(t *testing.T) {
	clk := clock.NewFake(time.Unix(1_000_000, 0))
	l := New(3, time.Minute, 100, clk)
	steps := []struct {
		advance   time.Duration
		allowed   bool
		remaining int
	}{
		{0, true, 2},
		{10 * time.Second, true, 1},
		{10 * time.Second, true, 0},
		{10 * time.Second, false, 0},
		{31 * time.Second, true, 0}, // first hit expired at t=61s
		{0, false, 0},
	}
	for i, s := range steps {
		clk.Advance(s.advance)
		d := l.Allow("ip")
		if d.Allowed != s.allowed || d.Remaining != s.remaining || d.Limit != 3 {
			t.Fatalf("step %d: %+v", i, d)
		}
		if !d.Allowed && d.RetryAfter <= 0 {
			t.Fatalf("step %d: RetryAfter = %v", i, d.RetryAfter)
		}
	}
	if d := l.Allow("other"); !d.Allowed {
		t.Fatal("keys must be independent")
	}
}

func TestMaxKeysFailClosed(t *testing.T) {
	clk := clock.NewFake(time.Unix(0, 0))
	l := New(5, time.Minute, 2, clk)
	l.Allow("a")
	l.Allow("b")
	if d := l.Allow("c"); d.Allowed {
		t.Fatal("full table must deny new keys")
	}
	if d := l.Allow("a"); !d.Allowed {
		t.Fatal("known key must still be served")
	}
	clk.Advance(2 * time.Minute)
	if d := l.Allow("c"); !d.Allowed {
		t.Fatal("expired keys must be evicted synchronously")
	}
	if l.Len() != 1 {
		t.Fatalf("len = %d", l.Len())
	}
}

func TestConcurrentAllow(t *testing.T) {
	l := New(1000, time.Minute, 10000, clock.System{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l.Start(ctx)
	var wg sync.WaitGroup
	var mu sync.Mutex
	allowed := 0
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				if l.Allow(fmt.Sprintf("k%d", i%3)).Allowed {
					mu.Lock()
					allowed++
					mu.Unlock()
				}
			}
		}(g)
	}
	wg.Wait()
	if allowed != 1600 {
		t.Fatalf("allowed = %d, want 1600", allowed)
	}
}

func TestPeekDoesNotRecord(t *testing.T) {
	clk := clock.NewFake(time.Unix(1_000_000, 0))
	l := New(2, 5*time.Minute, 100, clk)
	for i := 0; i < 5; i++ {
		if d := l.Peek("authfail:ip"); !d.Allowed || d.Remaining != 2 {
			t.Fatalf("peek %d on empty key: %+v", i, d)
		}
	}
	if l.Len() != 0 {
		t.Fatal("peek must not create keys")
	}
	l.Allow("authfail:ip")
	l.Allow("authfail:ip")
	d := l.Peek("authfail:ip")
	if d.Allowed || d.RetryAfter != 5*time.Minute {
		t.Fatalf("peek after limit: %+v", d)
	}
	clk.Advance(5*time.Minute + time.Second)
	if d := l.Peek("authfail:ip"); !d.Allowed {
		t.Fatalf("peek after window: %+v", d)
	}
}
