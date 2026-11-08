package proxy

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agile-consultant/internal/auditreport"
	"agile-consultant/internal/ports"
	"agile-consultant/internal/report/htmlkit"
)

// ReportSchemaVersion is the results.json schema: v2 adds schema_version,
// cases_sha, git_sha, extra[].category, audit-extra.jsonl and the HTML
// report. Runs without schema_version are v1.
const ReportSchemaVersion = 2

// Run report files.
const (
	ReportHTML       = "report.html"
	ReportMatrixHTML = "report-matrix.html"
	ReportPNG        = "report.png"
	AuditExtraFile   = "audit-extra.jsonl"
)

// Result is one case run in one mode.
type Result struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Category      string   `json:"category"`
	Mode          string   `json:"mode"`
	Expect        string   `json:"expect"`
	Fact          string   `json:"fact"`
	InputFact     string   `json:"input_fact"`
	Status        int      `json:"http_status"`
	Findings      string   `json:"findings"`
	UpstreamCalls int      `json:"upstream_calls"`
	Verdict       string   `json:"verdict"`
	Problems      []string `json:"problems,omitempty"`
	Notes         string   `json:"notes,omitempty"`
}

// ExtraResult is one scenario test outside the case table.
type ExtraResult struct {
	Name     string `json:"name"`
	Category string `json:"category,omitempty"`
	Passed   bool   `json:"passed"`
	Skipped  bool   `json:"skipped"`
	Details  string `json:"details"`
}

// Meta describes the run.
type Meta struct {
	SchemaVersion int    `json:"schema_version"`
	CasesSHA      string `json:"cases_sha"`
	GitSHA        string `json:"git_sha"`
	Upstream      string `json:"upstream"`
	// BaseURL is the openai-compat upstream without userinfo ("" otherwise).
	BaseURL   string    `json:"base_url,omitempty"`
	Started   time.Time `json:"started"`
	Finished  time.Time `json:"finished"`
	OutDir    string    `json:"out_dir"`
	Model     string    `json:"model"`
	Cases     int       `json:"cases"`
	GoTestRun string    `json:"go_test_exit"`
}

// Summary counts verdicts.
type Summary struct {
	Total           int `json:"total"`
	Pass            int `json:"pass"`
	Fail            int `json:"fail"`
	KnownGap        int `json:"known_gap"`
	UnexpectedCatch int `json:"unexpected_catch"`
	NA              int `json:"na"`
	ExtraPassed     int `json:"extra_passed"`
	ExtraFailed     int `json:"extra_failed"`
}

// Summarize counts verdicts.
func Summarize(rs []Result, ex []ExtraResult) Summary {
	var s Summary
	for _, r := range rs {
		s.Total++
		switch r.Verdict {
		case VerdictPass:
			s.Pass++
		case VerdictFail:
			s.Fail++
		case VerdictKnownGap:
			s.KnownGap++
		case VerdictUnexpectedCatch:
			s.UnexpectedCatch++
		default:
			s.NA++
		}
	}
	for _, e := range ex {
		switch {
		case e.Skipped:
		case e.Passed:
			s.ExtraPassed++
		default:
			s.ExtraFailed++
		}
	}
	return s
}

