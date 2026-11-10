package contracts

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Contract limits of step_result.v1, the acceptance verifier's answer. The
// criteria echo the acceptance list, so the same field limits as the other
// contracts apply.
const (
	// SchemaStepResult is the schema of the acceptance verifier's answer.
	SchemaStepResult = "step_result.v1"
	// MaxStepCriteria caps the criteria list; a longer list rejects the
	// answer (the verifier must echo the acceptance list, never grow it).
	MaxStepCriteria = 20
	// MaxStepEvidenceRunes: longer evidence is cut, not rejected.
	MaxStepEvidenceRunes = 200
)

// Step statuses of step_result.v1.
const (
	StepStatusDone   = "done"
	StepStatusFailed = "failed"
)

// StepResult is the acceptance verifier's answer: one verdict per acceptance
// criterion of one subtask.
type StepResult struct {
	Schema string `json:"schema"`
	// SubtaskID echoes the id of the subtask being verified; a mismatch is a
	// contract violation (the answer is not about this subtask).
	SubtaskID string          `json:"subtask_id"`
	Status    string          `json:"status"` // done | failed
	Criteria  []StepCriterion `json:"criteria"`
}

// StepCriterion is one acceptance criterion with the verifier's verdict.
type StepCriterion struct {
	Text     string `json:"text"`
	Met      bool   `json:"met"`
	Evidence string `json:"evidence"`
}

// Validate checks the schema and normalises the criteria.
func (s *StepResult) Validate() error {
	if s.Schema != SchemaStepResult {
		return fmt.Errorf("schema: ожидается %q, получено %q", SchemaStepResult, s.Schema)
	}
	if strings.TrimSpace(s.SubtaskID) == "" {
		return fmt.Errorf("subtask_id обязателен")
	}
	if len(s.SubtaskID) > MaxFieldBytes {
		return fmt.Errorf("subtask_id длиннее %d байт", MaxFieldBytes)
	}
	if s.Status != StepStatusDone && s.Status != StepStatusFailed {
		return fmt.Errorf("status: ожидается done или failed, получено %q", s.Status)
	}
	if s.Criteria == nil {
		return fmt.Errorf("criteria обязателен (пустой список — []")
	}
	if len(s.Criteria) < 1 || len(s.Criteria) > MaxStepCriteria {
		return fmt.Errorf("criteria: число критериев %d вне диапазона [1, %d]", len(s.Criteria), MaxStepCriteria)
	}
	for i := range s.Criteria {
		if err := s.Criteria[i].validate(); err != nil {
			return fmt.Errorf("criteria[%d]: %w", i, err)
		}
	}
	return nil
}

func (c *StepCriterion) validate() error {
	if strings.TrimSpace(c.Text) == "" {
		return fmt.Errorf("text обязателен")
	}
	for _, fl := range [...]struct{ name, v string }{{"text", c.Text}, {"evidence", c.Evidence}} {
		if len(fl.v) > MaxFieldBytes {
			return fmt.Errorf("%s длиннее %d байт", fl.name, MaxFieldBytes)
		}
		if !utf8.ValidString(fl.v) {
			return fmt.Errorf("%s: не UTF-8", fl.name)
		}
	}
	c.Evidence = cutRunes(c.Evidence, MaxStepEvidenceRunes)
	return nil
}

// Unmet returns the texts of the criteria that are not met.
func (s StepResult) Unmet() []string {
	var out []string
	for _, c := range s.Criteria {
		if !c.Met {
			out = append(out, c.Text)
		}
	}
	return out
}
