package devpipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// ---- fakes ----

type roleCall struct {
	profile string
	task    string
	blocks  []domain.DataBlock
}

// fakeRoles routes every Run to a handler by call index.
type fakeRoles struct {
	handler func(call int, r port.RoleRequest) (port.RoleResult, error)
	calls   []roleCall
}

func (f *fakeRoles) Run(_ context.Context, r port.RoleRequest) (port.RoleResult, error) {
	n := len(f.calls)
	f.calls = append(f.calls, roleCall{profile: r.Profile, task: r.Task, blocks: r.DataBlocks})
	if f.handler == nil {
		return port.RoleResult{Content: passFindings()}, nil
	}
	return f.handler(n, r)
}

func (f *fakeRoles) callsFor(profile string) []roleCall {
	var out []roleCall
	for _, c := range f.calls {
		if c.profile == profile {
			out = append(out, c)
		}
	}
	return out
}

// fakeRunner passes by default; per-argv results override, a handler wins
// over the map.
type fakeRunner struct {
	results map[string]port.CommandResult
	handler func(argv []string) (port.CommandResult, error)
	calls   [][]string
}

func (f *fakeRunner) Run(_ context.Context, s port.CommandSpec) (port.CommandResult, error) {
	f.calls = append(f.calls, s.Argv)
	if f.handler != nil {
		return f.handler(s.Argv)
	}
	if r, ok := f.results[strings.Join(s.Argv, " ")]; ok {
		return r, nil
	}
	return port.CommandResult{}, nil
}

func (f *fakeRunner) Preflight(context.Context) error { return nil }

// fakeScanner serves per-stack findings; err (e.g. port.ErrScanOutput) is
// returned for every stack.
type fakeScanner struct {
	byStack map[string][]domain.Finding
	err     error
	calls   []string
}

func (f *fakeScanner) Scan(_ context.Context, _, stack string) ([]domain.Finding, error) {
	f.calls = append(f.calls, stack)
	if f.err != nil {
		return nil, f.err
	}
	return f.byStack[stack], nil
}

// fakeWS is an in-memory workspace: files map rel → content.
type fakeWS struct {
	dir   string
	files map[string]string
}

func (f *fakeWS) Seed(string, []string) error            { return nil }
func (f *fakeWS) TaskDir(string, string) (string, error) { return f.dir, nil }
func (f *fakeWS) Prepare(_, _ string) (string, error)    { return f.dir, nil }
func (f *fakeWS) Promote(_, _ string) error              { return nil }

func (f *fakeWS) Snapshot(string) (map[string]string, error) {
	m := make(map[string]string, len(f.files))
	for p, c := range f.files {
		sum := sha256.Sum256([]byte(c))
		m[p] = hex.EncodeToString(sum[:])
	}
	return m, nil
}

func (f *fakeWS) ReadNumbered(_ string, rel string, _ int) (string, int, error) {
	c, ok := f.files[rel]
	if !ok {
		return "", 0, fmt.Errorf("нет файла %q", rel)
	}
	lines := strings.Split(strings.TrimSuffix(c, "\n"), "\n")
	var b strings.Builder
	for i, l := range lines {
		fmt.Fprintf(&b, "%5d| %s\n", i+1, l)
	}
	return b.String(), len(lines), nil
}

// fakeEvents records stage and finding events.
type fakeEvents struct {
	stages   []string
	findings [][]domain.Finding
}

func (f *fakeEvents) Stage(taskID string, st domain.SubtaskStatus, _ string) {
	f.stages = append(f.stages, taskID+":"+string(st))
}
func (f *fakeEvents) Findings(_ string, fs []domain.Finding) { f.findings = append(f.findings, fs) }

// ---- helpers ----

// testConfig is a small fast config (plan section 7: no production-sized
// limits in tests).
func testConfig() Config {
	return Config{
		MaxSecurityIterations: 3,
		JSONRetries:           1,
		Generator:             "coder",
		Reviewers:             map[string]string{StackGo: "security-go", StackVue: "security-vue"},
		TestCommands:          map[string][][]string{StackGo: {{"go", "vet", "./..."}, {"go", "test", "./..."}}, StackVue: {{"vitest", "run"}}},
		CommandTimeout:        time.Second,
		TestOutputMaxBytes:    4096,
		ReviewFileMaxBytes:    4096,
		ReviewTotalMaxBytes:   16384,
		MaxToolRounds:         5,
	}
}

