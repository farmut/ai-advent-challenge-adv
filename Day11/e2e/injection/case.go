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
	"unicode/utf8"
)

// ExpectedCases is the number of injection cases the suite must contain.
const ExpectedCases = 5

// MaxPayloadRunes bounds a payload so cases stay realistic chat inputs.
const MaxPayloadRunes = 480

// ErrInvalidCases is returned when the case file fails validation.
var ErrInvalidCases = errors.New("invalid injection cases")

// Detectors describes how a successful attack is recognised in the answer.
type Detectors struct {
	Regex      []string `json:"regex,omitempty"`
	PromptLeak bool     `json:"prompt_leak,omitempty"`
}

// Case is a single prompt-injection scenario.
type Case struct {
	ID         string    `json:"id"`
	Title      string    `json:"title"`
	OWASP      string    `json:"owasp"`
	Technique  string    `json:"technique"`
	Payload    string    `json:"payload"`
	Impact     string    `json:"impact"`
	WhyWorks   string    `json:"why_works"`
	Mitigation string    `json:"mitigation"`
	Detectors  Detectors `json:"detectors"`
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

// ValidateCases checks count, id uniqueness, payload size and detectors.
func ValidateCases(cases []Case) error {
	if len(cases) != ExpectedCases {
		return fmt.Errorf("%w: want %d cases, got %d", ErrInvalidCases, ExpectedCases, len(cases))
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
		if c.Payload == "" {
			return fmt.Errorf("%w: %s: empty payload", ErrInvalidCases, c.ID)
		}
		if n := utf8.RuneCountInString(c.Payload); n > MaxPayloadRunes {
			return fmt.Errorf("%w: %s: payload %d runes > %d", ErrInvalidCases, c.ID, n, MaxPayloadRunes)
		}
		if len(c.Detectors.Regex) == 0 && !c.Detectors.PromptLeak {
			return fmt.Errorf("%w: %s: no detectors", ErrInvalidCases, c.ID)
		}
		for _, re := range c.Detectors.Regex {
			if _, err := regexp.Compile(re); err != nil {
				return fmt.Errorf("%w: %s: regex %q: %v", ErrInvalidCases, c.ID, re, err)
			}
		}
	}
	return nil
}
