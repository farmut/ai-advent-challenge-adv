package execloop

import (
	"context"
	"errors"
	"testing"
	"time"

	"ai-adv-agent/internal/adapter/clock"
	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// ---- fakes ----

// fakeLLM is a scripted LLM client: every Chat returns the canned response.
type fakeLLM struct {
	resp  port.LLMResponse
	calls int
}

func (f *fakeLLM) Chat(context.Context, port.LLMRequest) (port.LLMResponse, error) {
	f.calls++
	return f.resp, nil
}

// fakeBudgetEvents records Budget events.
type fakeBudgetEvents struct {
	usages []domain.BudgetUsage
}

func (f *fakeBudgetEvents) Stage(string, domain.SubtaskStatus, string) {}
func (f *fakeBudgetEvents) Findings(string, []domain.Finding)          {}
func (f *fakeBudgetEvents) Budget(u domain.BudgetUsage)                { f.usages = append(f.usages, u) }

// testBudget builds a budget around the given deadline with generous limits;
// tests narrow the single dimension they exercise.
func testBudget(deadline time.Time) domain.Budget {
	return domain.Budget{
		MaxSubtasks:           5,
		MaxAttemptsPerSubtask: 3,
		MaxLLMCalls:           100,
		MaxTokens:             1_000_000,
		MaxCostUSD:            10,
		Deadline:              deadline,
		MaxReplans:            1,
	}
}

// Plan section 7.3: every MeteredLLM budget (calls, tokens, cost, deadline)
// fails the call with ErrBudgetExceeded — with a fake clock, no real waiting.

func TestMeteredCallsLimit(t *testing.T) {
	clk := clock.NewFake(time.Time{})
	b := testBudget(clk.Now().Add(time.Hour))
	b.MaxLLMCalls = 2
	inner := &fakeLLM{}
	m, err := NewMeteredLLM(inner, b, clk, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if _, err := m.Chat(context.Background(), port.LLMRequest{}); err != nil {
			t.Fatalf("вызов %d: неожиданная ошибка: %v", i+1, err)
		}
	}
	_, err = m.Chat(context.Background(), port.LLMRequest{})
	if !errors.Is(err, port.ErrBudgetExceeded) {
		t.Fatalf("третий вызов должен дать ErrBudgetExceeded, получено: %v", err)
	}
	if inner.calls != 2 {
		t.Fatalf("превышенный бюджет не должен доходить до LLM: вызовов %d", inner.calls)
	}
	if reason, ok := m.Breached(); !ok || reason == "" {
		t.Fatalf("Breached должен зафиксировать причину, получено %q, %v", reason, ok)
	}
}

func TestMeteredTokensLimit(t *testing.T) {
	clk := clock.NewFake(time.Time{})
	b := testBudget(clk.Now().Add(time.Hour))
	b.MaxTokens = 10
	m, err := NewMeteredLLM(&fakeLLM{resp: port.LLMResponse{Usage: domain.Usage{TotalTokens: 10}}}, b, clk, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Chat(context.Background(), port.LLMRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Chat(context.Background(), port.LLMRequest{}); !errors.Is(err, port.ErrBudgetExceeded) {
		t.Fatalf("второй вызов (10/10 токенов) должен дать ErrBudgetExceeded, получено: %v", err)
	}
}

func TestMeteredCostLimit(t *testing.T) {
	clk := clock.NewFake(time.Time{})
	b := testBudget(clk.Now().Add(time.Hour))
	b.MaxCostUSD = 0.5
	resp := port.LLMResponse{Guard: port.GuardMeta{CostUSD: 0.5, HasCost: true}}
	m, err := NewMeteredLLM(&fakeLLM{resp: resp}, b, clk, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Chat(context.Background(), port.LLMRequest{}); err != nil {
		t.Fatal(err)
	}
	if got := m.Usage().CostUSD; got != 0.5 {
		t.Fatalf("Usage().CostUSD = %v, ожидалось 0.5", got)
	}
	if _, err := m.Chat(context.Background(), port.LLMRequest{}); !errors.Is(err, port.ErrBudgetExceeded) {
		t.Fatalf("второй вызов ($0.5/$0.5) должен дать ErrBudgetExceeded, получено: %v", err)
	}
}

func TestMeteredDeadlineLimit(t *testing.T) {
	clk := clock.NewFake(time.Time{})
	b := testBudget(clk.Now().Add(time.Hour))
	m, err := NewMeteredLLM(&fakeLLM{}, b, clk, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	// Virtual time passes the deadline without any real waiting.
	clk.Advance(2 * time.Hour)
	if _, err := m.Chat(context.Background(), port.LLMRequest{}); !errors.Is(err, port.ErrBudgetExceeded) {
		t.Fatalf("вызов после дедлайна должен дать ErrBudgetExceeded, получено: %v", err)
	}
	if _, ok := m.Breached(); !ok {
		t.Fatal("deadline-нарушение должно фиксироваться в Breached")
	}
}

func TestMeteredContextCanceled(t *testing.T) {
	clk := clock.NewFake(time.Time{})
	m, err := NewMeteredLLM(&fakeLLM{}, testBudget(clk.Now().Add(time.Hour)), clk, nil, 10)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Chat(ctx, port.LLMRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("отменённый контекст должен вернуть ctx.Err, получено: %v", err)
	}
	if _, ok := m.Breached(); ok {
		t.Fatal("отмена контекста не должна считаться нарушением бюджета")
	}
}

func TestMeteredNotifyEveryN(t *testing.T) {
	clk := clock.NewFake(time.Time{})
	ev := &fakeBudgetEvents{}
	m, err := NewMeteredLLM(&fakeLLM{}, testBudget(clk.Now().Add(time.Hour)), clk, ev, 2)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := m.Chat(context.Background(), port.LLMRequest{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(ev.usages) != 2 {
		t.Fatalf("событий Budget %d, ожидалось 2 (после 2-го и 4-го вызова)", len(ev.usages))
	}
	if ev.usages[0].LLMCalls != 2 || ev.usages[1].LLMCalls != 4 {
		t.Fatalf("usage в событиях: %+v", ev.usages)
	}
}

func TestMeteredRejectsBadBudget(t *testing.T) {
	clk := clock.NewFake(time.Time{})
	bad := []domain.Budget{
		{MaxLLMCalls: 0, MaxTokens: 1, MaxCostUSD: 1, Deadline: clk.Now().Add(time.Hour)},
		{MaxLLMCalls: 1, MaxTokens: 0, MaxCostUSD: 1, Deadline: clk.Now().Add(time.Hour)},
		{MaxLLMCalls: 1, MaxTokens: 1, MaxCostUSD: 0, Deadline: clk.Now().Add(time.Hour)},
		{MaxLLMCalls: 1, MaxTokens: 1, MaxCostUSD: 1},
	}
	for i, b := range bad {
		if _, err := NewMeteredLLM(&fakeLLM{}, b, clk, nil, 10); err == nil {
			t.Fatalf("бюджет %d должен быть отклонён: %+v", i, b)
		}
	}
	if _, err := NewMeteredLLM(nil, testBudget(clk.Now().Add(time.Hour)), clk, nil, 10); err == nil {
		t.Fatal("nil inner должен быть отклонён")
	}
}
