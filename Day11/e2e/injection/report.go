package injection

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// Expectation modes for E2E_EXPECT.
const (
	ExpectVulnerable = "vulnerable"
	ExpectRobust     = "robust"
)

// RunMeta describes a single suite run.
type RunMeta struct {
	PromptName   string    `json:"prompt_name"`
	PromptFile   string    `json:"prompt_file"`
	PromptSHA256 string    `json:"prompt_sha256"`
	Expect       string    `json:"expect"`
	Model        string    `json:"model"`
	ModelPath    string    `json:"model_path"`
	Temperature  float64   `json:"temperature"`
	TopP         float64   `json:"top_p"`
	MaxTokens    int       `json:"max_tokens"`
	StartedAt    time.Time `json:"started_at"`
}

// Result is the outcome of a single case.
type Result struct {
	Case      Case          `json:"-"`
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Answer    string        `json:"answer"`
	Hits      []Hit         `json:"hits"`
	Succeeded bool          `json:"attack_succeeded"`
	Pass      bool          `json:"pass"`
	Format4   bool          `json:"format4"`
	Duration  time.Duration `json:"-"`
	DurationS float64       `json:"duration_s"`
	Timestamp time.Time     `json:"timestamp"`
	Error     string        `json:"error,omitempty"`
	CardHTML  string        `json:"card_html,omitempty"`
	CardPNG   string        `json:"card_png,omitempty"`
}

// Evaluate fills the verdict fields of r from its hits and the expectation.
func (r *Result) Evaluate(expect string) {
	r.Succeeded = len(r.Hits) > 0
	r.Pass = r.Error == "" && r.Succeeded == (expect == ExpectVulnerable)
	r.Format4 = HasFormat4(r.Answer)
	r.ID, r.Title = r.Case.ID, r.Case.Title
	r.DurationS = r.Duration.Seconds()
}

// ShortSHA returns the first 12 hex chars of a digest.
func ShortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

type cardView struct {
	Meta RunMeta
	R    Result
}

var funcs = template.FuncMap{
	"short": ShortSHA,
	"ts":    func(t time.Time) string { return t.Format("2006-01-02 15:04:05 MST") },
	"secs":  func(d time.Duration) string { return fmt.Sprintf("%.1f с", d.Seconds()) },
	"lines": func(s string) []string { return strings.Split(strings.TrimSpace(s), "\n") },
	"expectLabel": func(e string) string {
		if e == ExpectVulnerable {
			return "vulnerable — атака должна пройти (фиксация уязвимости)"
		}
		return "robust — атака должна быть заблокирована"
	},
}

const css = `
*{box-sizing:border-box}
body{margin:0;padding:24px;background:#f3f4f6;font:15px/1.5 -apple-system,"Segoe UI","Helvetica Neue",Arial,sans-serif;color:#111827}
.card{background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:24px 28px;max-width:1140px;margin:0 auto;box-shadow:0 1px 3px rgba(0,0,0,.06)}
h1{font-size:22px;margin:0 0 4px}
h2{font-size:15px;margin:20px 0 6px;color:#374151;text-transform:uppercase;letter-spacing:.04em}
.sub{color:#6b7280;font-size:14px}
.row{display:flex;gap:12px;align-items:center;flex-wrap:wrap;margin:12px 0}
.badge{display:inline-block;padding:6px 14px;border-radius:999px;font-weight:700;font-size:14px;color:#fff;letter-spacing:.03em}
.red{background:#dc2626}.green{background:#16a34a}.gray{background:#6b7280}.amber{background:#d97706}
.pass{background:#16a34a}.fail{background:#b91c1c}
table.meta{border-collapse:collapse;width:100%;font-size:14px}
table.meta td{padding:4px 8px;border-bottom:1px solid #f3f4f6;vertical-align:top}
table.meta td:first-child{color:#6b7280;width:210px;white-space:nowrap}
pre{background:#0f172a;color:#e2e8f0;padding:14px 16px;border-radius:8px;white-space:pre-wrap;word-break:break-word;font:13px/1.5 "SF Mono",Menlo,Consolas,monospace;margin:0}
pre.answer{background:#111827;border-left:4px solid #6366f1}
ul.hits{margin:0;padding-left:20px}
ul.hits code{background:#fee2e2;color:#991b1b;padding:1px 5px;border-radius:4px;font-size:13px}
.text p{margin:0 0 6px}
.cols{display:grid;grid-template-columns:1fr;gap:4px}
table.sum{border-collapse:collapse;width:100%;font-size:14px}
table.sum th,table.sum td{border-bottom:1px solid #e5e7eb;padding:8px;text-align:left;vertical-align:top}
table.sum th{background:#f9fafb;color:#374151}
table.sum td:first-child{white-space:nowrap}
table.sum a{color:#1d4ed8;text-decoration:none}
.note{margin:20px 0 0;padding:10px 14px;background:#ecfdf5;border-left:4px solid #16a34a;border-radius:6px;color:#065f46;font-size:14px}
`

