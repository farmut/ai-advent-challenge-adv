//go:build e2e

package proxy

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// TestProxyCases runs every case of testdata/cases.json in mask and block
// mode and checks the guard decision, upstream traffic and the audit log.
func TestProxyCases(t *testing.T) {
	for _, mode := range []string{"mask", "block"} {
		h := newHarness(t, mode, mode, nil)
		st.mu.Lock()
		st.auditByMode[mode] = h.auditPath
		st.mu.Unlock()
		for _, c := range st.cases {
			if c.Mode != "" && c.Mode != mode {
				continue
			}
			t.Run(mode+"/"+c.ID, func(t *testing.T) {
				r := runCase(t, h, c, mode)
				st.addResult(r)
				switch r.Verdict {
				case VerdictFail:
					t.Errorf("%s %s: expect %s, fact %s; problems: %s", mode, c.ID, r.Expect, r.Fact, strings.Join(r.Problems, "; "))
				case VerdictUnexpectedCatch:
					t.Logf("%s %s: known gap unexpectedly caught (%s)", mode, c.ID, r.Fact)
				case VerdictNA:
					t.Skip(r.Fact)
				}
			})
		}
	}
}

func runCase(t *testing.T, h *harness, c Case, mode string) Result {
	res := Result{ID: c.ID, Title: c.Title, Category: c.Category, Mode: mode, Expect: c.ExpectFor(mode), Notes: c.Notes}
	if st.realModel() && c.Category == "output" {
		res.Fact, res.Verdict = "n/a (требует fake)", VerdictNA
		return res
	}
	if st.fake != nil {
		usage := c.UpstreamUsage
		if usage == nil {
			usage = &Usage{Prompt: 40, Completion: 20}
		}
		st.fake.Script(c.UpstreamReply, usage)
	}
	before := st.rec.Count()
	rid := "e2e-" + mode + "-" + c.ID
	r := h.chat(t, localModel, c.Messages, map[string]string{"X-Request-ID": rid})
	bodies := st.rec.Since(before)
	res.UpstreamCalls = len(bodies)
	res.Status = r.status

	in, out := r.header.Get("X-Guard-Input"), r.header.Get("X-Guard-Output")
	res.Fact = Fact(r.status, r.code, in, out)
	res.InputFact = InputFact(r.status, r.code, in)
	judged := res.Fact
	if st.realModel() {
		judged = res.InputFact
		res.Fact = res.InputFact + " (output: " + out + ")"
	}
	found := ParseFindings(r.header.Get("X-Guard-Input-Findings"), r.header.Get("X-Guard-Output-Findings"))
	res.Findings = findingsString(found)
	res.Verdict = Verdict(res.Expect, judged)

	var problems []string
	if res.InputFact == FactBlocked && len(bodies) > 0 {
		problems = append(problems, "upstream вызван при block")
	}
	if c.Category == "input" && res.InputFact != FactBlocked {
		for _, b := range bodies {
			for _, s := range c.Secrets {
				if strings.Contains(b, s) {
					problems = append(problems, "секрет ушёл в upstream")
				}
			}
			if res.InputFact == FactMasked && c.Placeholder != "" && !strings.Contains(b, c.Placeholder) {
				problems = append(problems, "нет плейсхолдера "+c.Placeholder+" в теле к upstream")
			}
		}
	}
	if res.Expect != ExpectKnownGap && len(c.ExpectFindings) > 0 && judged == res.Expect {
		for _, f := range c.ExpectFindings {
			if found[f] == 0 {
				problems = append(problems, "нет находки "+f)
			}
		}
	}
	if judged == FactOutputRedacted || judged == FactOutputBlocked {
		for _, s := range c.Secrets {
			if strings.Contains(r.content, s) {
				problems = append(problems, "секрет в ответе клиенту")
			}
		}
	}
	if judged == FactOutputRedacted && c.Placeholder != "" && !strings.Contains(r.content, c.Placeholder) {
		problems = append(problems, "нет плейсхолдера в ответе")
	}
	if judged == FactOutputBlocked && (r.content != h.cfg.Guard.Output.RefusalText || r.finish != "content_filter") {
		problems = append(problems, fmt.Sprintf("ожидался отказ с content_filter, получено finish=%s", r.finish))
	}
	line := lastAuditLine(t, h.auditPath)
	if !strings.Contains(line, `"request_id":"`+rid+`"`) {
		problems = append(problems, "нет записи аудита")
	}
	for _, s := range c.Secrets {
		for _, e := range Encodings(s) {
			if strings.Contains(line, e) {
				problems = append(problems, "секрет в аудите")
			}
		}
	}
	res.Problems = problems
	if len(problems) > 0 && res.Verdict == VerdictPass {
		res.Verdict = VerdictFail
	}
	return res
}

func findingsString(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}
