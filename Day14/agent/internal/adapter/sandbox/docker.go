// Package sandbox implements port.CommandRunner on top of the docker CLI.
//
// Every command runs in a fresh, throw-away container of the sandbox image
// (Day14/sandbox/Dockerfile): no network, read-only root filesystem, all
// capabilities dropped, no privilege escalation, an unprivileged user, pid /
// memory / CPU limits and a size-capped tmpfs as the only writable place. The
// task directory is bind-mounted read-only at /src; sbx-run inside the image
// copies it to /tmp/work and execs the argv there. Nothing goes through a
// shell, neither on the host nor in the container.
//
// The runner is fail-closed: an unknown binary, a globally denied argv, a
// workdir outside the workspace root, an env key off the allowlist or an
// unreachable docker daemon is an error, never a fallback to running the
// command some other way.
package sandbox

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"ai-adv-agent/internal/policy"
	"ai-adv-agent/internal/port"
)

// Errors. ErrUnavailable means docker or the image cannot be used: callers
// that require the sandbox must stop (fail-closed). ErrRejected means this
// particular command is not allowed to run.
var (
	ErrUnavailable = errors.New("sandbox недоступен")
	ErrRejected    = errors.New("sandbox: команда отклонена")
)

// In-container layout, fixed by the image.
const (
	containerSrc  = "/src"
	containerTmp  = "/tmp"
	sbxRun        = "/usr/local/bin/sbx-run"
	sandboxUser   = "65532:65532"
	labelSandbox  = "ai-adv.sandbox"
	labelOwner    = "ai-adv.owner"
	namePrefix    = "sbx-"
	dockerRunFail = 125 // docker run's own failure (daemon, image, flags)
)

// Config is the runner's configuration (config.SandboxConfig, resolved).
type Config struct {
	DockerBin string
	Image     string
	// Platform ("linux/amd64") is passed as docker run --platform; empty =
	// the image's own. Set it when the image is not native to the daemon
	// (emulation), so docker neither warns nor guesses.
	Platform        string
	WorkspaceRoot   string // absolute
	Timeout         time.Duration
	KillTimeout     time.Duration
	MaxOutputBytes  int
	Memory          string
	CPUs            string
	Pids            int
	TmpfsSize       string
	CopyMaxFiles    int
	CopyMaxBytes    int64
	AllowedBinaries []string
}

// containerEnv is the fixed environment of every container. It is applied
// after the profile env, so a profile cannot override it.
var containerEnv = map[string]string{
	"GOFLAGS":                   "-mod=readonly",
	"GOPROXY":                   "off",
	"GOTOOLCHAIN":               "local",
	"GOCACHE":                   "/tmp/gocache",
	"GOTMPDIR":                  "/tmp",
	"GOMODCACHE":                "/opt/gomodcache",
	"GOTELEMETRY":               "off",
	"HOME":                      "/tmp",
	"XDG_CACHE_HOME":            "/tmp/.cache",
	"CI":                        "true",
	"NPM_CONFIG_OFFLINE":        "true",
	"NPM_CONFIG_IGNORE_SCRIPTS": "true",
}

// profileEnvRules is the allowlist (keys and values) for env coming with the
// command spec. It repeats the profile loader's allowlist: a value such as
// GOFLAGS=-toolexec=… would bypass the argv checks.
var profileEnvRules = map[string]func(string) bool{
	"GOTOOLCHAIN": func(v string) bool { return v == "local" },
	"GOPROXY":     func(v string) bool { return v == "off" },
	"GOFLAGS":     func(v string) bool { return v == "-mod=readonly" },
	"CGO_ENABLED": func(v string) bool { return v == "0" || v == "1" },
	"CI":          func(v string) bool { return v == "true" || v == "1" },
}

// hostEnvKeys is everything the docker CLI process inherits from the agent.
// LLM_API_KEY, PROXY_* and the rest of the agent's environment are not passed.
var hostEnvKeys = []string{
	"PATH", "HOME", "DOCKER_HOST", "DOCKER_CONFIG", "DOCKER_CONTEXT",
	"DOCKER_CERT_PATH", "DOCKER_TLS_VERIFY",
}

