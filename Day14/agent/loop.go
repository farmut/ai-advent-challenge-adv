package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/policy"
	"ai-adv-agent/internal/usecase/profilemap"
)

// maxGoalFileBytes caps a @file.md loop goal: the file is replayed into the
// planner prompt, so it must stay a goal, not a dump.
const maxGoalFileBytes = 64 << 10

// parseLoopArgs splits a /loop line into a subcommand and its argument. The
// subcommand is the second word when it is one of status/stop/resume (either
// language); otherwise the whole rest of the line is a goal (sub "goal").
// A bare "/loop" yields ("", ""). The goal argument keeps its original case;
// only the subcommand word is lower-cased for matching.
func parseLoopArgs(line string) (sub, arg string) {
	rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "/"))
	// The command word itself ("loop" or "цикл") is the first field.
	_, rest, _ = strings.Cut(rest, " ")
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", ""
	}
	word := strings.ToLower(strings.Fields(rest)[0])
	switch word {
	case "status", "статус":
		return "status", strings.TrimSpace(strings.TrimPrefix(rest, strings.Fields(rest)[0]))
	case "stop", "стоп":
		return "stop", strings.TrimSpace(strings.TrimPrefix(rest, strings.Fields(rest)[0]))
	case "resume", "продолжить":
		return "resume", strings.TrimSpace(strings.TrimPrefix(rest, strings.Fields(rest)[0]))
	}
	return "goal", rest
}

// goalFromArg turns a /loop argument into a goal. An argument starting with
// "@" is a goal file: read through the policy check (inside the read root,
// not deny-listed), at most maxGoalFileBytes, and its "- "/"* " bullets
// become the goal roots. A plain argument is the goal text itself.
func goalFromArg(arg string) (domain.Goal, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return domain.Goal{}, fmt.Errorf("цель пуста")
	}
	if !strings.HasPrefix(arg, "@") {
		return domain.Goal{Text: arg}, nil
	}
	return readGoalFile(strings.TrimPrefix(arg, "@"))
}

// readGoalFile loads a markdown goal file. The path is approved against a
// policy.FS whose read root is the working directory (with the default deny
// globs), so a goal can never smuggle in .env or .git contents. The file
// text becomes Goal.Text and every "- "/"* " bullet becomes a Goal.Root; a
// file without bullets is rejected — a plan needs concrete roots.
func readGoalFile(path string) (domain.Goal, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return domain.Goal{}, fmt.Errorf("рабочий каталог: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return domain.Goal{}, fmt.Errorf("цель %q: %w", path, err)
	}
	rel, err := filepath.Rel(cwd, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return domain.Goal{}, fmt.Errorf("цель %q вне рабочего каталога %q", path, cwd)
	}
	fs := policy.FS{ReadRoot: cwd, Deny: policy.DefaultDenyGlobs()}
	if fs.Denied(rel) {
		return domain.Goal{}, fmt.Errorf("цель %q запрещена политикой чтения", path)
	}
	f, err := os.Open(abs)
	if err != nil {
		return domain.Goal{}, fmt.Errorf("цель %q: %w", path, err)
	}
	defer f.Close()
	buf := make([]byte, maxGoalFileBytes+1)
	n, _ := f.Read(buf)
	if n > maxGoalFileBytes {
		return domain.Goal{}, fmt.Errorf("цель %q больше %d КиБ", path, maxGoalFileBytes>>10)
	}
	text := strings.TrimSpace(string(buf[:n]))
	if text == "" {
		return domain.Goal{}, fmt.Errorf("цель %q пуста", path)
	}
	var roots []string
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if rest, ok := strings.CutPrefix(ln, "- "); ok {
			roots = append(roots, strings.TrimSpace(rest))
		} else if rest, ok := strings.CutPrefix(ln, "* "); ok {
			roots = append(roots, strings.TrimSpace(rest))
		}
	}
	if len(roots) == 0 {
		return domain.Goal{}, fmt.Errorf("цель %q: нет пунктов «- …» или «* …» — добавьте корни цели списком", path)
	}
	return domain.Goal{Text: text, Roots: roots}, nil
}

// loopHeader is the live progress line of a running loop, shown in the TUI
// header while loopRunning is set.
type loopHeader struct {
	cur, total int    // current subtask (1-based) and total subtasks
	profile    string // profile of the current subtask
	secIter    int    // security iteration of the current attempt
	secMax     int    // configured security iterations cap
	tokens     int64  // tokens spent by the run
	cost       float64
	costMax    float64
}

