package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Summary output files (cmd/proxy-summary).
const (
	SummaryHTML       = "proxy-summary.html"
	SummaryMatrixHTML = "proxy-summary-matrix.html"
	SummaryPNG        = "proxy-summary.png"
	SummaryJSON       = "proxy-summary.json"
)

// ErrIncompleteRun marks a run whose (case, mode) set differs from cases.json.
var ErrIncompleteRun = errors.New("incomplete run")

// runFile mirrors results.json.
type runFile struct {
	Meta    Meta          `json:"meta"`
	Summary Summary       `json:"summary"`
	Results []Result      `json:"results"`
	Extra   []ExtraResult `json:"extra"`
}

// LoadRun reads results.json and the audit copies of a run directory.
func LoadRun(dir string) (*RunData, error) {
	b, err := os.ReadFile(filepath.Join(dir, "results.json"))
	if err != nil {
		return nil, err
	}
	var f runFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	if f.Meta.Upstream == "" {
		return nil, fmt.Errorf("%s: meta.upstream is empty", dir)
	}
	run := &RunData{Dir: dir, Meta: f.Meta, Results: f.Results, Extra: f.Extra}
	return LoadRunAudits(run,
		[]string{filepath.Join(dir, "audit-mask.jsonl"), filepath.Join(dir, "audit-block.jsonl")},
		[]string{filepath.Join(dir, AuditExtraFile)})
}

// complete reports whether the run has every case in both modes.
func complete(r *RunData, cases []Case) bool {
	want := map[string]bool{}
	for _, c := range cases {
		for _, m := range modes {
			if c.Mode == "" || c.Mode == m {
				want[c.ID+"/"+m] = true
			}
		}
	}
	got := map[string]bool{}
	for _, res := range r.Results {
		got[res.ID+"/"+res.Mode] = true
	}
	if len(got) != len(want) {
		return false
	}
	for k := range want {
		if !got[k] {
			return false
		}
	}
	return true
}

// MarkFreshness sets Stale (older report schema or other cases.json) and
// Failed (go test exit code ≠ 0).
func MarkFreshness(r *RunData, casesSHA string) {
	switch {
	case r.Meta.SchemaVersion < ReportSchemaVersion:
		r.Stale, r.StaleReason = true, fmt.Sprintf("схема отчёта v%d (до auth/HTML), нужен перепрогон", max(r.Meta.SchemaVersion, 1))
	case casesSHA != "" && r.Meta.CasesSHA != casesSHA:
		r.Stale, r.StaleReason = true, "cases.json изменился после прогона"
	}
	r.Failed = strings.TrimSpace(r.Meta.GoTestRun) != "0"
}

// SelectLatest returns the latest complete run per upstream (by
// meta.started) from the subdirectories of root. Incomplete or unreadable
// runs are reported as warnings.
func SelectLatest(root string, cases []Case, casesSHA string) (map[string]*RunData, []error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, []error{err}
	}
	latest := map[string]*RunData{}
	var warns []error
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, "results.json")); err != nil {
			continue
		}
		r, err := LoadRun(dir)
		if err != nil {
			warns = append(warns, err)
			continue
		}
		if !complete(r, cases) {
			warns = append(warns, fmt.Errorf("%s: %w", e.Name(), ErrIncompleteRun))
			continue
		}
		if cur := latest[r.Meta.Upstream]; cur == nil || r.Meta.Started.After(cur.Meta.Started) {
			latest[r.Meta.Upstream] = r
		}
	}
	for _, r := range latest {
		MarkFreshness(r, casesSHA)
	}
	return latest, warns
}

// summaryJSON is the machine-readable summary.
type summaryJSON struct {
	Generated time.Time             `json:"generated"`
	CasesSHA  string                `json:"cases_sha"`
	Runs      []summaryRunJSON      `json:"runs"`
	Columns   []Col                 `json:"columns"`
	Cells     []summaryCellJSON     `json:"cells"`
	Extra     []ExtraRow            `json:"extra"`
	Audit     []AggRow              `json:"audit"`
	Tokens    []TokenRow            `json:"tokens"`
	Examples  []summaryExampleJSON  `json:"log_examples"`
	Upstreams []string              `json:"upstreams"`
	Missing   []string              `json:"missing_upstreams,omitempty"`
	Counts    map[string][]Counter  `json:"counts"`
	Meta      map[string]summaryRef `json:"meta"`
}

