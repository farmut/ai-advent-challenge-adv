package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"agile-consultant/internal/auditreport"
	"agile-consultant/internal/ports"
	"agile-consultant/internal/report/htmlkit"
)

// placeholderRe matches the guard placeholders highlighted in logs.
var placeholderRe = regexp.MustCompile(`\[REDACTED_[A-Z_]+\]`)

// HighlightPlaceholders is the only helper that returns template.HTML for
// report data. It reveals invisible characters, escapes the text with
// html.EscapeString and only then wraps placeholders (which consist of
// "[", "]", A-Z and "_", unchanged by escaping) in <mark class="ph">.
func HighlightPlaceholders(s string) template.HTML {
	esc := html.EscapeString(htmlkit.RevealInvisible(s))
	return template.HTML(placeholderRe.ReplaceAllString(esc, `<mark class="ph">$0</mark>`)) //nolint:gosec // escaped above
}

// keyLikeRe catches key-shaped tokens in titles when the case secrets are
// unknown (old results without cases.json).
var keyLikeRe = regexp.MustCompile(`\b(?:sk-(?:proj-)?[A-Za-z0-9_-]{6,}|AKIA[0-9A-Z]{16}|gh[pousr]_[A-Za-z0-9]{20,})\b`)

// keyAllowlist are public documentation values that are not secrets.
var keyAllowlist = map[string]bool{"AKIAIOSFODNN7EXAMPLE": true}