// goStack are the binaries of the go stack; the rest belong to vue.
var goStack = map[string]bool{"go": true, "gofmt": true, "gosec": true}

// execFunc creates the docker CLI process. Tests replace it with a fake.
type execFunc func(ctx context.Context, bin string, args, env []string) *exec.Cmd

func defaultExec(ctx context.Context, bin string, args, env []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	return cmd
}

// Docker is a port.CommandRunner backed by docker run.
type Docker struct {
	cfg      Config
	allowed  map[string]bool
	owner    string // host:pid, the label value marking this process' containers
	instance string // random, part of every container name
	seq      atomic.Uint64

	exec    execFunc
	environ func() []string
	alive   func(pid int) bool
	host    string
}

var _ port.CommandRunner = (*Docker)(nil)

// New returns a runner. It validates the configuration but does not contact
// docker: call Preflight for that.
func New(cfg Config) (*Docker, error) {
	switch {
	case strings.TrimSpace(cfg.Image) == "" || strings.HasPrefix(cfg.Image, "-"):
		return nil, fmt.Errorf("sandbox: image не задан")
	case strings.TrimSpace(cfg.DockerBin) == "":
		return nil, fmt.Errorf("sandbox: docker_bin не задан")
	case !filepath.IsAbs(cfg.WorkspaceRoot) || filepath.Clean(cfg.WorkspaceRoot) == "/":
		return nil, fmt.Errorf("sandbox: workspace_root должен быть абсолютным каталогом, получено %q", cfg.WorkspaceRoot)
	case cfg.Timeout <= 0 || cfg.KillTimeout <= 0 || cfg.MaxOutputBytes <= 0 || cfg.Pids <= 0 ||
		cfg.CopyMaxFiles <= 0 || cfg.CopyMaxBytes <= 0:
		return nil, fmt.Errorf("sandbox: лимиты должны быть > 0")
	case cfg.Memory == "" || cfg.CPUs == "" || cfg.TmpfsSize == "":
		return nil, fmt.Errorf("sandbox: memory, cpus и tmpfs_size обязательны")
	}
	allowed := map[string]bool{}
	for _, b := range cfg.AllowedBinaries {
		if !policy.IsSandboxBinary(b) {
			return nil, fmt.Errorf("sandbox: %q нет в образе песочницы", b)
		}
		allowed[b] = true
	}
	if len(allowed) == 0 {
		return nil, fmt.Errorf("sandbox: allowed_binaries пуст")
	}
	host, _ := os.Hostname()
	var rnd [4]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return nil, fmt.Errorf("sandbox: random: %w", err)
	}
	d := &Docker{
		cfg:      cfg,
		allowed:  allowed,
		instance: hex.EncodeToString(rnd[:]),
		exec:     defaultExec,
		environ:  os.Environ,
		alive:    processAlive,
		host:     sanitizeLabel(host),
	}
	d.owner = d.host + ":" + strconv.Itoa(os.Getpid())
	return d, nil
}

