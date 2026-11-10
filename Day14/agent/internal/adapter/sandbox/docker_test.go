package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"ai-adv-agent/internal/policy"
	"ai-adv-agent/internal/port"
)

// The test binary doubles as a fake docker CLI: with SBX_FAKE_DOCKER set it
// behaves according to SBX_FAKE_MODE and exits, never reaching the tests.
func TestMain(m *testing.M) {
	if os.Getenv("SBX_FAKE_DOCKER") == "1" {
		os.Exit(fakeDocker(os.Args[1:], os.Getenv("SBX_FAKE_MODE")))
	}
	os.Exit(m.Run())
}

func fakeDocker(args []string, mode string) int {
	if len(args) == 0 {
		return 2
	}
	switch args[0] {
	case "version":
		if mode == "daemon-down" {
			fmt.Fprintln(os.Stderr, "Cannot connect to the Docker daemon")
			return 1
		}
		fmt.Println("29.9.2")
	case "image":
		if mode == "no-image" {
			fmt.Fprintln(os.Stderr, "No such image")
			return 1
		}
		if mode == "foreign-arch" {
			fmt.Println("linux/arm64")
			return 0
		}
		fmt.Println("linux/amd64")
	case "ps":
		fmt.Println(os.Getenv("SBX_FAKE_PS"))
	case "kill", "rm":
	case "run":
		switch mode {
		case "exit3":
			fmt.Print("out")
			fmt.Fprint(os.Stderr, "FAIL")
			return 3
		case "big":
			fmt.Print(strings.Repeat("x", 4096))
		case "sleep":
			time.Sleep(time.Minute)
		case "docker-error":
			fmt.Fprint(os.Stderr, "docker: Error response from daemon: No such image")
			return 125
		case "env":
			fmt.Print(strings.Join(os.Environ(), "\n"))
		default:
			fmt.Print("ok")
		}
	}
	return 0
}

// recorder captures every docker invocation the runner makes.
type recorder struct {
	mu    sync.Mutex
	calls [][]string
	envs  [][]string
	mode  string
	ps    string
}

func (r *recorder) exec(ctx context.Context, _ string, args, env []string) *exec.Cmd {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string(nil), args...))
	r.envs = append(r.envs, append([]string(nil), env...))
	r.mu.Unlock()
	cmd := exec.CommandContext(ctx, os.Args[0], args...)
	cmd.Env = append(append([]string(nil), env...),
		"SBX_FAKE_DOCKER=1", "SBX_FAKE_MODE="+r.mode, "SBX_FAKE_PS="+r.ps)
	return cmd
}

func (r *recorder) find(verb string) [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out [][]string
	for _, c := range r.calls {
		if len(c) > 0 && c[0] == verb {
			out = append(out, c)
		}
	}
	return out
}

func testConfig(root string) Config {
	return Config{
		DockerBin:       "docker",
		Image:           "ai-adv-sandbox:1",
		Platform:        "linux/amd64",
		WorkspaceRoot:   root,
		Timeout:         time.Minute,
		KillTimeout:     5 * time.Second,
		MaxOutputBytes:  1024,
		Memory:          "1g",
		CPUs:            "2",
		Pids:            256,
		TmpfsSize:       "1g",
		CopyMaxFiles:    5000,
		CopyMaxBytes:    50 << 20,
		AllowedBinaries: policy.SandboxBinaries,
	}
}

// newTestRunner returns a runner on a temp workspace with a fake docker and
// the task dir <root>/task.
func newTestRunner(t *testing.T, mode string) (*Docker, *recorder, string) {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task := filepath.Join(root, "task")
	if err := os.Mkdir(task, 0o755); err != nil {
		t.Fatal(err)
	}
	d, err := New(testConfig(root))
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{mode: mode}
	d.exec = rec.exec
	d.environ = func() []string {
		return []string{
			"PATH=/usr/bin", "HOME=/home/u", "DOCKER_HOST=unix:///var/run/docker.sock",
			"LLM_API_KEY=sk-secret", "PROXY_API_KEYS=k1,k2", "AWS_SECRET_ACCESS_KEY=x",
		}
	}
	d.instance = "test"
	d.owner = "host:42"
	return d, rec, task
}