// String renders the header line: "LOOP 3/7 · coder · sec-iter 2/3 · 412k tok · $0.31/1.00".
func (h loopHeader) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "LOOP %d/%d", h.cur, h.total)
	if h.profile != "" {
		fmt.Fprintf(&b, " · %s", h.profile)
	}
	if h.secIter > 0 {
		fmt.Fprintf(&b, " · sec-iter %d/%d", h.secIter, h.secMax)
	}
	if h.tokens > 0 {
		fmt.Fprintf(&b, " · %s tok", compactNum(h.tokens))
	}
	if h.costMax > 0 {
		fmt.Fprintf(&b, " · $%.2f/%.2f", h.cost, h.costMax)
	}
	return b.String()
}

// headerFromRun rebuilds the live header from the persisted run state.
// cur clamps into [1, total] so the line stays sane during planning (0/0).
func headerFromRun(run domain.LoopRun, secMax int) loopHeader {
	h := loopHeader{
		total:   len(run.Subtasks),
		secMax:  secMax,
		tokens:  run.Usage.Tokens,
		cost:    run.Usage.CostUSD,
		costMax: run.Budget.MaxCostUSD,
	}
	if h.total > 0 {
		h.cur = run.CurrentIdx + 1
		if h.cur < 1 {
			h.cur = 1
		}
		if h.cur > h.total {
			h.cur = h.total
		}
		st := run.Subtasks[h.cur-1]
		h.profile = st.Profile
		if n := len(st.Attempts); n > 0 {
			h.secIter = st.Attempts[n-1].SecurityIteration
		}
	}
	return h
}

