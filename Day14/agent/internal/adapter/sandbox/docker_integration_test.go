//go:build docker || sandbox

// Live tests against the real sandbox image. Build it first:
//
//	make sandbox-image            (Day14/agent)
//	go test -tags docker ./internal/adapter/sandbox/...
//
// SBX_IMAGE overrides the image (default ai-adv-sandbox:1), SBX_PLATFORM its
// platform (default linux/amd64, as make sandbox-image builds it).
package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"ai-adv-agent/internal/policy"
	"ai-adv-agent/internal/port"
)

func liveRunner(t *testing.T) (*Docker, string) {
	t.Helper()
	image := os.Getenv("SBX_IMAGE")
	if image == "" {
		image = "ai-adv-sandbox:1"
	}
	platform, ok := os.LookupEnv("SBX_PLATFORM")
	if !ok {
		platform = "linux/amd64"
	}
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d, err := New(Config{
		DockerBin: "docker", Image: image, Platform: platform, WorkspaceRoot: root,
		Timeout: 3 * time.Minute, KillTimeout: 10 * time.Second,
		MaxOutputBytes: 256 << 10, Memory: "1g", CPUs: "2", Pids: 256, TmpfsSize: "1g",
		CopyMaxFiles: 5000, CopyMaxBytes: 50 << 20, AllowedBinaries: policy.SandboxBinaries,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := d.Preflight(ctx); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	return d, root
}

// templateDir is Day14/sandbox/templates/<stack>.
func templateDir(t *testing.T, stack string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "sandbox", "templates", stack)
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func seedTemplate(t *testing.T, dst, stack string) {
	t.Helper()
	src := templateDir(t, stack)
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		writeFiles(t, dst, map[string]string{rel: string(data)})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func taskDir(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func mustRun(t *testing.T, d *Docker, spec port.CommandSpec) port.CommandResult {
	t.Helper()
	start := time.Now()
	res, err := d.Run(context.Background(), spec)
	if err != nil {
		t.Fatalf("%v: %v", spec.Argv, err)
	}
	t.Logf("%v: exit=%d in %s", spec.Argv, res.ExitCode, time.Since(start).Round(time.Millisecond))
	return res
}

// isolationTest runs inside the container as a Go test: gin works offline,
// the network is unreachable, only /tmp is writable, the user is 65532.
const isolationTest = `package app

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestGinOffline(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
}

func TestNoNetwork(t *testing.T) {
	c, err := net.DialTimeout("tcp", "1.1.1.1:80", 3*time.Second)
	if err == nil {
		c.Close()
		t.Fatal("NETWORK_REACHABLE")
	}
	if _, err := net.LookupHost("proxy.golang.org"); err == nil {
		t.Fatal("DNS_WORKS")
	}
}

func TestWritesConfinedToTmp(t *testing.T) {
	for _, p := range []string{"/src/pwned", "/pwned", "/opt/gomodcache/pwned", "/usr/local/bin/pwned", "/etc/pwned"} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err == nil {
			t.Errorf("WRITE_ALLOWED %s", p)
		}
	}
	if err := os.WriteFile("/tmp/work/ok", []byte("x"), 0o644); err != nil {
		t.Errorf("tmp not writable: %v", err)
	}
}

func TestUnprivileged(t *testing.T) {
	if os.Getuid() != 65532 || os.Getgid() != 65532 {
		t.Fatalf("UID %d GID %d", os.Getuid(), os.Getgid())
	}
	if _, err := os.Stat("/src/app_test.go"); err != nil {
		t.Fatalf("task not mounted at /src: %v", err)
	}
	if wd, _ := os.Getwd(); wd != "/tmp/work" {
		t.Fatalf("cwd %s", wd)
	}
	if os.Getenv("LLM_API_KEY") != "" || os.Getenv("PROXY_API_KEYS") != "" {
		t.Fatal("SECRET_IN_ENV")
	}
}
`

func TestLiveGoIsolation(t *testing.T) {
	t.Setenv("LLM_API_KEY", "sk-live-test-secret")
	d, root := liveRunner(t)
	dir := taskDir(t, root, "iso")
	seedTemplate(t, dir, "go")
	writeFiles(t, dir, map[string]string{"app_test.go": isolationTest})

	res := mustRun(t, d, port.CommandSpec{Argv: []string{"go", "test", "-count=1", "-v", "./..."}, Workdir: dir})
	if res.ExitCode != 0 {
		t.Fatalf("go test failed (exit %d)\nstdout:\n%s\nstderr:\n%s", res.ExitCode, res.Stdout, res.Stderr)
	}
	for _, want := range []string{"PASS: TestGinOffline", "PASS: TestNoNetwork", "PASS: TestWritesConfinedToTmp", "PASS: TestUnprivileged"} {
		if !strings.Contains(res.Stdout, want) {
			t.Errorf("missing %q in output:\n%s", want, res.Stdout)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Error("container wrote into the host task directory")
	}

	vet := mustRun(t, d, port.CommandSpec{Argv: []string{"go", "vet", "./..."}, Workdir: dir})
	if vet.ExitCode != 0 {
		t.Fatalf("go vet: %s", vet.Stderr)
	}
}

func TestLiveTimeoutKillsContainer(t *testing.T) {
	d, root := liveRunner(t)
	dir := taskDir(t, root, "slow")
	writeFiles(t, dir, map[string]string{
		"go.mod":       "module slow\n\ngo 1.26\n",
		"slow_test.go": "package slow\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestSlow(t *testing.T) { time.Sleep(10 * time.Minute) }\n",
	})
	start := time.Now()
	res, err := d.Run(context.Background(), port.CommandSpec{
		Argv: []string{"go", "test", "-timeout=20m", "./..."}, Workdir: dir, Timeout: 8 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut {
		t.Fatalf("not timed out: %+v", res)
	}
	if el := time.Since(start); el > 30*time.Second {
		t.Fatalf("timeout took %s", el)
	}
	// The container must be gone (killed, then removed by --rm).
	deadline := time.Now().Add(15 * time.Second)
	for {
		out, err := d.docker(context.Background(), "ps", "-a", "--filter", "label="+labelOwner+"="+d.owner, "--format", "{{.Names}}")
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(out) == "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("container survived the timeout: %s", out)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func TestLiveForeignDependencyRefused(t *testing.T) {
	d, root := liveRunner(t)
	dir := taskDir(t, root, "deps")
	writeFiles(t, dir, map[string]string{
		"go.mod":  "module deps\n\ngo 1.26\n\nrequire github.com/evil/backdoor v1.0.0\n",
		"main.go": "package main\n\nfunc main() {}\n",
	})
	res := mustRun(t, d, port.CommandSpec{Argv: []string{"go", "build", "./..."}, Workdir: dir})
	if res.ExitCode != 97 || !strings.Contains(res.Stderr, "зависимости не из шаблона") {
		t.Fatalf("exit %d stderr %s", res.ExitCode, res.Stderr)
	}
}

func TestLiveGosecFindsSQLInjection(t *testing.T) {
	d, root := liveRunner(t)
	dir := taskDir(t, root, "sast-go")
	seedTemplate(t, dir, "go")
	writeFiles(t, dir, map[string]string{"handler.go": `package app

import (
	"database/sql"
	"fmt"
)

func Find(db *sql.DB, name string) (*sql.Rows, error) {
	q := fmt.Sprintf("SELECT id FROM users WHERE name='%s'", name)
	return db.Query(q)
}
`})
	res := mustRun(t, d, port.CommandSpec{Argv: []string{"gosec", "-fmt=json", "-no-fail", "-quiet", "./..."}, Workdir: dir})
	if res.ExitCode != 0 || !strings.Contains(res.Stdout, `"rule_id": "G201"`) {
		t.Fatalf("gosec exit %d\nstdout: %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}
}

func TestLiveVue(t *testing.T) {
	d, root := liveRunner(t)
	dir := taskDir(t, root, "vue")
	seedTemplate(t, dir, "vue")
	writeFiles(t, dir, map[string]string{
		"src/Hello.vue": `<script setup lang="ts">
const props = defineProps<{ name: string; bio: string }>()
</script>

<template>
  <p class="hello">Hello, {{ props.name }}</p>
  <div v-html="props.bio"></div>
</template>
`,
		"src/hello.test.ts": `import { mount } from '@vue/test-utils'
import Hello from './Hello.vue'

describe('Hello', () => {
  it('renders the name', () => {
    expect(mount(Hello, { props: { name: 'sandbox', bio: '' } }).find('.hello').text()).toBe('Hello, sandbox')
  })
})
`,
	})
	if res := mustRun(t, d, port.CommandSpec{Argv: []string{"vue-tsc", "--noEmit"}, Workdir: dir}); res.ExitCode != 0 {
		t.Fatalf("vue-tsc: %s\n%s", res.Stdout, res.Stderr)
	}
	if res := mustRun(t, d, port.CommandSpec{Argv: []string{"vitest", "run"}, Workdir: dir}); res.ExitCode != 0 {
		t.Fatalf("vitest: %s\n%s", res.Stdout, res.Stderr)
	}
	res := mustRun(t, d, port.CommandSpec{Argv: []string{"eslint", "-c", "/opt/sandbox/eslint.security.mjs", "-f", "json", "."}, Workdir: dir})
	if !strings.Contains(res.Stdout, `"ruleId":"vue/no-v-html"`) {
		t.Fatalf("eslint exit %d\nstdout: %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}
}
