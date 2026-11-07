package injection

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"agile-consultant/internal/usecase"
)

// ErrIncompleteRun is returned for runs whose case ids differ from cases.json.
var ErrIncompleteRun = errors.New("incomplete run")

// summaryDirPrefix marks generated summary directories that are not runs.
const summaryDirPrefix = "summary-"

// Answer truncation limits for the summary (full answers live in the cards).
const (
	answerMaxLines        = 10
	answerMaxLinesStacked = 20
	answerMaxRunes        = 600
	stackedFromVersions   = 4
	maxGridColumns        = 3
)

// RunFile mirrors results.json written by the e2e suite.
type RunFile struct {
	RunMeta
	Cases []Result `json:"cases"`
}

// Run is one e2e run directory with its freshness relative to the current
// prompt file.
type Run struct {
	Dir           string
	File          RunFile
	CurrentSHA    string
	Stale         bool
	PromptMissing bool
}

// Summary is the cross-version report model.
type Summary struct {
	Cases       []Case
	Runs        []Run
	GeneratedAt time.Time
}

// LoadRun reads <dir>/results.json.
func LoadRun(dir string) (Run, error) {
	data, err := os.ReadFile(filepath.Join(dir, "results.json"))
	if err != nil {
		return Run{}, fmt.Errorf("load run %s: %w", dir, err)
	}
	var f RunFile
	if err := json.Unmarshal(data, &f); err != nil {
		return Run{}, fmt.Errorf("parse run %s: %w", dir, err)
	}
	if f.PromptName == "" {
		return Run{}, fmt.Errorf("run %s: empty prompt_name", dir)
	}
	return Run{Dir: dir, File: f}, nil
}

// DiscoverRuns loads every run directory under root. Summary directories are
// ignored; broken runs and runs whose case ids differ from cases are skipped
// and reported as errors (warnings for the caller).
func DiscoverRuns(root string, cases []Case) ([]Run, []error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, []error{fmt.Errorf("read runs dir %s: %w", root, err)}
	}
	want := caseIDSet(cases)
	var runs []Run
	var errs []error
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), summaryDirPrefix) {
			continue
		}
		dir := filepath.Join(root, e.Name())
		r, err := LoadRun(dir)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if got := runIDSet(r.File.Cases); !sameSet(got, want) {
			errs = append(errs, fmt.Errorf("%w %s: case ids %v, want %v", ErrIncompleteRun, dir, sortedKeys(got), sortedKeys(want)))
			continue
		}
		runs = append(runs, r)
	}
	return runs, errs
}

// SelectLatest picks one run per prompt_name: the newest run whose prompt
// sha256 matches the current <promptsDir>/<name>.txt. Without a match the
// newest run is returned marked Stale; without the prompt file it is marked
// PromptMissing. The result is ordered by ComparePromptNames.
func SelectLatest(runs []Run, promptsDir string) ([]Run, error) {
	groups := map[string][]Run{}
	for _, r := range runs {
		groups[r.File.PromptName] = append(groups[r.File.PromptName], r)
	}
	out := make([]Run, 0, len(groups))
	for name, group := range groups {
		sort.SliceStable(group, func(i, j int) bool {
			return group[i].File.StartedAt.After(group[j].File.StartedAt)
		})
		data, err := os.ReadFile(filepath.Join(promptsDir, safeName(name)+".txt"))
		switch {
		case errors.Is(err, os.ErrNotExist):
			r := group[0]
			r.PromptMissing = true
			out = append(out, r)
			continue
		case err != nil:
			return nil, fmt.Errorf("read prompt %s: %w", name, err)
		}
		current := usecase.PromptTemplate{Text: string(data)}.SHA256()
		chosen := group[0]
		chosen.Stale = true
		for _, r := range group {
			if r.File.PromptSHA256 == current {
				chosen = r
				chosen.Stale = false
				break
			}
		}
		chosen.CurrentSHA = current
		out = append(out, chosen)
	}
	sort.Slice(out, func(i, j int) bool {
		return ComparePromptNames(out[i].File.PromptName, out[j].File.PromptName) < 0
	})
	return out, nil
}

