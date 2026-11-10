// Package proxy is the e2e suite of the Day13 guard proxy (build tag e2e for
// the tests). Helpers in untagged files are unit-tested without the tag.
package proxy

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"agile-consultant/internal/ports"
	"agile-consultant/internal/testkit/fakeupstream"
)

// Facts and expectations.
const (
	FactPass           = "pass"
	FactMasked         = "masked"
	FactBlocked        = "blocked"
	FactOutputRedacted = "output_redacted"
	FactOutputBlocked  = "output_blocked"
	ExpectKnownGap     = "known_gap"
)

// Verdicts.
const (
	VerdictPass            = "PASS"
	VerdictFail            = "FAIL"
	VerdictKnownGap        = "KNOWN_GAP"
	VerdictUnexpectedCatch = "UNEXPECTED_CATCH"
	VerdictNA              = "N/A"
)

// Usage is a scripted upstream usage (alias of testkit/fakeupstream.Usage).
type Usage = fakeupstream.Usage

// Case is one e2e case from testdata/cases.json.
type Case struct {
	ID             string                       `json:"id"`
	Title          string                       `json:"title"`
	Category       string                       `json:"category"` // input|output
	Messages       []ports.ChatMessage          `json:"messages"`
	UpstreamReply  string                       `json:"upstream_reply"`
	UpstreamUsage  *Usage                       `json:"upstream_usage"`
	Mode           string                       `json:"mode"` // "" = both modes
	Expect         struct{ Mask, Block string } `json:"expect"`
	ExpectFindings []string                     `json:"expect_findings"`
	Secrets        []string                     `json:"secrets"`
	Placeholder    string                       `json:"placeholder"`
	Notes          string                       `json:"notes"`
}

// ExpectFor returns the expectation for mode ("mask"|"block").
func (c Case) ExpectFor(mode string) string {
	if mode == "block" {
		return c.Expect.Block
	}
	return c.Expect.Mask
}

// LoadCases reads and validates cases.
func LoadCases(path string) ([]Case, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cs []Case
	if err := json.Unmarshal(b, &cs); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	seen := map[string]bool{}
	valid := map[string]bool{FactPass: true, FactMasked: true, FactBlocked: true, FactOutputRedacted: true, FactOutputBlocked: true, ExpectKnownGap: true}
	for _, c := range cs {
		switch {
		case c.ID == "" || seen[c.ID]:
			return nil, fmt.Errorf("case id %q empty or duplicated", c.ID)
		case c.Category != "input" && c.Category != "output":
			return nil, fmt.Errorf("%s: bad category %q", c.ID, c.Category)
		case len(c.Messages) == 0:
			return nil, fmt.Errorf("%s: no messages", c.ID)
		case !valid[c.Expect.Mask] || !valid[c.Expect.Block]:
			return nil, fmt.Errorf("%s: bad expect %+v", c.ID, c.Expect)
		case c.Mode != "" && c.Mode != "mask" && c.Mode != "block":
			return nil, fmt.Errorf("%s: bad mode %q", c.ID, c.Mode)
		}
		seen[c.ID] = true
	}
	return cs, nil
}

// Fact derives the observed outcome from the response (plan section 8).
func Fact(status int, code, guardIn, guardOut string) string {
	if status == 400 && code == "guard_input_blocked" {
		return FactBlocked
	}
	if status != 200 {
		return fmt.Sprintf("error:%d:%s", status, code)
	}
	switch guardOut {
	case "blocked":
		return FactOutputBlocked
	case "redacted":
		return FactOutputRedacted
	}
	if guardIn == "masked" {
		return FactMasked
	}
	return FactPass
}

// InputFact ignores the output guard (used for input cases on a real model,
// whose answers are not deterministic).
func InputFact(status int, code, guardIn string) string {
	if status == 400 && code == "guard_input_blocked" {
		return FactBlocked
	}
	if status != 200 {
		return fmt.Sprintf("error:%d:%s", status, code)
	}
	if guardIn == "masked" {
		return FactMasked
	}
	return FactPass
}

// Verdict compares expectation and fact.
func Verdict(expect, fact string) string {
	if expect == ExpectKnownGap {
		if fact == FactPass {
			return VerdictKnownGap
		}
		return VerdictUnexpectedCatch
	}
	if expect == fact {
		return VerdictPass
	}
	return VerdictFail
}

// Encodings returns s and its base64/hex forms; none may appear in audits.
func Encodings(s string) []string {
	b := []byte(s)
	out := []string{s, base64.StdEncoding.EncodeToString(b), base64.RawStdEncoding.EncodeToString(b),
		base64.URLEncoding.EncodeToString(b), base64.RawURLEncoding.EncodeToString(b), hex.EncodeToString(b), strings.ToUpper(hex.EncodeToString(b))}
	sort.Strings(out)
	uniq := out[:0]
	for i, v := range out {
		if i == 0 || v != out[i-1] {
			uniq = append(uniq, v)
		}
	}
	return uniq
}

// ParseFindings parses "type=count,..." headers into a set of types.
func ParseFindings(headers ...string) map[string]int {
	out := map[string]int{}
	for _, h := range headers {
		for _, p := range strings.Split(h, ",") {
			k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
			if !ok || k == "" {
				continue
			}
			var n int
			_, _ = fmt.Sscanf(v, "%d", &n)
			out[k] += n
		}
	}
	return out
}
