package domain

import (
	"fmt"
	"regexp"
	"time"
)

// LoopStatus is the stage of one execution-loop run as a whole. SubtaskStatus
// (attempt.go) describes the stage of a single subtask inside the run.
type LoopStatus string

// Loop statuses.
const (
	LoopPlanning LoopStatus = "planning"
	LoopRunning  LoopStatus = "running"
	LoopPaused   LoopStatus = "paused"
	LoopDone     LoopStatus = "done"
	LoopFailed   LoopStatus = "failed"
	LoopAborted  LoopStatus = "aborted"
)

// loopTransitions defines the valid moves of the run-level state machine.
var loopTransitions = map[LoopStatus][]LoopStatus{
	LoopPlanning: {LoopRunning, LoopFailed, LoopAborted},
	LoopRunning:  {LoopPaused, LoopDone, LoopFailed, LoopAborted},
	LoopPaused:   {LoopRunning, LoopAborted},
}

// CanTransition reports whether the run may move from l to to.
func (l LoopStatus) CanTransition(to LoopStatus) bool {
	for _, ok := range loopTransitions[l] {
		if ok == to {
			return true
		}
	}
	return false
}

// subtaskTransitions defines the valid moves of one subtask through the
// generate → test → review → verify pipeline. blocked is deliberately empty:
// a blocked subtask leaves the blocked state only through an explicit user
// resume (TransitionResume), never as a normal pipeline step.
var subtaskTransitions = map[SubtaskStatus][]SubtaskStatus{
	SubtaskPending:    {SubtaskGenerating, SubtaskSkipped, SubtaskBlocked},
	SubtaskGenerating: {SubtaskTesting, SubtaskFixing, SubtaskFailed, SubtaskBlocked},
	SubtaskTesting:    {SubtaskReviewing, SubtaskFixing, SubtaskFailed, SubtaskBlocked},
	SubtaskReviewing:  {SubtaskVerifying, SubtaskFixing, SubtaskFailed, SubtaskBlocked},
	SubtaskFixing:     {SubtaskGenerating},
	SubtaskVerifying:  {SubtaskDone, SubtaskFixing, SubtaskFailed},
	SubtaskBlocked:    {},
}

// Transition advances the subtask FSM to the target stage, following the
// pattern of TaskState.Transition: an unknown or disallowed move is an error
// and leaves the subtask untouched.
func (s *Subtask) Transition(to SubtaskStatus, now time.Time) error {
	for _, ok := range subtaskTransitions[s.Status] {
		if ok == to {
			s.Status = to
			return nil
		}
	}
	return fmt.Errorf("недопустимый переход подзадачи: %s → %s", s.Status, to)
}

// TransitionResume moves a blocked subtask back to pending. This is the only
// way out of SubtaskBlocked and exists separately from Transition so a
// pipeline step can never accidentally unblock a subtask: only an explicit
// user decision ("/loop resume") may do it.
func (s *Subtask) TransitionResume(now time.Time) error {
	if s.Status != SubtaskBlocked {
		return fmt.Errorf("resume разрешён только для blocked, текущий статус: %s", s.Status)
	}
	s.Status = SubtaskPending
	return nil
}

// Budget is the spending envelope of one loop run.
type Budget struct {
	MaxSubtasks           int       `json:"max_subtasks"`
	MaxAttemptsPerSubtask int       `json:"max_attempts_per_subtask"`
	MaxLLMCalls           int       `json:"max_llm_calls"`
	MaxTokens             int64     `json:"max_tokens"`
	MaxCostUSD            float64   `json:"max_cost_usd"`
	Deadline              time.Time `json:"deadline"`
	MaxReplans            int       `json:"max_replans"`
}

// BudgetUsage is what the run has actually spent so far.
type BudgetUsage struct {
	LLMCalls int     `json:"llm_calls"`
	Tokens   int64   `json:"tokens"`
	CostUSD  float64 `json:"cost_usd"`
	Replans  int     `json:"replans"`
}

// Goal is the immutable objective of a loop run. Roots are the bullet points
// of the "@file" goal the user pointed the loop at; Text is the goal itself.
type Goal struct {
	Text  string   `json:"text"`
	Roots []string `json:"roots,omitempty"`
}