// Run executes s.Argv in a fresh container. A non-zero exit code is a result,
// not an error; errors mean the command was rejected (ErrRejected) or could
// not be run (ErrUnavailable, ctx errors).
func (d *Docker) Run(ctx context.Context, s port.CommandSpec) (port.CommandResult, error) {
	prep, err := d.prepare(s)
	if err != nil {
		return port.CommandResult{}, err
	}
	name := fmt.Sprintf("%s%s-%d", namePrefix, d.instance, d.seq.Add(1))
	args := d.runArgs(name, prep)

	runCtx, cancel := context.WithTimeout(ctx, prep.timeout)
	defer cancel()

	stdout := &capWriter{limit: prep.maxOutput}
	stderr := &capWriter{limit: prep.maxOutput}
	cmd := d.exec(runCtx, d.cfg.DockerBin, args, d.processEnv())
	cmd.Stdout, cmd.Stderr = stdout, stderr
	// Once the CLI is killed, do not wait forever for pipes a stray child
	// might still hold.
	cmd.WaitDelay = d.cfg.KillTimeout

	start := time.Now()
	runErr := cmd.Run()
	res := port.CommandResult{Duration: time.Since(start)}

	if runCtx.Err() != nil {
		// CommandContext killed only the CLI; the container keeps running
		// until it is killed by name. Use a context that survives ctx.
		d.kill(context.WithoutCancel(ctx), name)
		res.ExitCode = -1
		res.Stdout, res.Stderr = stdout.String(), stderr.String()
		res.Truncated = stdout.truncated || stderr.truncated
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		res.TimedOut = true
		return res, nil
	}

	res.Stdout, res.Stderr = stdout.String(), stderr.String()
	res.Truncated = stdout.truncated || stderr.truncated
	if runErr != nil {
		var ee *exec.ExitError
		if !errors.As(runErr, &ee) {
			return port.CommandResult{}, fmt.Errorf("%w: запуск %s: %v", ErrUnavailable, d.cfg.DockerBin, runErr)
		}
		res.ExitCode = ee.ExitCode()
		if res.ExitCode == dockerRunFail {
			return port.CommandResult{}, fmt.Errorf("%w: docker run: %s", ErrUnavailable, tail(res.Stderr, 400))
		}
	}
	return res, nil
}

// prepared is a validated CommandSpec.
type prepared struct {
	argv      []string
	workdir   string // real path
	stack     string
	env       []string // KEY=VALUE, sorted
	timeout   time.Duration
	maxOutput int
}

func reject(format string, a ...interface{}) error {
	return fmt.Errorf("%w: %s", ErrRejected, fmt.Sprintf(format, a...))
}

// prepare validates the spec: argv against the binary allowlist and the
// global deny list, the workdir against the workspace root, the env against
// its allowlist; and clamps the timeout and the output cap to the config.
func (d *Docker) prepare(s port.CommandSpec) (prepared, error) {
	if err := policy.CheckGlobalExec(s.Argv); err != nil {
		return prepared{}, reject("%v", err)
	}
	bin := s.Argv[0]
	if !d.allowed[bin] {
		return prepared{}, reject("программа %q не входит в sandbox.allowed_binaries", bin)
	}
	if bin == "npm" && !npmAllowed(s.Argv[1:]) {
		return prepared{}, reject("npm разрешён только как `npm ci --ignore-scripts [--offline]`")
	}

	stack := s.Stack
	switch stack {
	case "go", "vue":
	case "":
		stack = "vue"
		if goStack[bin] {
			stack = "go"
		}
	default:
		return prepared{}, reject("неизвестный стек %q (go или vue)", s.Stack)
	}

	workdir, err := d.checkWorkdir(s.Workdir)
	if err != nil {
		return prepared{}, err
	}

	env := map[string]string{}
	for k, v := range s.Env {
		rule, ok := profileEnvRules[k]
		if !ok || !rule(v) {
			return prepared{}, reject("переменная окружения %s не разрешена в песочнице", k)
		}
		env[k] = v
	}
	for k, v := range containerEnv {
		env[k] = v
	}
	pairs := make([]string, 0, len(env))
	for k, v := range env {
		pairs = append(pairs, k+"="+v)
	}
	sort.Strings(pairs)

	timeout := d.cfg.Timeout
	if s.Timeout > 0 && s.Timeout < timeout {
		timeout = s.Timeout
	}
	maxOut := d.cfg.MaxOutputBytes
	if s.MaxOutput > 0 && s.MaxOutput < maxOut {
		maxOut = s.MaxOutput
	}
	return prepared{
		argv: append([]string(nil), s.Argv...), workdir: workdir, stack: stack,
		env: pairs, timeout: timeout, maxOutput: maxOut,
	}, nil
}

