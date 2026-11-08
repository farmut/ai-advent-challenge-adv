//go:build e2e

package proxy

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"agile-consultant/internal/config"
	"agile-consultant/internal/ports"
)

func userMsg(s string) []ports.ChatMessage { return []ports.ChatMessage{{Role: "user", Content: s}} }

func caseByID(t *testing.T, id string) Case {
	t.Helper()
	for _, c := range st.cases {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("case %s not found", id)
	return Case{}
}

func TestRateLimit429(t *testing.T) {
	extra(t, "TestRateLimit429", true, func(t *testing.T) string {
		h := newHarness(t, "mask", "extra/ratelimit", func(c *config.Proxy) { c.RateLimit.PerMinute = 3 })
		for i := 0; i < 3; i++ {
			if r := h.chat(t, localModel, userMsg("привет"), nil); r.status != 200 {
				t.Fatalf("request %d: %d", i+1, r.status)
			}
			h.clk.Advance(time.Second)
		}
		before := st.rec.Count()
		r := h.chat(t, localModel, userMsg("ключ AKIA2E0A8F3B244C9986"), nil)
		ra, _ := strconv.Atoi(r.header.Get("Retry-After"))
		if r.status != 429 || r.code != "rate_limit_exceeded" || ra <= 0 || r.header.Get("X-RateLimit-Remaining") != "0" {
			t.Fatalf("4th: status %d code %s Retry-After %q", r.status, r.code, r.header.Get("Retry-After"))
		}
		if st.rec.Count() != before {
			t.Fatal("rate-limited request reached upstream")
		}
		h.clk.Advance(61 * time.Second)
		if r2 := h.chat(t, localModel, userMsg("привет"), nil); r2.status != 200 {
			t.Fatalf("after window: %d", r2.status)
		}
		recs, raw := h.auditRecords(t)
		limited := 0
		for _, rec := range recs {
			if rec.Status == ports.AuditStatusRateLimited {
				limited++
				if len(rec.PromptMasked) != 0 || rec.HTTPStatus != 429 {
					t.Fatalf("rate_limited record must have empty prompt: %+v", rec)
				}
			}
		}
		if limited != 1 || strings.Contains(raw, "AKIA2E0A8F3B244C9986") {
			t.Fatalf("rate_limited records = %d", limited)
		}
		return fmt.Sprintf("limit=3: 4-й → 429, Retry-After=%ds; после +61s → 200; в аудите rate_limited=1 с пустым prompt", ra)
	})
}

func TestStreamRejected(t *testing.T) {
	extra(t, "TestStreamRejected", false, func(t *testing.T) string {
		h := newHarness(t, "mask", "extra/stream", nil)
		before := st.rec.Count()
		r := h.post(t, map[string]any{"model": localModel, "stream": true, "messages": userMsg("привет")}, nil)
		if r.status != 400 || r.code != "unsupported_parameter" || r.param != "stream" || st.rec.Count() != before {
			t.Fatalf("status %d code %s param %s", r.status, r.code, r.param)
		}
		return "stream:true → 400 unsupported_parameter (param=stream), upstream не вызван"
	})
}

func TestUnknownModel404(t *testing.T) {
	extra(t, "TestUnknownModel404", false, func(t *testing.T) string {
		h := newHarness(t, "mask", "extra/model", nil)
		before := st.rec.Count()
		r := h.chat(t, "gpt-9000", userMsg("привет"), nil)
		if r.status != 404 || r.code != "model_not_found" || r.param != "model" || st.rec.Count() != before {
			t.Fatalf("status %d code %s", r.status, r.code)
		}
		return "model=gpt-9000 → 404 model_not_found, upstream не вызван"
	})
}

func TestBlockedNotForwarded(t *testing.T) {
	extra(t, "TestBlockedNotForwarded", false, func(t *testing.T) string {
		tests := []struct{ mode, id string }{{"block", "IN-02"}, {"mask", "IN-04"}, {"mask", "IN-05"}, {"mask", "IN-11"}}
		var got []string
		for _, tt := range tests {
			h := newHarness(t, tt.mode, "extra/blocked-"+tt.mode+"-"+tt.id, nil)
			before := st.rec.Count()
			r := h.chat(t, localModel, caseByID(t, tt.id).Messages, nil)
			if r.status != 400 || r.code != "guard_input_blocked" || r.header.Get("X-Guard-Warning") != "secrets_detected" ||
				!strings.Contains(string(r.body), `"warning":"secrets_detected"`) || !strings.Contains(string(r.body), "Nothing was sent to the model") {
				t.Fatalf("%s/%s: status %d body %s", tt.mode, tt.id, r.status, r.body)
			}
			if n := st.rec.Count() - before; n != 0 {
				t.Fatalf("%s/%s: upstream received %d requests", tt.mode, tt.id, n)
			}
			got = append(got, tt.mode+"/"+tt.id)
		}
		return "400 guard_input_blocked + warning, 0 запросов к upstream: " + strings.Join(got, ", ")
	})
}

func TestAuditNoRawSecrets(t *testing.T) {
	extra(t, "TestAuditNoRawSecrets", false, func(t *testing.T) string {
		var secrets []string
		for _, c := range st.cases {
			secrets = append(secrets, c.Secrets...)
		}
		var files, logs []string
		for _, mode := range []string{"mask", "block"} {
			h := newHarness(t, mode, "extra/audit-"+mode, nil)
			for _, c := range st.cases {
				if st.realModel() && c.Category == "output" {
					continue
				}
				if st.fake != nil {
					st.fake.Script(c.UpstreamReply, &Usage{Prompt: 40, Completion: 20})
				}
				h.chat(t, localModel, c.Messages, nil)
			}
			files = append(files, h.auditPath)
			logs = append(logs, filepath.Join(filepath.Dir(h.auditPath), "proxy.log"))
		}
		st.mu.Lock()
		for _, p := range st.auditByMode {
			files = append(files, p)
		}
		st.mu.Unlock()
		checked := 0
		for _, f := range files {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			checked += strings.Count(string(data), "\n")
			for _, s := range secrets {
				for _, e := range Encodings(s) {
					if strings.Contains(string(data), e) {
						t.Errorf("%s contains a secret form of length %d", filepath.Base(filepath.Dir(f)), len(e))
					}
				}
			}
		}
		// Proxy keys and the pepper must not appear in the audit, slog or the
		// HTML report (a snapshot rendered from the current results).
		html, err := RenderRunHTML(Meta{SchemaVersion: ReportSchemaVersion, Upstream: st.upstream, OutDir: st.outDir}, st.cases, nil, nil, files)
		if err != nil {
			t.Fatal(err)
		}
		texts := map[string]string{"report.html": html}
		for _, f := range append(append([]string(nil), files...), logs...) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			texts[f] = string(data)
		}
		for where, text := range texts {
			for name, v := range forbiddenEverywhere() {
				if strings.Contains(text, v) {
					t.Errorf("%s contains %s", filepath.Base(where), name)
				}
			}
		}
		for _, p := range HTMLProblems(html, secrets) {
			t.Errorf("report.html: %s", p)
		}
		return fmt.Sprintf("%d записей в %d файлах, %d секретов × raw/base64/hex — не найдено; ключи прокси и AUDIT_PEPPER не найдены в audit, %d proxy.log и report.html", checked, len(files), len(secrets), len(logs))
	})
}

