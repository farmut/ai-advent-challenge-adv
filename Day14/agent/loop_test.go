package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-adv-agent/internal/domain"
)

func TestGoalFromArg_PlainText(t *testing.T) {
	g, err := goalFromArg("сделай REST API")
	if err != nil {
		t.Fatal(err)
	}
	if g.Text != "сделай REST API" || len(g.Roots) != 0 {
		t.Fatalf("got %+v", g)
	}
}

func TestGoalFromArg_Empty(t *testing.T) {
	if _, err := goalFromArg("  "); err == nil {
		t.Fatal("empty goal must be rejected")
	}
}

func TestGoalFromArg_File(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	content := "# Цель\nСделать сервис.\n\n- internal/app\n- internal/domain\n* README.md\n"
	if err := os.WriteFile("goal.md", []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	g, err := goalFromArg("@goal.md")
	if err != nil {
		t.Fatal(err)
	}
	if g.Text != strings.TrimSpace(content) {
		t.Fatalf("Text = %q", g.Text)
	}
	wantRoots := []string{"internal/app", "internal/domain", "README.md"}
	if strings.Join(g.Roots, ",") != strings.Join(wantRoots, ",") {
		t.Fatalf("Roots = %v, want %v", g.Roots, wantRoots)
	}
}

func TestGoalFromArg_FileErrors(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	// No bullets → the plan would have no roots.
	if err := os.WriteFile("nobullets.md", []byte("просто текст цели"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := goalFromArg("@nobullets.md"); err == nil || !strings.Contains(err.Error(), "пунктов") {
		t.Fatalf("nobullets: got %v", err)
	}

	if _, err := goalFromArg("@missing.md"); err == nil {
		t.Fatal("missing file must be rejected")
	}

	// Deny-listed file (.env* is in the default deny globs).
	if err := os.WriteFile(".env.goal", []byte("- x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := goalFromArg("@.env.goal"); err == nil || !strings.Contains(err.Error(), "политик") {
		t.Fatalf("denied: got %v", err)
	}

	// Outside the working directory.
	out := filepath.Join(dir, "..", "elsewhere.md")
	if err := os.WriteFile(out, []byte("- x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := goalFromArg("@../elsewhere.md"); err == nil {
		t.Fatal("file outside the working dir must be rejected")
	}

	// Over 64 KiB.
	big := strings.Repeat("большая цель ", 7000) + "\n- x"
	if err := os.WriteFile("big.md", []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := goalFromArg("@big.md"); err == nil || !strings.Contains(err.Error(), "КиБ") {
		t.Fatalf("oversize: got %v", err)
	}
}

func TestCompactNum(t *testing.T) {
	cases := map[int64]string{0: "0", 950: "950", 412_000: "412k", 1_500_000: "1.5M"}
	for in, want := range cases {
		if got := compactNum(in); got != want {
			t.Errorf("compactNum(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestLoopHeaderString(t *testing.T) {
	h := loopHeader{cur: 3, total: 7, profile: "coder", secIter: 2, secMax: 3, tokens: 412_000, cost: 0.31, costMax: 1.0}
	want := "LOOP 3/7 · coder · sec-iter 2/3 · 412k tok · $0.31/1.00"
	if got := h.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	// Zero fields are omitted: no tokens/cost/sec-iter before the first call.
	h = loopHeader{cur: 1, total: 5, profile: "researcher"}
	want = "LOOP 1/5 · researcher"
	if got := h.String(); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestHeaderFromRun(t *testing.T) {
	run := domain.LoopRun{
		CurrentIdx: 2,
		Budget:     domain.Budget{MaxCostUSD: 2.5},
		Usage:      domain.BudgetUsage{Tokens: 12_000, CostUSD: 0.05},
		Subtasks: []domain.Subtask{
			{ID: "a1", Profile: "coder"},
			{ID: "a2", Profile: "coder"},
			{ID: "a3", Profile: "security-go", Attempts: []domain.Attempt{{N: 1, SecurityIteration: 2}}},
		},
	}
	h := headerFromRun(run, 3)
	if h.cur != 3 || h.total != 3 || h.profile != "security-go" || h.secIter != 2 || h.secMax != 3 ||
		h.tokens != 12_000 || h.cost != 0.05 || h.costMax != 2.5 {
		t.Fatalf("got %+v", h)
	}
	// An empty run (planning) must not panic or produce nonsense indexes.
	h = headerFromRun(domain.LoopRun{}, 3)
	if h.cur != 0 || h.total != 0 || h.String() != "LOOP 0/0" {
		t.Fatalf("empty run: got %+v / %q", h, h.String())
	}
}

func finding(sev domain.Severity, file, rule string) domain.Finding {
	return domain.Finding{ID: rule, Severity: sev, File: file, Line: 10, CWE: "CWE-89", Rule: rule, Issue: "SQL-инъекция", Fix: "плейсхолдеры"}
}

func TestRenderFindings(t *testing.T) {
	run := domain.LoopRun{ID: "run-1", Status: domain.LoopFailed, Subtasks: []domain.Subtask{
		{ID: "a1", Profile: "coder", Attempts: []domain.Attempt{
			{N: 1, Findings: []domain.Finding{finding(domain.SeverityHigh, "old.go", "sql-1")}},
			{N: 2, Findings: []domain.Finding{finding(domain.SeverityHigh, "main.go", "sql-2")}},
		}},
		{ID: "a2", Profile: "coder", Attempts: []domain.Attempt{
			{N: 1, Findings: []domain.Finding{finding(domain.SeverityLow, "web.vue", "vue-1")}},
		}},
	}}
	out := renderFindings(run)
	for _, want := range []string{"run-1", "Исправлены", "Остаются", "main.go:10", "old.go", "Medium/Low", "CWE-89"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// The fixed finding must not be listed as remaining, and totals count
	// every finding across attempts (3 total: 2 high + 1 low).
	if strings.Contains(out, "Исправлены") && !strings.Contains(out, "old.go:10") {
		t.Errorf("fixed finding must keep its location:\n%s", out)
	}
}

func TestRenderFindings_Empty(t *testing.T) {
	out := renderFindings(domain.LoopRun{ID: "run-2", Status: domain.LoopDone})
	if !strings.Contains(out, "Находок нет") {
		t.Fatalf("got %q", out)
	}
}

func TestRenderProfileList(t *testing.T) {
	out := renderProfileList([]domain.AgentProfile{
		{Name: "coder", Description: "пишет код"},
		{Name: "security-go"},
	})
	if !strings.Contains(out, "coder — пишет код") || !strings.Contains(out, "security-go — (без описания)") {
		t.Fatalf("got %q", out)
	}
	if got := renderProfileList(nil); !strings.Contains(got, "пуст") {
		t.Fatalf("got %q", got)
	}
}

func TestRenderProfile(t *testing.T) {
	rule := func(s string) domain.PermRule {
		r, err := domain.ParsePermRule(s)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	p := domain.AgentProfile{
		Name:        "coder",
		Description: "реализует подзадачи",
		Tools:       []domain.PermKind{domain.PermRead, domain.PermBash, domain.PermWrite},
		Allow: []domain.PermRule{
			rule("Read(./**)"),
			rule("Bash(go test:*)"),
			rule("Write(./out/**)"),
			rule("Edit(./**/*.go)"),
		},
		Deny: []domain.PermRule{rule("Edit(./go.mod)")},
	}
	out := renderProfile(p)
	for _, want := range []string{"coder", "реализует подзадачи", "Read(./**)", "Edit(./go.mod)", "WriteGlobs", "go test:*"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// fakeLoopStatus stands in for *execloop.Loop so the events adapter can be
// tested without a live loop.
type fakeLoopStatus struct {
	run     domain.LoopRun
	active  bool
	statusE error
}

func (f *fakeLoopStatus) Status() (domain.LoopRun, bool, error) {
	return f.run, f.active, f.statusE
}

// Stage/Findings/Budget lines must reach the log channel and refresh the
// header from the persisted run — with no live loop behind it.
func TestTuiLoopEvents(t *testing.T) {
	fs := &fakeLoopStatus{active: true, run: domain.LoopRun{
		CurrentIdx: 0,
		Budget:     domain.Budget{MaxCostUSD: 1},
		Usage:      domain.BudgetUsage{Tokens: 5_000, CostUSD: 0.01},
		Subtasks: []domain.Subtask{
			{ID: "a1", Profile: "coder", Attempts: []domain.Attempt{{N: 1, SecurityIteration: 1}}},
		},
	}}
	logCh := make(chan string, 8)
	hdrCh := make(chan loopHeader, 8)
	ev := tuiLoopEvents{loop: fs, secMax: 3, logCh: logCh, hdrCh: hdrCh}

	ev.Stage("a1", domain.SubtaskTesting, "команды пройдены")
	select {
	case line := <-logCh:
		if !strings.HasPrefix(line, "[loop] a1 → testing:") {
			t.Fatalf("unexpected log line %q", line)
		}
	default:
		t.Fatal("Stage must write a log line")
	}
	select {
	case h := <-hdrCh:
		if h.cur != 1 || h.total != 1 || h.profile != "coder" || h.secIter != 1 || h.tokens != 5_000 {
			t.Fatalf("unexpected header %+v", h)
		}
	default:
		t.Fatal("Stage must refresh the header")
	}

	ev.Findings("a1", []domain.Finding{finding(domain.SeverityHigh, "main.go", "sql-2")})
	select {
	case line := <-logCh:
		if !strings.Contains(line, "[security] a1: 1 находок (1 High)") {
			t.Fatalf("unexpected security line %q", line)
		}
	default:
		t.Fatal("Findings must write a log line")
	}

	// Inactive run: refresh is a no-op, nothing panics, no header sent.
	fs.active = false
	before := len(hdrCh)
	ev.Budget(domain.BudgetUsage{})
	if len(hdrCh) != before {
		t.Fatal("an inactive run must not refresh the header")
	}

	// Status error: same, no panic.
	fs.active = true
	fs.statusE = errors.New("disk gone")
	ev.Budget(domain.BudgetUsage{})
	if len(hdrCh) != before {
		t.Fatal("a status error must not refresh the header")
	}
}
