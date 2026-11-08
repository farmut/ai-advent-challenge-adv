// Package auditreport aggregates proxy audit logs (JSONL, schema v1 and v2)
// into a markdown or JSON report. It is used by cmd/audit-report and the e2e suite.
package auditreport

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agile-consultant/internal/ports"
)

// ContextRunes is the context width on each side of a placeholder.
const ContextRunes = 30

// Read loads records from paths; each path may be a glob.
func Read(paths []string) ([]ports.AuditRecord, []string, error) {
	var files []string
	for _, p := range paths {
		m, err := filepath.Glob(p)
		if err != nil {
			return nil, nil, fmt.Errorf("glob %s: %w", p, err)
		}
		if len(m) == 0 {
			return nil, nil, fmt.Errorf("no audit files match %s", p)
		}
		files = append(files, m...)
	}
	sort.Strings(files)
	var recs []ports.AuditRecord
	for _, f := range files {
		r, err := readFile(f)
		if err != nil {
			return nil, nil, err
		}
		recs = append(recs, r...)
	}
	return recs, files, nil
}

func readFile(path string) ([]ports.AuditRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []ports.AuditRecord
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16<<20)
	line := 0
	for sc.Scan() {
		line++
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var r ports.AuditRecord
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, line, err)
		}
		if r.V < 1 || r.V > ports.AuditVersion {
			return nil, fmt.Errorf("%s:%d: unsupported audit schema v%d (supported v1..v%d)", path, line, r.V, ports.AuditVersion)
		}
		out = append(out, r)
	}
	return out, sc.Err()
}

// Count is a named counter.
type Count struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// Tokens summarises usage and cost.
type Tokens struct {
	Prompt         int     `json:"prompt_tokens"`
	Completion     int     `json:"completion_tokens"`
	Total          int     `json:"total_tokens"`
	Upstream       int     `json:"records_upstream"`
	Estimated      int     `json:"records_estimated"`
	None           int     `json:"records_none"`
	EstimatedShare float64 `json:"estimated_share"`
	CostMicroUSD   int64   `json:"cost_micro_usd"`
	CostUSD        string  `json:"cost_usd"`
	ByModel        []Cost  `json:"by_model"`
}

// Cost is per-model usage.
type Cost struct {
	Model        string `json:"model"`
	Requests     int    `json:"requests"`
	Tokens       int    `json:"tokens"`
	CostMicroUSD int64  `json:"cost_micro_usd"`
	CostUSD      string `json:"cost_usd"`
	PriceRef     string `json:"price_ref"`
}

// FindingAgg counts findings by stage, type and action.
type FindingAgg struct {
	Stage  string `json:"stage"`
	Type   string `json:"type"`
	Action string `json:"action"`
	Count  int    `json:"count"`
}

// Intercept is one intercepted secret.
type Intercept struct {
	TS          time.Time `json:"ts"`
	RequestID   string    `json:"request_id"`
	Stage       string    `json:"stage"`
	Type        string    `json:"type"`
	View        string    `json:"view"`
	Action      string    `json:"action"`
	Derived     bool      `json:"derived"`
	Fingerprint string    `json:"fingerprint"`
	Context     string    `json:"context"`
}

// Repeat is a fingerprint seen in more than one request.
type Repeat struct {
	Fingerprint string   `json:"fingerprint"`
	Type        string   `json:"type"`
	Requests    []string `json:"requests"`
}

// Report is the aggregate.
type Report struct {
	Files    []string `json:"files"`
	Records  int      `json:"records"`
	ByStatus []Count  `json:"by_status"`
	ByModel  []Count  `json:"by_model"`
	// ByVersion counts records per schema version ("v1", "v2").
	ByVersion []Count `json:"by_version"`
	// ByKeyID counts authenticated requests per proxy key id (v2).
	ByKeyID []Count `json:"by_key_id"`
	// Unauthorized counts refused requests per reason (status
	// unauthorized, and rate_limited with reason auth_fail_limit).
	Unauthorized []Count      `json:"unauthorized"`
	Tokens       Tokens       `json:"tokens"`
	Findings     []FindingAgg `json:"findings"`
	Intercepted  []Intercept  `json:"intercepted"`
	Repeats      []Repeat     `json:"repeats"`
}

