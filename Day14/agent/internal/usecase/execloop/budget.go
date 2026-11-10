package execloop

import (
	"context"
	"fmt"
	"sync"
	"time"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// systemClock is the production wall clock. The usecase layer depends on
// port.Clock only; adapters (and tests) inject their own implementation.
type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) Sleep(ctx context.Context, d time.Duration) error {
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

// MeteredLLM wraps a port.LLMClient with the run's budget: before every call
// it checks the call/token/cost counters and the deadline against ctx; after
// a successful call it adds the usage and the guard-reported cost. The first
// exceeded budget fails the call with port.ErrBudgetExceeded and latches the
// reason, so the loop can abort even where an error passed through a
// string-formatting layer (the devpipeline outcome reason).
//
// Production wiring passes the meter to the role runner's LLM, so every role
// call of the run is counted; the planner and verifier also gate on Check
// before their runs.
type MeteredLLM struct {
	inner port.LLMClient

	mu     sync.Mutex
	budget domain.Budget
	usage  domain.BudgetUsage
	breach string // first exceeded budget, latched
	clock  port.Clock
	ev     Events
	everyN int
}

// NewMeteredLLM assembles the meter. inner, a budget with a set deadline and
// a notify cadence >= 1 are required; clk nil means the wall clock, ev nil
// means no budget events.
func NewMeteredLLM(inner port.LLMClient, b domain.Budget, clk port.Clock, ev Events, notifyEvery int) (*MeteredLLM, error) {
	if inner == nil {
		return nil, fmt.Errorf("execloop: MeteredLLM требует LLMClient")
	}
	if b.MaxLLMCalls < 1 || b.MaxTokens < 1 || b.MaxCostUSD <= 0 {
		return nil, fmt.Errorf("execloop: бюджет должен задавать max_llm_calls/max_tokens/max_cost_usd > 0")
	}
	if b.Deadline.IsZero() {
		return nil, fmt.Errorf("execloop: бюджет должен задавать deadline")
	}
	if notifyEvery < 1 {
		return nil, fmt.Errorf("execloop: budget_notify_every должен быть >= 1, получено %d", notifyEvery)
	}
	if clk == nil {
		clk = systemClock{}
	}
	if ev == nil {
		ev = nopEvents{}
	}
	return &MeteredLLM{inner: inner, budget: b, clock: clk, ev: ev, everyN: notifyEvery}, nil
}

// Check gates one LLM call on the budget. A nil meter lets everything through
// (tests that fake the roles and never call the metered LLM).
func (m *MeteredLLM) Check(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if reason := m.breachLocked(); reason != "" {
		m.breach = reason
		return fmt.Errorf("%w: %s", port.ErrBudgetExceeded, reason)
	}
	return nil
}

// breachLocked returns the reason of the first exceeded budget, "" when all
// budgets still hold.
func (m *MeteredLLM) breachLocked() string {
	switch {
	case m.usage.LLMCalls >= m.budget.MaxLLMCalls:
		return fmt.Sprintf("превышен лимит вызовов LLM (%d)", m.budget.MaxLLMCalls)
	case m.usage.Tokens >= m.budget.MaxTokens:
		return fmt.Sprintf("превышен лимит токенов (%d)", m.budget.MaxTokens)
	case m.usage.CostUSD >= m.budget.MaxCostUSD:
		return fmt.Sprintf("превышен лимит стоимости ($%.4f)", m.budget.MaxCostUSD)
	case !m.clock.Now().Before(m.budget.Deadline):
		return fmt.Sprintf("истёк дедлайн (%s)", m.budget.Deadline.Format(time.RFC3339))
	}
	return ""
}

// Chat implements port.LLMClient: gate, call, account, notify.
func (m *MeteredLLM) Chat(ctx context.Context, req port.LLMRequest) (port.LLMResponse, error) {
	if err := m.Check(ctx); err != nil {
		return port.LLMResponse{}, err
	}
	resp, err := m.inner.Chat(ctx, req)
	if err != nil {
		return port.LLMResponse{}, err
	}
	m.mu.Lock()
	m.usage.LLMCalls++
	m.usage.Tokens += int64(resp.Usage.TotalTokens)
	if resp.Guard.HasCost {
		m.usage.CostUSD += resp.Guard.CostUSD
	}
	usage := m.usage
	n := m.everyN
	m.mu.Unlock()
	if usage.LLMCalls%n == 0 {
		m.ev.Budget(usage)
	}
	return resp, nil
}

// Usage returns a copy of what the run has spent so far.
func (m *MeteredLLM) Usage() domain.BudgetUsage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.usage
}

// Breached reports whether a budget check failed and why. The reason stays
// latched after the first breach, so the loop can distinguish "subtask
// failed" from "budget spent" even where only the outcome reason survived.
func (m *MeteredLLM) Breached() (string, bool) {
	if m == nil {
		return "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.breach, m.breach != ""
}

// gate is the pre-run check shared by the planner and the verifier.
func (m *MeteredLLM) gate(ctx context.Context) error {
	return m.Check(ctx)
}
