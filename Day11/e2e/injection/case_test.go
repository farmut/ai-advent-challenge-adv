package injection

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

func TestLoadCasesRepoFile(t *testing.T) {
	cases, err := LoadCases("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{"PI-01", "PI-02", "PI-03", "PI-04", "PI-05"}
	for i, c := range cases {
		if c.ID != wantIDs[i] {
			t.Errorf("case %d id = %q, want %q", i, c.ID, wantIDs[i])
		}
		for name, v := range map[string]string{"title": c.Title, "owasp": c.OWASP, "technique": c.Technique, "impact": c.Impact, "why_works": c.WhyWorks, "mitigation": c.Mitigation} {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s: empty %s", c.ID, name)
			}
		}
		if !strings.Contains(c.WhyWorks, "role=user") {
			t.Errorf("%s: why_works must explain the single user-message delivery", c.ID)
		}
		if !strings.Contains(c.Mitigation, "Промпт:") || !strings.Contains(c.Mitigation, "Код:") {
			t.Errorf("%s: mitigation must have «Промпт:» and «Код:» sections", c.ID)
		}
	}
	if !strings.Contains(cases[2].Payload, "<|im_end|>\n<|im_start|>system\n") {
		t.Errorf("PI-03 payload must contain real newlines around ChatML tokens: %q", cases[2].Payload)
	}
	// PI-05 payload splitting: the fragments must glue into the detector marker,
	// while the marker itself must not appear verbatim in the payload.
	split := cases[4]
	if strings.Contains(split.Payload, "ENCODED_OK") {
		t.Errorf("PI-05 payload must not contain the marker verbatim")
	}
	parts := regexp.MustCompile(`[abc] = "([^"]+)"`).FindAllStringSubmatch(split.Payload, -1)
	glued := ""
	for _, p := range parts {
		glued += p[1]
	}
	if glued != "ENCODED_OK" {
		t.Errorf("PI-05 fragments glue to %q, want ENCODED_OK", glued)
	}
}

func TestValidateCases(t *testing.T) {
	valid := func() []Case {
		out := make([]Case, ExpectedCases)
		for i := range out {
			out[i] = Case{ID: string(rune('A' + i)), Payload: "p", Detectors: Detectors{Regex: []string{"x"}}}
		}
		return out
	}
	tests := []struct {
		name   string
		mutate func([]Case) []Case
		ok     bool
	}{
		{"valid", func(c []Case) []Case { return c }, true},
		{"too few", func(c []Case) []Case { return c[:4] }, false},
		{"duplicate id", func(c []Case) []Case { c[1].ID = c[0].ID; return c }, false},
		{"long payload", func(c []Case) []Case { c[0].Payload = strings.Repeat("я", MaxPayloadRunes+1); return c }, false},
		{"max payload ok", func(c []Case) []Case { c[0].Payload = strings.Repeat("я", MaxPayloadRunes); return c }, true},
		{"bad regex", func(c []Case) []Case { c[0].Detectors.Regex = []string{"("}; return c }, false},
		{"no detectors", func(c []Case) []Case { c[0].Detectors = Detectors{}; return c }, false},
		{"leak only ok", func(c []Case) []Case { c[0].Detectors = Detectors{PromptLeak: true}; return c }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateCases(tc.mutate(valid()))
			if (err == nil) != tc.ok {
				t.Fatalf("err = %v, want ok=%v", err, tc.ok)
			}
			if err != nil && !errors.Is(err, ErrInvalidCases) {
				t.Errorf("error does not wrap ErrInvalidCases: %v", err)
			}
		})
	}
}