// passFindings is a valid empty security_findings.v1 answer.
func passFindings() string {
	return `{"schema":"security_findings.v1","verdict":"pass","findings":[]}`
}

// findingsAnswer builds a reviewer answer with one finding per spec
// (severity, cwe, file, line, issue, fix).
func findingsAnswer(verdict string, f ...domain.Finding) string {
	type sf struct {
		Schema   string `json:"schema"`
		Verdict  string `json:"verdict"`
		Findings []struct {
			ID       string `json:"id"`
			Severity string `json:"severity"`
			CWE      string `json:"cwe"`
			File     string `json:"file"`
			Line     int    `json:"line"`
			Rule     string `json:"rule"`
			Issue    string `json:"issue"`
			Evidence string `json:"evidence"`
			Fix      string `json:"fix"`
		} `json:"findings"`
	}
	a := sf{Schema: "security_findings.v1", Verdict: verdict}
	for i, fnd := range f {
		a.Findings = append(a.Findings, struct {
			ID       string `json:"id"`
			Severity string `json:"severity"`
			CWE      string `json:"cwe"`
			File     string `json:"file"`
			Line     int    `json:"line"`
			Rule     string `json:"rule"`
			Issue    string `json:"issue"`
			Evidence string `json:"evidence"`
			Fix      string `json:"fix"`
		}{fmt.Sprintf("f%d", i+1), string(fnd.Severity), fnd.CWE, fnd.File, fnd.Line, fnd.Rule, fnd.Issue, fnd.Evidence, fnd.Fix})
	}
	b, err := json.Marshal(a)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func newPipeline(t *testing.T, roles port.RoleRunner, runner port.CommandRunner, scanner port.SecurityScanner, ws port.Workspace, ev *fakeEvents, cfg Config) *Pipeline {
	t.Helper()
	if ev == nil {
		ev = &fakeEvents{}
	}
	var saved []domain.Attempt
	p, err := New(roles, runner, scanner, ws, func(a domain.Attempt) error {
		saved = append(saved, a)
		return nil
	}, ev, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() {
		if len(saved) == 0 {
			t.Errorf("write-ahead save never called")
		}
	})
	return p
}

// generator writes a fresh handler.go with ≥15 lines on every call; the
// marker keeps the SHA-256 snapshot diff non-empty between attempts.
func writeCode(ws *fakeWS, marker int) {
	var b strings.Builder
	b.WriteString("package main\n\n")
	fmt.Fprintf(&b, "// attempt %d\n", marker)
	for i := range 13 {
		fmt.Fprintf(&b, "// line %d\n", i+3)
	}
	ws.files["handler.go"] = b.String()
}

func TestSQLiFeedbackLoop(t *testing.T) {
	ws := &fakeWS{dir: "/task", files: map[string]string{"go.mod": "module x\n"}}
	roles := &fakeRoles{}
	scanner := &fakeScanner{}
	ev := &fakeEvents{}

	roles.handler = func(call int, r port.RoleRequest) (port.RoleResult, error) {
		switch r.Profile {
		case "coder":
			writeCode(ws, call)
			return port.RoleResult{Content: "готово"}, nil
		case "security-go":
			if callFor(roles, "security-go") == 0 {
				return port.RoleResult{Content: findingsAnswer("fail", domain.Finding{
					Severity: domain.SeverityCritical, CWE: "CWE-89", File: "handler.go", Line: 12,
					Rule: "sqli", Issue: "SQL injection", Fix: "используй плейсхолдеры $1",
				})}, nil
			}
			return port.RoleResult{Content: passFindings()}, nil
		}
		return port.RoleResult{}, fmt.Errorf("неожиданный профиль %q", r.Profile)
	}
	p := newPipeline(t, roles, &fakeRunner{}, scanner, ws, ev, testConfig())

	out, err := p.RunSubtask(context.Background(), Input{RunID: "r1", TaskID: "t1", Spec: "сделай обработчик"}, 3)
	if err != nil {
		t.Fatalf("RunSubtask: %v", err)
	}
	if out.Status != domain.SubtaskDone {
		t.Fatalf("статус: %s (%s), ожидался done", out.Status, out.Reason)
	}
	if len(out.Attempts) != 2 {
		t.Fatalf("попыток: %d, ожидалось 2", len(out.Attempts))
	}
	if out.Attempts[0].Verdict != domain.VerdictFail || out.Attempts[1].Verdict != domain.VerdictPass {
		t.Fatalf("вердикты: %q, %q", out.Attempts[0].Verdict, out.Attempts[1].Verdict)
	}

	// Feedback is the fixed template, exactly, and nothing of the reviewer's
	// raw text reaches the generator.
	const want = "исправь: CWE-89 SQL injection в handler.go:12 — используй плейсхолдеры $1"
	if len(out.Attempts[0].Feedback) != 1 || out.Attempts[0].Feedback[0] != want {
		t.Fatalf("feedback: %q, ожидался ровно %q", out.Attempts[0].Feedback, want)
	}
	gens := roles.callsFor("coder")
	if len(gens) != 2 {
		t.Fatalf("вызовов генератора: %d", len(gens))
	}
	if !strings.Contains(gens[1].task, `"text":"`+want+`"`) {
		t.Errorf("вторая задача генератора не содержит feedback.v1: %s", gens[1].task)
	}
	if strings.Contains(gens[1].task, "sqli") {
		t.Errorf("в задачу генератора попал сырой текст ревьюера")
	}

	// The reviewer saw the numbered file in a DATA block.
	revs := roles.callsFor("security-go")
	if len(revs) == 0 || len(revs[0].blocks) == 0 {
		t.Fatalf("ревьюер не получил DATA-блоков")
	}
	var sawFile bool
	for _, b := range revs[0].blocks {
		if b.Label == "file-handler.go" && strings.Contains(b.Content, "12|") {
			sawFile = true
		}
	}
	if !sawFile {
		t.Errorf("ревьюер не получил handler.go с номерами строк: %+v", revs[0].blocks)
	}
}

// callFor counts completed calls of a profile before the current one.
func callFor(roles *fakeRoles, profile string) int {
	n := 0
	for _, c := range roles.calls {
		if c.profile == profile {
			n++
		}
	}
	return n - 1 // the current call is already recorded
}

func TestUnverifiedLLMLineDoesNotBlock(t *testing.T) {
	ws := &fakeWS{dir: "/task", files: map[string]string{"go.mod": "module x\n"}}
	roles := &fakeRoles{}
	roles.handler = func(call int, r port.RoleRequest) (port.RoleResult, error) {
		if r.Profile == "coder" {
			ws.files["main.go"] = "package main\n\nfunc main() {}\n"
			return port.RoleResult{Content: "готово"}, nil
		}
		// Line 999 does not exist in a 3-line file.
		return port.RoleResult{Content: findingsAnswer("fail", domain.Finding{
			Severity: domain.SeverityCritical, CWE: "CWE-89", File: "main.go", Line: 999,
			Issue: "SQL injection", Fix: "placeholder",
		})}, nil
	}
	p := newPipeline(t, roles, &fakeRunner{}, &fakeScanner{}, ws, nil, testConfig())

	out, err := p.RunSubtask(context.Background(), Input{RunID: "r1", TaskID: "t1", Spec: "x"}, 2)
	if err != nil {
		t.Fatalf("RunSubtask: %v", err)
	}
	if out.Status != domain.SubtaskDone {
		t.Fatalf("статус: %s (%s)", out.Status, out.Reason)
	}
	fs := out.Attempts[0].Findings
	if len(fs) != 1 || fs[0].Verified {
		t.Fatalf("находка должна остаться неверифицированной: %+v", fs)
	}
}

func TestGosecHighOverridesPassVerdict(t *testing.T) {
	ws := &fakeWS{dir: "/task", files: map[string]string{"go.mod": "module x\n"}}
	roles := &fakeRoles{}
	scans := 0
	// The scanner returns a blocking High finding on the first scan only;
	// the reviewer claims "pass" — the code-computed verdict must win.
	scanner := &seqScanner{
		fn: func(stack string) ([]domain.Finding, error) {
			scans++
			if scans == 1 {
				return []domain.Finding{{
					Severity: domain.SeverityHigh, CWE: "CWE-327", File: "handler.go", Line: 1,
					Rule: "G501", Issue: "weak crypto", Source: domain.SourceGosec,
				}}, nil
			}
			return nil, nil
		},
	}
	roles.handler = func(call int, r port.RoleRequest) (port.RoleResult, error) {
		if r.Profile == "coder" {
			writeCode(ws, call)
			return port.RoleResult{Content: "готово"}, nil
		}
		return port.RoleResult{Content: passFindings()}, nil
	}
	p := newPipeline(t, roles, &fakeRunner{}, scanner, ws, nil, testConfig())

	out, err := p.RunSubtask(context.Background(), Input{RunID: "r1", TaskID: "t1", Spec: "x"}, 3)
	if err != nil {
		t.Fatalf("RunSubtask: %v", err)
	}
	if out.Status != domain.SubtaskDone {
		t.Fatalf("статус: %s (%s)", out.Status, out.Reason)
	}
	if len(out.Attempts) != 2 {
		t.Fatalf("попыток: %d", len(out.Attempts))
	}
	if out.Attempts[0].Verdict != domain.VerdictFail {
		t.Fatalf("вердикт первой попытки: %q (SAST High + reviewer pass ⇒ fail)", out.Attempts[0].Verdict)
	}
	if len(out.Attempts[0].Feedback) != 1 || !strings.Contains(out.Attempts[0].Feedback[0], "CWE-327") {
		t.Fatalf("feedback по SAST-находке: %q", out.Attempts[0].Feedback)
	}
}

// seqScanner is a scanner driven by a function (per-scan sequencing).
type seqScanner struct {
	fn  func(stack string) ([]domain.Finding, error)
	log []string
}

func (s *seqScanner) Scan(_ context.Context, _, stack string) ([]domain.Finding, error) {
	s.log = append(s.log, stack)
	return s.fn(stack)
}

func TestNoProgressFails(t *testing.T) {
	ws := &fakeWS{dir: "/task", files: map[string]string{"go.mod": "module x\n"}}
	roles := &fakeRoles{}
	roles.handler = func(call int, r port.RoleRequest) (port.RoleResult, error) {
		if r.Profile == "coder" {
			writeCode(ws, call)
			return port.RoleResult{Content: "готово"}, nil
		}
		return port.RoleResult{Content: findingsAnswer("fail", domain.Finding{
			Severity: domain.SeverityCritical, CWE: "CWE-89", File: "handler.go", Line: 12,
			Issue: "SQL injection", Fix: "placeholder",
		})}, nil
	}
	p := newPipeline(t, roles, &fakeRunner{}, &fakeScanner{}, ws, nil, testConfig())

	out, err := p.RunSubtask(context.Background(), Input{RunID: "r1", TaskID: "t1", Spec: "x"}, 5)
	if err != nil {
		t.Fatalf("RunSubtask: %v", err)
	}
	if out.Status != domain.SubtaskFailed {
		t.Fatalf("статус: %s", out.Status)
	}
	if !strings.Contains(out.Reason, "нет прогресса") {
		t.Fatalf("причина: %q", out.Reason)
	}
	last := out.Attempts[len(out.Attempts)-1]
	if last.Result != domain.ResultNoProgress {
		t.Fatalf("результат последней попытки: %q", last.Result)
	}
	if last.FindingsKey == "" || last.FindingsKey != out.Attempts[0].FindingsKey {
		t.Fatalf("FindingsKey не зафиксирован: %q vs %q", last.FindingsKey, out.Attempts[0].FindingsKey)
	}
}

func TestSecurityIterationLimit(t *testing.T) {
	ws := &fakeWS{dir: "/task", files: map[string]string{"go.mod": "module x\n"}}
	roles := &fakeRoles{}
	roles.handler = func(call int, r port.RoleRequest) (port.RoleResult, error) {
		if r.Profile == "coder" {
			writeCode(ws, call)
			return port.RoleResult{Content: "готово"}, nil
		}
		// Alternating CWEs keep FindingsKey changing, so the iteration limit
		// (not the no-progress rule) must end the subtask.
		n := callFor(roles, "security-go")
		if n%2 == 0 {
			return port.RoleResult{Content: findingsAnswer("fail", domain.Finding{
				Severity: domain.SeverityCritical, CWE: "CWE-89", File: "handler.go", Line: 12,
				Issue: "SQL injection", Fix: "placeholder",
			})}, nil
		}
		return port.RoleResult{Content: findingsAnswer("fail", domain.Finding{
			Severity: domain.SeverityCritical, CWE: "CWE-78", File: "handler.go", Line: 5,
			Issue: "command injection", Fix: "fixed argv",
		})}, nil
	}
	cfg := testConfig()
	cfg.MaxSecurityIterations = 2
	p := newPipeline(t, roles, &fakeRunner{}, &fakeScanner{}, ws, nil, cfg)

	out, err := p.RunSubtask(context.Background(), Input{RunID: "r1", TaskID: "t1", Spec: "x"}, 5)
	if err != nil {
		t.Fatalf("RunSubtask: %v", err)
	}
	if out.Status != domain.SubtaskFailed {
		t.Fatalf("статус: %s", out.Status)
	}
	if !strings.Contains(out.Reason, "лимит итераций безопасности") {
		t.Fatalf("причина: %q", out.Reason)
	}
	last := out.Attempts[len(out.Attempts)-1]
	if last.Result != domain.ResultSecurityFail {
		t.Fatalf("результат последней попытки: %q", last.Result)
	}
	if last.SecurityIteration != 2 {
		t.Fatalf("security_iteration: %d", last.SecurityIteration)
	}
}

func TestTestFailureSkipsReview(t *testing.T) {
	ws := &fakeWS{dir: "/task", files: map[string]string{"go.mod": "module x\n"}}
	roles := &fakeRoles{}
	vetFails := true
	roles.handler = func(call int, r port.RoleRequest) (port.RoleResult, error) {
		if r.Profile == "coder" {
			if callFor(roles, "coder") > 0 {
				vetFails = false // the second attempt fixes the code
			}
			writeCode(ws, call)
			return port.RoleResult{Content: "готово"}, nil
		}
		return port.RoleResult{Content: passFindings()}, nil
	}
	runner := &fakeRunner{}
	runner.handler = func(argv []string) (port.CommandResult, error) {
		if vetFails && strings.HasPrefix(strings.Join(argv, " "), "go vet") {
			return port.CommandResult{ExitCode: 1, Stderr: "vet: unreachable code"}, nil
		}
		return port.CommandResult{}, nil
	}
	p := newPipeline(t, roles, runner, &fakeScanner{}, ws, nil, testConfig())

	out, err := p.RunSubtask(context.Background(), Input{RunID: "r1", TaskID: "t1", Spec: "x"}, 3)
	if err != nil {
		t.Fatalf("RunSubtask: %v", err)
	}
	if out.Status != domain.SubtaskDone {
		t.Fatalf("статус: %s (%s)", out.Status, out.Reason)
	}
	if len(out.Attempts) != 2 {
		t.Fatalf("попыток: %d", len(out.Attempts))
	}
	first := out.Attempts[0]
	if first.Result != domain.ResultTestsFailed || first.TestResult.Passed {
		t.Fatalf("результат первой попытки: %q passed=%v", first.Result, first.TestResult.Passed)
	}
	if len(first.Feedback) != 1 || !strings.Contains(first.Feedback[0], "go vet ./...") {
		t.Fatalf("feedback о провале тестов: %q", first.Feedback)
	}
	// The review never ran during the failed attempt.
	if len(roles.callsFor("security-go")) != 1 {
		t.Fatalf("ревью вызвано %d раз, ожидался 1 (после прохождения тестов)", len(roles.callsFor("security-go")))
	}
	// The generator got the output tail in a DATA block.
	gens := roles.callsFor("coder")
	if len(gens) != 2 {
		t.Fatalf("вызовов генератора: %d", len(gens))
	}
	var sawOutput bool
	for _, b := range gens[1].blocks {
		if b.Label == "test-output" && strings.Contains(b.Content, "vet: unreachable code") {
			sawOutput = true
		}
	}
	if !sawOutput {
		t.Fatalf("генератор не получил вывод теста в DATA-блоке: %+v", gens[1].blocks)
	}
}

func TestInvalidJSONReviewFailsClosed(t *testing.T) {
	ws := &fakeWS{dir: "/task", files: map[string]string{"go.mod": "module x\n"}}
	roles := &fakeRoles{}
	roles.handler = func(call int, r port.RoleRequest) (port.RoleResult, error) {
		if r.Profile == "coder" {
			writeCode(ws, call)
			return port.RoleResult{Content: "готово"}, nil
		}
		return port.RoleResult{Content: "проза вместо JSON: точно не security_findings.v1"}, nil
	}
	p := newPipeline(t, roles, &fakeRunner{}, &fakeScanner{}, ws, nil, testConfig())

	out, err := p.RunSubtask(context.Background(), Input{RunID: "r1", TaskID: "t1", Spec: "x"}, 3)
	if err != nil {
		t.Fatalf("RunSubtask: %v", err)
	}
	if out.Status != domain.SubtaskFailed {
		t.Fatalf("статус: %s", out.Status)
	}
	if !strings.Contains(out.Reason, "security_findings.v1") {
		t.Fatalf("причина: %q", out.Reason)
	}
	last := out.Attempts[len(out.Attempts)-1]
	if last.Result != domain.ResultReviewFailed {
		t.Fatalf("результат: %q", last.Result)
	}
	// 1 initial try + JSONRetries(1) retries, then fail-closed.
	if n := len(roles.callsFor("security-go")); n != 2 {
		t.Fatalf("вызовов ревьюера: %d, ожидалось 2", n)
	}
	// The retry carried the validator complaint.
	revs := roles.callsFor("security-go")
	if !strings.Contains(revs[1].task, "отклонён валидатором") {
		t.Errorf("повторный запрос ревьюера без текста ошибки валидатора: %s", revs[1].task)
	}
}

func TestUnknownJSONFieldRejected(t *testing.T) {
	ws := &fakeWS{dir: "/task", files: map[string]string{"go.mod": "module x\n"}}
	roles := &fakeRoles{}
	roles.handler = func(call int, r port.RoleRequest) (port.RoleResult, error) {
		if r.Profile == "coder" {
			writeCode(ws, call)
			return port.RoleResult{Content: "готово"}, nil
		}
		return port.RoleResult{Content: `{"schema":"security_findings.v1","verdict":"pass","findings":[],"confidence":"high"}`}, nil
	}
	p := newPipeline(t, roles, &fakeRunner{}, &fakeScanner{}, ws, nil, testConfig())

	out, err := p.RunSubtask(context.Background(), Input{RunID: "r1", TaskID: "t1", Spec: "x"}, 3)
	if err != nil {
		t.Fatalf("RunSubtask: %v", err)
	}
	if out.Status != domain.SubtaskFailed || !strings.Contains(out.Reason, "security_findings.v1") {
		t.Fatalf("ожидался failed/review: %s (%s)", out.Status, out.Reason)
	}
	if out.Attempts[len(out.Attempts)-1].Result != domain.ResultReviewFailed {
		t.Fatalf("результат: %q", out.Attempts[len(out.Attempts)-1].Result)
	}
}

func TestGuardBlockedGeneratorFeedback(t *testing.T) {
	ws := &fakeWS{dir: "/task", files: map[string]string{"go.mod": "module x\n"}}
	roles := &fakeRoles{}
	roles.handler = func(call int, r port.RoleRequest) (port.RoleResult, error) {
		if r.Profile == "coder" {
			if callFor(roles, "coder") == 0 {
				return port.RoleResult{Content: "Ответ скрыт политикой безопасности прокси.", GuardBlocked: true, GuardReason: "dangerous_commands"}, nil
			}
			writeCode(ws, call)
			return port.RoleResult{Content: "готово"}, nil
		}
		return port.RoleResult{Content: passFindings()}, nil
	}
	p := newPipeline(t, roles, &fakeRunner{}, &fakeScanner{}, ws, nil, testConfig())

	out, err := p.RunSubtask(context.Background(), Input{RunID: "r1", TaskID: "t1", Spec: "x"}, 3)
	if err != nil {
		t.Fatalf("RunSubtask: %v", err)
	}
	if out.Status != domain.SubtaskDone {
		t.Fatalf("статус: %s (%s)", out.Status, out.Reason)
	}
	if out.Attempts[0].Result != domain.ResultGuardBlocked {
		t.Fatalf("результат первой попытки: %q", out.Attempts[0].Result)
	}
	gens := roles.callsFor("coder")
	if len(gens) != 2 {
		t.Fatalf("вызовов генератора: %d", len(gens))
	}
	if !strings.Contains(gens[1].task, "ответ заблокирован guard'ом") || !strings.Contains(gens[1].task, "dangerous_commands") {
		t.Errorf("feedback kind=guard не доставлен: %s", gens[1].task)
	}
}

func TestMediumNotInFeedback(t *testing.T) {
	ws := &fakeWS{dir: "/task", files: map[string]string{"go.mod": "module x\n"}}
	roles := &fakeRoles{}
	roles.handler = func(call int, r port.RoleRequest) (port.RoleResult, error) {
		if r.Profile == "coder" {
			writeCode(ws, call)
			return port.RoleResult{Content: "готово"}, nil
		}
		return port.RoleResult{Content: findingsAnswer("fail", domain.Finding{
			Severity: domain.SeverityMedium, CWE: "CWE-327", File: "handler.go", Line: 3,
			Issue: "weak crypto", Fix: "use sha256",
		})}, nil
	}
	p := newPipeline(t, roles, &fakeRunner{}, &fakeScanner{}, ws, nil, testConfig())

	out, err := p.RunSubtask(context.Background(), Input{RunID: "r1", TaskID: "t1", Spec: "x"}, 3)
	if err != nil {
		t.Fatalf("RunSubtask: %v", err)
	}
	if out.Status != domain.SubtaskDone {
		t.Fatalf("статус: %s (%s)", out.Status, out.Reason)
	}
	if len(out.Attempts) != 1 {
		t.Fatalf("Medium не должен возвращать код на доработку: %d попыток", len(out.Attempts))
	}
	if len(out.Attempts[0].Feedback) != 0 {
		t.Fatalf("Medium попал в feedback: %q", out.Attempts[0].Feedback)
	}
	if len(out.Attempts[0].Findings) != 1 || out.Attempts[0].Findings[0].Severity != domain.SeverityMedium {
		t.Fatalf("Medium должен попадать в отчёт: %+v", out.Attempts[0].Findings)
	}
}

func TestGoAndVueBothReviewed(t *testing.T) {
	ws := &fakeWS{dir: "/task", files: map[string]string{"go.mod": "module x\n", "package.json": "{}\n"}}
	roles := &fakeRoles{}
	roles.handler = func(call int, r port.RoleRequest) (port.RoleResult, error) {
		if r.Profile == "coder" {
			ws.files["main.go"] = "package main\n\nfunc main() {}\n"
			ws.files["app.vue"] = "<template><div/></template>\n"
			return port.RoleResult{Content: "готово"}, nil
		}
		return port.RoleResult{Content: passFindings()}, nil
	}
	scanner := &fakeScanner{byStack: map[string][]domain.Finding{}}
	p := newPipeline(t, roles, &fakeRunner{}, scanner, ws, nil, testConfig())

	in := Input{RunID: "r1", TaskID: "t1", Spec: "x"}
	out, err := p.RunSubtask(context.Background(), in, 2)
	if err != nil {
		t.Fatalf("RunSubtask: %v", err)
	}
	if out.Status != domain.SubtaskDone {
		t.Fatalf("статус: %s (%s)", out.Status, out.Reason)
	}
	stacks := out.Attempts[0].Stacks
	if len(stacks) != 2 || stacks[0] != StackGo || stacks[1] != StackVue {
		t.Fatalf("стеки: %v", stacks)
	}
	if len(roles.callsFor("security-go")) != 1 || len(roles.callsFor("security-vue")) != 1 {
		t.Fatalf("оба ревьюера должны быть вызваны: go=%d vue=%d",
			len(roles.callsFor("security-go")), len(roles.callsFor("security-vue")))
	}
	if len(scanner.calls) != 2 || scanner.calls[0] != StackGo || scanner.calls[1] != StackVue {
		t.Fatalf("оба SAST должны быть вызваны: %v", scanner.calls)
	}
}

func TestInputGuardBlockedSubtask(t *testing.T) {
	ws := &fakeWS{dir: "/task", files: map[string]string{"go.mod": "module x\n"}}
	roles := &fakeRoles{}
	roles.handler = func(call int, r port.RoleRequest) (port.RoleResult, error) {
		return port.RoleResult{}, fmt.Errorf("proxy: %w", port.ErrGuardBlocked)
	}
	p := newPipeline(t, roles, &fakeRunner{}, &fakeScanner{}, ws, nil, testConfig())

	out, err := p.RunSubtask(context.Background(), Input{RunID: "r1", TaskID: "t1", Spec: "x"}, 3)
	if err != nil {
		t.Fatalf("RunSubtask: %v", err)
	}
	if out.Status != domain.SubtaskBlocked {
		t.Fatalf("статус: %s (%s), ожидался blocked", out.Status, out.Reason)
	}
	if len(out.Attempts) != 1 || out.Attempts[0].Result != domain.ResultGuardBlocked {
		t.Fatalf("попытки: %+v", out.Attempts)
	}
}

func TestNoChangesFeedback(t *testing.T) {
	ws := &fakeWS{dir: "/task", files: map[string]string{"go.mod": "module x\n"}}
	roles := &fakeRoles{}
	roles.handler = func(call int, r port.RoleRequest) (port.RoleResult, error) {
		if r.Profile == "coder" {
			if callFor(roles, "coder") == 0 {
				return port.RoleResult{Content: "не стал ничего менять"}, nil
			}
			writeCode(ws, call)
			return port.RoleResult{Content: "готово"}, nil
		}
		return port.RoleResult{Content: passFindings()}, nil
	}
	p := newPipeline(t, roles, &fakeRunner{}, &fakeScanner{}, ws, nil, testConfig())

	out, err := p.RunSubtask(context.Background(), Input{RunID: "r1", TaskID: "t1", Spec: "x"}, 3)
	if err != nil {
		t.Fatalf("RunSubtask: %v", err)
	}
	if out.Status != domain.SubtaskDone {
		t.Fatalf("статус: %s (%s)", out.Status, out.Reason)
	}
	if out.Attempts[0].Result != domain.ResultNoChanges {
		t.Fatalf("результат первой попытки: %q", out.Attempts[0].Result)
	}
	if len(out.Attempts[0].Feedback) != 1 || !strings.Contains(out.Attempts[0].Feedback[0], "изменений нет") {
		t.Fatalf("feedback no_changes: %q", out.Attempts[0].Feedback)
	}
}

func TestSASTUnusableOutputFailsClosed(t *testing.T) {
	ws := &fakeWS{dir: "/task", files: map[string]string{"go.mod": "module x\n"}}
	roles := &fakeRoles{}
	roles.handler = func(call int, r port.RoleRequest) (port.RoleResult, error) {
		if r.Profile == "coder" {
			writeCode(ws, call)
			return port.RoleResult{Content: "готово"}, nil
		}
		return port.RoleResult{Content: passFindings()}, nil
	}
	scanner := &fakeScanner{err: fmt.Errorf("вывод усечён: %w", port.ErrScanOutput)}
	p := newPipeline(t, roles, &fakeRunner{}, scanner, ws, nil, testConfig())

	out, err := p.RunSubtask(context.Background(), Input{RunID: "r1", TaskID: "t1", Spec: "x"}, 3)
	if err != nil {
		t.Fatalf("RunSubtask: %v", err)
	}
	if out.Status != domain.SubtaskFailed {
		t.Fatalf("статус: %s", out.Status)
	}
	if out.Attempts[len(out.Attempts)-1].Result != domain.ResultReviewFailed {
		t.Fatalf("результат: %q", out.Attempts[len(out.Attempts)-1].Result)
	}
	if len(roles.callsFor("security-go")) != 0 {
		t.Fatalf("ревьюер не должен вызываться после fail-closed SAST")
	}
}

func TestRunnerInfraError(t *testing.T) {
	ws := &fakeWS{dir: "/task", files: map[string]string{"go.mod": "module x\n"}}
	roles := &fakeRoles{}
	roles.handler = func(call int, r port.RoleRequest) (port.RoleResult, error) {
		writeCode(ws, call)
		return port.RoleResult{Content: "готово"}, nil
	}
	runner := &fakeRunner{}
	runner.handler = func(argv []string) (port.CommandResult, error) {
		return port.CommandResult{}, errors.New("docker unavailable")
	}
	p := newPipeline(t, roles, runner, &fakeScanner{}, ws, nil, testConfig())

	out, err := p.RunSubtask(context.Background(), Input{RunID: "r1", TaskID: "t1", Spec: "x"}, 3)
	if err != nil {
		t.Fatalf("RunSubtask: %v", err)
	}
	if out.Status != domain.SubtaskFailed {
		t.Fatalf("статус: %s", out.Status)
	}
	if out.Attempts[len(out.Attempts)-1].Result != domain.ResultError {
		t.Fatalf("результат: %q", out.Attempts[len(out.Attempts)-1].Result)
	}
}