func TestRunArgsGolden(t *testing.T) {
	d, rec, task := newTestRunner(t, "ok")
	res, err := d.Run(context.Background(), port.CommandSpec{
		Argv:    []string{"go", "test", "./..."},
		Workdir: task,
		Env:     map[string]string{"CGO_ENABLED": "0", "GOFLAGS": "-mod=readonly"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || res.Stdout != "ok" {
		t.Fatalf("result %+v", res)
	}
	runs := rec.find("run")
	if len(runs) != 1 {
		t.Fatalf("docker run calls: %v", rec.calls)
	}
	want := []string{
		"run", "--rm", "--pull", "never",
		"--platform", "linux/amd64",
		"--name", "sbx-test-1",
		"--label", "ai-adv.sandbox=1",
		"--label", "ai-adv.owner=host:42",
		"--network", "none",
		"--read-only",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--user", "65532:65532",
		"--pids-limit", "256",
		"--memory", "1g",
		"--memory-swap", "1g",
		"--cpus", "2",
		"--tmpfs", "/tmp:rw,exec,nosuid,nodev,mode=1777,size=1g",
		"--mount", "type=bind,source=" + task + ",target=/src,readonly",
		"--workdir", "/tmp",
		"--env", "CGO_ENABLED=0",
		"--env", "CI=true",
		"--env", "GOCACHE=/tmp/gocache",
		"--env", "GOFLAGS=-mod=readonly",
		"--env", "GOMODCACHE=/opt/gomodcache",
		"--env", "GOPROXY=off",
		"--env", "GOTELEMETRY=off",
		"--env", "GOTMPDIR=/tmp",
		"--env", "GOTOOLCHAIN=local",
		"--env", "HOME=/tmp",
		"--env", "NPM_CONFIG_IGNORE_SCRIPTS=true",
		"--env", "NPM_CONFIG_OFFLINE=true",
		"--env", "XDG_CACHE_HOME=/tmp/.cache",
		"ai-adv-sandbox:1",
		"/usr/local/bin/sbx-run", "--stack", "go",
		"--max-files", "5000", "--max-bytes", "52428800",
		"--", "go", "test", "./...",
	}
	if !slices.Equal(runs[0], want) {
		t.Fatalf("docker run argv mismatch\n got: %q\nwant: %q", runs[0], want)
	}
	for i, a := range runs[0] {
		if (a == "--env" || a == "-e") && !strings.Contains(runs[0][i+1], "=") {
			t.Errorf("env without value: %q", runs[0][i+1])
		}
	}
}

func TestProcessEnvHasNoSecrets(t *testing.T) {
	d, rec, task := newTestRunner(t, "env")
	res, err := d.Run(context.Background(), port.CommandSpec{Argv: []string{"go", "version"}, Workdir: task})
	if err != nil {
		t.Fatal(err)
	}
	env := rec.envs[0]
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(hostEnvKeys, k) {
			t.Errorf("docker CLI got env %q", kv)
		}
	}
	for _, secret := range []string{"sk-secret", "LLM_API_KEY", "PROXY_API_KEYS", "AWS_SECRET"} {
		if strings.Contains(strings.Join(env, "\n"), secret) || strings.Contains(strings.Join(rec.calls[0], " "), secret) {
			t.Errorf("secret %q leaked to docker", secret)
		}
	}
	if !strings.Contains(res.Stdout, "DOCKER_HOST=unix:///var/run/docker.sock") {
		t.Errorf("DOCKER_HOST must reach the CLI: %q", res.Stdout)
	}
}

func TestRunRejects(t *testing.T) {
	d, rec, task := newTestRunner(t, "ok")
	outside := t.TempDir()
	link := filepath.Join(task, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		spec port.CommandSpec
	}{
		{"empty argv", port.CommandSpec{Workdir: task}},
		{"binary not allowed", port.CommandSpec{Argv: []string{"curl", "http://x"}, Workdir: task}},
		{"shell", port.CommandSpec{Argv: []string{"sh", "-c", "id"}, Workdir: task}},
		{"binary by path", port.CommandSpec{Argv: []string{"/usr/local/go/bin/go", "test"}, Workdir: task}},
		{"global deny go run", port.CommandSpec{Argv: []string{"go", "run", "."}, Workdir: task}},
		{"global deny toolexec", port.CommandSpec{Argv: []string{"go", "test", "-toolexec=x", "./..."}, Workdir: task}},
		{"shell metachar", port.CommandSpec{Argv: []string{"go", "test", "./...;id"}, Workdir: task}},
		{"npm install", port.CommandSpec{Argv: []string{"npm", "install", "left-pad"}, Workdir: task}},
		{"npm ci with scripts", port.CommandSpec{Argv: []string{"npm", "ci"}, Workdir: task}},
		{"npm ci extra flag", port.CommandSpec{Argv: []string{"npm", "ci", "--ignore-scripts", "--registry=http://x"}, Workdir: task}},
		{"workdir outside root", port.CommandSpec{Argv: []string{"go", "test"}, Workdir: outside}},
		{"workdir symlink escape", port.CommandSpec{Argv: []string{"go", "test"}, Workdir: link}},
		{"workdir relative", port.CommandSpec{Argv: []string{"go", "test"}, Workdir: "task"}},
		{"workdir dotdot", port.CommandSpec{Argv: []string{"go", "test"}, Workdir: filepath.Join(task, "..", "..")}},
		{"workdir missing", port.CommandSpec{Argv: []string{"go", "test"}, Workdir: filepath.Join(task, "nope")}},
		{"env key not allowed", port.CommandSpec{Argv: []string{"go", "test"}, Workdir: task, Env: map[string]string{"LLM_API_KEY": "x"}}},
		{"env value not allowed", port.CommandSpec{Argv: []string{"go", "test"}, Workdir: task, Env: map[string]string{"GOFLAGS": "-toolexec=/bin/sh"}}},
		{"unknown stack", port.CommandSpec{Argv: []string{"go", "test"}, Workdir: task, Stack: "rust"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := d.Run(context.Background(), tt.spec)
			if !errors.Is(err, ErrRejected) {
				t.Fatalf("err = %v, want ErrRejected", err)
			}
		})
	}
	if runs := rec.find("run"); len(runs) != 0 {
		t.Fatalf("rejected commands reached docker: %v", runs)
	}
}

func TestRunAllowsNpmCiAndNarrowedBinaries(t *testing.T) {
	d, rec, task := newTestRunner(t, "ok")
	if _, err := d.Run(context.Background(), port.CommandSpec{
		Argv: []string{"npm", "ci", "--offline", "--ignore-scripts"}, Workdir: task,
	}); err != nil {
		t.Fatalf("npm ci --ignore-scripts: %v", err)
	}
	run := rec.find("run")[0]
	if i := slices.Index(run, "--stack"); i < 0 || run[i+1] != "vue" {
		t.Fatalf("npm must infer the vue stack: %q", run)
	}

	cfg := testConfig(d.cfg.WorkspaceRoot)
	cfg.AllowedBinaries = []string{"go"}
	narrow, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	narrow.exec = rec.exec
	if _, err := narrow.Run(context.Background(), port.CommandSpec{Argv: []string{"gofmt", "-l", "."}, Workdir: task}); !errors.Is(err, ErrRejected) {
		t.Fatalf("binary outside allowed_binaries: err = %v", err)
	}
}

func TestRunWorkdirIsRoot(t *testing.T) {
	d, _, _ := newTestRunner(t, "ok")
	if _, err := d.Run(context.Background(), port.CommandSpec{Argv: []string{"go", "vet", "./..."}, Workdir: d.cfg.WorkspaceRoot}); err != nil {
		t.Fatalf("workspace root itself must be accepted: %v", err)
	}
}

func TestRunExitCodeAndTruncation(t *testing.T) {
	d, _, task := newTestRunner(t, "exit3")
	res, err := d.Run(context.Background(), port.CommandSpec{Argv: []string{"go", "test", "./..."}, Workdir: task})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 3 || res.Stdout != "out" || res.Stderr != "FAIL" || res.Truncated || res.TimedOut {
		t.Fatalf("result %+v", res)
	}

	d, _, task = newTestRunner(t, "big")
	res, err = d.Run(context.Background(), port.CommandSpec{Argv: []string{"go", "test"}, Workdir: task, MaxOutput: 100})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || len(res.Stdout) != 100 {
		t.Fatalf("spec cap below config: truncated=%v len=%d", res.Truncated, len(res.Stdout))
	}
	res, err = d.Run(context.Background(), port.CommandSpec{Argv: []string{"go", "test"}, Workdir: task, MaxOutput: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || len(res.Stdout) != 1024 {
		t.Fatalf("spec cap above config must be clamped: truncated=%v len=%d", res.Truncated, len(res.Stdout))
	}
}

func TestRunTimeoutKillsContainer(t *testing.T) {
	d, rec, task := newTestRunner(t, "sleep")
	res, err := d.Run(context.Background(), port.CommandSpec{
		Argv: []string{"go", "test", "./..."}, Workdir: task, Timeout: 50 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut || res.ExitCode != -1 {
		t.Fatalf("result %+v", res)
	}
	kills := rec.find("kill")
	if len(kills) != 1 || !slices.Equal(kills[0], []string{"kill", "sbx-test-1"}) {
		t.Fatalf("kill calls: %v", kills)
	}
}

func TestRunParentCancelKillsContainer(t *testing.T) {
	d, rec, task := newTestRunner(t, "sleep")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Cancel once docker run has started.
		for len(rec.find("run")) == 0 {
			time.Sleep(time.Millisecond)
		}
		cancel()
	}()
	res, err := d.Run(ctx, port.CommandSpec{Argv: []string{"go", "test"}, Workdir: task})
	if !errors.Is(err, context.Canceled) || res.TimedOut {
		t.Fatalf("err = %v, res = %+v", err, res)
	}
	if len(rec.find("kill")) != 1 {
		t.Fatalf("container not killed on cancel: %v", rec.calls)
	}
}

func TestRunDockerFailureIsUnavailable(t *testing.T) {
	d, _, task := newTestRunner(t, "docker-error")
	_, err := d.Run(context.Background(), port.CommandSpec{Argv: []string{"go", "test"}, Workdir: task})
	if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), "No such image") {
		t.Fatalf("err = %v", err)
	}
}

func TestNoDockerBinary(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	cfg.DockerBin = filepath.Join(root, "no-such-docker")
	d, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Preflight(context.Background()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Preflight: err = %v, want ErrUnavailable", err)
	}
	if _, err := d.Run(context.Background(), port.CommandSpec{Argv: []string{"go", "test"}, Workdir: root}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Run: err = %v, want ErrUnavailable", err)
	}
}

func TestPreflight(t *testing.T) {
	tests := []struct {
		mode    string
		wantErr bool
	}{
		{"ok", false},
		{"daemon-down", true},
		{"no-image", true},
		{"foreign-arch", true},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			d, rec, _ := newTestRunner(t, tt.mode)
			err := d.Preflight(context.Background())
			if tt.wantErr != (err != nil) {
				t.Fatalf("err = %v", err)
			}
			if tt.wantErr && !errors.Is(err, ErrUnavailable) {
				t.Fatalf("err = %v, want ErrUnavailable", err)
			}
			if !tt.wantErr {
				if img := rec.find("image"); len(img) != 1 || img[0][len(img[0])-1] != "ai-adv-sandbox:1" {
					t.Fatalf("image inspect: %v", rec.calls)
				}
			}
		})
	}
}

func TestCleanupRemovesOnlyStaleOwnContainers(t *testing.T) {
	d, rec, _ := newTestRunner(t, "ok")
	d.host = "myhost"
	d.alive = func(pid int) bool { return pid == 100 }
	self := os.Getpid()
	rec.ps = strings.Join([]string{
		"sbx-a-1\tmyhost:100",                   // live owner
		"sbx-b-1\tmyhost:200",                   // dead owner -> remove
		"sbx-c-1\totherhost:200",                // other host
		fmt.Sprintf("sbx-d-1\tmyhost:%d", self), // this process
		"web-1\tmyhost:200",                     // not ours by name
		"sbx-e-1\t",                             // no owner label
		"sbx-f-1\tmyhost:300",                   // dead owner -> remove
	}, "\n")
	n, err := d.Cleanup(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rms := rec.find("rm")
	if n != 2 || len(rms) != 1 || !slices.Equal(rms[0], []string{"rm", "-f", "sbx-b-1", "sbx-f-1"}) {
		t.Fatalf("n=%d rm=%v", n, rms)
	}
}

func TestNewValidates(t *testing.T) {
	root := t.TempDir()
	mutate := []struct {
		name string
		f    func(*Config)
	}{
		{"no image", func(c *Config) { c.Image = "" }},
		{"flag image", func(c *Config) { c.Image = "--privileged" }},
		{"relative root", func(c *Config) { c.WorkspaceRoot = "workspace" }},
		{"fs root", func(c *Config) { c.WorkspaceRoot = "/" }},
		{"zero timeout", func(c *Config) { c.Timeout = 0 }},
		{"zero output", func(c *Config) { c.MaxOutputBytes = 0 }},
		{"unknown binary", func(c *Config) { c.AllowedBinaries = []string{"go", "bash"} }},
		{"no binaries", func(c *Config) { c.AllowedBinaries = nil }},
	}
	for _, m := range mutate {
		t.Run(m.name, func(t *testing.T) {
			cfg := testConfig(root)
			m.f(&cfg)
			if _, err := New(cfg); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestCapWriter(t *testing.T) {
	w := &capWriter{limit: 5}
	for _, chunk := range []string{"ab", "cd", "ef", "gh"} {
		if n, err := w.Write([]byte(chunk)); n != len(chunk) || err != nil {
			t.Fatalf("Write = %d, %v", n, err)
		}
	}
	if w.String() != "abcde" || !w.truncated {
		t.Fatalf("got %q truncated=%v", w.String(), w.truncated)
	}
	u := &capWriter{limit: 2}
	_, _ = u.Write([]byte("я")) // 2 bytes: fits
	_, _ = u.Write([]byte("я"))
	if u.String() != "я" {
		t.Fatalf("utf8: %q", u.String())
	}
	v := &capWriter{limit: 1}
	_, _ = v.Write([]byte("я"))
	if v.String() != "�" {
		t.Fatalf("cut rune must be replaced: %q", v.String())
	}
}
