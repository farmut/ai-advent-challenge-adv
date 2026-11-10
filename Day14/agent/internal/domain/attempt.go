package domain

import "time"

// SubtaskStatus is the stage of one subtask of an execution loop. The
// transition table and the subtask type itself arrive with the loop (PR7);
// the pipeline already reports its stages with these values.
type SubtaskStatus string

// Subtask statuses.
const (
	SubtaskPending    SubtaskStatus = "pending"
	SubtaskGenerating SubtaskStatus = "generating"
	SubtaskTesting    SubtaskStatus = "testing"
	SubtaskReviewing  SubtaskStatus = "reviewing"
	SubtaskFixing     SubtaskStatus = "fixing"
	SubtaskVerifying  SubtaskStatus = "verifying"
	SubtaskDone       SubtaskStatus = "done"
	SubtaskFailed     SubtaskStatus = "failed"
	SubtaskBlocked    SubtaskStatus = "blocked"
	SubtaskSkipped    SubtaskStatus = "skipped"
)

// AttemptResult says how one generate → test → review attempt ended.
type AttemptResult string

// Attempt results.
const (
	ResultPassed       AttemptResult = "passed"        // tests and security review passed
	ResultNoChanges    AttemptResult = "no_changes"    // the generator changed nothing
	ResultGuardBlocked AttemptResult = "guard_blocked" // the guard proxy replaced the generator's answer
	ResultTestsFailed  AttemptResult = "tests_failed"
	ResultSecurityFail AttemptResult = "security_failed" // verified Critical/High findings
	ResultReviewFailed AttemptResult = "review_failed"   // reviewer or SAST output unusable (fail-closed)
	ResultNoProgress   AttemptResult = "no_progress"     // same blocking findings as the previous review
	ResultError        AttemptResult = "error"           // infrastructure error (sandbox, LLM)
)

// CommandOutcome is the persisted summary of one sandboxed command. The raw
// output is not kept: it may quote code and it goes to the generator only
// inside a DATA block.
type CommandOutcome struct {
	Argv     []string `json:"argv"`
	Stack    string   `json:"stack"`
	ExitCode int      `json:"exit_code"`
	TimedOut bool     `json:"timed_out,omitempty"`
}

// TestResult summarises the test stage of an attempt. Passed is decided by
// exit codes only.
type TestResult struct {
	Ran      bool             `json:"ran"`
	Passed   bool             `json:"passed"`
	Commands []CommandOutcome `json:"commands,omitempty"`
}

// Attempt is the write-ahead record of one generate → test → review attempt.
type Attempt struct {
	N int `json:"n"`
	// Stage is the last stage the attempt reached.
	Stage  SubtaskStatus `json:"stage"`
	Result AttemptResult `json:"result,omitempty"`
	// SecurityIteration counts reviews within the subtask (1-based, 0 = none).
	SecurityIteration int        `json:"security_iteration,omitempty"`
	Changed           []string   `json:"changed,omitempty"`
	Stacks            []string   `json:"stacks,omitempty"`
	TestResult        TestResult `json:"test_result"`
	Findings          []Finding  `json:"findings,omitempty"`
	Verdict           Verdict    `json:"verdict,omitempty"`
	FindingsKey       string     `json:"findings_key,omitempty"`
	// Feedback is the code-built feedback the next attempt receives.
	Feedback    []string  `json:"feedback,omitempty"`
	Warnings    []string  `json:"warnings,omitempty"`
	Error       string    `json:"error,omitempty"`
	Interrupted bool      `json:"interrupted,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at,omitempty"`
}

// WorkRoots are the absolute directories a role works in: Read is where it
// may look, Write where it may change files ("" = read-only).
type WorkRoots struct {
	Read, Write string
}

// DataBlock is untrusted content handed to a role (file text, command
// output). The runner wraps every block in nonce delimiters.
type DataBlock struct {
	Label   string
	Content string
}
