package proxy

import (
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestUpstreamOrder(t *testing.T) {
	tests := []struct {
		name string
		runs map[string]*RunData
		want []string
	}{
		{"none", nil, []string{"fake", "mlx"}},
		{"fake only", map[string]*RunData{"fake": {}}, []string{"fake", "mlx"}},
		{"compat and others sorted", map[string]*RunData{"openai-compat": {}, "fake": {}, "cloud": {}}, []string{"fake", "mlx", "cloud", "openai-compat"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := UpstreamOrder(tt.runs); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("UpstreamOrder = %v, want %v", got, tt.want)
			}
		})
	}
	if !IsFakeUpstream("fake") || IsFakeUpstream("mlx") || IsFakeUpstream("openai-compat") {
		t.Fatal("IsFakeUpstream")
	}
}

func TestCompatBaseURL(t *testing.T) {
	tests := []struct {
		raw, want, err string
	}{
		{"http://127.0.0.1:11434/v1", "http://127.0.0.1:11434/v1", ""},
		{" http://127.0.0.1:1234/v1/ ", "http://127.0.0.1:1234/v1", ""},
		{"https://llm.example.com/openai/v1", "https://llm.example.com/openai/v1", ""},
		{"", "", "required"},
		{"127.0.0.1:11434", "", "http(s)"},
		{"ftp://127.0.0.1/v1", "", "http(s)"},
		{"http://user:secret@127.0.0.1:11434/v1", "", "userinfo"},
		{"http://127.0.0.1:11434/v1?key=x", "", "query"},
		{"http://127.0.0.1:11434/v1#frag", "", "fragment"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			u, err := CompatBaseURL(tt.raw)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("err = %v, want %q", err, tt.err)
				}
				if strings.Contains(err.Error(), "secret") {
					t.Fatalf("error leaks userinfo: %v", err)
				}
				return
			}
			if err != nil || u.String() != tt.want {
				t.Fatalf("CompatBaseURL = %v, %v; want %s", u, err, tt.want)
			}
		})
	}
	u, _ := url.Parse("http://u:p@127.0.0.1:11434/v1?k=v#f")
	if got := RedactURL(u); got != "http://127.0.0.1:11434/v1" {
		t.Fatalf("RedactURL = %q", got)
	}
	if RedactURL(nil) != "" {
		t.Fatal("RedactURL(nil)")
	}
}

// An openai-compat run gets its own column group with base_url (without
// userinfo) in the run line; its verdicts are judged on the input fact.
func TestSummaryWithCompatUpstream(t *testing.T) {
	t.Setenv("CHROME_HEADLESS_SHELL", filepath.Join(t.TempDir(), "no-chrome"))
	cases := repoCases(t)
	root := t.TempDir()
	t0 := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	writeRun(t, root, "fake-1", Meta{SchemaVersion: ReportSchemaVersion, Upstream: "fake", Started: t0, GoTestRun: "0", CasesSHA: "abc"}, allResults(cases))
	writeRun(t, root, "compat-1", Meta{SchemaVersion: ReportSchemaVersion, Upstream: "openai-compat", BaseURL: "http://user:pw@127.0.0.1:11434/v1",
		Model: "qwen2.5:7b-instruct", Started: t0, GoTestRun: "0", CasesSHA: "abc"}, allResults(cases))
	latest, warns := SelectLatest(root, cases, "abc")
	if len(warns) != 0 || latest["openai-compat"] == nil {
		t.Fatalf("latest %v warns %v", latest, warns)
	}
	ups := UpstreamOrder(latest)
	out := t.TempDir()
	if _, err := WriteSummary(out, PageInput{Title: "s", Upstreams: ups, Runs: latest, Cases: cases, Generated: t0}, "abc"); err != nil {
		t.Fatal(err)
	}
	html, err := os.ReadFile(filepath.Join(out, SummaryHTML))
	if err != nil {
		t.Fatal(err)
	}
	h := string(html)
	for _, want := range []string{"openai-compat · mask", "openai-compat · block", "base_url <code>http://127.0.0.1:11434/v1</code>"} {
		if !strings.Contains(h, want) {
			t.Errorf("summary html lacks %q", want)
		}
	}
	if strings.Contains(h, "user:pw") {
		t.Error("summary html leaks base_url userinfo")
	}
	if got := judgedFact(Result{Fact: "x", InputFact: FactMasked}, "openai-compat"); got != FactMasked {
		t.Fatalf("openai-compat must be judged on the input fact: %q", got)
	}
	if got := judgedFact(Result{Fact: FactOutputRedacted, InputFact: FactPass}, "fake"); got != FactOutputRedacted {
		t.Fatalf("fake must be judged on the full fact: %q", got)
	}
}
