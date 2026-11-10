package usecase

import (
	"os"
	"regexp"
	"strings"

	"ai-adv-agent/internal/domain"
)

// redacted is what a secret becomes in persisted state: the reader sees that
// something was removed without keeping the secret itself.
const redacted = "[REDACTED]"

// secretPatterns are the well-known credential shapes that must never reach
// the loop state file: LLM and AWS keys, PEM private keys, HTTP bearer tokens
// and GitHub personal access tokens.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[^-]*-----END [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`Bearer \S+`),
	regexp.MustCompile(`ghp_[A-Za-z0-9]{20,}`),
}

// Apply scrubs secrets out of a loop run before it is persisted. Feedback,
// errors, warnings, changed-file lists and every textual field of every
// finding are run through the patterns, and the literal value of LLM_API_KEY
// is removed wherever it appears. The run is modified in place; raw prompts
// and full outputs never enter the state file at all (only ArtifactRef-style
// references and code-built summaries do).
func Apply(run *domain.LoopRun) {
	if run == nil {
		return
	}
	literal := os.Getenv("LLM_API_KEY")
	if len(literal) < 4 {
		literal = ""
	}
	for i := range run.Subtasks {
		st := &run.Subtasks[i]
		for j := range st.Attempts {
			a := &st.Attempts[j]
			a.Error = scrub(a.Error, literal)
			a.FindingsKey = scrub(a.FindingsKey, literal)
			a.Feedback = scrubSlice(a.Feedback, literal)
			a.Warnings = scrubSlice(a.Warnings, literal)
			a.Changed = scrubSlice(a.Changed, literal)
			for k := range a.Findings {
				f := a.Findings[k]
				f.Issue = scrub(f.Issue, literal)
				f.Evidence = scrub(f.Evidence, literal)
				f.Fix = scrub(f.Fix, literal)
				a.Findings[k] = f
			}
		}
	}
}

// scrubSlice returns a scrubbed copy of ss (nil stays nil).
func scrubSlice(ss []string, literal string) []string {
	if ss == nil {
		return nil
	}
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = scrub(s, literal)
	}
	return out
}

// scrub replaces every pattern match — and the literal secret, when set —
// with the redaction marker.
func scrub(s, literal string) string {
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, redacted)
	}
	if literal != "" {
		s = strings.ReplaceAll(s, literal, redacted)
	}
	return s
}
