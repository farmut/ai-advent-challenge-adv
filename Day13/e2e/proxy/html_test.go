package proxy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"agile-consultant/internal/config"
	"agile-consultant/internal/ports"
)

func TestHighlightPlaceholdersXSS(t *testing.T) {
	tests := []struct {
		name, in, want string
		absent         []string
	}{
		{"placeholder", `key [REDACTED_API_KEY] ok`, `key <mark class="ph">[REDACTED_API_KEY]</mark> ok`, nil},
		{"script", `<script>alert(1)</script>[REDACTED_EMAIL]`, `&lt;script&gt;alert(1)&lt;/script&gt;<mark class="ph">[REDACTED_EMAIL]</mark>`, []string{"<script"}},
		{"attr break", `"><img src=x onerror=alert(1)>`, `&#34;&gt;&lt;img src=x onerror=alert(1)&gt;`, []string{"<img"}},
		{"fake mark", `<mark class="ph">[REDACTED_X]</mark>`, `&lt;mark class=&#34;ph&#34;&gt;<mark class="ph">[REDACTED_X]</mark>&lt;/mark&gt;`, nil},
		{"lowercase not a placeholder", `[redacted_x] [REDACTED_]`, `[redacted_x] [REDACTED_]`, []string{"<mark"}},
		{"placeholder with html inside", `[REDACTED_<b>]`, `[REDACTED_&lt;b&gt;]`, []string{"<mark", "<b>"}},
		{"invisible", "a​b‮", "a[U+200B]b[U+202E]", nil},
		{"ampersand", `a&b`, `a&amp;b`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(HighlightPlaceholders(tt.in))
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
			for _, a := range tt.absent {
				if strings.Contains(got, a) {
					t.Fatalf("%q must not appear in %q", a, got)
				}
			}
		})
	}
}