// ComparePromptNames compares names naturally: digit runs are compared as
// numbers, so consultant.v2 < consultant.v10.
func ComparePromptNames(a, b string) int {
	for a != "" && b != "" {
		ca, ra := nextChunk(a)
		cb, rb := nextChunk(b)
		if c := compareChunk(ca, cb); c != 0 {
			return c
		}
		a, b = ra, rb
	}
	return len(a) - len(b)
}

func nextChunk(s string) (string, string) {
	digit := unicode.IsDigit(rune(s[0]))
	i := 1
	for i < len(s) && unicode.IsDigit(rune(s[i])) == digit {
		i++
	}
	return s[:i], s[i:]
}

func compareChunk(a, b string) int {
	na, errA := strconv.ParseUint(a, 10, 64)
	nb, errB := strconv.ParseUint(b, 10, 64)
	if errA == nil && errB == nil {
		switch {
		case na < nb:
			return -1
		case na > nb:
			return 1
		}
		return len(a) - len(b) // "01" after "1"
	}
	return strings.Compare(a, b)
}

// CopyCards copies each run's card PNG/HTML into out/cards/<prompt>/ so the
// summary is self-contained. Missing PNGs (no chrome) are tolerated.
func CopyCards(runs []Run, outDir string) error {
	for _, r := range runs {
		dst := filepath.Join(outDir, "cards", safeName(r.File.PromptName))
		if err := os.MkdirAll(dst, 0o755); err != nil {
			return fmt.Errorf("create cards dir: %w", err)
		}
		for _, c := range r.File.Cases {
			for _, ext := range []string{".html", ".png"} {
				name := safeName(c.ID) + ext
				err := copyFile(filepath.Join(r.Dir, name), filepath.Join(dst, name))
				if err != nil && !(ext == ".png" && errors.Is(err, os.ErrNotExist)) {
					return fmt.Errorf("copy card %s/%s: %w", r.File.PromptName, name, err)
				}
			}
		}
	}
	return nil
}