// checkWorkdir resolves symlinks and requires the result to be a directory
// inside (or equal to) the workspace root. Characters that would change the
// meaning of the --mount value are refused.
func (d *Docker) checkWorkdir(dir string) (string, error) {
	if dir == "" || !filepath.IsAbs(dir) {
		return "", reject("workdir должен быть абсолютным путём, получено %q", dir)
	}
	root, err := filepath.EvalSymlinks(d.cfg.WorkspaceRoot)
	if err != nil {
		return "", reject("workspace_root %q недоступен: %v", d.cfg.WorkspaceRoot, err)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", reject("workdir %q недоступен: %v", dir, err)
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", reject("workdir %q вне sandbox.workspace_root %q", dir, d.cfg.WorkspaceRoot)
	}
	fi, err := os.Stat(real)
	if err != nil || !fi.IsDir() {
		return "", reject("workdir %q не каталог", dir)
	}
	if strings.ContainsAny(real, ",\"'\n\r\x00") {
		return "", reject("workdir %q содержит недопустимые символы", dir)
	}
	return real, nil
}

// npmAllowed accepts only `npm ci --ignore-scripts` with an optional
// --offline, in any order.
func npmAllowed(args []string) bool {
	if len(args) == 0 || args[0] != "ci" {
		return false
	}
	ignore := false
	for _, a := range args[1:] {
		switch a {
		case "--ignore-scripts":
			ignore = true
		case "--offline":
		default:
			return false
		}
	}
	return ignore
}

// runArgs builds the docker run argv. The order is fixed so tests can compare
// it as a golden value.
func (d *Docker) runArgs(name string, p prepared) []string {
	args := []string{
		"run", "--rm", "--pull", "never",
	}
	if d.cfg.Platform != "" {
		args = append(args, "--platform", d.cfg.Platform)
	}
	args = append(args,
		"--name", name,
		"--label", labelSandbox+"=1",
		"--label", labelOwner+"="+d.owner,
		"--network", "none",
		"--read-only",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--user", sandboxUser,
		"--pids-limit", strconv.Itoa(d.cfg.Pids),
		"--memory", d.cfg.Memory,
		"--memory-swap", d.cfg.Memory,
		"--cpus", d.cfg.CPUs,
		"--tmpfs", containerTmp+":rw,exec,nosuid,nodev,mode=1777,size="+d.cfg.TmpfsSize,
		"--mount", "type=bind,source="+p.workdir+",target="+containerSrc+",readonly",
		"--workdir", containerTmp,
	)
	for _, kv := range p.env {
		args = append(args, "--env", kv)
	}
	args = append(args,
		d.cfg.Image,
		sbxRun,
		"--stack", p.stack,
		"--max-files", strconv.Itoa(d.cfg.CopyMaxFiles),
		"--max-bytes", strconv.FormatInt(d.cfg.CopyMaxBytes, 10),
		"--",
	)
	return append(args, p.argv...)
}

// processEnv is the docker CLI's own environment: only what it needs to
// reach the daemon.
func (d *Docker) processEnv() []string {
	keep := map[string]bool{}
	for _, k := range hostEnvKeys {
		keep[k] = true
	}
	var env []string
	for _, kv := range d.environ() {
		k, _, ok := strings.Cut(kv, "=")
		if ok && keep[k] {
			env = append(env, kv)
		}
	}
	return env
}

// docker runs a short docker CLI command bounded by KillTimeout.
func (d *Docker) docker(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, d.cfg.KillTimeout)
	defer cancel()
	out := &capWriter{limit: 64 << 10}
	errOut := &capWriter{limit: 4 << 10}
	cmd := d.exec(ctx, d.cfg.DockerBin, args, d.processEnv())
	cmd.Stdout, cmd.Stderr = out, errOut
	cmd.WaitDelay = d.cfg.KillTimeout
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(errOut.String()); msg != "" {
			return out.String(), fmt.Errorf("docker %s: %w: %s", args[0], err, tail(msg, 400))
		}
		return out.String(), fmt.Errorf("docker %s: %w", args[0], err)
	}
	return out.String(), nil
}

func (d *Docker) kill(ctx context.Context, name string) {
	// Errors are expected when the container already exited (--rm removed
	// it). A container that survives is removed by the next Cleanup.
	_, _ = d.docker(ctx, "kill", name)
}