// compactNum formats a token count the way the header shows it: 950 → "950",
// 412_000 → "412k", 1_500_000 → "1.5M".
func compactNum(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%dk", n/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// tuiLoopEvents adapts execloop.Events to the TUI: stage/security lines go
// to the log channel and every event refreshes the live header from the
// persisted run. All text is plain (the log pipeline escapes markup itself).
type tuiLoopEvents struct {
	loop   execloopStatus // source of the current run state
	secMax int
	logCh  chan<- string
	hdrCh  chan<- loopHeader
}

// execloopStatus is the subset of *execloop.Loop the events adapter needs;
// a direct *execloop.Loop satisfies it (set after construction — the loop
// needs the events at build time, the adapter needs the loop to refresh).
type execloopStatus interface {
	Status() (domain.LoopRun, bool, error)
}

// Stage logs a subtask stage change and refreshes the header.
func (e tuiLoopEvents) Stage(taskID string, to domain.SubtaskStatus, msg string) {
	e.logCh <- fmt.Sprintf("[loop] %s → %s: %s", taskID, to, msg)
	e.refresh()
}

// Findings logs a security-review result. Blocking severities are named in
// the line ("1 Critical, 2 High") so the renderer can paint the whole row
// red, and the header is refreshed.
func (e tuiLoopEvents) Findings(taskID string, fs []domain.Finding) {
	bySev := map[domain.Severity]int{}
	for _, f := range fs {
		bySev[f.Severity]++
	}
	var parts []string
	for _, sev := range []domain.Severity{domain.SeverityCritical, domain.SeverityHigh, domain.SeverityMedium, domain.SeverityLow} {
		if n := bySev[sev]; n > 0 {
			s := string(sev)
			parts = append(parts, fmt.Sprintf("%d %s%s", n, strings.ToUpper(s[:1]), s[1:]))
		}
	}
	detail := "без находок"
	if len(parts) > 0 {
		detail = strings.Join(parts, ", ")
	}
	e.logCh <- fmt.Sprintf("[security] %s: %d находок (%s)", taskID, len(fs), detail)
	e.refresh()
}

// Budget refreshes the header on the meter's cadence.
func (e tuiLoopEvents) Budget(domain.BudgetUsage) { e.refresh() }

func (e tuiLoopEvents) refresh() {
	run, active, err := e.loop.Status()
	if err != nil || !active {
		return
	}
	e.hdrCh <- headerFromRun(run, e.secMax)
}

// renderFindings formats the findings of the last attempt per subtask and the
// run totals for /findings. Earlier-attempt blocking findings that vanished
// from the last attempt count as fixed; blocking findings of the last
// attempts remain open; the rest is informational.
func renderFindings(run domain.LoopRun) string {
	var lastByTask []domain.Finding
	seen := map[string]bool{}
	var fixed, remaining, info []domain.Finding
	total := map[domain.Severity]int{}
	for i := range run.Subtasks {
		st := &run.Subtasks[i]
		if len(st.Attempts) == 0 {
			continue
		}
		last := st.Attempts[len(st.Attempts)-1]
		for _, f := range last.Findings {
			lastByTask = append(lastByTask, f)
			total[f.Severity]++
			seen[f.ID+f.File+f.Rule] = true
			if f.Severity.Blocking() {
				remaining = append(remaining, f)
			} else {
				info = append(info, f)
			}
		}
		for _, att := range st.Attempts[:len(st.Attempts)-1] {
			for _, f := range att.Findings {
				total[f.Severity]++
				key := f.ID + f.File + f.Rule
				if f.Severity.Blocking() && !seen[key] {
					fixed = append(fixed, f)
				}
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Находки безопасности запуска %s (статус %s):\n", run.ID, run.Status)
	fmt.Fprintf(&b, "\nИтог: critical %d · high %d · medium %d · low %d · info %d\n",
		total[domain.SeverityCritical], total[domain.SeverityHigh], total[domain.SeverityMedium],
		total[domain.SeverityLow], total[domain.SeverityInfo])
	if len(fixed) > 0 {
		writeFindingGroup(&b, "Исправлены (были, в последней попытке отсутствуют)", fixed)
	}
	if len(remaining) > 0 {
		writeFindingGroup(&b, "Остаются (критичные/высокие в последней попытке)", remaining)
	}
	if len(info) > 0 {
		writeFindingGroup(&b, "Medium/Low в последней попытке", info)
	}
	if len(lastByTask) == 0 && len(fixed) == 0 {
		b.WriteString("\nНаходок нет.\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeFindingGroup(b *strings.Builder, title string, fs []domain.Finding) {
	fmt.Fprintf(b, "\n%s:\n", title)
	for _, f := range fs {
		loc := f.File
		if f.Line > 0 {
			loc = fmt.Sprintf("%s:%d", f.File, f.Line)
		}
		cwe := ""
		if f.CWE != "" {
			cwe = " (" + f.CWE + ")"
		}
		fmt.Fprintf(b, "  • [%s] %s%s: %s", f.Severity, loc, cwe, f.Issue)
		if f.Fix != "" {
			fmt.Fprintf(b, " — исправление: %s", f.Fix)
		}
		b.WriteString("\n")
	}
}

// renderProfileList formats the /profile catalogue: one "name — description"
// line per profile.
func renderProfileList(profiles []domain.AgentProfile) string {
	if len(profiles) == 0 {
		return "Каталог профилей пуст."
	}
	var b strings.Builder
	b.WriteString("Профили:\n")
	for _, p := range profiles {
		desc := strings.TrimSpace(p.Description)
		if desc == "" {
			desc = "(без описания)"
		}
		fmt.Fprintf(&b, "  • %s — %s\n", p.Name, desc)
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderProfile formats one /profile <name> entry: description, tools, the
// raw allow/deny rules and the mapped grant (write globs, exec policy).
func renderProfile(p domain.AgentProfile) string {
	var b strings.Builder
	desc := strings.TrimSpace(p.Description)
	if desc == "" {
		desc = "(без описания)"
	}
	fmt.Fprintf(&b, "Профиль %s\n%s\n", p.Name, desc)
	if len(p.Stack) > 0 {
		fmt.Fprintf(&b, "\nСтеки: %s\n", strings.Join(p.Stack, ", "))
	}
	if len(p.Tools) > 0 {
		tools := make([]string, len(p.Tools))
		for i, t := range p.Tools {
			tools[i] = string(t)
		}
		fmt.Fprintf(&b, "Инструменты: %s\n", strings.Join(tools, ", "))
	}
	writePermRules := func(title string, rules []domain.PermRule) {
		if len(rules) == 0 {
			return
		}
		fmt.Fprintf(&b, "%s:\n", title)
		for _, r := range rules {
			fmt.Fprintf(&b, "  • %s\n", r)
		}
	}
	writePermRules("\nПрава (allow)", p.Allow)
	writePermRules("Запреты (deny)", p.Deny)

	grant, err := profilemap.Map(p)
	if err != nil {
		fmt.Fprintf(&b, "\n⚠ Маппинг прав: %v\n", err)
	} else {
		if len(grant.WriteGlobs) > 0 {
			fmt.Fprintf(&b, "\nWriteGlobs: %s\n", strings.Join(grant.WriteGlobs, ", "))
		}
		if len(grant.ExecAllow) > 0 {
			fmt.Fprintf(&b, "Выполнение (allow): %s\n", strings.Join(grant.ExecAllow, ", "))
		}
		if len(grant.ExecDeny) > 0 {
			fmt.Fprintf(&b, "Выполнение (deny): %s\n", strings.Join(grant.ExecDeny, ", "))
		}
		if len(grant.DenyGlobs) > 0 {
			fmt.Fprintf(&b, "Запрещённые пути: %s\n", strings.Join(grant.DenyGlobs, ", "))
		}
		for _, n := range grant.Notes {
			fmt.Fprintf(&b, "• %s\n", n)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