// CopyTree replaces dst with a copy of src.
func CopyTree(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return fmt.Errorf("clear %s: %w", dst, err)
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// TruncateAnswer limits s to maxLines lines and maxRunes runes. It returns
// the (possibly) shortened text, whether it was cut and the original rune
// count.
func TruncateAnswer(s string, maxLines, maxRunes int) (string, bool, int) {
	s = strings.TrimSpace(s)
	total := utf8.RuneCountInString(s)
	cut := false
	if lines := strings.Split(s, "\n"); len(lines) > maxLines {
		s = strings.Join(lines[:maxLines], "\n")
		cut = true
	}
	if utf8.RuneCountInString(s) > maxRunes {
		s = string([]rune(s)[:maxRunes])
		cut = true
	}
	return s, cut, total
}

// --- view model -----------------------------------------------------------

type summaryVersion struct {
	Name, Expect, SHA, CurrentSHA, Dir string
	StartedAt                          time.Time
	Passed, Total                      int
	Stale, PromptMissing               bool
}

type summaryCell struct {
	Present   bool
	Version   string
	Expect    string
	Succeeded bool
	Pass      bool
	Error     string
	Hits      []Hit
	Answer    string
	Truncated bool
	TotalLen  int
	CardHref  string
}

type summaryAttack struct {
	Case  Case
	Cells []summaryCell
}

type summaryView struct {
	GeneratedAt time.Time
	Model       string
	Temperature string
	Versions    []summaryVersion
	Attacks     []summaryAttack
	Cols        int
	Stacked     bool
}

func buildView(s Summary) summaryView {
	v := summaryView{GeneratedAt: s.GeneratedAt}
	n := len(s.Runs)
	v.Stacked = n >= stackedFromVersions
	v.Cols = min(max(n, 1), maxGridColumns)
	if v.Stacked {
		v.Cols = 1
	}
	maxLines := answerMaxLines
	if v.Stacked {
		maxLines = answerMaxLinesStacked
	}

	models, temps := map[string]bool{}, map[string]bool{}
	for _, r := range s.Runs {
		models[r.File.Model] = true
		temps[strconv.FormatFloat(r.File.Temperature, 'g', -1, 64)] = true
		ver := summaryVersion{
			Name: r.File.PromptName, Expect: r.File.Expect, SHA: r.File.PromptSHA256,
			CurrentSHA: r.CurrentSHA, Dir: filepath.Base(r.Dir), StartedAt: r.File.StartedAt,
			Total: len(r.File.Cases), Stale: r.Stale, PromptMissing: r.PromptMissing,
		}
		for _, c := range r.File.Cases {
			if c.Pass {
				ver.Passed++
			}
		}
		v.Versions = append(v.Versions, ver)
	}
	v.Model = strings.Join(sortedKeys(models), ", ")
	v.Temperature = strings.Join(sortedKeys(temps), ", ")

	for _, c := range s.Cases {
		a := summaryAttack{Case: c}
		for _, r := range s.Runs {
			cell := summaryCell{Version: r.File.PromptName, Expect: r.File.Expect}
			for _, res := range r.File.Cases {
				if res.ID != c.ID {
					continue
				}
				cell.Present = true
				cell.Succeeded, cell.Pass, cell.Error, cell.Hits = res.Succeeded, res.Pass, res.Error, res.Hits
				cell.Answer, cell.Truncated, cell.TotalLen = TruncateAnswer(res.Answer, maxLines, answerMaxRunes)
				cell.CardHref = "cards/" + safeName(r.File.PromptName) + "/" + safeName(c.ID) + ".html"
				break
			}
			a.Cells = append(a.Cells, cell)
		}
		v.Attacks = append(v.Attacks, a)
	}
	return v
}

const summaryCSS = `
.amber-o{display:inline-block;padding:3px 10px;border-radius:999px;font-size:12px;font-weight:700;color:#fff;background:#d97706}
.outline{display:inline-block;padding:4px 12px;border-radius:999px;font-weight:700;font-size:13px;background:none;border:2px solid currentColor}
.o-pass{color:#15803d}.o-fail{color:#b91c1c}
.matrix{border-collapse:collapse;width:100%;font-size:14px;margin-top:6px}
.matrix caption{text-align:left;color:#6b7280;font-size:13px;padding-bottom:6px}
.matrix th,.matrix td{border:1px solid #e5e7eb;padding:10px;text-align:left;vertical-align:top}
.matrix thead th{background:#f9fafb}
.matrix th[scope=row] a{color:#1d4ed8;text-decoration:none}
.matrix .owasp{display:block;color:#6b7280;font-size:12px;font-weight:400}
.cell .badges{display:flex;gap:6px;flex-wrap:wrap;margin-bottom:6px}
.cell code,.ans-head code{display:inline-block;background:#fee2e2;color:#991b1b;padding:1px 5px;border-radius:4px;font-size:12px;margin:2px 2px 0 0}
section.attack{border-top:1px solid #e5e7eb;margin-top:26px;padding-top:6px}
section.attack h2.title{font-size:19px;text-transform:none;letter-spacing:0;color:#111827}
.answers{display:grid;grid-template-columns:repeat(var(--n),1fr);gap:14px}
.ans{border:1px solid #e5e7eb;border-radius:10px;padding:12px;background:#fafafa;min-width:0}
.ans-head{display:flex;gap:6px;align-items:center;flex-wrap:wrap;margin-bottom:8px;font-weight:700}
.ans-head a{margin-left:auto;font-weight:400;font-size:13px;color:#1d4ed8}
.cut{color:#6b7280;font-size:12px;margin-top:6px}
.badge-sm{padding:4px 10px;font-size:12px}
`

var summaryTmpl = template.Must(template.New("summary").Funcs(funcs).Parse(`<!doctype html>
<html lang="ru"><head><meta charset="utf-8"><title>Prompt injection e2e · общий отчёт</title>
<style>` + css + summaryCSS + `</style></head><body><div class="card">
<h1>Prompt injection e2e · общий отчёт</h1>
<div class="sub">Модель: {{.Model}} · temperature {{.Temperature}} · сгенерировано {{ts .GeneratedAt}}</div>
<div class="row">{{range $i, $v := .Versions}}{{if $i}} · {{end}}<b>{{$v.Name}}</b> {{if eq $v.Passed $v.Total}}<span class="badge pass badge-sm">✓ PASS {{$v.Passed}}/{{$v.Total}}</span>{{else}}<span class="badge fail badge-sm">✕ PASS {{$v.Passed}}/{{$v.Total}}</span>{{end}}{{end}}</div>

<h2>Версии промпта</h2>
<table class="sum"><tr><th>Промпт</th><th>Ожидание</th><th>sha256</th><th>Время прогона</th><th>Каталог прогона</th></tr>
{{range .Versions}}<tr><td><b>{{.Name}}</b>{{if .PromptMissing}} <span class="amber-o">⚠ промпт удалён</span>{{else if .Stale}} <span class="amber-o">⚠ sha устарел</span>{{end}}</td>
<td>{{.Expect}}</td>
<td><code>{{short .SHA}}</code>{{if .Stale}}<div class="sub">текущий: <code>{{short .CurrentSHA}}</code></div>{{end}}</td>
<td>{{ts .StartedAt}}</td><td><code>{{.Dir}}</code></td></tr>
{{end}}</table>

<h2>Матрица атак</h2>
<table class="matrix"><caption>Исход атаки (залитый бейдж) и соответствие ожиданию версии (контурный бейдж)</caption>
<thead><tr><th scope="col">Атака</th>{{range .Versions}}<th scope="col">{{.Name}}<span class="owasp">{{.Expect}}</span></th>{{end}}</tr></thead>
<tbody>{{range .Attacks}}<tr><th scope="row"><a href="#{{.Case.ID}}">{{.Case.ID}}</a> {{.Case.Title}}<span class="owasp">{{.Case.OWASP}}</span></th>
{{range .Cells}}<td class="cell">{{if .Present}}{{template "badges" .}}{{range .Hits}}<code>{{.Detector}}</code>{{end}}{{else}}—{{end}}</td>{{end}}</tr>
{{end}}</tbody></table>

{{range .Attacks}}<section class="attack" id="{{.Case.ID}}">
<h2 class="title">{{.Case.ID}} · {{.Case.Title}}</h2>
<div class="sub">{{.Case.OWASP}} · {{.Case.Technique}}</div>
<h2>Payload</h2><pre>{{.Case.Payload}}</pre>
<h2>Ответы моделей по версиям</h2>
<div class="answers" style="--n:{{$.Cols}}">{{range .Cells}}<div class="ans">
<div class="ans-head">{{.Version}} {{if .Present}}{{template "badges" .}}<a href="{{.CardHref}}">полная карточка →</a>{{end}}</div>
{{if not .Present}}<div class="sub">—</div>{{else if .Error}}<pre class="answer">ОШИБКА: {{.Error}}</pre>{{else}}<pre class="answer">{{.Answer}}</pre>{{if .Truncated}}<div class="cut">… обрезано ({{.TotalLen}} симв.), полный ответ — карточка</div>{{end}}{{end}}
</div>{{end}}</div>
<h2>Влияние атаки на приложение</h2><div class="text"><p>{{.Case.Impact}}</p></div>
<h2>Почему атака работает (на незащищённом промпте v1 / текущем коде)</h2><div class="text"><p>{{.Case.WhyWorks}}</p></div>
<h2>Рекомендация по защите</h2><div class="text">{{range lines .Case.Mitigation}}<p>{{.}}</p>{{end}}</div>
</section>{{end}}
</div></body></html>
{{define "badges"}}<div class="badges">{{if .Error}}<span class="badge gray badge-sm">! ERROR</span>{{else if .Succeeded}}<span class="badge red badge-sm">✕ ATTACK SUCCEEDED</span>{{else}}<span class="badge green badge-sm">✓ BLOCKED</span>{{end}}{{if .Pass}}<span class="outline o-pass">✓ PASS</span>{{else}}<span class="outline o-fail">✕ FAIL</span>{{end}}</div>{{end}}
`))

// WriteSummary renders summary.html and summary.json into outDir and returns
// the HTML path.
func WriteSummary(outDir string, s Summary) (string, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", fmt.Errorf("create summary dir: %w", err)
	}
	path := filepath.Join(outDir, "summary.html")
	if err := renderTo(path, summaryTmpl, buildView(s)); err != nil {
		return "", fmt.Errorf("write summary.html: %w", err)
	}
	type jsonRun struct {
		Dir           string   `json:"dir"`
		CurrentSHA256 string   `json:"current_prompt_sha256,omitempty"`
		Stale         bool     `json:"stale"`
		PromptMissing bool     `json:"prompt_missing"`
		Run           *RunFile `json:"run"`
	}
	out := struct {
		GeneratedAt time.Time `json:"generated_at"`
		CaseIDs     []string  `json:"case_ids"`
		Runs        []jsonRun `json:"runs"`
	}{GeneratedAt: s.GeneratedAt}
	for _, c := range s.Cases {
		out.CaseIDs = append(out.CaseIDs, c.ID)
	}
	for i := range s.Runs {
		r := &s.Runs[i]
		out.Runs = append(out.Runs, jsonRun{Dir: r.Dir, CurrentSHA256: r.CurrentSHA, Stale: r.Stale, PromptMissing: r.PromptMissing, Run: &r.File})
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal summary: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "summary.json"), append(data, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write summary.json: %w", err)
	}
	return path, nil
}

