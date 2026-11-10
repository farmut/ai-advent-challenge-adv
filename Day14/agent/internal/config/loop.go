package config

import (
	"fmt"
	"time"
)

// LoopConfig configures the execution loop (usecase/execloop): the budget of
// one run and the behaviour at the edges where a fully autonomous loop can
// get stuck. Every limit lives here and is mandatory: zero is a
// misconfiguration, never "unlimited" (plan section 7: loop budgets are
// fail-closed).
type LoopConfig struct {
	// MaxSubtasks caps the subtasks of one run, including the subtasks a
	// replan adds (>= 1).
	MaxSubtasks int `yaml:"max_subtasks"`
	// MaxAttemptsPerSubtask caps generate → test → review attempts per
	// subtask (>= 1).
	MaxAttemptsPerSubtask int `yaml:"max_attempts_per_subtask"`
	// MaxLLMCalls caps the LLM calls of the whole run (>= 1).
	MaxLLMCalls int `yaml:"max_llm_calls"`
	// MaxTokens caps the tokens of the whole run (>= 1).
	MaxTokens int64 `yaml:"max_tokens"`
	// MaxCostUSD caps the spend of the whole run as reported by the guard
	// proxy (X-Guard-Cost-USD); a zero-priced local model still spends
	// calls and tokens (>= 0, but 0 is refused: the budget must be real).
	MaxCostUSD float64 `yaml:"max_cost_usd"`
	// Deadline bounds the wall time of one run (> 0).
	Deadline time.Duration `yaml:"deadline"`
	// MaxReplans caps how many times the planner may rebuild the pending
	// subtasks after a failure (>= 0; 0 disables replanning).
	MaxReplans int `yaml:"max_replans"`
	// OnAskUser decides what happens when a role still tries to ask the
	// user a question in the fully autonomous loop: "decide" suppresses the
	// question and tells the model to decide itself, "fail" blocks the
	// subtask.
	OnAskUser string `yaml:"on_ask_user"`
}

// Ask-user policies.
const (
	LoopAskDecide = "decide"
	LoopAskFail   = "fail"
)

// DefaultLoop returns production-safe run budgets.
func DefaultLoop() LoopConfig {
	return LoopConfig{
		MaxSubtasks:           10,
		MaxAttemptsPerSubtask: 4,
		MaxLLMCalls:           150,
		MaxTokens:             1_500_000,
		MaxCostUSD:            1.0,
		Deadline:              time.Hour,
		MaxReplans:            2,
		OnAskUser:             LoopAskDecide,
	}
}

// Ceilings for the loop budgets. Nothing above them is a sane run.
const (
	maxSubtasksCeil           = 50
	maxAttemptsPerSubtaskCeil = 5
	maxLLMCallsCeil           = 10_000
	maxReplansCeil            = 10
	loopDeadlineCeil          = 24 * time.Hour
)

// validateLoop checks every loop budget. Zero and negative values are errors:
// an "unlimited" autonomous loop is refused, not silently run (fail-closed).
func validateLoop(l LoopConfig) error {
	if l.MaxSubtasks < 1 || l.MaxSubtasks > maxSubtasksCeil {
		return fmt.Errorf("loop.max_subtasks должен быть в [1, %d], получено %d", maxSubtasksCeil, l.MaxSubtasks)
	}
	if l.MaxAttemptsPerSubtask < 1 || l.MaxAttemptsPerSubtask > maxAttemptsPerSubtaskCeil {
		return fmt.Errorf("loop.max_attempts_per_subtask должен быть в [1, %d], получено %d", maxAttemptsPerSubtaskCeil, l.MaxAttemptsPerSubtask)
	}
	if l.MaxLLMCalls < 1 || l.MaxLLMCalls > maxLLMCallsCeil {
		return fmt.Errorf("loop.max_llm_calls должен быть в [1, %d], получено %d", maxLLMCallsCeil, l.MaxLLMCalls)
	}
	if l.MaxTokens < 1 {
		return fmt.Errorf("loop.max_tokens должен быть > 0, получено %d", l.MaxTokens)
	}
	if l.MaxCostUSD <= 0 {
		return fmt.Errorf("loop.max_cost_usd должен быть > 0, получено %g", l.MaxCostUSD)
	}
	if l.Deadline <= 0 || l.Deadline > loopDeadlineCeil {
		return fmt.Errorf("loop.deadline должен быть в (0, %s], получено %s", loopDeadlineCeil, l.Deadline)
	}
	if l.MaxReplans < 0 || l.MaxReplans > maxReplansCeil {
		return fmt.Errorf("loop.max_replans должен быть в [0, %d], получено %d", maxReplansCeil, l.MaxReplans)
	}
	switch l.OnAskUser {
	case LoopAskDecide, LoopAskFail:
	default:
		return fmt.Errorf("loop.on_ask_user должен быть %q или %q, получено %q", LoopAskDecide, LoopAskFail, l.OnAskUser)
	}
	return nil
}
