package proxy

import (
	"strings"
	"testing"
)

func TestLoadCases(t *testing.T) {
	cs, err := LoadCases("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) < 10 {
		t.Fatalf("need >= 10 cases, got %d", len(cs))
	}
	required := map[string]bool{"IN-01": false, "IN-02": false, "IN-03": false, "IN-04": false, "IN-05": false}
	for _, c := range cs {
		if _, ok := required[c.ID]; ok {
			required[c.ID] = true
		}
	}
	for id, ok := range required {
		if !ok {
			t.Fatalf("required case %s missing", id)
		}
	}
}

func TestFactAndVerdict(t *testing.T) {
	tests := []struct {
		status          int
		code, in, out   string
		fact, input     string
		expect, verdict string
	}{
		{400, "guard_input_blocked", "blocked", "", FactBlocked, FactBlocked, FactBlocked, VerdictPass},
		{200, "", "masked", "pass", FactMasked, FactMasked, FactMasked, VerdictPass},
		{200, "", "masked", "redacted", FactOutputRedacted, FactMasked, FactMasked, VerdictFail},
		{200, "", "pass", "blocked", FactOutputBlocked, FactPass, FactOutputBlocked, VerdictPass},
		{200, "", "pass", "pass", FactPass, FactPass, ExpectKnownGap, VerdictKnownGap},
		{200, "", "masked", "pass", FactMasked, FactMasked, ExpectKnownGap, VerdictUnexpectedCatch},
		{502, "upstream_error", "", "", "error:502:upstream_error", "error:502:upstream_error", FactPass, VerdictFail},
	}
	for _, tt := range tests {
		f := Fact(tt.status, tt.code, tt.in, tt.out)
		if f != tt.fact || InputFact(tt.status, tt.code, tt.in) != tt.input || Verdict(tt.expect, f) != tt.verdict {
			t.Fatalf("%+v: fact %s verdict %s", tt, f, Verdict(tt.expect, f))
		}
	}
}

func TestEncodingsAndFindings(t *testing.T) {
	enc := strings.Join(Encodings("AKIA"), " ")
	for _, want := range []string{"AKIA", "QUtJQQ==", "414b4941", "414B4941"} {
		if !strings.Contains(enc, want) {
			t.Fatalf("encodings lack %s: %s", want, enc)
		}
	}
	got := ParseFindings("email=1,phone=2", "", "email=1")
	if got["email"] != 2 || got["phone"] != 2 {
		t.Fatalf("got %v", got)
	}
}

func TestSummarize(t *testing.T) {
	s := Summarize([]Result{{Verdict: VerdictPass}, {Verdict: VerdictFail}, {Verdict: VerdictKnownGap}, {Verdict: VerdictNA}},
		[]ExtraResult{{Passed: true}, {Passed: false}, {Skipped: true}})
	if s.Total != 4 || s.Pass != 1 || s.Fail != 1 || s.KnownGap != 1 || s.NA != 1 || s.ExtraPassed != 1 || s.ExtraFailed != 1 {
		t.Fatalf("summary %+v", s)
	}
}