// WriteReport writes results.json, report.md, report.html,
// report-matrix.html, report.png (when Chrome is available) and the audit
// copies to meta.OutDir. auditByMode maps "mask"/"block" to the suite audit
// files; extraAudits are the extra-test audit files (concatenated into
// audit-extra.jsonl for the log examples).
func WriteReport(meta Meta, cases []Case, rs []Result, ex []ExtraResult, auditByMode map[string]string, extraAudits []string) (string, error) {
	if err := os.MkdirAll(meta.OutDir, 0o700); err != nil {
		return "", err
	}
	sum := Summarize(rs, ex)
	js, err := json.MarshalIndent(map[string]any{"meta": meta, "summary": sum, "results": rs, "extra": ex}, "", "  ")
	if err != nil {
		return "", err
	}
	// meta.out_dir, model paths and problems must not reveal local absolute
	// paths: the module root becomes "<root>", the home directory "~".
	js = []byte(SanitizePaths(string(js)))
	if err := os.WriteFile(filepath.Join(meta.OutDir, "results.json"), js, 0o600); err != nil {
		return "", err
	}

	var files []string
	var intercepted []string
	modes := make([]string, 0, len(auditByMode))
	for m := range auditByMode {
		modes = append(modes, m)
	}
	sort.Strings(modes)
	for _, m := range modes {
		src := auditByMode[m]
		data, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		dst := filepath.Join(meta.OutDir, "audit-"+m+".jsonl")
		if err := os.WriteFile(dst, data, 0o600); err != nil {
			return "", err
		}
		files = append(files, dst)
		sc := bufio.NewScanner(strings.NewReader(string(data)))
		sc.Buffer(make([]byte, 0, 64*1024), 16<<20)
		for sc.Scan() {
			var r ports.AuditRecord
			if json.Unmarshal(sc.Bytes(), &r) == nil && (len(r.Input.Findings) > 0 || len(r.Output.Findings) > 0) {
				intercepted = append(intercepted, sc.Text())
			}
		}
	}
	if err := os.WriteFile(filepath.Join(meta.OutDir, "intercepted.jsonl"), []byte(strings.Join(intercepted, "\n")+"\n"), 0o600); err != nil {
		return "", err
	}
	var rep auditreport.Report
	if len(files) > 0 {
		recs, fs, err := auditreport.Read(files)
		if err != nil {
			return "", err
		}
		rep = auditreport.Build(recs, fs)
	}
	md := SanitizePaths(renderMarkdown(meta, sum, rs, ex, rep))
	path := filepath.Join(meta.OutDir, "report.md")
	if err := os.WriteFile(path, []byte(md), 0o600); err != nil {
		return "", err
	}

	extraPath := filepath.Join(meta.OutDir, AuditExtraFile)
	if err := concatFiles(extraPath, extraAudits); err != nil {
		return "", err
	}
	var extraFiles []string
	if len(extraAudits) > 0 {
		extraFiles = []string{extraPath}
	}
	run, err := LoadRunAudits(&RunData{Dir: meta.OutDir, Meta: meta, Results: rs, Extra: ex}, files, extraFiles)
	if err != nil {
		return "", err
	}
	if err := WriteRunHTML(cases, run); err != nil {
		return "", err
	}
	return path, nil
}

// WriteRunHTML writes report.html, report-matrix.html and report.png of
// one run. A missing Chrome only skips the PNG (with a warning).
func WriteRunHTML(cases []Case, run *RunData) error {
	page := BuildPage(PageInput{
		Title:     fmt.Sprintf("Day13 guard-proxy e2e · upstream %s", run.Meta.Upstream),
		Upstreams: []string{run.Meta.Upstream}, Runs: map[string]*RunData{run.Meta.Upstream: run},
		Cases: cases, Generated: time.Now(), FullHref: ReportHTML,
	})
	full, err := RenderFull(page)
	if err != nil {
		return err
	}
	matrix, err := RenderMatrix(page)
	if err != nil {
		return err
	}
	htmlPath, matrixPath := filepath.Join(run.Dir, ReportHTML), filepath.Join(run.Dir, ReportMatrixHTML)
	if err := os.WriteFile(htmlPath, []byte(full), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(matrixPath, []byte(matrix), 0o644); err != nil {
		return err
	}
	return TryScreenshot(matrixPath, filepath.Join(run.Dir, ReportPNG), MatrixHeight(page))
}

// TryScreenshot renders htmlPath to pngPath with headless Chrome. Without
// Chrome it prints a warning and returns nil.
func TryScreenshot(htmlPath, pngPath string, height int) error {
	chrome, err := htmlkit.FindChrome()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %s skipped: %v\n", filepath.Base(pngPath), err)
		return nil
	}
	return htmlkit.Screenshot(context.Background(), chrome, htmlPath, pngPath, height)
}

// RenderRunHTML renders the full HTML report of a run from the given audit
// files (used by tests to check a snapshot before the run ends).
func RenderRunHTML(meta Meta, cases []Case, rs []Result, ex []ExtraResult, auditFiles []string) (string, error) {
	run, err := LoadRunAudits(&RunData{Dir: meta.OutDir, Meta: meta, Results: rs, Extra: ex}, auditFiles, nil)
	if err != nil {
		return "", err
	}
	return RenderFull(BuildPage(PageInput{Title: "snapshot", Upstreams: []string{meta.Upstream},
		Runs: map[string]*RunData{meta.Upstream: run}, Cases: cases, Generated: time.Now()}))
}

