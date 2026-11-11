package execloop

import (
	"context"
	"strings"
	"testing"
	"time"

	"ai-adv-agent/internal/adapter/clock"
	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// TestTerminateReportFinalState — регрессия PEER-03: сохранённый отчёт должен
// содержать терминальный статус и фактический usage из meter, а не «running»
// с нулевыми счётчиками.
func TestTerminateReportFinalState(t *testing.T) {
	clk := clock.NewFake(time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC))
	inner := &fakeLLM{resp: port.LLMResponse{
		Usage: domain.Usage{TotalTokens: 42},
		Guard: port.GuardMeta{HasCost: true, CostUSD: 0.01},
	}}
	meter, err := NewMeteredLLM(inner, domain.Budget{
		MaxLLMCalls: 10, MaxTokens: 1000, MaxCostUSD: 1.0,
		Deadline: clk.Now().Add(time.Hour),
	}, clk, nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := meter.Chat(context.Background(), port.LLMRequest{}); err != nil {
		t.Fatal(err)
	}

	rig := newRig(t, nil, nil, defaultRoles(newFakeWS(), oneSubtaskPlan()), nil)
	// Подменяем meter на настроенный (newRig передаёт nil).
	loop := rig.loop
	loop.meter = meter

	run := domain.LoopRun{
		SchemaVersion: 1,
		ID:            "peer03",
		Goal:          domain.Goal{Text: "цель", Roots: []string{"цель"}},
		Status:        domain.LoopRunning, // документируемый симптом: отчёт раньше сохранял running
		Budget: domain.Budget{
			MaxSubtasks: 5, MaxAttemptsPerSubtask: 3, MaxLLMCalls: 10,
			MaxTokens: 1000, MaxCostUSD: 1.0, Deadline: clk.Now().Add(time.Hour), MaxReplans: 1,
		},
		Subtasks: []domain.Subtask{
			{ID: "st1", Title: "t", Spec: "s", Profile: "coder", Acceptance: []string{"a"}, Root: 0, Status: domain.SubtaskPending},
		},
		CreatedAt: clk.Now(),
		UpdatedAt: clk.Now(),
	}
	out, err := loop.terminate(&run, domain.LoopDone)
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != domain.LoopDone {
		t.Fatalf("status %q, want done", out.Status)
	}
	for _, want := range []string{"Статус: **done**", "вызовов LLM 1/10", "токенов 42/1000", "$0.0100/$1.00"} {
		if !strings.Contains(out.Report, want) {
			t.Errorf("report lacks %q:\n%s", want, out.Report)
		}
	}
	if out.Usage.LLMCalls != 1 || out.Usage.Tokens != 42 {
		t.Fatalf("usage %+v, want calls=1 tokens=42", out.Usage)
	}
}