// Subtask is one unit of work of a loop run. Attempts is the write-ahead log
// of every generate → test → review attempt, in execution order.
type Subtask struct {
	ID         string        `json:"id"`
	Title      string        `json:"title"`
	Spec       string        `json:"spec"`
	Profile    string        `json:"profile"`
	Stack      []string      `json:"stack,omitempty"`
	DependsOn  []string      `json:"depends_on,omitempty"`
	Acceptance []string      `json:"acceptance"`
	Root       int           `json:"root"`
	Status     SubtaskStatus `json:"status"`
	Attempts   []Attempt     `json:"attempts,omitempty"`
}

// LoopRun is the persisted state of one execution loop run. It is written
// atomically on every stage change (write-ahead), so a crash or restart
// resumes from the last saved state.
type LoopRun struct {
	SchemaVersion int         `json:"schema_version"`
	ID            string      `json:"id"`
	Goal          Goal        `json:"goal"`
	Status        LoopStatus  `json:"status"`
	Subtasks      []Subtask   `json:"subtasks"`
	CurrentIdx    int         `json:"current_idx"`
	Budget        Budget      `json:"budget"`
	Usage         BudgetUsage `json:"usage"`
	CreatedAt     time.Time   `json:"created_at"`
	UpdatedAt     time.Time   `json:"updated_at"`
	Report        string      `json:"report,omitempty"`
}

// Limits enforced by LoopRun.Validate. They mirror the hard ceilings of the
// plan so a hand-edited or corrupted state file is rejected on load.
const (
	LoopSchemaVersion = 1
	loopMaxSubtasks   = 50
	loopMaxAttempts   = 5
	loopMaxFindings   = 20
	loopMaxFieldLen   = 2048
)

// LoopIDPattern is the subtask/run id format: lowercase ids keep file names
// derived from them portable and free of traversal characters.
var LoopIDPattern = regexp.MustCompile(`^[a-z0-9-]{1,32}$`)

