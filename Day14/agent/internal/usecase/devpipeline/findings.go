package devpipeline

import (
	"path"
	"sort"
	"strings"

	"ai-adv-agent/internal/domain"
)

// lineCounter returns the line count of a file in the task directory, or
// ok=false when it cannot be read as text.
type lineCounter func(rel string) (lines int, ok bool)

// verify marks a finding Verified when its file is a regular text file of
// the task snapshot and its line is within the file. The path is normalised
// first; absolute paths and ".." never verify. The same rule applies to LLM
// and SAST findings: a reporter's claim alone verifies nothing.
func verify(fs []domain.Finding, snapshot map[string]string, count lineCounter) []domain.Finding {
	out := make([]domain.Finding, 0, len(fs))
	for _, f := range fs {
		f.Verified = false
		rel, ok := normRel(f.File)
		if ok {
			f.File = rel
			if _, exists := snapshot[rel]; exists {
				if n, ok := count(rel); ok && f.Line >= 1 && f.Line <= n {
					f.Verified = true
				}
			}
		}
		if len(f.Sources) == 0 {
			f.Sources = []domain.FindingSource{f.Source}
		}
		out = append(out, f)
	}
	return out
}

// normRel cleans a reported path to the snapshot's form.
func normRel(p string) (string, bool) {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" || path.IsAbs(p) {
		return p, false
	}
	c := path.Clean(p)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") {
		return p, false
	}
	return c, true
}

// lineWindow is how far apart two reports of the same issue may be.
const lineWindow = 2

// merge de-duplicates findings on (file, CWE, |Δline| ≤ 2). Only findings
// with the same verification status merge: an unverified claim must not
// borrow a verified one's standing. The higher severity wins; empty
// issue/fix/evidence are filled from the other report and sources united.
// The result is sorted: blocking first, then by severity, file and line.
func merge(fs []domain.Finding) []domain.Finding {
	var out []domain.Finding
next:
	for _, f := range fs {
		for i := range out {
			o := &out[i]
			if o.Verified != f.Verified || o.File != f.File || !strings.EqualFold(o.CWE, f.CWE) {
				continue
			}
			if d := o.Line - f.Line; d > lineWindow || d < -lineWindow {
				continue
			}
			*o = combine(*o, f)
			continue next
		}
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.IsBlocking() != b.IsBlocking() {
			return a.IsBlocking()
		}
		if a.Severity.Rank() != b.Severity.Rank() {
			return a.Severity.Rank() > b.Severity.Rank()
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	return out
}

func combine(a, b domain.Finding) domain.Finding {
	keep, other := a, b
	if b.Severity.Rank() > a.Severity.Rank() {
		keep, other = b, a
	}
	if keep.Issue == "" {
		keep.Issue = other.Issue
	}
	if keep.Fix == "" {
		keep.Fix = other.Fix
	}
	if keep.Evidence == "" {
		keep.Evidence = other.Evidence
	}
	seen := map[domain.FindingSource]bool{}
	var srcs []domain.FindingSource
	for _, s := range append(append([]domain.FindingSource{}, a.Sources...), b.Sources...) {
		if !seen[s] {
			seen[s] = true
			srcs = append(srcs, s)
		}
	}
	keep.Sources = srcs
	return keep
}