// Preflight checks that the daemon answers, the image is present locally
// (--pull never: the runner never pulls) and built for sandbox.platform,
// then removes containers
// left over by dead agent processes. Any failure wraps ErrUnavailable.
func (d *Docker) Preflight(ctx context.Context) error {
	v, err := d.docker(ctx, "version", "--format", "{{.Server.Version}}")
	if err != nil {
		return fmt.Errorf("%w: docker daemon не отвечает: %v", ErrUnavailable, err)
	}
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("%w: docker daemon не сообщил версию", ErrUnavailable)
	}
	img, err := d.docker(ctx, "image", "inspect", "--format", "{{.Os}}/{{.Architecture}}", d.cfg.Image)
	if err != nil {
		return fmt.Errorf("%w: образ %s не найден локально (make sandbox-image): %v", ErrUnavailable, d.cfg.Image, err)
	}
	// With --pull never a platform mismatch would fail every run; report it
	// once, here, with the fix.
	if want := d.cfg.Platform; want != "" && strings.TrimSpace(img) != platformBase(want) {
		return fmt.Errorf("%w: образ %s собран для %s, а sandbox.platform = %s — пересоберите: make sandbox-image",
			ErrUnavailable, d.cfg.Image, strings.TrimSpace(img), want)
	}
	if _, err := d.Cleanup(ctx); err != nil {
		return fmt.Errorf("%w: очистка старых контейнеров: %v", ErrUnavailable, err)
	}
	return nil
}

// Cleanup force-removes sandbox containers whose owner process on this host
// is gone (a crashed agent, a kill that did not land). Containers of live
// agent processes and of other hosts are left alone. It returns how many
// containers were removed.
func (d *Docker) Cleanup(ctx context.Context) (int, error) {
	out, err := d.docker(ctx, "ps", "-a",
		"--filter", "label="+labelSandbox+"=1",
		"--format", `{{.Names}}\t{{.Label "`+labelOwner+`"}}`)
	if err != nil {
		return 0, err
	}
	var stale []string
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		name, owner, _ := strings.Cut(strings.TrimSpace(sc.Text()), "\t")
		if !strings.HasPrefix(name, namePrefix) || strings.HasPrefix(name, "-") {
			continue
		}
		host, pidStr, ok := strings.Cut(owner, ":")
		if !ok || host != d.host {
			continue
		}
		pid, err := strconv.Atoi(pidStr)
		if err != nil || pid <= 0 {
			continue
		}
		if pid == os.Getpid() || d.alive(pid) {
			continue
		}
		stale = append(stale, name)
	}
	if len(stale) == 0 {
		return 0, nil
	}
	if _, err := d.docker(ctx, append([]string{"rm", "-f"}, stale...)...); err != nil {
		return 0, err
	}
	return len(stale), nil
}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

// sanitizeLabel keeps a hostname safe as a label value and as the first
// part of "host:pid".
func sanitizeLabel(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "localhost"
	}
	return b.String()
}

// platformBase drops the variant: "linux/arm64/v8" -> "linux/arm64".
func platformBase(p string) string {
	parts := strings.SplitN(p, "/", 3)
	if len(parts) < 2 {
		return p
	}
	return parts[0] + "/" + parts[1]
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// capWriter keeps the first limit bytes and drops the rest, reporting the
// full length to the writer so the process never sees a broken pipe.
type capWriter struct {
	buf       []byte
	limit     int
	truncated bool
}

func (w *capWriter) Write(p []byte) (int, error) {
	room := w.limit - len(w.buf)
	if room >= len(p) {
		w.buf = append(w.buf, p...)
		return len(p), nil
	}
	if room > 0 {
		w.buf = append(w.buf, p[:room]...)
	}
	w.truncated = true
	return len(p), nil
}

// String returns the captured output as valid UTF-8 (a cut in the middle of a
// rune is replaced).
func (w *capWriter) String() string { return strings.ToValidUTF8(string(w.buf), "�") }
