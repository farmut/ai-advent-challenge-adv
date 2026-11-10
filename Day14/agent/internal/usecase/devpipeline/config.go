// Package devpipeline runs one subtask through generate → test → security
// review, feeding code-built feedback back to the generator until the code
// passes, the security iterations run out or no progress is made.
//
// Trust boundaries:
//   - which files changed is computed from SHA-256 snapshots, never taken from
//     the generator's report;
//   - tests pass or fail by exit code;
//   - the reviewer is read-only, sees the changed files with line numbers in
//     nonce DATA blocks and must answer with security_findings.v1;
//   - every finding is checked against the task directory (file exists, line
//     in range) and only verified Critical/High findings block;
//   - the verdict is computed by code; the reviewer's own verdict is ignored;
//   - the generator receives feedback.v1 rendered from finding fields by a
//     fixed template, never the reviewer's raw text.
package devpipeline

import (
	"fmt"
	"sort"
	"time"
)

// Stacks the pipeline knows.
const (
	StackGo  = "go"
	StackVue = "vue"
)

// Config holds every limit of the pipeline. Nothing here has a hidden
// default in code: config.Pipeline supplies the values.
type Config struct {
	// MaxSecurityIterations caps security reviews per subtask (>= 1).
	MaxSecurityIterations int
	// JSONRetries is how many times a reviewer answer that fails the
	// contract is re-requested (>= 0). After that the review fails closed.
	JSONRetries int
	// Generator is the default generator profile (Input.Profile overrides).
	Generator string
	// Reviewers maps a stack to its security reviewer profile.
	Reviewers map[string]string
	// TestCommands maps a stack to the commands that must all exit 0.
	TestCommands map[string][][]string
	// CommandTimeout caps each test command (0 = the sandbox timeout).
	CommandTimeout time.Duration
	// TestOutputMaxBytes is the tail of test output the generator sees.
	TestOutputMaxBytes int
	// ReviewFileMaxBytes / ReviewTotalMaxBytes bound the source shown to a
	// reviewer per file and in total.
	ReviewFileMaxBytes  int
	ReviewTotalMaxBytes int
	// MaxToolRounds caps a role's tool-calling rounds.
	MaxToolRounds int
}

// Validate rejects a config the pipeline cannot run safely.
func (c Config) Validate() error {
	if c.MaxSecurityIterations < 1 {
		return fmt.Errorf("pipeline: max_security_iterations должен быть >= 1, получено %d", c.MaxSecurityIterations)
	}
	if c.JSONRetries < 0 {
		return fmt.Errorf("pipeline: json_retries должен быть >= 0, получено %d", c.JSONRetries)
	}
	if c.Generator == "" {
		return fmt.Errorf("pipeline: generator_profile не задан")
	}
	if c.TestOutputMaxBytes <= 0 || c.ReviewFileMaxBytes <= 0 || c.ReviewTotalMaxBytes <= 0 {
		return fmt.Errorf("pipeline: лимиты test_output_max_bytes/review_file_max_bytes/review_total_max_bytes должны быть > 0")
	}
	if c.MaxToolRounds <= 0 {
		return fmt.Errorf("pipeline: max_tool_rounds должен быть > 0, получено %d", c.MaxToolRounds)
	}
	if c.CommandTimeout < 0 {
		return fmt.Errorf("pipeline: command_timeout не может быть отрицательным")
	}
	for _, st := range []string{StackGo, StackVue} {
		if c.Reviewers[st] == "" {
			return fmt.Errorf("pipeline: reviewers.%s не задан", st)
		}
		if len(c.TestCommands[st]) == 0 {
			return fmt.Errorf("pipeline: test_commands.%s пуст — без тестов код не проверяется", st)
		}
		for i, argv := range c.TestCommands[st] {
			if len(argv) == 0 {
				return fmt.Errorf("pipeline: test_commands.%s[%d] пуст", st, i)
			}
		}
	}
	for st := range c.Reviewers {
		if st != StackGo && st != StackVue {
			return fmt.Errorf("pipeline: неизвестный стек %q в reviewers", st)
		}
	}
	return nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