// LoadRunAudits reads the case and extra audit files (missing files are
// skipped) into run.
func LoadRunAudits(run *RunData, caseFiles, extraFiles []string) (*RunData, error) {
	var err error
	if run.Records, err = readExisting(caseFiles); err != nil {
		return nil, err
	}
	if run.ExtraRecords, err = readExisting(extraFiles); err != nil {
		return nil, err
	}
	return run, nil
}

func readExisting(paths []string) ([]ports.AuditRecord, error) {
	var ok []string
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && fi.Size() > 0 {
			ok = append(ok, p)
		}
	}
	if len(ok) == 0 {
		return nil, nil
	}
	recs, _, err := auditreport.Read(ok)
	return recs, err
}

func concatFiles(dst string, srcs []string) error {
	var b []byte
	for _, s := range srcs {
		data, err := os.ReadFile(s)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		b = append(b, data...)
	}
	return os.WriteFile(dst, b, 0o600)
}

// FileSHA256 returns the hex sha256 of a file.
func FileSHA256(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}

// GitSHA returns "git rev-parse HEAD" of dir (with "-dirty" when the
// directory has uncommitted or untracked changes), or "" without git.
func GitSHA(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	sha := strings.TrimSpace(string(out))
	if st, err := exec.Command("git", "-C", dir, "status", "--porcelain", "--", ".").Output(); err == nil && len(strings.TrimSpace(string(st))) > 0 {
		sha += "-dirty"
	}
	return sha
}

