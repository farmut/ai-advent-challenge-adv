//go:build e2e

// Package e2e_test runs the deterministic execution-loop scenario from the
// plan (section 4): fake upstream -> Day13 guard proxy -> agent --loop, with
// the real docker sandbox. Everything is local; the LLM is fully scripted.
//
// The tests need docker and the sandbox image (make sandbox-image). Without
// them the whole suite skips: nothing here can fall back to a less isolated
// path.
package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-adv-agent/internal/domain"
)

const e2eKey = "e2e-key-0123456789abcdef0123456789abcd"

var (
	agentRoot string
	day13Dir  string
	binDir    string
	dockerOK  bool
)

// TestMain builds the agent, the guard proxy and the fake upstream into a
// temp directory and checks the docker prerequisites once.
func TestMain(m *testing.M) {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	agentRoot = filepath.Dir(wd)       // Day14/agent
	day13Dir = filepath.Dir(agentRoot) // Day14
	day13Dir = filepath.Join(filepath.Dir(day13Dir), "Day13")

	binDir, err = os.MkdirTemp("", "loop-e2e-bin")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(binDir)

	build := func(dir, out, pkg string) bool {
		cmd := exec.Command("go", "build", "-o", filepath.Join(binDir, out), pkg)
		cmd.Dir = dir
		if b, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "build %s: %v\n%s\n", pkg, err, b)
			return false
		}
		return true
	}
	ok := build(agentRoot, "ai-adv-agent", ".") &&
		build(day13Dir, "llm-proxy", "./cmd/llm-proxy") &&
		build(day13Dir, "fake-upstream", "./cmd/fake-upstream")

	dockerOK = ok && dockerAvailable()
	if !dockerOK {
		fmt.Fprintln(os.Stderr, "e2e prerequisites missing (build or docker/image) — tests will skip")
	}
	os.Exit(m.Run())
}

func dockerAvailable() bool {
	if _, err := exec.LookPath("docker"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}").Run(); err != nil {
		return false
	}
	return exec.CommandContext(ctx, "docker", "image", "inspect", "ai-adv-sandbox:1").Run() == nil
}

