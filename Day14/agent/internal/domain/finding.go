package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// Severity is the security severity of a finding.
type Severity string

// Severities, highest first.
const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
)

// Rank orders severities: critical=5 … info=1, unknown=0.
func (s Severity) Rank() int {
	switch s {
	case SeverityCritical:
		return 5
	case SeverityHigh:
		return 4
	case SeverityMedium:
		return 3
	case SeverityLow:
		return 2
	case SeverityInfo:
		return 1
	}
	return 0
}

// Blocking reports whether a finding of this severity sends the code back to
// the generator (Critical and High). Medium and below only reach the report.
func (s Severity) Blocking() bool { return s.Rank() >= SeverityHigh.Rank() }

// ParseSeverity accepts a severity in any letter case. Anything else is an
// error: an unknown severity must not silently become "info".
func ParseSeverity(s string) (Severity, error) {
	sev := Severity(strings.ToLower(strings.TrimSpace(s)))
	if sev.Rank() == 0 {
		return "", fmt.Errorf("неизвестная серьёзность %q (critical, high, medium, low, info)", s)
	}
	return sev, nil
}

// FindingSource names who reported a finding.
type FindingSource string

// Finding sources.
const (
	SourceLLM    FindingSource = "llm"
	SourceGosec  FindingSource = "gosec"
	SourceESLint FindingSource = "eslint"
)

// Finding is one security issue in the task directory.
type Finding struct {
	ID       string   `json:"id"`
	Severity Severity `json:"severity"`
	CWE      string   `json:"cwe"`
	// File is relative to the task directory, slash-separated.
	File     string        `json:"file"`
	Line     int           `json:"line"`
	Rule     string        `json:"rule,omitempty"`
	Issue    string        `json:"issue"`
	Evidence string        `json:"evidence,omitempty"`
	Fix      string        `json:"fix,omitempty"`
	Source   FindingSource `json:"source"`
	// Sources lists every reporter after de-duplication (Source among them).
	Sources []FindingSource `json:"sources,omitempty"`
	// Verified is set by code, never by a reviewer: the file exists in the
	// task directory and the line is within it. Unverified findings are
	// reported but never block.
	Verified bool `json:"verified"`
}

// IsBlocking reports whether this finding fails the review.
func (f Finding) IsBlocking() bool { return f.Verified && f.Severity.Blocking() }

// Verdict is the outcome of a security review.
type Verdict string

// Verdicts.
const (
	VerdictPass Verdict = "pass"
	VerdictFail Verdict = "fail"
)

// ComputeVerdict is the only source of a review verdict: fail if and only if
// some verified finding is High or Critical. What a reviewer claims as its
// verdict is ignored.
func ComputeVerdict(fs []Finding) Verdict {
	for _, f := range fs {
		if f.IsBlocking() {
			return VerdictFail
		}
	}
	return VerdictPass
}

// Blocking returns the findings that fail the review, in input order.
func Blocking(fs []Finding) []Finding {
	var out []Finding
	for _, f := range fs {
		if f.IsBlocking() {
			out = append(out, f)
		}
	}
	return out
}

// FindingsKey fingerprints the blocking findings as the sorted set of
// "file|cwe" pairs. Lines are left out on purpose: a fix attempt that only
// moves the vulnerable code is not progress. Two iterations with the same key
// mean the generator is not converging. Empty when nothing blocks.
func FindingsKey(fs []Finding) string {
	set := map[string]bool{}
	for _, f := range Blocking(fs) {
		set[f.File+"|"+strings.ToUpper(f.CWE)] = true
	}
	if len(set) == 0 {
		return ""
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sum := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	return hex.EncodeToString(sum[:])
}