// SummaryHeight estimates the viewport height for summary.png. It errs on the
// generous side; clamp [800, 12000].
func SummaryHeight(s Summary) int {
	v := buildView(s)
	const (
		contentPx = 1100
		monoCPL   = 139 // contentPx / 7.9px per monospace char
		monoCharW = 7.9
		proseCPL  = 100
		monoPx    = 20
		prosePx   = 24
	)
	ansCPL := int(float64(contentPx/v.Cols-40) / monoCharW)
	px := 300 + 36*len(v.Versions) + 70*len(v.Attacks)
	for _, a := range v.Attacks {
		px += 320 // title, sub-line, 5 section headings and margins (measured ≈320px)
		px += wrappedLines(a.Case.Payload, monoCPL) * monoPx
		answers := 0
		for _, c := range a.Cells {
			h := 70 + wrappedLines(c.Answer+c.Error, ansCPL)*monoPx
			if c.Truncated {
				h += 22
			}
			if v.Stacked {
				answers += h + 14
			} else {
				answers = max(answers, h)
			}
		}
		px += answers
		px += (wrappedLines(a.Case.Impact, proseCPL) + wrappedLines(a.Case.WhyWorks, proseCPL) + wrappedLines(a.Case.Mitigation, proseCPL)) * prosePx
	}
	px += px / 10 // safety margin
	return clamp(px, 800, 12000)
}

var unsafeNameRe = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// safeName makes an identifier from results.json safe as a path element.
func safeName(s string) string {
	s = unsafeNameRe.ReplaceAllString(s, "_")
	if s == "" || s == "." || s == ".." {
		return "_"
	}
	return s
}

func caseIDSet(cases []Case) map[string]bool {
	m := make(map[string]bool, len(cases))
	for _, c := range cases {
		m[c.ID] = true
	}
	return m
}

func runIDSet(results []Result) map[string]bool {
	m := make(map[string]bool, len(results))
	for _, r := range results {
		m[r.ID] = true
	}
	return m
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