func TestMaskText(t *testing.T) {
	tests := []struct {
		name, in string
		secrets  []string
		want     string
	}{
		{"case secret", "Base64 от sk-proj-abc123XYZ789", []string{"sk-proj-abc123XYZ789"}, "Base64 от [REDACTED]"},
		{"longest first", "sk-proj-abc123XYZ789 и sk-proj-abc123", []string{"sk-proj-abc123", "sk-proj-abc123XYZ789"}, "[REDACTED] и [REDACTED]"},
		{"split parts", `Секрет по частям "sk-" + "proj-abc"`, []string{"sk-proj-abc", "proj-abc"}, `Секрет по частям "sk-" + "[REDACTED]"`},
		{"key-like without secrets", "ключ sk-proj-QwErTy123456 и AKIA2E0A8F3B244C9986", nil, "ключ [REDACTED] и [REDACTED]"},
		{"allowlisted example", "AKIAIOSFODNN7EXAMPLE из allowlist", nil, "AKIAIOSFODNN7EXAMPLE из allowlist"},
		{"base64 form", "c2stcHJvai1hYmMxMjNYWVo3ODk=", []string{"sk-proj-abc123XYZ789"}, "[REDACTED]"},
		{"invisible", "a​b", nil, "a[U+200B]b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaskText(tt.in, tt.secrets); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHTMLProblems(t *testing.T) {
	secret := "sk-proj-abc123XYZ789"
	tests := []struct {
		name, doc string
		n         int
	}{
		{"clean", "<html>ok [REDACTED]</html>", 0},
		{"raw", "x " + secret, 1},
		{"base64", "x c2stcHJvai1hYmMxMjNYWVo3ODk=", 1},
		{"hex", "x 736b2d70726f6a2d6162633132335859", 0},
		{"script", "<SCRIPT>", 1},
		{"invisible", "a​b", 1},
		{"bidi", "a‮b", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HTMLProblems(tt.doc, []string{secret})
			if len(got) != tt.n {
				t.Fatalf("problems %v, want %d", got, tt.n)
			}
			for _, p := range got {
				if strings.Contains(p, secret) {
					t.Fatal("problem text leaks the secret")
				}
			}
		})
	}
}

func repoCases(t *testing.T) []Case {
	t.Helper()
	_, f, _, _ := runtime.Caller(0)
	cs, err := LoadCases(filepath.Join(filepath.Dir(f), "testdata", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

// sampleRuns builds a fake run (all outcomes) and a v1 mlx run (N/A output).
func sampleRuns(t *testing.T, cases []Case) map[string]*RunData {
	t.Helper()
	fake := &RunData{Dir: "/r/fake-1", Meta: Meta{SchemaVersion: 2, Upstream: "fake", Model: "fake", GoTestRun: "0", Started: time.Unix(0, 0)}}
	mlx := &RunData{Dir: "/r/mlx-1", Meta: Meta{Upstream: "mlx", Model: "/m/baseline-qwen", GoTestRun: "1"}, Stale: true, StaleReason: "схема v1", Failed: true}
	for _, c := range cases {
		for _, m := range modes {
			exp := c.ExpectFor(m)
			r := Result{ID: c.ID, Title: c.Title, Category: c.Category, Mode: m, Expect: exp, Fact: exp, InputFact: exp, Verdict: VerdictPass, Findings: "api_key_openai=1", UpstreamCalls: 1}
			if exp == ExpectKnownGap {
				r.Fact, r.InputFact, r.Verdict = FactPass, FactPass, VerdictKnownGap
			}
			if c.ID == "IN-09" && m == "block" {
				r.Fact, r.Verdict, r.Problems = FactBlocked, VerdictFail, []string{"секрет ушёл в upstream"}
			}
			fake.Results = append(fake.Results, r)
			mr := r
			if c.Category == "output" {
				mr.Fact, mr.InputFact, mr.Verdict = "n/a (требует fake)", "", VerdictNA
			}
			mlx.Results = append(mlx.Results, mr)
		}
	}
	fake.Extra = []ExtraResult{{Name: "AUTH-01", Category: CatAuth, Passed: true, Details: "401"}, {Name: "TestRateLimit429", Passed: true, Details: "429"},
		{Name: "TestCostTracking", Passed: true, Details: "paid 100/50 → $0.000120"}}
	mlx.Extra = []ExtraResult{{Name: "TestRateLimit429", Skipped: true, Details: "только fake upstream"}}
	fake.Records = []ports.AuditRecord{
		{V: 2, RequestID: "e2e-mask-IN-02", Status: "ok", Input: ports.AuditInput{Action: "masked", Findings: []ports.AuditFinding{{Type: "aws_access_key", View: "raw", Count: 1, Action: "mask", Fingerprints: []string{"0123456789ab"}, Placeholder: "[REDACTED_AWS_KEY]"}}},
			PromptMasked: []ports.ChatMessage{{Role: "user", Content: "ключ [REDACTED_AWS_KEY] <script>alert(1)</script>"}}, Output: ports.AuditOutput{Action: "pass"}},
		{V: 2, RequestID: "e2e-mask-IN-04", Status: "input_blocked", Input: ports.AuditInput{Action: "blocked", Findings: []ports.AuditFinding{{Type: "api_key_openai", View: "decoded", Derived: true, Count: 1, Action: "block"}}}},
		{V: 2, RequestID: "e2e-mask-OUT-03", Status: "output_blocked", ResponseMasked: "[SYSTEM_LEAK]", Output: ports.AuditOutput{Action: "blocked"},
			PromptMasked: []ports.ChatMessage{{Role: "system", Content: "[SYSTEM sha=aaaaaaaaaaaa runes=10]"}}},
		{V: 2, RequestID: "e2e-mask-IN-09", Status: "ok", Input: ports.AuditInput{Action: "pass"}, Output: ports.AuditOutput{Action: "pass"}, Usage: ports.AuditUsage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150}, CostMicroUSD: 120},
	}
	fake.ExtraRecords = []ports.AuditRecord{{V: 2, RequestID: "u1", Status: "unauthorized", Reason: "missing", HTTPStatus: 401}, {V: 2, RequestID: "r1", Status: "rate_limited", HTTPStatus: 429}}
	return map[string]*RunData{"fake": fake, "mlx": mlx}
}

func TestBuildAndRenderPage(t *testing.T) {
	cases := repoCases(t)
	page := BuildPage(PageInput{Title: "t", Upstreams: []string{"fake", "mlx"}, Runs: sampleRuns(t, cases), Cases: cases, Generated: time.Unix(0, 0), FullHref: "proxy-summary.html"})
	if len(page.Cols) != 4 || page.Cols[0].Key != "fake-mask" || page.Cols[3].Key != "mlx-block" {
		t.Fatalf("cols %+v", page.Cols)
	}
	if len(page.Groups) != 2 || len(page.Groups[0].Rows) != 15 || len(page.Groups[1].Rows) != 5 {
		t.Fatalf("groups %d", len(page.Groups))
	}
	tot := page.Counts[2].Cells
	if tot[0].Pass != 19 || tot[0].Gap != 1 || tot[1].Fail != 1 || tot[2].NA != 5 {
		t.Fatalf("totals %+v", tot)
	}
	keys := map[string]bool{}
	for _, e := range page.Examples {
		keys[e.Key] = true
	}
	for _, k := range []string{"masked", "derived-block", "output_blocked", "rate_limited", "unauthorized", "fp-pass"} {
		if !keys[k] {
			t.Errorf("no example %s", k)
		}
	}
	full, err := RenderFull(page)
	if err != nil {
		t.Fatal(err)
	}
	matrix, err := RenderMatrix(page)
	if err != nil {
		t.Fatal(err)
	}
	var secrets []string
	for _, c := range cases {
		secrets = append(secrets, c.Secrets...)
	}
	for name, doc := range map[string]string{"full": full, "matrix": matrix} {
		for _, want := range []string{`lang="ru"`, `Content-Security-Policy`, `default-src 'none'`, `id="IN-02-fake-mask"`, `<caption>`, `scope="col"`,
			"✕ ПОЙМАН · BLOCK", "◐ ЗАМАСКИРОВАН", "! KNOWN GAP", "N/A · требует fake", "o-fail-cell", "STALE", "FAILED", "max-width:1140px"} {
			if !strings.Contains(doc, want) {
				t.Errorf("%s lacks %q", name, want)
			}
		}
		if p := HTMLProblems(doc, secrets); len(p) > 0 {
			t.Errorf("%s: %v", name, p)
		}
	}
	for _, want := range []string{`<mark class="ph">[REDACTED_AWS_KEY]</mark>`, `\u003cscript\u003e`, "<details open>", `<pre class="log">`, "$0.000120",
		"auth NEW", "секрет не записан: только type/view/fingerprint", "TLS", "[SYSTEM sha=", "Что поймали"} {
		if !strings.Contains(full, want) {
			t.Errorf("full lacks %q", want)
		}
	}
	if !strings.Contains(matrix, "Extra-тесты:") || strings.Contains(matrix, `<pre class="log">`) {
		t.Error("matrix page must have the extra line and no logs")
	}
	if h := MatrixHeight(page); h < 2200 || h > 4000 {
		t.Errorf("matrix height %d", h)
	}
}

func TestExtraCategory(t *testing.T) {
	for name, want := range map[string]string{"AUTH-03": CatAuth, "TestRateLimit429": CatRate, "TestXForwardedForIgnored": CatRate,
		"TestCostTracking": CatCost, "TestAuditFilePerms": CatAudit, "TestStreamRejected": CatAPI} {
		if got := ExtraCategory(name); got != want {
			t.Errorf("%s: %s, want %s", name, got, want)
		}
	}
}

func TestReportsHideHomeAndRoot(t *testing.T) {
	t.Setenv("CHROME_HEADLESS_SHELL", filepath.Join(t.TempDir(), "no-chrome"))
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	cases := repoCases(t)
	runs := sampleRuns(t, cases)
	abs := filepath.Join(home, "proj", "Day13", "models", "baseline")
	runs["fake"].Dir = filepath.Join(home, "proj", "reports", "fake-1")
	runs["mlx"].Meta.Model = abs
	runs["fake"].Records[0].UpstreamModel = abs // old audit with an absolute path
	out := t.TempDir()
	if _, err := WriteSummary(out, PageInput{Title: "s", Upstreams: []string{"fake", "mlx"}, Runs: runs, Cases: cases, Generated: time.Unix(0, 0)}, "abc"); err != nil {
		t.Fatal(err)
	}
	run := runs["fake"]
	run.Dir = t.TempDir()
	if err := WriteRunHTML(cases, run); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{filepath.Join(out, SummaryHTML), filepath.Join(out, SummaryMatrixHTML), filepath.Join(out, SummaryJSON), filepath.Join(run.Dir, ReportHTML)} {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), home) {
			t.Errorf("%s contains the home directory", filepath.Base(f))
		}
	}
	if got := SanitizePaths(abs); !strings.HasPrefix(got, "~/") {
		t.Fatalf("SanitizePaths = %q", got)
	}
}

// results.json passes through SanitizePaths: meta.out_dir, the model path
// and problems never contain the home directory or the module root.
func TestResultsJSONHidesHomeAndRoot(t *testing.T) {
	t.Setenv("CHROME_HEADLESS_SHELL", filepath.Join(t.TempDir(), "no-chrome"))
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip(err)
	}
	wd, _ := os.Getwd()
	root := config.ModuleRoot(wd)
	cases := repoCases(t)
	out := t.TempDir()
	meta := Meta{SchemaVersion: ReportSchemaVersion, Upstream: "fake", OutDir: out, Model: filepath.Join(home, "proj", "models", "m"), GoTestRun: "0"}
	rs := allResults(cases)
	rs[0].Problems = []string{"see " + filepath.Join(root, "reports", "x")}
	if _, err := WriteReport(meta, cases, rs, nil, map[string]string{}, nil); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "results.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), home) || strings.Contains(string(b), root) {
		t.Fatalf("results.json contains an absolute local path:\n%.400s", b)
	}
	var f runFile
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("results.json must stay valid JSON: %v", err)
	}
	if f.Meta.Model != "~/proj/models/m" || !strings.HasPrefix(f.Results[0].Problems[0], "see <root>/") {
		t.Fatalf("sanitized meta/results: %q %q", f.Meta.Model, f.Results[0].Problems)
	}
}