// MaskText replaces the case secrets (raw, base64, hex; longest first) and
// key-shaped tokens with [REDACTED] and reveals invisible characters. It is
// applied to every case-derived string shown in the HTML (titles, notes).
func MaskText(s string, secrets []string) string {
	var forms []string
	for _, sec := range secrets {
		if len(sec) >= 4 {
			forms = append(forms, Encodings(sec)...)
		}
	}
	sort.Slice(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	for _, f := range forms {
		s = strings.ReplaceAll(s, f, "[REDACTED]")
	}
	s = keyLikeRe.ReplaceAllStringFunc(s, func(m string) string {
		if keyAllowlist[m] {
			return m
		}
		return "[REDACTED]"
	})
	return htmlkit.RevealInvisible(s)
}

// HTMLProblems lists security problems of a rendered report: case secrets
// (raw/base64/hex), <script, and literal invisible or bidi characters. The
// messages never contain the secret values.
func HTMLProblems(doc string, secrets []string) []string {
	var out []string
	for i, s := range secrets {
		for _, e := range Encodings(s) {
			if len(e) >= 4 && strings.Contains(doc, e) {
				out = append(out, fmt.Sprintf("secret #%d in a raw/encoded form of length %d", i, len(e)))
				break
			}
		}
	}
	if strings.Contains(strings.ToLower(doc), "<script") {
		out = append(out, "contains <script")
	}
	for i, r := range doc {
		if r != '\n' && r != '\t' && (htmlkit.IsInvisible(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Bidi_Control, r)) {
			out = append(out, fmt.Sprintf("literal invisible U+%04X at byte %d", r, i))
			break
		}
	}
	return out
}

// RunData is one e2e run as input of the HTML report.
type RunData struct {
	Dir     string
	Meta    Meta
	Results []Result
	Extra   []ExtraResult
	// Records are the case audit records (audit-mask/block.jsonl).
	Records []ports.AuditRecord
	// ExtraRecords come from extra-test harnesses (rate limit, auth, ...).
	ExtraRecords []ports.AuditRecord
	// Stale marks a run of an older report schema or cases.json.
	Stale       bool
	StaleReason string
	// Failed marks a run whose go test exited non-zero.
	Failed bool
}

// PageInput configures a report page.
type PageInput struct {
	Title string
	// Upstreams are the column groups in order (UpstreamOrder: "fake",
	// "mlx", then any other meta.upstream such as "openai-compat"); a missing
	// run yields empty columns.
	Upstreams []string
	Runs      map[string]*RunData
	Cases     []Case
	Generated time.Time
	// FullHref links the matrix page to the full page.
	FullHref string
}

// Page is the view model of report.html / proxy-summary.html.
type Page struct {
	Title, Generated, Model, FullHref string
	Cols                              []Col
	Runs                              []RunInfo
	Counts                            []CountRow
	Groups                            []Group
	NCols                             int
	Extras                            []ExtraRow
	ExtraUps                          []string
	ExtraLine                         []ExtraLine
	Caught, Missed                    []CatchItem
	NotCovered                        []string
	Examples                          []Example
	Agg                               []AggRow
	Tokens                            []TokenRow
	Cost                              CostCheck
	Risks                             []string
}

// Col is one matrix column (upstream × mode).
type Col struct {
	Key, Label, Upstream, Mode string
	Present, Stale, Failed     bool
	StaleReason                string
	Pass, Gap, Fail, NA, Total int
}

// RunInfo is a header line about a run.
type RunInfo struct {
	Upstream, Dir, Started, Model, GitSHA, CasesSHA string
	// BaseURL is the openai-compat upstream (without userinfo), "" otherwise.
	BaseURL                string
	Schema                 int
	Present, Stale, Failed bool
	StaleReason            string
	Summary                Summary
}

// CountRow is a counter row (IN / OUT / total).
type CountRow struct {
	Title string
	Total bool
	Cells []Counter
}

// Counter counts verdicts of one column.
type Counter struct {
	Present             bool
	Pass, Gap, Fail, NA int
}

// Group is a matrix row group.
type Group struct {
	Title string
	Rows  []Row
}

// Row is one case.
type Row struct {
	ID, Title, Expect string
	Cells             []Cell
}

// Cell is one case × column result.
type Cell struct {
	Anchor                 string
	Present, Fail, NA      bool
	Badge, BadgeClass      string
	Outline, OutlineClass  string
	Codes, Problems        []string
	UpstreamCalls          int
	ShowCalls, Unconfirmed bool
}

// ExtraRow is one extra test across upstreams.
type ExtraRow struct {
	Name, Category, CatLabel, Details string
	New                               bool
	Cells                             []ExtraCell
}

// ExtraCell is an extra test status on one upstream.
type ExtraCell struct {
	Present       bool
	Status, Class string
}

// ExtraLine is the one-line extra summary of an upstream.
type ExtraLine struct {
	Upstream            string
	Present             bool
	Passed, Failed, Skp int
	Cats                []ExtraCat
}

// ExtraCat counts extra tests of a category.
type ExtraCat struct {
	Label         string
	New           bool
	Passed, Total int
}

// CatchItem is a "caught / missed" entry.
type CatchItem struct {
	ID, Title, Note string
	Where           []string
}

// Example is one intercepted-request log example.
type Example struct {
	Key, Title, Caption, Source, JSON string
	Long                              bool
}

// AggRow is a stage × type × action audit aggregate.
type AggRow struct {
	Stage, Type, Action string
	Counts              []int
}

// TokenRow summarises tokens and cost of a run.
type TokenRow struct {
	Upstream                       string
	Records, Prompt, Completion    int
	Total                          int
	CostUSD                        string
	SrcUpstream, SrcEstimated, Src int
}

// CostCheck is the deterministic cost check of TestCostTracking.
type CostCheck struct {
	Present, Passed bool
	Details         string
}

// Extra test categories.
const (
	CatAuth  = "auth"
	CatRate  = "rate"
	CatCost  = "cost"
	CatAudit = "audit"
	CatAPI   = "api"
)

// ExtraCategory derives the category of an extra test from its name (old
// results have no category field).
func ExtraCategory(name string) string {
	switch {
	case strings.HasPrefix(name, "AUTH-"):
		return CatAuth
	case strings.Contains(name, "RateLimit"), strings.Contains(name, "XForwardedFor"):
		return CatRate
	case strings.Contains(name, "Cost"):
		return CatCost
	case strings.Contains(name, "Audit"):
		return CatAudit
	}
	return CatAPI
}

func catLabel(c string) string {
	switch c {
	case CatAuth:
		return "auth"
	case CatRate:
		return "rate"
	case CatCost:
		return "cost"
	case CatAudit:
		return "audit"
	}
	return "api"
}

var modes = []string{"mask", "block"}

// BuildPage builds the view model.
func BuildPage(in PageInput) Page {
	p := Page{Title: in.Title, Generated: in.Generated.Format("2006-01-02 15:04:05 MST"), FullHref: in.FullHref}
	secretsByID := map[string][]string{}
	var allSecrets []string
	for _, c := range in.Cases {
		secretsByID[c.ID] = c.Secrets
		allSecrets = append(allSecrets, c.Secrets...)
	}
	mask := func(s string) string { return MaskText(s, allSecrets) }

	var models []string
	for _, up := range in.Upstreams {
		r := in.Runs[up]
		info := RunInfo{Upstream: up, Present: r != nil}
		if r != nil {
			info.Dir, info.Started, info.Model = r.Dir, r.Meta.Started.Format("2006-01-02 15:04:05"), r.Meta.Model
			if r.Meta.BaseURL != "" {
				if u, err := url.Parse(r.Meta.BaseURL); err == nil {
					info.BaseURL = RedactURL(u)
				}
			}
			info.GitSHA, info.CasesSHA, info.Schema = shortSHA(r.Meta.GitSHA), shortSHA(r.Meta.CasesSHA), max(r.Meta.SchemaVersion, 1)
			info.Stale, info.StaleReason, info.Failed = r.Stale, r.StaleReason, r.Failed
			info.Summary = Summarize(r.Results, r.Extra)
			models = append(models, up+": "+modelName(r.Meta.Model))
		}
		p.Runs = append(p.Runs, info)
		for _, m := range modes {
			col := Col{Key: up + "-" + m, Label: up + " · " + m, Upstream: up, Mode: m, Present: r != nil}
			if r != nil {
				col.Stale, col.StaleReason, col.Failed = r.Stale, r.StaleReason, r.Failed
			}
			p.Cols = append(p.Cols, col)
		}
	}
	p.Model = strings.Join(models, " · ")
	p.NCols = len(p.Cols) + 1

	// matrix and counters
	cases := in.Cases
	if len(cases) == 0 {
		cases = casesFromResults(in)
	}
	counters := map[string][]Counter{"IN": make([]Counter, len(p.Cols)), "OUT": make([]Counter, len(p.Cols))}
	groups := []Group{{Title: "Input guard"}, {Title: "Output guard"}}
	for _, c := range cases {
		row := Row{ID: c.ID, Title: mask(c.Title), Expect: expectText(c)}
		g, grp := 0, "IN"
		if c.Category == "output" {
			g, grp = 1, "OUT"
		}
		for i, col := range p.Cols {
			cell := Cell{Anchor: c.ID + "-" + col.Key}
			r := in.Runs[col.Upstream]
			var res *Result
			if r != nil {
				res = findResult(r.Results, c.ID, col.Mode)
			}
			if res != nil {
				cell = buildCell(cell, *res, col.Upstream, c)
				cnt := &counters[grp][i]
				cnt.Present = true
				switch res.Verdict {
				case VerdictPass:
					cnt.Pass++
					p.Cols[i].Pass++
				case VerdictKnownGap:
					cnt.Gap++
					p.Cols[i].Gap++
				case VerdictNA:
					cnt.NA++
					p.Cols[i].NA++
				default:
					cnt.Fail++
					p.Cols[i].Fail++
				}
				p.Cols[i].Total++
			}
			row.Cells = append(row.Cells, cell)
		}
		groups[g].Rows = append(groups[g].Rows, row)
	}
	for _, g := range groups {
		if len(g.Rows) > 0 {
			p.Groups = append(p.Groups, g)
		}
	}
	total := make([]Counter, len(p.Cols))
	for i := range p.Cols {
		for _, k := range []string{"IN", "OUT"} {
			c := counters[k][i]
			total[i].Present = total[i].Present || c.Present
			total[i].Pass += c.Pass
			total[i].Gap += c.Gap
			total[i].Fail += c.Fail
			total[i].NA += c.NA
		}
	}
	p.Counts = []CountRow{{Title: "IN (input guard)", Cells: counters["IN"]}, {Title: "OUT (output guard)", Cells: counters["OUT"]}, {Title: "Итого", Total: true, Cells: total}}

	p.buildExtras(in)
	p.buildCatch(in, cases, mask)
	p.buildExamples(in)
	p.buildAudit(in)
	p.Risks = risks
	return p
}

func casesFromResults(in PageInput) []Case {
	seen := map[string]bool{}
	var out []Case
	for _, up := range in.Upstreams {
		if r := in.Runs[up]; r != nil {
			for _, res := range r.Results {
				if !seen[res.ID] {
					seen[res.ID] = true
					out = append(out, Case{ID: res.ID, Title: res.Title, Category: res.Category})
				}
			}
		}
	}
	return out
}

func findResult(rs []Result, id, mode string) *Result {
	for i := range rs {
		if rs[i].ID == id && rs[i].Mode == mode {
			return &rs[i]
		}
	}
	return nil
}

func modelName(m string) string {
	if i := strings.LastIndexAny(m, "/\\"); i >= 0 {
		return m[i+1:]
	}
	return m
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

var expectLabels = map[string]string{
	FactPass: "pass", FactMasked: "masked", FactBlocked: "blocked", FactOutputRedacted: "output redacted",
	FactOutputBlocked: "output blocked", ExpectKnownGap: "known gap",
}

func expectText(c Case) string {
	m, b := expectLabels[c.Expect.Mask], expectLabels[c.Expect.Block]
	if m == "" && b == "" {
		return ""
	}
	if m == b {
		return "ожидание: " + m
	}
	return "ожидание: mask → " + m + " · block → " + b
}

// judgedFact is the fact the verdict was computed from (input-only on a
// real model: every upstream except fake).
func judgedFact(r Result, upstream string) string {
	if !IsFakeUpstream(upstream) && r.InputFact != "" {
		return r.InputFact
	}
	return r.Fact
}

func buildCell(cell Cell, r Result, upstream string, c Case) Cell {
	cell.Present = true
	cell.UpstreamCalls, cell.ShowCalls = r.UpstreamCalls, r.Verdict != VerdictNA
	for _, part := range strings.Split(r.Findings, ",") {
		if part = strings.TrimSpace(part); part != "" {
			cell.Codes = append(cell.Codes, part)
		}
	}
	for _, pr := range r.Problems {
		cell.Problems = append(cell.Problems, MaskText(pr, c.Secrets))
	}
	switch r.Verdict {
	case VerdictNA:
		cell.NA, cell.Badge, cell.BadgeClass = true, "N/A · требует fake", "gray"
		return cell
	case VerdictKnownGap:
		cell.Badge, cell.BadgeClass = "! KNOWN GAP", "amber"
		return cell
	case VerdictUnexpectedCatch:
		cell.Fail, cell.Badge, cell.BadgeClass = true, "⚠ UNEXPECTED_CATCH", "fail-b"
		cell.Outline, cell.OutlineClass = "✕ FAIL", "o-fail"
		return cell
	}
	switch judgedFact(r, upstream) {
	case FactBlocked:
		cell.Badge, cell.BadgeClass = "✕ ПОЙМАН · BLOCK", "red"
	case FactMasked:
		cell.Badge, cell.BadgeClass = "◐ ЗАМАСКИРОВАН", "indigo"
	case FactOutputRedacted:
		cell.Badge, cell.BadgeClass = "◐ OUTPUT REDACTED", "indigo-o"
	case FactOutputBlocked:
		cell.Badge, cell.BadgeClass = "✕ OUTPUT BLOCKED", "red"
	case FactPass:
		cell.Badge, cell.BadgeClass = "✓ чисто", "green"
		if c.Expect.Mask == FactPass && c.Expect.Block == FactPass {
			cell.Badge = "✓ PASS · FP-контроль"
		}
	default:
		cell.Badge, cell.BadgeClass = "⚠ ПРОВАЛ", "fail-b"
	}
	if r.Verdict == VerdictPass {
		cell.Outline, cell.OutlineClass = "✓ PASS", "o-pass"
	} else {
		cell.Fail, cell.Outline, cell.OutlineClass = true, "✕ FAIL", "o-fail"
	}
	return cell
}

func (p *Page) buildExtras(in PageInput) {
	idx := map[string]int{}
	p.ExtraUps = in.Upstreams
	for ui, up := range in.Upstreams {
		r := in.Runs[up]
		line := ExtraLine{Upstream: up, Present: r != nil}
		cats := map[string]*ExtraCat{}
		var catOrder []string
		if r != nil {
			for _, e := range r.Extra {
				cat := e.Category
				if cat == "" {
					cat = ExtraCategory(e.Name)
				}
				i, ok := idx[e.Name]
				if !ok {
					i = len(p.Extras)
					idx[e.Name] = i
					p.Extras = append(p.Extras, ExtraRow{Name: e.Name, Category: cat, CatLabel: catLabel(cat), New: cat == CatAuth,
						Details: htmlkit.RevealInvisible(e.Details), Cells: make([]ExtraCell, len(in.Upstreams))})
				}
				cell := ExtraCell{Present: true, Status: "✓ PASS", Class: "o-pass"}
				switch {
				case e.Skipped:
					cell.Status, cell.Class = "SKIP · только fake", "o-skip"
					line.Skp++
				case !e.Passed:
					cell.Status, cell.Class = "✕ FAIL", "o-fail"
					line.Failed++
				default:
					line.Passed++
				}
				p.Extras[i].Cells[ui] = cell
				if !e.Skipped {
					if p.Extras[i].Details == "" || IsFakeUpstream(up) {
						p.Extras[i].Details = htmlkit.RevealInvisible(e.Details)
					}
					c := cats[cat]
					if c == nil {
						c = &ExtraCat{Label: catLabel(cat), New: cat == CatAuth}
						cats[cat] = c
						catOrder = append(catOrder, cat)
					}
					c.Total++
					if e.Passed {
						c.Passed++
					}
				}
				if e.Name == "TestCostTracking" && IsFakeUpstream(up) {
					p.Cost = CostCheck{Present: !e.Skipped, Passed: e.Passed && !e.Skipped, Details: e.Details}
				}
			}
		}
		sort.SliceStable(catOrder, func(a, b int) bool { return catRank(catOrder[a]) < catRank(catOrder[b]) })
		for _, k := range catOrder {
			line.Cats = append(line.Cats, *cats[k])
		}
		p.ExtraLine = append(p.ExtraLine, line)
	}
	for i := range p.Extras {
		for j := range p.Extras[i].Cells {
			if !p.Extras[i].Cells[j].Present {
				p.Extras[i].Cells[j] = ExtraCell{Status: "—", Class: "o-skip"}
			}
		}
	}
	sort.SliceStable(p.Extras, func(a, b int) bool { return catRank(p.Extras[a].Category) < catRank(p.Extras[b].Category) })
}

func catRank(c string) int {
	switch c {
	case CatAuth:
		return 0
	case CatRate:
		return 1
	case CatCost:
		return 2
	case CatAudit:
		return 3
	}
	return 4
}

func (p *Page) buildCatch(in PageInput, cases []Case, mask func(string) string) {
	for _, c := range cases {
		var caught, missed []string
		note := ""
		for _, col := range p.Cols {
			r := in.Runs[col.Upstream]
			if r == nil {
				continue
			}
			res := findResult(r.Results, c.ID, col.Mode)
			if res == nil {
				continue
			}
			f := judgedFact(*res, col.Upstream)
			switch res.Verdict {
			case VerdictPass:
				if f != FactPass {
					caught = append(caught, col.Label+": "+expectLabels[f])
				}
			case VerdictKnownGap:
				missed = append(missed, col.Label+": known gap")
				note = c.Notes
			case VerdictUnexpectedCatch:
				missed = append(missed, col.Label+": known gap неожиданно пойман ("+f+")")
			case VerdictFail:
				missed = append(missed, col.Label+": ПРОВАЛ, факт "+f)
			}
		}
		if len(caught) > 0 {
			p.Caught = append(p.Caught, CatchItem{ID: c.ID, Title: mask(c.Title), Where: caught})
		}
		if len(missed) > 0 {
			p.Missed = append(p.Missed, CatchItem{ID: c.ID, Title: mask(c.Title), Where: missed, Note: mask(note)})
		}
	}
	p.NotCovered = []string{
		"реверс строки, base32/gzip и другие кодировки вне base64/hex/url;",
		"утечка секрета частями в разных запросах (детектор видит один запрос);",
		"секреты в base64 в ответе модели (output guard сканирует только raw/normalized);",
		"на реальных моделях (mlx, openai-compat) output-кейсы не проверяются: ответ модели недетерминирован (N/A).",
	}
}

type exampleKind struct {
	key, title, caption string
	prefer              []string
	match               func(r ports.AuditRecord) bool
}

const captionSecret = "секрет не записан: только type/view/fingerprint"

var exampleKinds = []exampleKind{
	{"masked", "Input masked: плейсхолдер вместо ключа ушёл в модель", captionSecret, []string{"e2e-mask-IN-02", "e2e-mask-IN-10"},
		func(r ports.AuditRecord) bool { return r.Status == ports.AuditStatusOK && r.Input.Action == "masked" }},
	{"derived-block", "Input blocked: derived-находка (base64) блокируется даже в mask", captionSecret, []string{"e2e-mask-IN-04", "e2e-mask-IN-05"},
		func(r ports.AuditRecord) bool {
			if r.Status != ports.AuditStatusInputBlocked {
				return false
			}
			for _, f := range r.Input.Findings {
				if f.Derived {
					return true
				}
			}
			return false
		}},
	{"output_redacted", "Output redacted: ключи в ответе модели заменены", captionSecret, []string{"e2e-mask-OUT-01"},
		func(r ports.AuditRecord) bool { return r.Output.Action == "redacted" }},
	{"output_blocked", "Output blocked: утечка system prompt (system prompt хранится как hash)", captionSecret + "; system prompt — только [SYSTEM sha=… runes=N]", []string{"e2e-mask-OUT-03", "e2e-mask-OUT-04"},
		func(r ports.AuditRecord) bool { return r.Status == ports.AuditStatusOutputBlocked }},
	{"rate_limited", "Rate limited: 429 по IP, prompt не записан", "запрос не разбирался: prompt и usage не записаны", nil,
		func(r ports.AuditRecord) bool { return r.Status == ports.AuditStatusRateLimited && r.Reason == "" }},
	{"unauthorized", "Unauthorized (NEW): 401 без валидного ключа", "токен и его HMAC не записаны: только reason и client_ip_hash", nil,
		func(r ports.AuditRecord) bool { return r.Status == ports.AuditStatusUnauthorized }},
	{"fp-pass", "FP-контроль: 16 цифр без Luhn прошли без срабатывания", "находок нет: текст записан как есть (уже проверен guard)", []string{"e2e-mask-IN-09", "e2e-block-IN-09"},
		func(r ports.AuditRecord) bool {
			return r.Status == ports.AuditStatusOK && r.Input.Action == "pass" && r.Output.Action == "pass" && len(r.Input.Findings) == 0
		}},
}

func (p *Page) buildExamples(in PageInput) {
	for _, k := range exampleKinds {
		var found *ports.AuditRecord
		src := ""
	search:
		for _, up := range in.Upstreams {
			r := in.Runs[up]
			if r == nil {
				continue
			}
			all := append(append([]ports.AuditRecord(nil), r.Records...), r.ExtraRecords...)
			for _, id := range k.prefer {
				for i := range all {
					if all[i].RequestID == id && k.match(all[i]) {
						found, src = &all[i], up
						break search
					}
				}
			}
			for i := range all {
				if k.match(all[i]) {
					found, src = &all[i], up
					break search
				}
			}
		}
		if found == nil {
			continue
		}
		b, err := json.MarshalIndent(found, "", "  ")
		if err != nil {
			continue
		}
		js := string(b)
		p.Examples = append(p.Examples, Example{Key: k.key, Title: k.title, Caption: k.caption, Source: src + " · audit v" + fmt.Sprint(found.V),
			JSON: js, Long: strings.Count(js, "\n") > 40})
	}
}

func (p *Page) buildAudit(in PageInput) {
	type key struct{ stage, typ, action string }
	agg := map[key][]int{}
	var keys []key
	for ui, up := range in.Upstreams {
		r := in.Runs[up]
		if r == nil {
			p.Tokens = append(p.Tokens, TokenRow{Upstream: up})
			continue
		}
		rep := auditreport.Build(r.Records, nil)
		for _, f := range rep.Findings {
			k := key{f.Stage, f.Type, f.Action}
			if agg[k] == nil {
				agg[k] = make([]int, len(in.Upstreams))
				keys = append(keys, k)
			}
			agg[k][ui] += f.Count
		}
		t := auditreport.Build(append(append([]ports.AuditRecord(nil), r.Records...), r.ExtraRecords...), nil)
		p.Tokens = append(p.Tokens, TokenRow{Upstream: up, Records: t.Records, Prompt: t.Tokens.Prompt, Completion: t.Tokens.Completion,
			Total: t.Tokens.Total, CostUSD: t.Tokens.CostUSD, SrcUpstream: t.Tokens.Upstream, SrcEstimated: t.Tokens.Estimated, Src: t.Tokens.None})
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.stage != b.stage {
			return a.stage < b.stage
		}
		if a.typ != b.typ {
			return a.typ < b.typ
		}
		return a.action < b.action
	})
	for _, k := range keys {
		p.Agg = append(p.Agg, AggRow{Stage: k.stage, Type: k.typ, Action: k.action, Counts: agg[k]})
	}
}

var risks = []string{
	"Аутентификация есть: Bearer-ключи из PROXY_API_KEYS (sha256 + constant-time сравнение, key_id = HMAC). Ключи статические: ротация через рестарт, нет per-key лимитов и отзыва без рестарта.",
	"TLS у прокси нет. По умолчанию listen только loopback; адрес не на loopback разрешён лишь с proxy.allow_non_loopback: true и требует TLS-терминатора перед прокси, иначе ключи и промпты идут открытым текстом.",
	"Перебор ключей ограничен 10 неудачами за 5 минут с IP (в памяти процесса); распределённый перебор с многих IP этим не останавливается.",
	"Derived-находки (base64/hex/url, склейка) блокируются даже в mask: возможны ложные блокировки легитимных закодированных данных.",
	"Regex-детекторы не ловят обфускацию вне перечисленных представлений (ROT13, слова, реверс, base32, разбиение по запросам).",
	"Output guard не стримит (stream=true → 400) и проверяет ответ целиком; ответы реальной модели недетерминированы.",
	"Rate limit (20/мин по IP) в памяти процесса: сбрасывается при рестарте и не разделяется между инстансами.",
	"Цены облачных моделей в конфиге надо сверять с прайсом провайдера (поле ref).",
}

var funcs = template.FuncMap{
	"ph": HighlightPlaceholders,
}

const reportCSS = `
*{box-sizing:border-box}
body{margin:0;padding:24px;background:#f3f4f6;font:15px/1.5 -apple-system,"Segoe UI","Helvetica Neue",Arial,sans-serif;color:#111827}
.card{background:#fff;border:1px solid #e5e7eb;border-radius:12px;padding:24px 28px;max-width:1140px;margin:0 auto;box-shadow:0 1px 3px rgba(0,0,0,.06)}
h1{font-size:22px;margin:0 0 4px}
h2{font-size:15px;margin:22px 0 6px;color:#374151;text-transform:uppercase;letter-spacing:.04em}
.sub{color:#6b7280;font-size:13px}
.row{display:flex;gap:10px;align-items:center;flex-wrap:wrap;margin:12px 0}
.badge{display:inline-block;padding:3px 10px;border-radius:999px;font-weight:700;font-size:12px;color:#fff;letter-spacing:.02em;white-space:nowrap}
.red{background:#b91c1c}.green{background:#15803d}.gray{background:#4b5563}.amber{background:#b45309}.indigo{background:#4338ca}
.indigo-o{background:none;color:#4338ca;border:2px solid #4338ca;padding:1px 8px}
.fail-b{background:#fee2e2;color:#991b1b;border:2px solid #b91c1c;padding:1px 8px}
.outline{display:inline-block;padding:1px 8px;border-radius:999px;font-weight:700;font-size:12px;background:none;border:2px solid currentColor;white-space:nowrap}
.o-pass{color:#15803d}.o-fail{color:#b91c1c}.o-skip{color:#6b7280}
.colbadge{border:1px solid #e5e7eb;border-radius:10px;padding:8px 12px;flex:1 1 0;min-width:0;max-width:300px;background:#fafafa}
.colbadge b{display:block;font-size:14px;margin-bottom:4px}
.colbadge.stale{border-color:#b45309;background:#fffbeb}.colbadge.failed{border:3px solid #b91c1c;background:#fee2e2}
table{border-collapse:collapse;width:100%;font-size:13px}
caption{text-align:left;color:#6b7280;font-size:12px;padding-bottom:6px;caption-side:top}
th,td{border:1px solid #e5e7eb;padding:6px 8px;text-align:left;vertical-align:top}
thead th{background:#f9fafb}
tr.group th{background:#eef2ff;color:#312e81;font-size:12px;text-transform:uppercase;letter-spacing:.03em}
th[scope=row] .exp{display:block;color:#6b7280;font-size:12px;font-weight:400}
td.cell .b{display:flex;gap:4px;flex-wrap:wrap;margin-bottom:4px}
td.cell .badge,td.cell .outline{font-size:11px;padding:1px 7px}
td.cell .indigo-o,td.cell .fail-b{padding:0 6px}
table.matrix th[scope=row]{width:26%}
td.cell.o-fail-cell{border:3px solid #b91c1c;background:#fee2e2}
td.cell.na{background:#f3f4f6}
td.cell code,.chips code{display:inline-block;background:#e0e7ff;color:#3730a3;padding:0 4px;border-radius:4px;font-size:11px;margin:2px 2px 0 0}
.counts td,.counts th{text-align:center}.counts td:first-child,.counts th:first-child{text-align:left}
.counts tr.total td{font-weight:700;background:#f9fafb}
.bad{color:#b91c1c;font-weight:700}.good{color:#15803d;font-weight:700}
.cat{display:inline-block;padding:0 6px;border-radius:4px;font-size:11px;font-weight:700;background:#e5e7eb;color:#374151;margin-right:4px}
.cat.new{background:#4338ca;color:#fff}
.extraline{font-size:13px;margin-top:10px}
.cols2{display:grid;grid-template-columns:1fr 1fr;gap:18px}
.cols2 ul{margin:0;padding-left:18px;font-size:13px}.cols2 li{margin-bottom:6px}
figure{margin:14px 0;border:1px solid #e5e7eb;border-radius:10px;padding:10px 12px;background:#fafafa}
figcaption{font-size:13px;margin-bottom:6px}figcaption b{display:block;font-size:14px}
pre.log{background:#0f172a;color:#e2e8f0;padding:12px 14px;border-radius:8px;white-space:pre-wrap;word-break:break-word;font:12px/1.45 "SF Mono",Menlo,Consolas,monospace;margin:6px 0 0}
mark.ph{background:#fde68a;color:#78350f;border-radius:3px;padding:0 2px}
details summary{cursor:pointer;color:#1d4ed8;font-size:13px}
.formula{background:#ecfdf5;border-left:4px solid #15803d;border-radius:6px;padding:8px 12px;font-size:13px;margin-top:8px}
.warn{background:#fffbeb;border-left:4px solid #b45309;border-radius:6px;padding:8px 12px;font-size:13px;margin-top:8px;color:#78350f}
ul.risks li{margin-bottom:4px;font-size:13px}
`

const csp = `<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'">`

const reportParts = `
{{define "head"}}<!doctype html>
<html lang="ru"><head><meta charset="utf-8">` + csp + `<title>{{.Title}}</title><style>` + reportCSS + `</style></head><body><div class="card">{{end}}

{{define "header"}}<h1>{{.Title}}</h1>
<div class="sub">Модель: {{if .Model}}{{.Model}}{{else}}—{{end}} · сгенерировано {{.Generated}}</div>
{{range .Runs}}<div class="sub"><b>{{.Upstream}}</b>: {{if .Present}}{{if .BaseURL}}base_url <code>{{.BaseURL}}</code> · {{end}}<code>{{.Dir}}</code> · запуск {{.Started}} · git <code>{{if .GitSHA}}{{.GitSHA}}{{else}}—{{end}}</code> · cases <code>{{if .CasesSHA}}{{.CasesSHA}}{{else}}—{{end}}</code> · схема v{{.Schema}}{{if .Stale}} · <span class="bad">STALE: {{.StaleReason}}</span>{{end}}{{if .Failed}} · <span class="bad">FAILED (go test ≠ 0)</span>{{end}}{{else}}нет прогона{{end}}</div>
{{end}}{{end}}

{{define "badges"}}<div class="row">{{range .Cols}}<div class="colbadge{{if .Failed}} failed{{else if .Stale}} stale{{end}}"><b>{{.Label}}</b>{{if not .Present}}<span class="badge gray">— нет прогона</span>{{else}}{{if .Fail}}<span class="badge red">✕ ПРОВАЛ {{.Fail}}</span>{{else}}<span class="badge green">✓ PASS {{.Pass}}/{{.Total}}</span>{{end}} {{if .Gap}}<span class="badge amber">! GAP {{.Gap}}</span>{{end}} {{if .NA}}<span class="badge gray">N/A {{.NA}}</span>{{end}}{{if .Stale}}<div class="sub bad">STALE · {{.StaleReason}}</div>{{end}}{{if .Failed}}<div class="sub bad">FAILED · go test ≠ 0</div>{{end}}{{end}}</div>{{end}}</div>{{end}}

{{define "counts"}}<h2 id="counts">Счётчики</h2>
<table class="counts"><caption>PASS — ожидание совпало · KNOWN GAP — известный пропуск · ПРОВАЛ — FAIL или UNEXPECTED_CATCH · N/A — кейс требует fake upstream</caption>
<thead><tr><th scope="col" rowspan="2">Группа</th>{{range .Cols}}<th scope="colgroup" colspan="4">{{.Label}}</th>{{end}}</tr>
<tr>{{range .Cols}}<th scope="col">PASS</th><th scope="col">GAP</th><th scope="col">ПРОВАЛ</th><th scope="col">N/A</th>{{end}}</tr></thead>
<tbody>{{range .Counts}}<tr{{if .Total}} class="total"{{end}}><td>{{.Title}}</td>{{range .Cells}}{{if .Present}}<td class="good">{{.Pass}}</td><td>{{.Gap}}</td><td{{if .Fail}} class="bad"{{end}}>{{.Fail}}</td><td>{{.NA}}</td>{{else}}<td>—</td><td>—</td><td>—</td><td>—</td>{{end}}{{end}}</tr>
{{end}}</tbody></table>{{end}}

{{define "matrix"}}<h2 id="matrix">Матрица кейсов</h2>
<table class="matrix"><caption>Залитый бейдж — исход: <span class="badge red">✕ ПОЙМАН · BLOCK</span> <span class="badge indigo">◐ ЗАМАСКИРОВАН</span> <span class="badge indigo-o">◐ OUTPUT REDACTED</span> <span class="badge red">✕ OUTPUT BLOCKED</span> <span class="badge green">✓ PASS · FP-контроль / чисто</span> <span class="badge amber">! KNOWN GAP</span> <span class="badge fail-b">⚠ ПРОВАЛ / UNEXPECTED_CATCH</span> <span class="badge gray">N/A · требует fake</span>. Контурный — проверка ожидания: <span class="outline o-pass">✓ PASS</span> <span class="outline o-fail">✕ FAIL</span>. <code>type=n</code> — находки guard. На реальных моделях (не fake) исход считается по input guard.</caption>
<thead><tr><th scope="col">Кейс</th>{{range .Cols}}<th scope="col">{{.Label}}</th>{{end}}</tr></thead>
<tbody>{{range .Groups}}<tr class="group"><th scope="rowgroup" colspan="{{$.NCols}}">{{.Title}}</th></tr>
{{range .Rows}}<tr><th scope="row">{{.ID}} {{.Title}}<span class="exp">{{.Expect}}</span></th>
{{range .Cells}}<td id="{{.Anchor}}" class="cell{{if .Fail}} o-fail-cell{{end}}{{if .NA}} na{{end}}">{{if .Present}}<div class="b"><span class="badge {{.BadgeClass}}">{{.Badge}}</span>{{if .Outline}}<span class="outline {{.OutlineClass}}">{{.Outline}}</span>{{end}}</div>{{range .Codes}}<code>{{.}}</code>{{end}}{{if .ShowCalls}}<div class="sub">upstream: {{if .UpstreamCalls}}да ({{.UpstreamCalls}}){{else}}нет{{end}}</div>{{end}}{{range .Problems}}<div class="sub bad">{{.}}</div>{{end}}{{else}}—{{end}}</td>{{end}}</tr>
{{end}}{{end}}</tbody></table>{{end}}

{{define "extraline"}}<div class="extraline"><b>Extra-тесты:</b> {{range $i, $l := .ExtraLine}}{{if $i}} · {{end}}<b>{{$l.Upstream}}</b> {{if $l.Present}}{{if $l.Failed}}<span class="bad">✕ {{$l.Passed}} PASS, {{$l.Failed}} FAIL</span>{{else}}<span class="good">✓ {{$l.Passed}} PASS</span>{{end}}{{if $l.Skp}} <span class="sub">({{$l.Skp}} skip)</span>{{end}} <span class="chips">{{range $l.Cats}}<span class="cat{{if .New}} new{{end}}">{{.Label}}{{if .New}} NEW{{end}} {{.Passed}}/{{.Total}}</span>{{end}}</span>{{else}}<span class="sub">нет прогона</span>{{end}}{{end}}</div>{{end}}
`

var fullTmpl = template.Must(template.New("full").Funcs(funcs).Parse(`{{template "head" .}}
{{template "header" .}}
{{template "badges" .}}
{{template "counts" .}}
{{template "matrix" .}}

<h2 id="extra">Extra-тесты</h2>
<table><caption>Сценарии вне матрицы. Метки: <span class="cat new">auth NEW</span> авторизация · <span class="cat">rate</span> лимиты · <span class="cat">cost</span> стоимость · <span class="cat">audit</span> аудит · <span class="cat">api</span> контракт API</caption>
<thead><tr><th scope="col">Тест</th>{{range .ExtraUps}}<th scope="col">{{.}}</th>{{end}}<th scope="col">Детали</th></tr></thead>
<tbody>{{range .Extras}}<tr><th scope="row"><span class="cat{{if .New}} new{{end}}">{{.CatLabel}}{{if .New}} NEW{{end}}</span>{{.Name}}</th>{{range .Cells}}<td><span class="outline {{.Class}}">{{.Status}}</span></td>{{end}}<td>{{.Details}}</td></tr>
{{end}}</tbody></table>

<h2 id="caught">Что поймали / что пропустили</h2>
<div class="cols2"><div><b class="good">Поймали</b><ul>{{range .Caught}}<li><b>{{.ID}}</b> {{.Title}}<div class="sub">{{range $i, $w := .Where}}{{if $i}} · {{end}}{{$w}}{{end}}</div></li>{{else}}<li>нет</li>{{end}}</ul></div>
<div><b class="bad">Пропустили</b><ul>{{range .Missed}}<li><b>{{.ID}}</b> {{.Title}}<div class="sub">{{range $i, $w := .Where}}{{if $i}} · {{end}}{{$w}}{{end}}</div>{{if .Note}}<div class="sub">{{.Note}}</div>{{end}}</li>{{end}}{{range .NotCovered}}<li class="sub">Не покрыто: {{.}}</li>{{end}}</ul></div></div>

<h2 id="logs">Примеры логов перехваченных запросов</h2>
<div class="sub">Записи аудита (JSONL) как есть: только *_masked поля, findings с type/view/fingerprint (HMAC). Плейсхолдеры подсвечены.</div>
{{range .Examples}}<figure id="log-{{.Key}}"><figcaption><b>{{.Title}}</b>{{.Caption}} · <span class="sub">{{.Source}}</span></figcaption>
{{if .Long}}<details open><summary>запись аудита</summary><pre class="log">{{ph .JSON}}</pre></details>{{else}}<pre class="log">{{ph .JSON}}</pre>{{end}}</figure>
{{else}}<div class="sub">нет записей аудита</div>{{end}}

<h2 id="audit">Аудит: stage × type × action</h2>
<table><caption>Число находок в audit-mask.jsonl + audit-block.jsonl каждого прогона</caption>
<thead><tr><th scope="col">Stage</th><th scope="col">Type</th><th scope="col">Action</th>{{range .ExtraUps}}<th scope="col">{{.}}</th>{{end}}</tr></thead>
<tbody>{{range .Agg}}<tr><td>{{.Stage}}</td><td>{{.Type}}</td><td>{{.Action}}</td>{{range .Counts}}<td>{{.}}</td>{{end}}</tr>{{end}}</tbody></table>

<h2 id="cost">Токены и стоимость</h2>
<table><caption>Все записи аудита прогона (кейсы и extra). Заблокированные на входе запросы не отправляются в модель: usage.source=none, стоимость 0</caption>
<thead><tr><th scope="col">Upstream</th><th scope="col">Записей</th><th scope="col">Prompt</th><th scope="col">Completion</th><th scope="col">Total</th><th scope="col">Cost USD</th><th scope="col">usage: upstream / estimated / none</th></tr></thead>
<tbody>{{range .Tokens}}<tr><th scope="row">{{.Upstream}}</th>{{if .Records}}<td>{{.Records}}</td><td>{{.Prompt}}</td><td>{{.Completion}}</td><td>{{.Total}}</td><td>${{.CostUSD}}</td><td>{{.SrcUpstream}} / {{.SrcEstimated}} / {{.Src}}</td>{{else}}<td colspan="6">—</td>{{end}}</tr>{{end}}</tbody></table>
<div class="formula"><b>$0.000120</b> = 100 prompt × 400000 µ$/1M + 50 completion × 1600000 µ$/1M = 40 + 80 = 120 µ$ (int64 micro-USD, округление вверх). Проверка TestCostTracking (fake): {{if .Cost.Present}}{{if .Cost.Passed}}<span class="good">✓ PASS</span>{{else}}<span class="bad">✕ FAIL</span>{{end}} — {{.Cost.Details}}{{else}}<span class="sub">нет fake-прогона</span>{{end}}</div>

<h2 id="risks">Риски</h2>
<ul class="risks">{{range .Risks}}<li>{{.}}</li>{{end}}</ul>
</div></body></html>
` + reportParts))

var matrixTmpl = template.Must(template.New("matrix-page").Funcs(funcs).Parse(`{{template "head" .}}
{{template "header" .}}
{{template "badges" .}}
{{template "counts" .}}
{{template "matrix" .}}
{{template "extraline" .}}
{{if .FullHref}}<div class="sub" style="margin-top:10px">Extra-тесты, логи перехваченных запросов, аудит, стоимость и риски — {{.FullHref}}.</div>{{end}}
</div></body></html>
` + reportParts))

// RenderFull renders the full report page.
func RenderFull(p Page) (string, error) { return render(fullTmpl, p) }

// RenderMatrix renders the compact page used for the PNG.
func RenderMatrix(p Page) (string, error) { return render(matrixTmpl, p) }

func render(t *template.Template, p Page) (string, error) {
	var b bytes.Buffer
	if err := t.Execute(&b, p); err != nil {
		return "", err
	}
	return SanitizePaths(b.String()), nil
}

// MatrixHeight estimates the PNG height of the matrix page with a margin
// (too tall only leaves background at the bottom; too short cuts rows).
// With four columns a cell is ~200px wide, so badges and finding codes wrap
// more often than with two.
func MatrixHeight(p Page) int {
	rowH := 80
	if len(p.Cols) > 2 {
		rowH = 100
	}
	h := 560 + len(p.Groups)*40 + len(p.Runs)*44 + 200
	for _, g := range p.Groups {
		for _, r := range g.Rows {
			extra := 0
			for _, c := range r.Cells {
				if n := (len(c.Codes) - 1) * 18; n > extra && len(p.Cols) > 2 {
					extra = n
				}
			}
			h += rowH + extra
		}
	}
	if h < 900 {
		h = 900
	}
	if h > 8000 {
		h = 8000
	}
	return h
}