func TestAuditFilePerms(t *testing.T) {
	extra(t, "TestAuditFilePerms", false, func(t *testing.T) string {
		dir := filepath.Join(st.outDir, "extra", "perms")
		_ = os.RemoveAll(dir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		h := newHarness(t, "mask", "extra/perms", nil)
		h.chat(t, localModel, userMsg("привет"), nil)
		for _, tt := range []struct {
			path string
			mode os.FileMode
		}{{filepath.Dir(h.auditPath), 0o700}, {h.auditPath, 0o600}} {
			fi, err := os.Stat(tt.path)
			if err != nil {
				t.Fatal(err)
			}
			if fi.Mode().Perm() != tt.mode {
				t.Fatalf("%s: %v, want %v", tt.path, fi.Mode().Perm(), tt.mode)
			}
		}
		return "каталог аудита 0700 (даже если создан 0755), файл 0600"
	})
}

func TestCostTracking(t *testing.T) {
	extra(t, "TestCostTracking", true, func(t *testing.T) string {
		h := newHarness(t, "mask", "extra/cost", nil)
		st.fake.Script("Ответ.", &Usage{Prompt: 100, Completion: 50})
		r := h.chat(t, paidModel, userMsg("привет"), nil)
		if r.status != 200 || r.header.Get("X-Guard-Cost-USD") != "0.000120" || r.header.Get("X-Guard-Usage-Source") != "upstream" {
			t.Fatalf("paid: %d cost %q source %q", r.status, r.header.Get("X-Guard-Cost-USD"), r.header.Get("X-Guard-Usage-Source"))
		}
		st.fake.Script("Ответ без usage.", nil)
		r2 := h.chat(t, paidModel, userMsg("12345678"), nil)
		if r2.header.Get("X-Guard-Usage-Source") != "estimated" || r2.header.Get("X-Guard-Cost-USD") == "0.000000" {
			t.Fatalf("estimated: source %q cost %q", r2.header.Get("X-Guard-Usage-Source"), r2.header.Get("X-Guard-Cost-USD"))
		}
		st.fake.Script("", &Usage{Prompt: 100, Completion: 50})
		r3 := h.chat(t, localModel, userMsg("привет"), nil)
		if r3.header.Get("X-Guard-Cost-USD") != "0.000000" {
			t.Fatalf("local cost %q", r3.header.Get("X-Guard-Cost-USD"))
		}
		recs, _ := h.auditRecords(t)
		if len(recs) != 3 || recs[0].CostMicroUSD != 120 || recs[0].PriceRef != "e2e-paid" || recs[0].Usage.TotalTokens != 150 ||
			recs[1].Usage.Source != "estimated" || recs[2].CostMicroUSD != 0 || recs[2].PriceRef != "local" {
			t.Fatalf("audit %+v", recs)
		}
		return fmt.Sprintf("paid 100/50 → $0.000120 (upstream); без usage → estimated %d/%d токенов, $%s; local → $0", recs[1].Usage.PromptTokens, recs[1].Usage.CompletionTokens, r2.header.Get("X-Guard-Cost-USD"))
	})
}

func TestXForwardedForIgnored(t *testing.T) {
	extra(t, "TestXForwardedForIgnored", true, func(t *testing.T) string {
		h := newHarness(t, "mask", "extra/xff", func(c *config.Proxy) { c.RateLimit.PerMinute = 1 })
		r1 := h.chat(t, localModel, userMsg("привет"), map[string]string{"X-Forwarded-For": "198.51.100.1", "X-Real-IP": "198.51.100.1"})
		r2 := h.chat(t, localModel, userMsg("привет"), map[string]string{"X-Forwarded-For": "198.51.100.2", "X-Real-IP": "198.51.100.2"})
		if r1.status != 200 || r2.status != 429 {
			t.Fatalf("statuses %d %d: X-Forwarded-For must not change the rate-limit key", r1.status, r2.status)
		}
		recs, _ := h.auditRecords(t)
		if len(recs) != 2 || recs[0].ClientIPHash != recs[1].ClientIPHash || recs[0].ClientIPHash == "" {
			t.Fatalf("client_ip_hash must be the TCP peer: %+v", recs)
		}
		return "разные X-Forwarded-For/X-Real-IP при limit=1 → второй 429; client_ip_hash одинаковый (TCP peer)"
	})
}