var cardTmpl = template.Must(template.New("card").Funcs(funcs).Parse(`<!doctype html>
<html lang="ru"><head><meta charset="utf-8"><title>{{.R.Case.ID}} — {{.R.Case.Title}}</title>
<style>` + css + `</style></head><body><div class="card">
<h1>{{.R.Case.ID}} · {{.R.Case.Title}}</h1>
<div class="sub">{{.R.Case.OWASP}}</div>
<div class="row">
{{if .R.Error}}<span class="badge gray">ERROR</span>{{else if .R.Succeeded}}<span class="badge red">ATTACK SUCCEEDED</span>{{else}}<span class="badge green">BLOCKED</span>{{end}}
{{if .R.Pass}}<span class="badge pass">PASS</span>{{else}}<span class="badge fail">FAIL</span>{{end}}
{{if .R.Format4}}<span class="badge gray">format4: да</span>{{else}}<span class="badge amber">format4: нет</span>{{end}}
</div>
<table class="meta">
<tr><td>Техника</td><td>{{.R.Case.Technique}}</td></tr>
<tr><td>Ожидание (E2E_EXPECT)</td><td>{{expectLabel .Meta.Expect}}</td></tr>
<tr><td>Промпт</td><td>{{.Meta.PromptName}} · sha256 {{short .Meta.PromptSHA256}}</td></tr>
<tr><td>Модель</td><td>{{.Meta.Model}} · temperature {{.Meta.Temperature}} · top_p {{.Meta.TopP}} · max_tokens {{.Meta.MaxTokens}}</td></tr>
<tr><td>Время ответа</td><td>{{secs .R.Duration}}</td></tr>
<tr><td>Timestamp</td><td>{{ts .R.Timestamp}}</td></tr>
</table>
<h2>Payload (вопрос пользователя)</h2>
<pre>{{.R.Case.Payload}}</pre>
<h2>Ответ модели (raw)</h2>
{{if .R.Error}}<pre class="answer">ОШИБКА: {{.R.Error}}</pre>{{else}}<pre class="answer">{{.R.Answer}}</pre>{{end}}
<h2>Сработавшие детекторы</h2>
{{if .R.Hits}}<ul class="hits">{{range .R.Hits}}<li>{{.Detector}} → <code>{{.Match}}</code></li>{{end}}</ul>{{else}}<div class="sub">нет — признаков успешной атаки не найдено</div>{{end}}
{{if and (not .R.Succeeded) (not .R.Error)}}<div class="note">Атака отбита промптом {{.Meta.PromptName}}. Ниже — анализ уязвимости, от которой защищает промпт.</div>{{end}}
<h2>Влияние атаки на приложение</h2><div class="text"><p>{{.R.Case.Impact}}</p></div>
<h2>Почему атака работает (на незащищённом промпте v1 / текущем коде)</h2><div class="text"><p>{{.R.Case.WhyWorks}}</p></div>
<h2>Рекомендация по защите</h2><div class="text">{{range lines .R.Case.Mitigation}}<p>{{.}}</p>{{end}}</div>
</div></body></html>
`))

type indexView struct {
	Meta    RunMeta
	Results []Result
	Passed  int
	Total   int
}

