package proxy

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeRun writes a results.json (and an audit file) for tests.
func writeRun(t *testing.T, root, name string, meta Meta, results []Result) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"meta": meta, "results": results, "extra": []ExtraResult{{Name: "TestStreamRejected", Passed: true}}})
	if err := os.WriteFile(filepath.Join(dir, "results.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	rec := `{"v":1,"request_id":"e2e-mask-IN-09","status":"ok","input":{"action":"pass"},"output":{"action":"pass"},"prompt_masked":[]}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "audit-mask.jsonl"), []byte(rec), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func allResults(cases []Case) []Result {
	var rs []Result
	for _, c := range cases {
		for _, m := range modes {
			rs = append(rs, Result{ID: c.ID, Title: c.Title, Category: c.Category, Mode: m, Expect: c.ExpectFor(m), Fact: c.ExpectFor(m), Verdict: VerdictPass})
		}
	}
	return rs
}

func TestSelectLatest(t *testing.T) {
	cases := repoCases(t)
	root := t.TempDir()
	t0 := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	writeRun(t, root, "fake-old", Meta{SchemaVersion: 2, Upstream: "fake", Started: t0, GoTestRun: "0", CasesSHA: "abc"}, allResults(cases))
	writeRun(t, root, "fake-new", Meta{SchemaVersion: 2, Upstream: "fake", Started: t0.Add(time.Hour), GoTestRun: "0", CasesSHA: "abc"}, allResults(cases))
	writeRun(t, root, "fake-incomplete", Meta{SchemaVersion: 2, Upstream: "fake", Started: t0.Add(2 * time.Hour), GoTestRun: "0", CasesSHA: "abc"}, allResults(cases)[:5])
	writeRun(t, root, "mlx-v1", Meta{Upstream: "mlx", Started: t0, GoTestRun: "0"}, allResults(cases))
	writeRun(t, root, "other-failed", Meta{SchemaVersion: 2, Upstream: "cloud", Started: t0, GoTestRun: "1", CasesSHA: "old"}, allResults(cases))
	_ = os.MkdirAll(filepath.Join(root, "not-a-run"), 0o700)

	latest, warns := SelectLatest(root, cases, "abc")
	if len(warns) != 1 || !errors.Is(warns[0], ErrIncompleteRun) {
		t.Fatalf("warnings %v", warns)
	}
	f, m, c := latest["fake"], latest["mlx"], latest["cloud"]
	if f == nil || filepath.Base(f.Dir) != "fake-new" || f.Stale || f.Failed {
		t.Fatalf("fake %+v", f)
	}
	if m == nil || !m.Stale || m.StaleReason == "" || m.Failed || len(m.Records) != 1 {
		t.Fatalf("mlx v1 must be STALE and loaded: %+v", m)
	}
	if c == nil || !c.Stale || !c.Failed {
		t.Fatalf("cloud must be STALE (cases sha) and FAILED: %+v", c)
	}
}

func TestWriteSummaryWithoutChrome(t *testing.T) {
	t.Setenv("CHROME_HEADLESS_SHELL", filepath.Join(t.TempDir(), "no-chrome"))
	cases := repoCases(t)
	out := t.TempDir()
	files, err := WriteSummary(out, PageInput{Title: "s", Upstreams: []string{"fake", "mlx"}, Runs: sampleRuns(t, cases), Cases: cases, Generated: time.Unix(0, 0)}, "abc")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("files %v", files)
	}
	var sj map[string]any
	b, _ := os.ReadFile(filepath.Join(out, SummaryJSON))
	if err := json.Unmarshal(b, &sj); err != nil || sj["cells"] == nil || sj["runs"] == nil {
		t.Fatalf("summary json: %v", err)
	}
}
