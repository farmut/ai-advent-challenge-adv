package clock

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFake_SleepAdvancesWithoutWaiting(t *testing.T) {
	f := NewFake(time.Time{})
	start := f.Now()
	if err := f.Sleep(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	if got := f.Now().Sub(start); got != time.Hour {
		t.Errorf("advanced %v, want 1h", got)
	}
	if f.Total() != time.Hour || len(f.Sleeps()) != 1 {
		t.Errorf("sleeps = %v", f.Sleeps())
	}
}

func TestFake_SleepHonoursCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewFake(time.Time{}).Sleep(ctx, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestSystem_SleepCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (System{}).Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}
