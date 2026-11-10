// Package execloop drives a whole execution loop: it asks the planner for a
// plan.v1, runs every subtask through the generate → test → security-review
// pipeline, verifies the acceptance criteria with a read-only LLM judge,
// replans the pending subtasks when one fails and writes a markdown report.
//
// Trust boundaries:
//   - the goal is code-built and immutable: the planner sees it only inside
//     nonce DATA blocks and the run's Goal never changes after Start;
//   - every budget (calls, tokens, cost, deadline) is checked before each LLM
//     call by MeteredLLM; the loop also watches the deadline between stages;
//   - the acceptance verdict is computed by code from step_result.v1
//     (all criteria met, one per acceptance item), never from the judge's
//     own status field alone;
//   - run state is persisted write-ahead through port.LoopRepository on
//     every stage change, so a crash or restart resumes from the last save.
package execloop

import (
	"fmt"
	"strings"
	"time"
)

// Profile is one entry of the planner's catalogue: the profile names a
// subtask may use, with the description the planner reasons about.
type Profile struct {
	Name        string
	Description string
}

// Ask-user policies (config.LoopConfig.on_ask_user).
const (
	AskDecide = "decide"
	AskFail   = "fail"
)

// Ceilings of the loop budgets. They mirror the config ceilings: a hand-built
// Config is checked against the same limits as a loaded one.
const (
	maxSubtasksCeil           = 50
	maxAttemptsPerSubtaskCeil = 5
	maxLLMCallsCeil           = 10_000
	maxReplansCeil            = 10
	loopDeadlineCeil          = 24 * time.Hour
	maxJSONRetriesCeil        = 10
)

// Config holds every limit of the execution loop. Nothing here has a hidden
// default: config.Loop supplies the values, app wires them through.
type Config struct {
	// MaxSubtasks caps the subtasks of one run, replans included (>= 1).
	MaxSubtasks int
	// MaxAttemptsPerSubtask caps generate → test → review attempts per
	// subtask (>= 1).
	MaxAttemptsPerSubtask int
	// MaxLLMCalls caps the LLM calls of the whole run (>= 1).
	MaxLLMCalls int
	// MaxTokens caps the tokens of the whole run (>= 1).
	MaxTokens int64
	// MaxCostUSD caps the spend of the whole run (>= 0, but 0 is refused).
	MaxCostUSD float64
	// Deadline bounds the wall time of one run from its (re)start (> 0).
	Deadline time.Duration
	// MaxReplans caps planner rebuilds after a failure (>= 0).
	MaxReplans int
	// OnAskUser is what happens when a role still asks the user a question:
	// AskDecide suppresses it (the model must decide itself), AskFail
	// blocks the subtask.
	OnAskUser string
	// Profiles is the catalogue of profile names the planner may put into a
	// subtask. It must contain the planner and verifier profiles.
	Profiles []Profile
	// JSONRetries is how many times a planner/verifier answer that breaks
	// its contract is re-requested (>= 0).
	JSONRetries int
	// BudgetNotifyEvery is the meter's Events.Budget cadence: every N LLM
	// calls (>= 1).
	BudgetNotifyEvery int
	// VerifyFileMaxBytes / VerifyTotalMaxBytes bound the source shown to the
	// acceptance verifier per file and in total.
	VerifyFileMaxBytes  int
	VerifyTotalMaxBytes int
	// WorkspaceRoot is the absolute workspace root; it appears in the report
	// as the base path. "" means the report omits it.
	WorkspaceRoot string
}

// Validate rejects a config the loop cannot run safely. Zero and negative
// budgets are errors: an autonomous loop is fail-closed, never unlimited.
func (c Config) Validate() error {
	if c.MaxSubtasks < 1 || c.MaxSubtasks > maxSubtasksCeil {
		return fmt.Errorf("execloop: max_subtasks должен быть в [1, %d], получено %d", maxSubtasksCeil, c.MaxSubtasks)
	}
	if c.MaxAttemptsPerSubtask < 1 || c.MaxAttemptsPerSubtask > maxAttemptsPerSubtaskCeil {
		return fmt.Errorf("execloop: max_attempts_per_subtask должен быть в [1, %d], получено %d", maxAttemptsPerSubtaskCeil, c.MaxAttemptsPerSubtask)
	}
	if c.MaxLLMCalls < 1 || c.MaxLLMCalls > maxLLMCallsCeil {
		return fmt.Errorf("execloop: max_llm_calls должен быть в [1, %d], получено %d", maxLLMCallsCeil, c.MaxLLMCalls)
	}
	if c.MaxTokens < 1 {
		return fmt.Errorf("execloop: max_tokens должен быть > 0, получено %d", c.MaxTokens)
	}
	if c.MaxCostUSD <= 0 {
		return fmt.Errorf("execloop: max_cost_usd должен быть > 0, получено %g", c.MaxCostUSD)
	}
	if c.Deadline <= 0 || c.Deadline > loopDeadlineCeil {
		return fmt.Errorf("execloop: deadline должен быть в (0, %s], получено %s", loopDeadlineCeil, c.Deadline)
	}
	if c.MaxReplans < 0 || c.MaxReplans > maxReplansCeil {
		return fmt.Errorf("execloop: max_replans должен быть в [0, %d], получено %d", maxReplansCeil, c.MaxReplans)
	}
	switch c.OnAskUser {
	case AskDecide, AskFail:
	default:
		return fmt.Errorf("execloop: on_ask_user должен быть %q или %q, получено %q", AskDecide, AskFail, c.OnAskUser)
	}
	if c.JSONRetries < 0 || c.JSONRetries > maxJSONRetriesCeil {
		return fmt.Errorf("execloop: json_retries должен быть в [0, %d], получено %d", maxJSONRetriesCeil, c.JSONRetries)
	}
	if c.BudgetNotifyEvery < 1 {
		return fmt.Errorf("execloop: budget_notify_every должен быть >= 1, получено %d", c.BudgetNotifyEvery)
	}
	if c.VerifyFileMaxBytes < 1 || c.VerifyTotalMaxBytes < 1 {
		return fmt.Errorf("execloop: verify_file_max_bytes/verify_total_max_bytes должны быть > 0")
	}
	if c.VerifyTotalMaxBytes < c.VerifyFileMaxBytes {
		return fmt.Errorf("execloop: verify_total_max_bytes (%d) меньше verify_file_max_bytes (%d)", c.VerifyTotalMaxBytes, c.VerifyFileMaxBytes)
	}
	if len(c.Profiles) == 0 {
		return fmt.Errorf("execloop: каталог профилей пуст")
	}
	seen := make(map[string]bool, len(c.Profiles))
	for _, p := range c.Profiles {
		if strings.TrimSpace(p.Name) == "" {
			return fmt.Errorf("execloop: профиль с пустым именем")
		}
		if seen[p.Name] {
			return fmt.Errorf("execloop: дубль профиля %q", p.Name)
		}
		seen[p.Name] = true
	}
	if !seen[ProfilePlanner] {
		return fmt.Errorf("execloop: каталог профилей не содержит профиль планировщика %q", ProfilePlanner)
	}
	if !seen[ProfileVerifier] {
		return fmt.Errorf("execloop: каталог профилей не содержит профиль верификатора %q", ProfileVerifier)
	}
	return nil
}

// known returns the profile-name set the plan validator checks against.
func (c Config) known() map[string]bool {
	m := make(map[string]bool, len(c.Profiles))
	for _, p := range c.Profiles {
		m[p.Name] = true
	}
	return m
}
