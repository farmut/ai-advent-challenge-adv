package config

import (
	"strings"
	"testing"
	"time"
)

// validateLoop is the load-time gate of every loop budget (plan section 7.3):
// zero/negative where "unlimited" is forbidden, values above the ceilings,
// and the on_ask_user policy spelling.

func validLoop(t *testing.T) LoopConfig {
	t.Helper()
	return DefaultLoop()
}

func TestLoopDefaultValid(t *testing.T) {
	if err := validateLoop(validLoop(t)); err != nil {
		t.Fatalf("DefaultLoop не проходит валидацию: %v", err)
	}
}

func TestLoopLimitsValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*LoopConfig)
		want   string
	}{
		{"zero subtasks", func(l *LoopConfig) { l.MaxSubtasks = 0 }, "max_subtasks"},
		{"negative subtasks", func(l *LoopConfig) { l.MaxSubtasks = -1 }, "max_subtasks"},
		{"subtasks above ceiling", func(l *LoopConfig) { l.MaxSubtasks = 51 }, "max_subtasks"},
		{"zero attempts", func(l *LoopConfig) { l.MaxAttemptsPerSubtask = 0 }, "max_attempts_per_subtask"},
		{"attempts above ceiling", func(l *LoopConfig) { l.MaxAttemptsPerSubtask = 6 }, "max_attempts_per_subtask"},
		{"zero llm_calls", func(l *LoopConfig) { l.MaxLLMCalls = 0 }, "max_llm_calls"},
		{"llm_calls above ceiling", func(l *LoopConfig) { l.MaxLLMCalls = 10001 }, "max_llm_calls"},
		{"zero tokens", func(l *LoopConfig) { l.MaxTokens = 0 }, "max_tokens"},
		{"negative tokens", func(l *LoopConfig) { l.MaxTokens = -1 }, "max_tokens"},
		{"zero cost", func(l *LoopConfig) { l.MaxCostUSD = 0 }, "max_cost_usd"},
		{"negative cost", func(l *LoopConfig) { l.MaxCostUSD = -0.5 }, "max_cost_usd"},
		{"zero deadline", func(l *LoopConfig) { l.Deadline = 0 }, "deadline"},
		{"negative deadline", func(l *LoopConfig) { l.Deadline = -time.Minute }, "deadline"},
		{"deadline above ceiling", func(l *LoopConfig) { l.Deadline = 25 * time.Hour }, "deadline"},
		{"negative replans", func(l *LoopConfig) { l.MaxReplans = -1 }, "max_replans"},
		{"replans above ceiling", func(l *LoopConfig) { l.MaxReplans = 11 }, "max_replans"},
		{"replans zero allowed", func(l *LoopConfig) { l.MaxReplans = 0 }, ""},
		{"unknown on_ask_user", func(l *LoopConfig) { l.OnAskUser = "ask" }, "on_ask_user"},
		{"empty on_ask_user", func(l *LoopConfig) { l.OnAskUser = "" }, "on_ask_user"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := validLoop(t)
			tc.mutate(&l)
			err := validateLoop(l)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("ожидалась валидная конфигурация, получена ошибка: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("ожидалась ошибка про %q, получено nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ошибка должна упоминать %q: %v", tc.want, err)
			}
		})
	}
}

// TestLoopYAMLLoad checks the YAML keys decode onto the defaults.
func TestLoopYAMLLoad(t *testing.T) {
	yaml := `
loop:
  max_subtasks: 5
  max_attempts_per_subtask: 2
  max_llm_calls: 50
  max_tokens: 500000
  max_cost_usd: 2.5
  deadline: 10m
  max_replans: 1
  on_ask_user: fail
`
	cfg, err := loadYAML(t, yaml)
	if err != nil {
		t.Fatalf("loadYAML: %v", err)
	}
	if err := validateLoop(cfg.Loop); err != nil {
		t.Fatalf("validateLoop: %v", err)
	}
	got := cfg.Loop
	want := LoopConfig{
		MaxSubtasks:           5,
		MaxAttemptsPerSubtask: 2,
		MaxLLMCalls:           50,
		MaxTokens:             500000,
		MaxCostUSD:            2.5,
		Deadline:              10 * time.Minute,
		MaxReplans:            1,
		OnAskUser:             LoopAskFail,
	}
	if got != want {
		t.Fatalf("loop config mismatch:\n got: %+v\nwant: %+v", got, want)
	}
}

// TestLoopYAMLLoadZeroRejected checks the fail-closed rule at file level: a
// file that zeroes a loop budget is refused by Validate, not silently run.
func TestLoopYAMLLoadZeroRejected(t *testing.T) {
	yaml := `
loop:
  max_llm_calls: 0
`
	cfg, err := loadYAML(t, yaml)
	if err != nil {
		t.Fatalf("loadYAML: %v", err)
	}
	if err := validateLoop(cfg.Loop); err == nil || !strings.Contains(err.Error(), "max_llm_calls") {
		t.Fatalf("нулевой loop.max_llm_calls должен давать ошибку валидации, получено: %v", err)
	}
}