func requireDocker(t *testing.T) {
	t.Helper()
	if !dockerOK {
		t.Skip("needs docker and the ai-adv-sandbox:1 image (make sandbox-image)")
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// replaceOnce substitutes old->new exactly once and fails the test otherwise.
func replaceOnce(t *testing.T, src, old, new string) string {
	t.Helper()
	if strings.Count(src, old) != 1 {
		t.Fatalf("anchor %q occurs %d times, want 1", old, strings.Count(src, old))
	}
	return strings.Replace(src, old, new, 1)
}

// harness keeps one fake upstream + one proxy alive for a test.
type harness struct {
	dir       string // t.TempDir()
	fakeLog   bytes.Buffer
	fakeCmd   *exec.Cmd
	proxyCmd  *exec.Cmd
	ProxyURL  string
	AuditPath string
}

// startHarness launches the fake upstream with the given script and the guard
// proxy in front of it, then waits for the proxy /readyz.
func startHarness(t *testing.T, script string) *harness {
	t.Helper()
	h := &harness{dir: t.TempDir()}
	fakePort := freePort(t)
	proxyPort := freePort(t)
	h.ProxyURL = fmt.Sprintf("http://127.0.0.1:%d", proxyPort)
	h.AuditPath = filepath.Join(h.dir, "audit.jsonl")

	h.fakeCmd = exec.Command(
		filepath.Join(binDir, "fake-upstream"),
		"-listen", fmt.Sprintf("127.0.0.1:%d", fakePort),
		"-script", script)
	h.fakeCmd.Stdout = &h.fakeLog
	h.fakeCmd.Stderr = &h.fakeLog
	if err := h.fakeCmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.fakeCmd.Process.Kill() })

	raw, err := os.ReadFile(filepath.Join(day13Dir, "configs", "proxy.loop-e2e.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(raw)
	cfg = replaceOnce(t, cfg, "listen: 127.0.0.1:8090", fmt.Sprintf("listen: 127.0.0.1:%d", proxyPort))
	cfg = replaceOnce(t, cfg, "base_url: http://127.0.0.1:18099/v1",
		fmt.Sprintf("base_url: http://127.0.0.1:%d/v1", fakePort))
	cfg = replaceOnce(t, cfg, "path: logs/audit/proxy-e2e-audit.jsonl", "path: "+h.AuditPath)
	proxyCfg := filepath.Join(h.dir, "proxy.yaml")
	if err := os.WriteFile(proxyCfg, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	h.proxyCmd = exec.Command(filepath.Join(binDir, "llm-proxy"), "--config", proxyCfg)
	h.proxyCmd.Env = append(os.Environ(), "PROXY_API_KEYS="+e2eKey)
	proxyLog, _ := os.Create(filepath.Join(h.dir, "proxy.log"))
	h.proxyCmd.Stdout = proxyLog
	h.proxyCmd.Stderr = proxyLog
	if err := h.proxyCmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.proxyCmd.Process.Kill() })

	waitReady(t, h.ProxyURL+"/readyz")
	return h
}

func waitReady(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := func() error {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			io.Copy(io.Discard, resp.Body)
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("status %d", resp.StatusCode)
			}
			return nil
		}()
		cancel()
		if err == nil {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("%s did not become ready in 30s", url)
}

// agentOverrides maps an exact YAML fragment to its replacement in the loop
// e2e config copy.
type agentOverrides map[string]string

// runAgent copies testdata/loop/agent.e2e.yaml with the standard rewrites
// (proxy URL, workspace root, session file into the harness dir) plus the
// per-test overrides, then runs the agent --loop and returns its exit code.
func (h *harness) runAgent(t *testing.T, overrides agentOverrides, goalArg string) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(agentRoot, "testdata", "loop", "agent.e2e.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := string(raw)
	cfg = replaceOnce(t, cfg, "base_url: http://127.0.0.1:8090/v1", "base_url: "+h.ProxyURL+"/v1")
	cfg = replaceOnce(t, cfg, "workspace_root: ../../workspace-loop-e2e",
		"workspace_root: "+filepath.Join(h.dir, "workspace"))
	cfg = replaceOnce(t, cfg, "dir: loop_e2e_session.json",
		"dir: "+filepath.Join(h.dir, "session.json"))
	// profiles.dir is resolved against the config file location; the copy
	// lives in a temp dir, so anchor it to the real profiles directory.
	cfg = replaceOnce(t, cfg, "dir: ../../profiles",
		"dir: "+filepath.Join(agentRoot, "profiles"))
	// The same applies to the sandbox templates.
	cfg = replaceOnce(t, cfg, "go: ../../../sandbox/templates/go",
		"go: "+filepath.Join(agentRoot, "..", "sandbox", "templates", "go"))
	cfg = replaceOnce(t, cfg, "vue: ../../../sandbox/templates/vue",
		"vue: "+filepath.Join(agentRoot, "..", "sandbox", "templates", "vue"))
	for old, new := range overrides {
		cfg = replaceOnce(t, cfg, old, new)
	}
	cfgPath := filepath.Join(h.dir, "agent.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(binDir, "ai-adv-agent"),
		"--config", cfgPath, "--loop", goalArg)
	cmd.Dir = agentRoot
	cmd.Env = append(os.Environ(), "LLM_API_KEY="+e2eKey)
	agentLog, _ := os.Create(filepath.Join(h.dir, "agent.log"))
	cmd.Stdout = agentLog
	cmd.Stderr = agentLog

	err = cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("agent timed out after 8m, see %s", filepath.Join(h.dir, "agent.log"))
	}
	if err == nil {
		return 0
	}
	logTail, _ := os.ReadFile(filepath.Join(h.dir, "agent.log"))
	if ee, ok := err.(*exec.ExitError); ok {
		t.Logf("agent log:\n%s", logTail)
		return ee.ExitCode()
	}
	t.Fatalf("agent run: %v\nlog:\n%s", err, logTail)
	return -1
}

