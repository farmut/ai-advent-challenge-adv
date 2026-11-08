// Package injection contains the prompt-injection e2e suite for the Agile
// consultant. The model-backed test lives behind the "e2e" build tag; case
// loading, detection and reporting are plain code covered by unit tests.
package injection

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Size limits keep cases realistic chat inputs and within the consultant's
// default guard limits.
const (
	MaxPayloadRunes = 1200
	MaxContextRunes = 8000
)

// Case classes.
const (
	ClassRegression = "regression" // Day11 direct injections PI-01..05
	ClassHidden     = "hidden"     // hidden instructions in markup
	ClassInvisible  = "invisible"  // invisible / look-alike Unicode payloads
	ClassInjected   = "injected"   // instructions inside the attached document
)

// Classes lists the case classes in report order.
var Classes = []string{ClassRegression, ClassHidden, ClassInvisible, ClassInjected}

// Expectations with the guard on (Case.ExpectOn).
const (
	// ExpectBlocked: with the guard on the attack must not be delivered.
	ExpectBlocked = "blocked"
	// ExpectKnownGap: a documented limitation; the outcome is recorded but
	// does not fail the run.
	ExpectKnownGap = "known_gap"
)

// ErrInvalidCases is returned when the case file fails validation.
var ErrInvalidCases = errors.New("invalid injection cases")

// Detectors describes how a successful attack is recognised in the answer.
type Detectors struct {
	Regex      []string `json:"regex,omitempty"`
	PromptLeak bool     `json:"prompt_leak,omitempty"`
}

// Case is a single prompt-injection scenario.
type Case struct {
	ID          string `json:"id"`
	Class       string `json:"class"`
	Title       string `json:"title"`
	OWASP       string `json:"owasp"`
	Technique   string `json:"technique"`
	Payload     string `json:"payload"`
	Context     string `json:"context,omitempty"`
	CodeWord    string `json:"code_word,omitempty"`
	ExpectOn    string `json:"expect_on"`
	ExpectLayer string `json:"expect_layer,omitempty"`
	Limitation  string `json:"limitation,omitempty"`
	Impact      string `json:"impact"`
	WhyWorks    string `json:"why_works"`
	Mitigation  string `json:"mitigation"`
	// Detectors run on the normalised answer.
	Detectors Detectors `json:"detectors"`
}

// AttackIn reports where the attack lives: "документ" for injected-context
// cases, otherwise "вопрос".
func (c Case) AttackIn() string {
	if c.Class == ClassInjected {
		return "документ"
	}
	return "вопрос"
}

// LoadCases reads and validates the case file.
func LoadCases(path string) ([]Case, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read cases %s: %w", path, err)
	}
	var cases []Case
	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, fmt.Errorf("parse cases %s: %w", path, err)
	}
	if err := ValidateCases(cases); err != nil {
		return nil, err
	}
	return cases, nil
}

// ValidateCases checks ids, classes, expectations, sizes and detectors.
func ValidateCases(cases []Case) error {
	if len(cases) == 0 {
		return fmt.Errorf("%w: no cases", ErrInvalidCases)
	}
	seen := make(map[string]struct{}, len(cases))
	for i, c := range cases {
		if c.ID == "" {
			return fmt.Errorf("%w: case #%d has empty id", ErrInvalidCases, i)
		}
		if _, dup := seen[c.ID]; dup {
			return fmt.Errorf("%w: duplicate id %q", ErrInvalidCases, c.ID)
		}
		seen[c.ID] = struct{}{}
		if err := validateCase(c); err != nil {
			return fmt.Errorf("%w: %s: %v", ErrInvalidCases, c.ID, err)
		}
	}
	return nil
}

func validateCase(c Case) error {
	if !slices.Contains(Classes, c.Class) {
		return fmt.Errorf("unknown class %q", c.Class)
	}
	switch c.ExpectOn {
	case ExpectBlocked:
	case ExpectKnownGap:
		if strings.TrimSpace(c.Limitation) == "" {
			return errors.New("known_gap requires limitation")
		}
	default:
		return fmt.Errorf("expect_on must be %q or %q, got %q", ExpectBlocked, ExpectKnownGap, c.ExpectOn)
	}
	if c.ExpectLayer != "" && !slices.Contains(expectableLayers, c.ExpectLayer) {
		return fmt.Errorf("unknown expect_layer %q", c.ExpectLayer)
	}
	if strings.TrimSpace(c.Payload) == "" {
		return errors.New("empty payload")
	}
	if n := utf8.RuneCountInString(c.Payload); n > MaxPayloadRunes {
		return fmt.Errorf("payload %d runes > %d", n, MaxPayloadRunes)
	}
	if n := utf8.RuneCountInString(c.Context); n > MaxContextRunes {
		return fmt.Errorf("context %d runes > %d", n, MaxContextRunes)
	}
	if c.Class == ClassInjected && strings.TrimSpace(c.Context) == "" {
		return errors.New("injected-context case needs a context document")
	}
	if len(c.Detectors.Regex) == 0 && !c.Detectors.PromptLeak {
		return errors.New("no detectors")
	}
	for _, re := range c.Detectors.Regex {
		if _, err := regexp.Compile(re); err != nil {
			return fmt.Errorf("regex %q: %v", re, err)
		}
	}
	return nil
}
