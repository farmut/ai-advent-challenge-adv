package llm_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"ai-adv-agent/internal/adapter/clock"
	"ai-adv-agent/internal/adapter/llm"
	"ai-adv-agent/internal/port"
)

func TestTokenBucket_BurstThenWait(t *testing.T) {
	clk := clock.NewFake(time.Time{})
	b, err := llm.NewTokenBucket(60, 2, clk) // 1 token/s
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := b.Wait(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if s := clk.Sleeps(); len(s) != 1 || s[0] != time.Second {
		t.Errorf("third call should wait 1s after a burst of 2, sleeps = %v", s)
	}
}

func TestTokenBucket_RefillsOverTimeUpToBurst(t *testing.T) {
	clk := clock.NewFake(time.Time{})
	b, _ := llm.NewTokenBucket(60, 2, clk)
	ctx := context.Background()
	_ = b.Wait(ctx)
	_ = b.Wait(ctx)
	clk.Advance(time.Hour) // refill is capped at burst, not 3600 tokens
	for i := 0; i < 2; i++ {
		_ = b.Wait(ctx)
	}
	if len(clk.Sleeps()) != 0 {
		t.Fatalf("refilled burst must not wait, sleeps = %v", clk.Sleeps())
	}
	_ = b.Wait(ctx)
	if s := clk.Sleeps(); len(s) != 1 {
		t.Errorf("burst cap ignored, sleeps = %v", s)
	}
}

func TestTokenBucket_DisabledNeverWaits(t *testing.T) {
	var b *llm.TokenBucket // llm.rate_limit.enabled: false
	for i := 0; i < 100; i++ {
		if err := b.Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTokenBucket_CancelReturnsReservation(t *testing.T) {
	clk := clock.NewFake(time.Time{})
	b, _ := llm.NewTokenBucket(60, 1, clk)
	_ = b.Wait(context.Background()) // drain
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := b.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	clk.Advance(time.Second) // one token accrues
	if err := b.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(clk.Sleeps()) != 0 {
		t.Errorf("a cancelled waiter must not consume the next token, sleeps = %v", clk.Sleeps())
	}
}

func TestTokenBucket_RejectsInvalid(t *testing.T) {
	clk := clock.NewFake(time.Time{})
	for _, c := range [][2]int{{0, 1}, {1, 0}, {-1, 2}} {
		if _, err := llm.NewTokenBucket(c[0], c[1], clk); err == nil {
			t.Errorf("per_minute=%d burst=%d must be rejected", c[0], c[1])
		}
	}
}

// The retrying client consults the bucket before every attempt.
func TestRetryingClient_UsesBucket(t *testing.T) {
	clk := clock.NewFake(time.Time{})
	b, _ := llm.NewTokenBucket(60, 2, clk)
	calls := 0
	inner := llmFunc(func(context.Context, port.LLMRequest) (port.LLMResponse, error) {
		calls++
		return port.LLMResponse{Content: "ok"}, nil
	})
	rc, err := llm.NewRetryingClient(inner, defaultPolicy, b, clk)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := rc.Chat(context.Background(), hiReq); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 3 || clk.Total() != time.Second {
		t.Errorf("calls=%d waited=%v, want 3 calls and 1s of limiter wait", calls, clk.Total())
	}
}