func renderMarkdown(meta Meta, sum Summary, rs []Result, ex []ExtraResult, rep auditreport.Report) string {
	c := auditreport.Cell
	var b strings.Builder
	fmt.Fprintf(&b, "# Day13 guard-proxy e2e — upstream `%s`\n\n", meta.Upstream)
	if meta.BaseURL != "" {
		fmt.Fprintf(&b, "- Upstream base_url: `%s`\n", meta.BaseURL)
	}
	fmt.Fprintf(&b, "- Схема отчёта: v%d, cases sha `%s`, git `%s`; HTML: `%s`, PNG: `%s`\n", meta.SchemaVersion, shortSHA(meta.CasesSHA), shortSHA(meta.GitSHA), ReportHTML, ReportPNG)
	fmt.Fprintf(&b, "- Запуск: %s — %s\n- Модель: `%s`\n- Кейсов: %d, прогонов (кейс × режим): %d\n- Итог: **PASS %d, FAIL %d, KNOWN_GAP %d, UNEXPECTED_CATCH %d, N/A %d**; extra: %d passed, %d failed\n- Каталог: `%s`\n\n",
		meta.Started.Format(time.RFC3339), meta.Finished.Format(time.RFC3339), meta.Model, meta.Cases, sum.Total,
		sum.Pass, sum.Fail, sum.KnownGap, sum.UnexpectedCatch, sum.NA, sum.ExtraPassed, sum.ExtraFailed, meta.OutDir)

	b.WriteString("## 1. Кейсы\n\n| ID | Кейс | Режим | Ожидание | Факт | Находки | Upstream вызван | Итог |\n|---|---|---|---|---|---|---|---|\n")
	for _, r := range rs {
		called := "нет"
		if r.UpstreamCalls > 0 {
			called = fmt.Sprintf("да (%d)", r.UpstreamCalls)
		}
		verdict := r.Verdict
		if len(r.Problems) > 0 {
			verdict += ": " + strings.Join(r.Problems, "; ")
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s |\n", r.ID, c(r.Title), r.Mode, r.Expect, c(r.Fact), c(r.Findings), called, c(verdict))
	}

	b.WriteString("\n## 2. Поймали\n\n")
	caught := 0
	for _, r := range rs {
		if r.Verdict == VerdictPass && r.Fact != FactPass {
			fmt.Fprintf(&b, "- %s (%s, %s): %s — %s\n", r.ID, r.Mode, c(r.Title), r.Fact, c(r.Findings))
			caught++
		}
	}
	if caught == 0 {
		b.WriteString("Нет.\n")
	}
	b.WriteString("\n## 3. Пропустили (known gaps и ошибки)\n\n")
	missed := 0
	for _, r := range rs {
		switch r.Verdict {
		case VerdictKnownGap:
			fmt.Fprintf(&b, "- %s (%s, %s): known gap — %s\n", r.ID, r.Mode, c(r.Title), c(r.Notes))
			missed++
		case VerdictUnexpectedCatch:
			fmt.Fprintf(&b, "- %s (%s, %s): known gap неожиданно пойман (%s) — пересмотреть кейс\n", r.ID, r.Mode, c(r.Title), r.Fact)
			missed++
		case VerdictFail:
			fmt.Fprintf(&b, "- %s (%s, %s): **FAIL** ожидание %s, факт %s; %s\n", r.ID, r.Mode, c(r.Title), r.Expect, r.Fact, c(strings.Join(r.Problems, "; ")))
			missed++
		}
	}
	b.WriteString("- Не покрыто детекторами вообще: реверс строки, base32/gzip, утечка секрета частями в разных запросах, секреты в base64 в ответе модели (output guard сканирует только raw/normalized).\n")
	if missed == 0 {
		b.WriteString("- В прогоне пропусков нет.\n")
	}

	b.WriteString("\n## 4. Extra-тесты\n\n| Тест | Итог | Детали |\n|---|---|---|\n")
	for _, e := range ex {
		st := "PASS"
		switch {
		case e.Skipped:
			st = "SKIP"
		case !e.Passed:
			st = "FAIL"
		}
		fmt.Fprintf(&b, "| %s | %s | %s |\n", e.Name, st, c(e.Details))
	}

	fmt.Fprintf(&b, "\n## 5. Перехваченные секреты (audit-report)\n\nИсточник: `audit-mask.jsonl`, `audit-block.jsonl`; записей с находками — в `intercepted.jsonl`. Значения секретов в аудит не пишутся: только тип, представление, HMAC-fingerprint и замаскированный контекст.\n\n")
	fmt.Fprintf(&b, "| ts | request_id | stage | type | view | action | fingerprint | контекст ±%d рун |\n|---|---|---|---|---|---|---|---|\n", auditreport.ContextRunes)
	for _, i := range rep.Intercepted {
		view := i.View
		if i.Derived {
			view += " (derived)"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | `%s` | %s |\n", i.TS.UTC().Format("15:04:05"), c(i.RequestID), i.Stage, c(i.Type), c(view), c(i.Action), i.Fingerprint, c(i.Context))
	}
	b.WriteString("\n| Stage | Type | Action | Count |\n|---|---|---|---|\n")
	for _, f := range rep.Findings {
		fmt.Fprintf(&b, "| %s | %s | %s | %d |\n", f.Stage, c(f.Type), c(f.Action), f.Count)
	}

	t := rep.Tokens
	fmt.Fprintf(&b, "\n## 6. Токены и стоимость\n\nЗаписей аудита: %d. Prompt %d, completion %d, total %d токенов; стоимость **$%s**. Источник usage: upstream %d, estimated %d, none %d.\n\n",
		rep.Records, t.Prompt, t.Completion, t.Total, t.CostUSD, t.Upstream, t.Estimated, t.None)
	b.WriteString("| Model | Requests | Tokens | Cost USD | Price ref |\n|---|---|---|---|---|\n")
	for _, m := range t.ByModel {
		fmt.Fprintf(&b, "| %s | %d | %d | %s | %s |\n", c(m.Model), m.Requests, m.Tokens, m.CostUSD, c(m.PriceRef))
	}
	b.WriteString("\nЗаблокированные на входе запросы не отправляются в модель: usage.source=none, стоимость 0. Стоимость paid-модели проверяется в TestCostTracking (100/50 токенов по 400000/1600000 micro-USD за 1M = $0.000120).\n")

	b.WriteString(`
## 7. Риски

- Аутентификация есть (Bearer-ключи из PROXY_API_KEYS, key_id в аудите), но ключи статические: ротация через рестарт, нет per-key лимитов.
- TLS у прокси нет: listen по умолчанию только loopback; адрес не на loopback допустим лишь с proxy.allow_non_loopback и TLS-терминатором перед прокси.
- Перебор ключей ограничен 10 неудачами за 5 минут с IP (в памяти процесса).
- Derived-находки (base64/hex/url, склейка) блокируются даже в mask mode: точное маскирование невозможно, возможны ложные блокировки на легитимных закодированных данных.
- Regex-детекторы не ловят обфускацию вне перечисленных представлений (ROT13, слова, реверс, base32, разбиение по запросам).
- Output guard не стримит (stream=true → 400) и проверяет ответ целиком; ответы реальной модели недетерминированы.
- Rate limit в памяти процесса: сбрасывается при рестарте, не разделяется между инстансами.
- Цены облачных моделей в конфиге надо сверять с прайсом провайдера (поле ref).
`)
	return b.String()
}