var indexTmpl = template.Must(template.New("index").Funcs(funcs).Parse(`<!doctype html>
<html lang="ru"><head><meta charset="utf-8"><title>Prompt injection e2e — {{.Meta.PromptName}}</title>
<style>` + css + `</style></head><body><div class="card">
<h1>Prompt injection e2e · {{.Meta.PromptName}}</h1>
<div class="sub">Agile-консультант · прогон {{ts .Meta.StartedAt}}</div>
<div class="row">{{if eq .Passed .Total}}<span class="badge pass">PASS {{.Passed}}/{{.Total}}</span>{{else}}<span class="badge fail">PASS {{.Passed}}/{{.Total}}</span>{{end}}</div>
<table class="meta">
<tr><td>Ожидание (E2E_EXPECT)</td><td>{{expectLabel .Meta.Expect}}</td></tr>
<tr><td>Промпт</td><td>{{.Meta.PromptName}} · sha256 {{.Meta.PromptSHA256}}</td></tr>
<tr><td>Модель</td><td>{{.Meta.Model}} · temperature {{.Meta.Temperature}} · top_p {{.Meta.TopP}} · max_tokens {{.Meta.MaxTokens}}</td></tr>
</table>
<h2>Результаты</h2>
<table class="sum"><tr><th>ID</th><th>Атака</th><th>OWASP</th><th>Результат атаки</th><th>Детекторы</th><th>format4</th><th>Тест</th></tr>
{{range .Results}}<tr><td><b><a href="{{.Case.ID}}.html">{{.Case.ID}}</a></b></td><td>{{.Case.Title}}</td><td>{{.Case.OWASP}}</td>
<td>{{if .Error}}<span class="badge gray">ERROR</span>{{else if .Succeeded}}<span class="badge red">SUCCEEDED</span>{{else}}<span class="badge green">BLOCKED</span>{{end}}</td>
<td>{{range .Hits}}<div>{{.Detector}}</div>{{else}}—{{end}}</td>
<td>{{if .Format4}}да{{else}}нет{{end}}</td>
<td>{{if .Pass}}<span class="badge pass">PASS</span>{{else}}<span class="badge fail">FAIL</span>{{end}}</td></tr>
{{end}}</table>
</div></body></html>
`))

// WriteCard renders the HTML card for r into dir and returns its path.
func WriteCard(dir string, meta RunMeta, r Result) (string, error) {
	path := filepath.Join(dir, r.Case.ID+".html")
	if err := renderTo(path, cardTmpl, cardView{Meta: meta, R: r}); err != nil {
		return "", fmt.Errorf("write card %s: %w", r.Case.ID, err)
	}
	return path, nil
}

// WriteIndex renders index.html with the summary table and returns its path.
func WriteIndex(dir string, meta RunMeta, results []Result) (string, error) {
	v := indexView{Meta: meta, Results: results, Total: len(results)}
	for _, r := range results {
		if r.Pass {
			v.Passed++
		}
	}
	path := filepath.Join(dir, "index.html")
	if err := renderTo(path, indexTmpl, v); err != nil {
		return "", fmt.Errorf("write index: %w", err)
	}
	return path, nil
}

// WriteResultsJSON writes results.json with run metadata and per-case data.
func WriteResultsJSON(dir string, meta RunMeta, results []Result) (string, error) {
	out := struct {
		RunMeta
		Cases []Result `json:"cases"`
	}{meta, results}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal results: %w", err)
	}
	path := filepath.Join(dir, "results.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("write results: %w", err)
	}
	return path, nil
}

func renderTo(path string, t *template.Template, data any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := t.Execute(f, data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// CardHeight estimates the viewport height (px) needed to render the card
// without clipping, clamped to [600, 4000]. The estimate is deliberately
// generous: extra whitespace is harmless, clipping is not.
func CardHeight(r Result) int {
	const (
		monoCPL  = 115 // 13px monospace in a ~1100px block
		proseCPL = 100 // 15px proportional Cyrillic text
		monoPx   = 20
		prosePx  = 24
		fixedPx  = 820 // header, badges, meta table, section headings, paddings
	)
	px := fixedPx
	px += wrappedLines(r.Case.Payload, monoCPL) * monoPx
	px += wrappedLines(r.Answer+r.Error, monoCPL) * monoPx
	px += wrappedLines(r.Case.Impact, proseCPL) * prosePx
	px += wrappedLines(r.Case.WhyWorks, proseCPL) * prosePx
	px += wrappedLines(r.Case.Mitigation, proseCPL) * prosePx
	px += len(r.Hits) * 30
	return clamp(px, 600, 4000)
}

func wrappedLines(s string, cpl int) int {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		n += 1 + utf8.RuneCountInString(l)/cpl
	}
	return n
}

// IndexHeight estimates the viewport height for index.html.
func IndexHeight(n int) int {
	return clamp(420+n*70, 600, 4000)
}

func clamp(v, lo, hi int) int {
	return min(max(v, lo), hi)
}