// Build aggregates records.
func Build(recs []ports.AuditRecord, files []string) Report {
	r := Report{Files: files, Records: len(recs)}
	status, model := map[string]int{}, map[string]int{}
	versions, keyIDs, unauth := map[string]int{}, map[string]int{}, map[string]int{}
	costs := map[string]*Cost{}
	fagg := map[FindingAgg]int{}
	type rep struct {
		typ  string
		reqs map[string]bool
	}
	repeats := map[string]*rep{}
	for _, rec := range recs {
		status[rec.Status]++
		versions[fmt.Sprintf("v%d", rec.V)]++
		if rec.KeyID != "" {
			keyIDs[rec.KeyID]++
		}
		if rec.Status == ports.AuditStatusUnauthorized || rec.Reason == ports.AuthReasonFailLimit {
			unauth[rec.Status+"/"+rec.Reason]++
			continue // refused before parsing: no model, prompt or usage
		}
		model[rec.Model]++
		r.Tokens.Prompt += rec.Usage.PromptTokens
		r.Tokens.Completion += rec.Usage.CompletionTokens
		r.Tokens.Total += rec.Usage.TotalTokens
		switch rec.Usage.Source {
		case ports.UsageSourceUpstream:
			r.Tokens.Upstream++
		case ports.UsageSourceEstimated:
			r.Tokens.Estimated++
		default:
			r.Tokens.None++
		}
		r.Tokens.CostMicroUSD += rec.CostMicroUSD
		c := costs[rec.Model]
		if c == nil {
			c = &Cost{Model: rec.Model}
			costs[rec.Model] = c
		}
		c.Requests++
		c.Tokens += rec.Usage.TotalTokens
		c.CostMicroUSD += rec.CostMicroUSD
		if rec.PriceRef != "" {
			c.PriceRef = rec.PriceRef
		}

		promptText := joinPrompt(rec.PromptMasked)
		stages := []struct {
			name string
			fs   []ports.AuditFinding
			text string
		}{{"input", rec.Input.Findings, promptText}, {"output", rec.Output.Findings, rec.ResponseMasked}}
		for _, st := range stages {
			for _, f := range st.fs {
				fagg[FindingAgg{Stage: st.name, Type: f.Type, Action: f.Action}] += f.Count
				ctx := contextAround(st.text, f.Placeholder)
				for _, fp := range f.Fingerprints {
					r.Intercepted = append(r.Intercepted, Intercept{TS: rec.TS, RequestID: rec.RequestID, Stage: st.name,
						Type: f.Type, View: f.View, Action: f.Action, Derived: f.Derived, Fingerprint: fp, Context: ctx})
					k := f.Type + "/" + fp
					if repeats[k] == nil {
						repeats[k] = &rep{typ: f.Type, reqs: map[string]bool{}}
					}
					repeats[k].reqs[rec.RequestID] = true
				}
			}
		}
	}
	if used := r.Tokens.Upstream + r.Tokens.Estimated; used > 0 {
		r.Tokens.EstimatedShare = float64(r.Tokens.Estimated) / float64(used)
	}
	r.Tokens.CostUSD = formatUSD(r.Tokens.CostMicroUSD)
	for _, c := range costs {
		c.CostUSD = formatUSD(c.CostMicroUSD)
		r.Tokens.ByModel = append(r.Tokens.ByModel, *c)
	}
	sort.Slice(r.Tokens.ByModel, func(i, j int) bool { return r.Tokens.ByModel[i].Model < r.Tokens.ByModel[j].Model })
	r.ByStatus, r.ByModel = counts(status), counts(model)
	r.ByVersion, r.ByKeyID, r.Unauthorized = counts(versions), counts(keyIDs), counts(unauth)
	for k, n := range fagg {
		k.Count = n
		r.Findings = append(r.Findings, k)
	}
	sort.Slice(r.Findings, func(i, j int) bool {
		a, b := r.Findings[i], r.Findings[j]
		if a.Stage != b.Stage {
			return a.Stage < b.Stage
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		return a.Action < b.Action
	})
	for k, v := range repeats {
		if len(v.reqs) < 2 {
			continue
		}
		rp := Repeat{Fingerprint: k[strings.IndexByte(k, '/')+1:], Type: v.typ}
		for id := range v.reqs {
			rp.Requests = append(rp.Requests, id)
		}
		sort.Strings(rp.Requests)
		r.Repeats = append(r.Repeats, rp)
	}
	sort.Slice(r.Repeats, func(i, j int) bool {
		if len(r.Repeats[i].Requests) != len(r.Repeats[j].Requests) {
			return len(r.Repeats[i].Requests) > len(r.Repeats[j].Requests)
		}
		return r.Repeats[i].Fingerprint < r.Repeats[j].Fingerprint
	})
	return r
}

func joinPrompt(ms []ports.ChatMessage) string {
	parts := make([]string, 0, len(ms))
	for _, m := range ms {
		parts = append(parts, m.Role+": "+m.Content)
	}
	return strings.Join(parts, " | ")
}

// contextAround returns ±ContextRunes runes around the first placeholder
// occurrence; the text is already masked, so the context holds no secrets.
func contextAround(text, placeholder string) string {
	if placeholder == "" {
		return ""
	}
	i := strings.Index(text, placeholder)
	if i < 0 {
		return ""
	}
	before := []rune(text[:i])
	after := []rune(text[i+len(placeholder):])
	if len(before) > ContextRunes {
		before = append([]rune("…"), before[len(before)-ContextRunes:]...)
	}
	if len(after) > ContextRunes {
		after = append(after[:ContextRunes], []rune("…")...)
	}
	return string(before) + placeholder + string(after)
}

func counts(m map[string]int) []Count {
	out := make([]Count, 0, len(m))
	for k, v := range m {
		out = append(out, Count{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func formatUSD(micro int64) string {
	return fmt.Sprintf("%d.%06d", micro/1_000_000, micro%1_000_000)
}

// Cell escapes a markdown table cell.
func Cell(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ⏎ ")
	s = strings.ReplaceAll(s, "|", "\\|")
	if s == "" {
		return "—"
	}
	return s
}

// Markdown renders the report sections 1-5.
func Markdown(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Audit report\n\nRecords: **%d** from %d file(s).\n\n", r.Records, len(r.Files))
	b.WriteString(MarkdownBody(r, 2))
	return b.String()
}

// MarkdownBody renders the sections with headings of the given level.
func MarkdownBody(r Report, level int) string {
	h := strings.Repeat("#", level)
	var b strings.Builder
	fmt.Fprintf(&b, "%s 1. Итоги\n\n| Status | Count |\n|---|---|\n", h)
	for _, c := range r.ByStatus {
		fmt.Fprintf(&b, "| %s | %d |\n", Cell(c.Key), c.Count)
	}
	b.WriteString("\n| Model | Count |\n|---|---|\n")
	for _, c := range r.ByModel {
		fmt.Fprintf(&b, "| %s | %d |\n", Cell(c.Key), c.Count)
	}
	b.WriteString("\n| Schema | Count |\n|---|---|\n")
	for _, c := range r.ByVersion {
		fmt.Fprintf(&b, "| %s | %d |\n", Cell(c.Key), c.Count)
	}
	if len(r.ByKeyID) > 0 {
		b.WriteString("\n| key_id | Requests |\n|---|---|\n")
		for _, c := range r.ByKeyID {
			fmt.Fprintf(&b, "| `%s` | %d |\n", c.Key, c.Count)
		}
	}
	if len(r.Unauthorized) > 0 {
		b.WriteString("\n| Отказ auth (status/reason) | Count |\n|---|---|\n")
		for _, c := range r.Unauthorized {
			fmt.Fprintf(&b, "| %s | %d |\n", Cell(c.Key), c.Count)
		}
	}
	t := r.Tokens
	fmt.Fprintf(&b, "\n%s 2. Токены и стоимость\n\nPrompt: %d, completion: %d, total: %d. Cost: **$%s**. Usage source: upstream %d, estimated %d, none %d (доля estimated среди отправленных: %.0f%%).\n\n",
		h, t.Prompt, t.Completion, t.Total, t.CostUSD, t.Upstream, t.Estimated, t.None, t.EstimatedShare*100)
	b.WriteString("| Model | Requests | Tokens | Cost USD | Price ref |\n|---|---|---|---|---|\n")
	for _, c := range t.ByModel {
		fmt.Fprintf(&b, "| %s | %d | %d | %s | %s |\n", Cell(c.Model), c.Requests, c.Tokens, c.CostUSD, Cell(c.PriceRef))
	}
	fmt.Fprintf(&b, "\n%s 3. Находки по типам\n\n| Stage | Type | Action | Count |\n|---|---|---|---|\n", h)
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "| %s | %s | %s | %d |\n", f.Stage, Cell(f.Type), Cell(f.Action), f.Count)
	}
	fmt.Fprintf(&b, "\n%s 4. Перехваченные секреты\n\n| ts | request_id | stage | type | view | action | fingerprint | контекст ±%d рун |\n|---|---|---|---|---|---|---|---|\n", h, ContextRunes)
	for _, i := range r.Intercepted {
		view := i.View
		if i.Derived {
			view += " (derived)"
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | `%s` | %s |\n", i.TS.UTC().Format(time.RFC3339), Cell(i.RequestID), i.Stage,
			Cell(i.Type), Cell(view), Cell(i.Action), i.Fingerprint, Cell(i.Context))
	}
	fmt.Fprintf(&b, "\n%s 5. Повторы fingerprint\n\n", h)
	if len(r.Repeats) == 0 {
		b.WriteString("Повторов нет.\n")
	} else {
		b.WriteString("| fingerprint | type | requests |\n|---|---|---|\n")
		for _, rp := range r.Repeats {
			fmt.Fprintf(&b, "| `%s` | %s | %d: %s |\n", rp.Fingerprint, Cell(rp.Type), len(rp.Requests), Cell(strings.Join(rp.Requests, ", ")))
		}
	}
	return b.String()
}