type summaryRunJSON struct {
	Upstream    string  `json:"upstream"`
	Dir         string  `json:"dir"`
	Stale       bool    `json:"stale"`
	StaleReason string  `json:"stale_reason,omitempty"`
	Failed      bool    `json:"failed"`
	Summary     Summary `json:"summary"`
}

type summaryCellJSON struct {
	ID, Column, Badge string
	Fail              bool
}

type summaryExampleJSON struct {
	Key, Title, Source string
}

type summaryRef struct {
	SchemaVersion int    `json:"schema_version"`
	CasesSHA      string `json:"cases_sha"`
	GitSHA        string `json:"git_sha"`
	GoTestExit    string `json:"go_test_exit"`
}

// WriteSummary renders proxy-summary.html, proxy-summary-matrix.html,
// proxy-summary.json and proxy-summary.png (when Chrome is available) into
// outDir and returns the file paths.
func WriteSummary(outDir string, in PageInput, casesSHA string) ([]string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	in.FullHref = SummaryHTML
	page := BuildPage(in)
	full, err := RenderFull(page)
	if err != nil {
		return nil, err
	}
	matrix, err := RenderMatrix(page)
	if err != nil {
		return nil, err
	}
	sj := summaryJSON{Generated: in.Generated, CasesSHA: casesSHA, Columns: page.Cols, Extra: page.Extras, Audit: page.Agg, Tokens: page.Tokens,
		Upstreams: in.Upstreams, Counts: map[string][]Counter{}, Meta: map[string]summaryRef{}}
	for _, cr := range page.Counts {
		sj.Counts[cr.Title] = cr.Cells
	}
	for _, up := range in.Upstreams {
		r := in.Runs[up]
		if r == nil {
			sj.Missing = append(sj.Missing, up)
			continue
		}
		sj.Runs = append(sj.Runs, summaryRunJSON{Upstream: up, Dir: r.Dir, Stale: r.Stale, StaleReason: r.StaleReason, Failed: r.Failed, Summary: Summarize(r.Results, r.Extra)})
		sj.Meta[up] = summaryRef{SchemaVersion: r.Meta.SchemaVersion, CasesSHA: r.Meta.CasesSHA, GitSHA: r.Meta.GitSHA, GoTestExit: r.Meta.GoTestRun}
	}
	for _, g := range page.Groups {
		for _, row := range g.Rows {
			for i, c := range row.Cells {
				if c.Present {
					sj.Cells = append(sj.Cells, summaryCellJSON{ID: row.ID, Column: page.Cols[i].Key, Badge: c.Badge, Fail: c.Fail})
				}
			}
		}
	}
	for _, e := range page.Examples {
		sj.Examples = append(sj.Examples, summaryExampleJSON{Key: e.Key, Title: e.Title, Source: e.Source})
	}
	js, err := json.MarshalIndent(sj, "", "  ")
	if err != nil {
		return nil, err
	}
	paths := map[string][]byte{SummaryHTML: []byte(full), SummaryMatrixHTML: []byte(matrix), SummaryJSON: []byte(SanitizePaths(string(js)))}
	names := []string{SummaryHTML, SummaryMatrixHTML, SummaryJSON}
	var out []string
	for _, n := range names {
		p := filepath.Join(outDir, n)
		if err := os.WriteFile(p, paths[n], 0o644); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	png := filepath.Join(outDir, SummaryPNG)
	if err := TryScreenshot(filepath.Join(outDir, SummaryMatrixHTML), png, MatrixHeight(page)); err != nil {
		return out, fmt.Errorf("screenshot: %w", err)
	}
	if _, err := os.Stat(png); err == nil {
		out = append(out, png)
	}
	sort.Strings(out)
	return out, nil
}