// loopRun reads the single persisted loop run file the agent left next to its
// session file. Terminal runs are archived as <session>.loop.<id>.json, a
// paused one stays active as <session>.loop.json.
func (h *harness) loopRun(t *testing.T) (domain.LoopRun, string) {
	t.Helper()
	var matches []string
	for _, pat := range []string{"*.loop.json", "*.loop.*.json"} {
		m, err := filepath.Glob(filepath.Join(h.dir, pat))
		if err != nil {
			t.Fatal(err)
		}
		matches = append(matches, m...)
	}
	if len(matches) != 1 {
		t.Fatalf("loop run files: %v", matches)
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var run domain.LoopRun
	if err := json.Unmarshal(raw, &run); err != nil {
		t.Fatal(err)
	}
	if err := run.Validate(); err != nil {
		t.Fatalf("persisted run does not validate: %v", err)
	}
	return run, matches[0]
}

// baseFile returns the promoted base content of a workspace file.
func (h *harness) baseFile(t *testing.T, rel string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(h.dir, "workspace", "*", "base", rel))
	if err != nil || len(matches) != 1 {
		var tree []string
		files, _ := filepath.Glob(filepath.Join(h.dir, "workspace", "*", "*"))
		tree = append(tree, files...)
		files, _ = filepath.Glob(filepath.Join(h.dir, "workspace", "*", "*", "*"))
		tree = append(tree, files...)
		t.Fatalf("base file %s: %v, err=%v; workspace tree: %v", rel, matches, err, tree)
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// auditRecord is the subset of the proxy audit record this suite asserts on.
type auditRecord struct {
	CostMicroUSD            int64 `json:"cost_micro_usd"`
	ResponseToolCallsMasked []struct {
		ID       string `json:"id"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	} `json:"response_tool_calls_masked"`
	PromptMasked []struct {
		Role      string          `json:"role"`
		ToolCalls json.RawMessage `json:"tool_calls"`
	} `json:"prompt_masked"`
}

func (h *harness) auditRecords(t *testing.T) []auditRecord {
	t.Helper()
	raw, err := os.ReadFile(h.AuditPath)
	if err != nil {
		t.Fatal(err)
	}
	var recs []auditRecord
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var r auditRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("audit line: %v", err)
		}
		recs = append(recs, r)
	}
	if len(recs) == 0 {
		t.Fatal("no audit records")
	}
	if strings.Contains(string(raw), e2eKey) {
		t.Fatal("raw API key leaked into the proxy audit log")
	}
	return recs
}

const goalArg = "@testdata/loop/sqli_goal.md"

// TestLoopE2EHappyPath is the plan's section-4 scenario: the generator first
// writes a SQLi endpoint, the code pipeline (tests, gosec, reviewer) fails it,
// and the second attempt — driven by the exact template feedback — fixes it.
func TestLoopE2EHappyPath(t *testing.T) {
	requireDocker(t)
	h := startHarness(t, filepath.Join(agentRoot, "testdata", "loop", "fake_script.json"))

	code := h.runAgent(t, nil, goalArg)
	if code != 0 {
		t.Fatalf("exit code %d, want 0 (done)", code)
	}

	run, runPath := h.loopRun(t)
	if run.Status != domain.LoopDone {
		t.Fatalf("run status %q, want done", run.Status)
	}
	if len(run.Subtasks) != 1 {
		t.Fatalf("subtasks %d, want 1", len(run.Subtasks))
	}
	st := run.Subtasks[0]
	if st.Status != domain.SubtaskDone {
		t.Fatalf("subtask status %q, want done", st.Status)
	}
	if len(st.Attempts) != 2 {
		t.Fatalf("attempts %d, want 2", len(st.Attempts))
	}
	a1, a2 := st.Attempts[0], st.Attempts[1]
	if len(a1.Findings) == 0 || a1.Findings[0].Severity != domain.SeverityCritical ||
		a1.Findings[0].CWE != "CWE-89" || !a1.Findings[0].Verified {
		t.Fatalf("attempt 1 findings: %+v", a1.Findings)
	}
	for _, f := range a2.Findings {
		if f.IsBlocking() {
			t.Fatalf("attempt 2 has a blocking finding: %+v", f)
		}
	}
	// The feedback the generator received is the code-built template, not the
	// reviewer's prose (the plan's FEEDBACK_MISSING check, moved to the
	// persisted attempt because the static script cannot inspect requests).
	wantSub := "исправь: CWE-89 SQL injection в main.go:19 — используй плейсхолдер $1 и передавай name аргументом в db.Query"
	joined := strings.Join(a1.Feedback, "\n")
	if !strings.Contains(joined, wantSub) {
		t.Fatalf("attempt 1 feedback %q does not contain %q", joined, wantSub)
	}

	raw, err := os.ReadFile(runPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), e2eKey) {
		t.Fatal("API key leaked into the persisted loop run")
	}
	fi, err := os.Stat(runPath)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("loop run mode %o, want 600", fi.Mode().Perm())
	}

	base := h.baseFile(t, "main.go")
	if !strings.Contains(base, "name=$1") {
		t.Fatal("base/main.go does not use the $1 placeholder")
	}
	if strings.Contains(base, `fmt.Sprintf("SELECT`) {
		t.Fatal("base/main.go still contains the SQLi query")
	}

	var cost int64
	var toolCallRecords int
	for _, r := range h.auditRecords(t) {
		cost += r.CostMicroUSD
		if len(r.ResponseToolCallsMasked) > 0 {
			toolCallRecords++
		}
		for _, m := range r.PromptMasked {
			if len(m.ToolCalls) > 0 && string(m.ToolCalls) != "null" {
				toolCallRecords++
			}
		}
	}
	if cost <= 0 {
		t.Fatal("total audit cost is 0, want > 0")
	}
	if toolCallRecords == 0 {
		t.Fatal("no audit record carries tool_calls")
	}
}

// TestLoopE2ENoProgress is the plan's negative variant (а): the reviewer
// keeps returning the same Critical finding and the generator only shuffles
// comments, so the second attempt stops with no_progress and the run fails.
func TestLoopE2ENoProgress(t *testing.T) {
	requireDocker(t)
	h := startHarness(t, filepath.Join(agentRoot, "testdata", "loop", "fake_script_stuck.json"))

	code := h.runAgent(t, nil, goalArg)
	if code != 2 {
		t.Fatalf("exit code %d, want 2 (failed)", code)
	}
	run, _ := h.loopRun(t)
	if run.Status != domain.LoopFailed {
		t.Fatalf("run status %q, want failed", run.Status)
	}
	st := run.Subtasks[0]
	if len(st.Attempts) != 2 {
		t.Fatalf("attempts %d, want 2", len(st.Attempts))
	}
	if got := st.Attempts[1].Result; got != domain.ResultNoProgress {
		t.Fatalf("attempt 2 result %q, want %q", got, domain.ResultNoProgress)
	}
}

// TestLoopE2ESandboxUnavailable is variant (б): with a broken docker binary
// the run must fail with exit 4 before any LLM call happens.
func TestLoopE2ESandboxUnavailable(t *testing.T) {
	requireDocker(t)
	h := startHarness(t, filepath.Join(agentRoot, "testdata", "loop", "fake_script.json"))

	code := h.runAgent(t, agentOverrides{"docker_bin: docker": "docker_bin: /bin/false"}, goalArg)
	if code != 4 {
		t.Fatalf("exit code %d, want 4 (sandbox unavailable)", code)
	}
	if n := strings.Count(h.fakeLog.String(), "request role="); n != 0 {
		t.Fatalf("fake upstream saw %d LLM requests, want 0 (preflight must fail first)", n)
	}
}

// TestLoopE2EBudgetAbort is variant (в): a tiny max_llm_calls budget aborts
// the run with exit 3.
func TestLoopE2EBudgetAbort(t *testing.T) {
	requireDocker(t)
	h := startHarness(t, filepath.Join(agentRoot, "testdata", "loop", "fake_script.json"))

	code := h.runAgent(t, agentOverrides{"max_llm_calls: 500": "max_llm_calls: 3"}, goalArg)
	if code != 3 {
		t.Fatalf("exit code %d, want 3 (aborted)", code)
	}
	run, _ := h.loopRun(t)
	if run.Status != domain.LoopAborted {
		t.Fatalf("run status %q, want aborted", run.Status)
	}
}