// Validate checks the run against the persistence invariants. Anything that
// fails here must never reach the state file: a struct that does not validate
// cannot be resumed after a crash.
func (r LoopRun) Validate() error {
	if r.SchemaVersion != LoopSchemaVersion {
		return fmt.Errorf("loop: неподдерживаемая версия схемы %d (ожидается %d)", r.SchemaVersion, LoopSchemaVersion)
	}
	if !LoopIDPattern.MatchString(r.ID) {
		return fmt.Errorf("loop: невалидный id %q (нужен %s)", r.ID, LoopIDPattern)
	}
	switch r.Status {
	case LoopPlanning, LoopRunning, LoopPaused, LoopDone, LoopFailed, LoopAborted:
	default:
		return fmt.Errorf("loop: неизвестный статус %q", r.Status)
	}
	if len(r.Subtasks) > loopMaxSubtasks {
		return fmt.Errorf("loop: слишком много подзадач: %d (максимум %d)", len(r.Subtasks), loopMaxSubtasks)
	}
	if r.CurrentIdx < 0 || r.CurrentIdx > len(r.Subtasks) {
		return fmt.Errorf("loop: current_idx %d вне диапазона [0, %d]", r.CurrentIdx, len(r.Subtasks))
	}
	if err := checkLoopFieldLen("goal", r.Goal.Text); err != nil {
		return err
	}
	for _, root := range r.Goal.Roots {
		if err := checkLoopFieldLen("goal.root", root); err != nil {
			return err
		}
	}
	if err := checkLoopFieldLen("report", r.Report); err != nil {
		return err
	}
	if r.Budget.MaxSubtasks < 1 || r.Budget.MaxSubtasks > loopMaxSubtasks {
		return fmt.Errorf("loop: budget.max_subtasks %d вне диапазона [1, %d]", r.Budget.MaxSubtasks, loopMaxSubtasks)
	}
	if r.Budget.MaxAttemptsPerSubtask < 1 || r.Budget.MaxAttemptsPerSubtask > loopMaxAttempts {
		return fmt.Errorf("loop: budget.max_attempts_per_subtask %d вне диапазона [1, %d]", r.Budget.MaxAttemptsPerSubtask, loopMaxAttempts)
	}
	seen := make(map[string]bool, len(r.Subtasks))
	for i := range r.Subtasks {
		st := &r.Subtasks[i]
		if !LoopIDPattern.MatchString(st.ID) {
			return fmt.Errorf("loop: невалидный id подзадачи %q (нужен %s)", st.ID, LoopIDPattern)
		}
		if seen[st.ID] {
			return fmt.Errorf("loop: дубль id подзадачи %q", st.ID)
		}
		seen[st.ID] = true
		switch st.Status {
		case SubtaskPending, SubtaskGenerating, SubtaskTesting, SubtaskReviewing,
			SubtaskFixing, SubtaskVerifying, SubtaskDone, SubtaskFailed, SubtaskBlocked, SubtaskSkipped:
		default:
			return fmt.Errorf("loop: неизвестный статус подзадачи %q (%s)", st.Status, st.ID)
		}
		if st.Title == "" {
			return fmt.Errorf("loop: пустой title подзадачи %s", st.ID)
		}
		if st.Spec == "" {
			return fmt.Errorf("loop: пустой spec подзадачи %s", st.ID)
		}
		if err := checkLoopFieldLen("title", st.Title); err != nil {
			return err
		}
		if err := checkLoopFieldLen("spec", st.Spec); err != nil {
			return err
		}
		if err := checkLoopFieldLen("profile", st.Profile); err != nil {
			return err
		}
		if len(st.Acceptance) == 0 {
			return fmt.Errorf("loop: пустой acceptance подзадачи %s", st.ID)
		}
		for _, a := range st.Acceptance {
			if err := checkLoopFieldLen("acceptance", a); err != nil {
				return err
			}
		}
		for _, v := range st.Stack {
			if err := checkLoopFieldLen("stack", v); err != nil {
				return err
			}
		}
		for _, v := range st.DependsOn {
			if err := checkLoopFieldLen("depends_on", v); err != nil {
				return err
			}
		}
		if len(st.Attempts) > loopMaxAttempts {
			return fmt.Errorf("loop: слишком много попыток у подзадачи %s: %d (максимум %d)", st.ID, len(st.Attempts), loopMaxAttempts)
		}
		for j := range st.Attempts {
			a := &st.Attempts[j]
			if len(a.Findings) > loopMaxFindings {
				return fmt.Errorf("loop: слишком много находок в попытке %d подзадачи %s: %d (максимум %d)", a.N, st.ID, len(a.Findings), loopMaxFindings)
			}
			switch a.Stage {
			case "", SubtaskGenerating, SubtaskTesting, SubtaskReviewing, SubtaskFixing, SubtaskVerifying, SubtaskDone:
			default:
				return fmt.Errorf("loop: неизвестная стадия попытки %d подзадачи %s: %q", a.N, st.ID, a.Stage)
			}
			if err := checkLoopFieldLen("error", a.Error); err != nil {
				return err
			}
			if err := checkLoopFieldLen("findings_key", a.FindingsKey); err != nil {
				return err
			}
			for _, v := range a.Feedback {
				if err := checkLoopFieldLen("feedback", v); err != nil {
					return err
				}
			}
			for _, v := range a.Warnings {
				if err := checkLoopFieldLen("warning", v); err != nil {
					return err
				}
			}
			for _, v := range a.Changed {
				if err := checkLoopFieldLen("changed", v); err != nil {
					return err
				}
			}
			for k := range a.Findings {
				f := &a.Findings[k]
				if err := checkLoopFieldLen("finding.issue", f.Issue); err != nil {
					return err
				}
				if err := checkLoopFieldLen("finding.evidence", f.Evidence); err != nil {
					return err
				}
				if err := checkLoopFieldLen("finding.fix", f.Fix); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// checkLoopFieldLen rejects string fields longer than loopMaxFieldLen bytes.
func checkLoopFieldLen(field, v string) error {
	if len(v) > loopMaxFieldLen {
		return fmt.Errorf("loop: поле %s длиннее %d байт (%d)", field, loopMaxFieldLen, len(v))
	}
	return nil
}
