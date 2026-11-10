package contracts

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"ai-adv-agent/internal/domain"
)

// Contract limits of security_findings.v1.
const (
	SchemaSecurityFindings = "security_findings.v1"
	// MaxFindings: findings beyond it are dropped (and reported), not an error.
	MaxFindings = 20
	// MaxFieldBytes: a longer field rejects the answer.
	MaxFieldBytes = 2048
	// MaxEvidenceRunes: longer evidence is cut, not rejected.
	MaxEvidenceRunes = 200
)

// SecurityFindings is the reviewer's answer.
type SecurityFindings struct {
	Schema string `json:"schema"`
	// Verdict is validated for shape but never used: the verdict is computed
	// from the findings by domain.ComputeVerdict.
	Verdict  string            `json:"verdict"`
	Findings []SecurityFinding `json:"findings"`
	// Dropped counts findings cut by MaxFindings.
	Dropped int `json:"-"`
}

// SecurityFinding is one finding as the reviewer reports it.
type SecurityFinding struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	CWE      string `json:"cwe"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Rule     string `json:"rule"`
	Issue    string `json:"issue"`
	Evidence string `json:"evidence"`
	Fix      string `json:"fix"`
}

var cweRe = regexp.MustCompile(`^CWE-[0-9]{1,5}$`)

// Validate checks the schema and normalises the findings.
func (s *SecurityFindings) Validate() error {
	if s.Schema != SchemaSecurityFindings {
		return fmt.Errorf("schema: ожидается %q, получено %q", SchemaSecurityFindings, s.Schema)
	}
	if s.Verdict != string(domain.VerdictPass) && s.Verdict != string(domain.VerdictFail) {
		return fmt.Errorf("verdict: ожидается pass или fail, получено %q", s.Verdict)
	}
	if s.Findings == nil {
		return fmt.Errorf("findings обязателен (пустой список — []")
	}
	if len(s.Findings) > MaxFindings {
		s.Dropped = len(s.Findings) - MaxFindings
		s.Findings = s.Findings[:MaxFindings]
	}
	for i := range s.Findings {
		if err := s.Findings[i].validate(); err != nil {
			return fmt.Errorf("findings[%d]: %w", i, err)
		}
	}
	return nil
}

func (f *SecurityFinding) validate() error {
	fields := [...]struct{ name, v string }{
		{"id", f.ID}, {"severity", f.Severity}, {"cwe", f.CWE}, {"file", f.File},
		{"rule", f.Rule}, {"issue", f.Issue}, {"evidence", f.Evidence}, {"fix", f.Fix},
	}
	for _, fl := range fields {
		if len(fl.v) > MaxFieldBytes {
			return fmt.Errorf("%s длиннее %d байт", fl.name, MaxFieldBytes)
		}
		if !utf8.ValidString(fl.v) {
			return fmt.Errorf("%s: не UTF-8", fl.name)
		}
	}
	if strings.TrimSpace(f.ID) == "" {
		return fmt.Errorf("id обязателен")
	}
	sev, err := domain.ParseSeverity(f.Severity)
	if err != nil {
		return err
	}
	f.Severity = string(sev)
	f.CWE = strings.ToUpper(strings.TrimSpace(f.CWE))
	if !cweRe.MatchString(f.CWE) {
		return fmt.Errorf("cwe: ожидается вида CWE-89, получено %q", f.CWE)
	}
	if strings.TrimSpace(f.File) == "" {
		return fmt.Errorf("file обязателен")
	}
	if f.Line < 1 {
		return fmt.Errorf("line должен быть >= 1, получено %d", f.Line)
	}
	if strings.TrimSpace(f.Issue) == "" {
		return fmt.Errorf("issue обязателен")
	}
	if strings.TrimSpace(f.Fix) == "" {
		return fmt.Errorf("fix обязателен: конкретное действие по исправлению")
	}
	f.Evidence = cutRunes(f.Evidence, MaxEvidenceRunes)
	return nil
}

// ToDomain converts the reviewer's findings. Verified stays false: only the
// pipeline, after checking file and line, may set it.
func (s SecurityFindings) ToDomain() []domain.Finding {
	out := make([]domain.Finding, 0, len(s.Findings))
	for _, f := range s.Findings {
		out = append(out, domain.Finding{
			ID:       f.ID,
			Severity: domain.Severity(f.Severity),
			CWE:      f.CWE,
			File:     f.File,
			Line:     f.Line,
			Rule:     f.Rule,
			Issue:    f.Issue,
			Evidence: f.Evidence,
			Fix:      f.Fix,
			Source:   domain.SourceLLM,
		})
	}
	return out
}

// cutRunes shortens s to at most n runes, marking the cut with "…".
func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
