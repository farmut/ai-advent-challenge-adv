package main

import (
	"bytes"
	"encoding/json"
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agile-consultant/e2e/proxy"
)

const casesPath = "../../e2e/proxy/testdata/cases.json"

func writeRun(t *testing.T, root, name string, meta proxy.Meta, cases []proxy.Case) {
	t.Helper()
	dir := filepath.Join(root, name)
	_ = os.MkdirAll(dir, 0o700)
	var rs []proxy.Result
	for _, c := range cases {
		for _, m := range []string{"mask", "block"} {
			rs = append(rs, proxy.Result{ID: c.ID, Title: c.Title, Category: c.Category, Mode: m, Fact: c.ExpectFor(m), Verdict: proxy.VerdictPass})
		}
	}
	b, _ := json.Marshal(map[string]any{"meta": meta, "results": rs})
	if err := os.WriteFile(filepath.Join(dir, "results.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRunMarksStaleV1AndCopies(t *testing.T) {
	t.Setenv("CHROME_HEADLESS_SHELL", filepath.Join(t.TempDir(), "no-chrome"))
	cases, err := proxy.LoadCases(casesPath)
	if err != nil {
		t.Fatal(err)
	}
	sha, _ := proxy.FileSHA256(casesPath)
	runs, out, copyTo := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "summary")
	writeRun(t, runs, "fake-1", proxy.Meta{SchemaVersion: proxy.ReportSchemaVersion, Upstream: "fake", CasesSHA: sha, GoTestRun: "0", Started: time.Now()}, cases)
	writeRun(t, runs, "mlx-1", proxy.Meta{Upstream: "mlx", GoTestRun: "0", Started: time.Now()}, cases)
	_ = os.MkdirAll(copyTo, 0o755)
	_ = os.WriteFile(filepath.Join(copyTo, "summary.html"), []byte("day12"), 0o644)

	var stdout, stderr bytes.Buffer
	if err := run([]string{"--runs", runs, "--cases", casesPath, "--out", out, "--copy-to", copyTo}, &stdout, &stderr); err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "fake: fake-1 (current)") || !strings.Contains(stdout.String(), "mlx: mlx-1 (STALE") {
		t.Fatalf("stdout %s", stdout.String())
	}
	for _, f := range []string{proxy.SummaryHTML, proxy.SummaryMatrixHTML, proxy.SummaryJSON} {
		if _, err := os.Stat(filepath.Join(copyTo, f)); err != nil {
			t.Errorf("not copied: %s", f)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(copyTo, "summary.html")); string(b) != "day12" {
		t.Error("copy-to must keep other files")
	}
	html, _ := os.ReadFile(filepath.Join(out, proxy.SummaryHTML))
	if !strings.Contains(string(html), "STALE") {
		t.Error("html must show STALE")
	}
}

func TestRunListsAnyUpstream(t *testing.T) {
	t.Setenv("CHROME_HEADLESS_SHELL", filepath.Join(t.TempDir(), "no-chrome"))
	cases, err := proxy.LoadCases(casesPath)
	if err != nil {
		t.Fatal(err)
	}
	sha, _ := proxy.FileSHA256(casesPath)
	runs, out := t.TempDir(), t.TempDir()
	writeRun(t, runs, "compat-1", proxy.Meta{SchemaVersion: proxy.ReportSchemaVersion, Upstream: "openai-compat", BaseURL: "http://127.0.0.1:11434/v1",
		CasesSHA: sha, GoTestRun: "0", Started: time.Now()}, cases)
	var stdout, stderr bytes.Buffer
	if err := run([]string{"--runs", runs, "--cases", casesPath, "--out", out}, &stdout, &stderr); err != nil {
		t.Fatalf("%v\n%s", err, stderr.String())
	}
	for _, want := range []string{"fake: no run", "mlx: no run", "openai-compat: compat-1 (current)"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout lacks %q:\n%s", want, stdout.String())
		}
	}
	raw, _ := os.ReadFile(filepath.Join(out, proxy.SummaryHTML))
	page := html.UnescapeString(string(raw))
	if !strings.Contains(page, "fake + mlx + openai-compat") || !strings.Contains(page, "openai-compat · mask") {
		t.Error("html must have openai-compat columns and title")
	}
}

func TestRunErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no out", []string{"--cases", casesPath}},
		{"no runs", []string{"--cases", casesPath, "--runs", t.TempDir(), "--out", t.TempDir()}},
		{"bad cases", []string{"--cases", "nope.json", "--out", t.TempDir()}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := run(tt.args, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
