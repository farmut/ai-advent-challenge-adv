package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func TestCopyTree(t *testing.T) {
	src := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	write(t, outside, "secret", 0o600)
	write(t, filepath.Join(src, "main.go"), "package main", 0o600)
	write(t, filepath.Join(src, "pkg", "a.go"), "package pkg", 0o644)
	write(t, filepath.Join(src, "run.sh"), "#!", 0o755)
	write(t, filepath.Join(src, "node_modules", "x", "index.js"), "x", 0o644)
	write(t, filepath.Join(src, ".git", "HEAD"), "ref", 0o644)
	if err := os.Symlink(outside, filepath.Join(src, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(src, "dirlink")); err != nil {
		t.Fatal(err)
	}

	dst := filepath.Join(t.TempDir(), "work")
	if err := copyTree(src, dst, 100, 1<<20); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"main.go", "pkg/a.go", "run.sh"} {
		if _, err := os.Stat(filepath.Join(dst, want)); err != nil {
			t.Errorf("%s not copied: %v", want, err)
		}
	}
	for _, skipped := range []string{"link", "dirlink", "node_modules", ".git"} {
		if _, err := os.Lstat(filepath.Join(dst, skipped)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s must be skipped, got err=%v", skipped, err)
		}
	}
	if fi, _ := os.Stat(filepath.Join(dst, "main.go")); fi.Mode().Perm() != 0o644 {
		t.Errorf("main.go mode = %v, want 0644", fi.Mode().Perm())
	}
	if fi, _ := os.Stat(filepath.Join(dst, "run.sh")); fi.Mode().Perm() != 0o755 {
		t.Errorf("run.sh mode = %v, want 0755", fi.Mode().Perm())
	}
}

func TestCopyTreeLimits(t *testing.T) {
	tests := []struct {
		name     string
		files    int
		size     int
		maxFiles int
		maxBytes int64
	}{
		{"too many files", 5, 1, 4, 1 << 20},
		{"too many bytes", 2, 600, 100, 1000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := t.TempDir()
			for i := 0; i < tt.files; i++ {
				write(t, filepath.Join(src, "f"+string(rune('a'+i))), strings.Repeat("x", tt.size), 0o644)
			}
			err := copyTree(src, filepath.Join(t.TempDir(), "w"), tt.maxFiles, tt.maxBytes)
			if !errors.Is(err, errLimit) {
				t.Fatalf("err = %v, want errLimit", err)
			}
		})
	}
}

func TestCheckGoDeps(t *testing.T) {
	cache := t.TempDir()
	write(t, filepath.Join(cache, "cache/download/github.com/gin-gonic/gin/@v/v1.12.0.mod"), "module x", 0o644)
	write(t, filepath.Join(cache, "cache/download/github.com/!burnt!sushi/toml/@v/v1.0.0.mod"), "module y", 0o644)

	tests := []struct {
		name, gomod string
		wantErr     string
	}{
		{"no go.mod", "", ""},
		{"all present", "module a\n\ngo 1.26\n\nrequire github.com/gin-gonic/gin v1.12.0\nrequire (\n\tgithub.com/BurntSushi/toml v1.0.0 // indirect\n)\n", ""},
		{"missing", "module a\nrequire (\n\tgithub.com/gin-gonic/gin v1.12.0\n\tevil.example/x v0.1.0\n)\n", "evil.example/x@v0.1.0"},
		{"traversal", "module a\nrequire ../../etc v1.0.0\n", "../../etc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.gomod != "" {
				write(t, filepath.Join(dir, "go.mod"), tt.gomod, 0o644)
			}
			err := checkGoDeps(dir, cache)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestCheckVueDeps(t *testing.T) {
	tpl := filepath.Join(t.TempDir(), "package.json")
	write(t, tpl, `{"name":"tpl","dependencies":{"vue":"3.5.45"},"devDependencies":{"vitest":"5.1.0"}}`, 0o644)
	tests := []struct {
		name, pkg string
		wantErr   bool
	}{
		{"no package.json", "", false},
		{"same deps, other name", `{"name":"app","dependencies":{"vue":"3.5.45"},"devDependencies":{"vitest":"5.1.0"}}`, false},
		{"extra dep", `{"dependencies":{"vue":"3.5.45","left-pad":"1.0.0"},"devDependencies":{"vitest":"5.1.0"}}`, true},
		{"other version", `{"dependencies":{"vue":"3.4.0"},"devDependencies":{"vitest":"5.1.0"}}`, true},
		{"broken json", `{`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if tt.pkg != "" {
				write(t, filepath.Join(dir, "package.json"), tt.pkg, 0o644)
			}
			err := checkVueDeps(dir, tpl)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRunRefusesUnknownBinary(t *testing.T) {
	var stderr bytes.Buffer
	o := options{stack: "go", argv: []string{"sh", "-c", "id"}, stderr: &stderr}
	if code := run(o); code != exitUsage {
		t.Fatalf("code = %d, want %d", code, exitUsage)
	}
}

func TestRunExecsInWorkdir(t *testing.T) {
	src := t.TempDir()
	write(t, filepath.Join(src, "a.go"), "package a", 0o644)
	work := filepath.Join(t.TempDir(), "work")
	var gotPath string
	var gotArgv []string
	o := options{
		stack: "go", src: src, work: work, modCache: t.TempDir(),
		maxFiles: 10, maxBytes: 1 << 20, argv: []string{"go", "test", "./..."},
		stderr:   &bytes.Buffer{},
		lookPath: func(string) (string, error) { return "/usr/local/go/bin/go", nil },
		execProcess: func(path string, argv, _ []string) error {
			gotPath, gotArgv = path, argv
			return errors.New("stop")
		},
	}
	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if code := run(o); code != 126 {
		t.Fatalf("code = %d, want 126 (exec stub failed)", code)
	}
	if gotPath != "/usr/local/go/bin/go" || strings.Join(gotArgv, " ") != "go test ./..." {
		t.Fatalf("exec %q %q", gotPath, gotArgv)
	}
	if _, err := os.Stat(filepath.Join(work, "a.go")); err != nil {
		t.Fatalf("not copied: %v", err)
	}
}

func TestParseArgs(t *testing.T) {
	if _, err := parseArgs([]string{"--stack", "rust", "--", "go"}); err == nil {
		t.Error("unknown stack accepted")
	}
	if _, err := parseArgs([]string{"--stack", "go", "--"}); err == nil {
		t.Error("empty argv accepted")
	}
	o, err := parseArgs([]string{"--stack", "vue", "--", "vitest", "run"})
	if err != nil || strings.Join(o.argv, " ") != "vitest run" {
		t.Fatalf("parse: %v %v", o.argv, err)
	}
}

func TestLinkNodeModules(t *testing.T) {
	src := t.TempDir()
	write(t, filepath.Join(src, "vue", "index.js"), "x", 0o644)
	write(t, filepath.Join(src, "@vue", "test-utils", "index.js"), "x", 0o644)
	write(t, filepath.Join(src, ".bin", "vitest"), "x", 0o755)
	dst := filepath.Join(t.TempDir(), "node_modules")
	if err := linkNodeModules(src, dst); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"vue/index.js", "@vue/test-utils/index.js", ".bin/vitest"} {
		if _, err := os.Stat(filepath.Join(dst, p)); err != nil {
			t.Errorf("%s not reachable: %v", p, err)
		}
	}
	// The directory itself is writable even though src would not be.
	if err := os.Mkdir(filepath.Join(dst, ".vite-temp"), 0o755); err != nil {
		t.Fatalf("node_modules not writable: %v", err)
	}
	if fi, err := os.Lstat(dst); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("node_modules must be a real directory: %v %v", fi, err)
	}
}

func TestSeedGoCache(t *testing.T) {
	seed := t.TempDir()
	write(t, filepath.Join(seed, "ab", "abcd-a"), "action", 0o444)
	write(t, filepath.Join(seed, "README"), "readme", 0o444)
	write(t, filepath.Join(seed, "trim.txt"), "1", 0o444)
	dst := filepath.Join(t.TempDir(), "gocache")
	if err := seedGoCache(seed, dst); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dst, "ab", "abcd-a")); err != nil || string(data) != "action" {
		t.Fatalf("entry not readable through the link: %q %v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(dst, "trim.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("trim.txt must not be linked: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dst, "ab", "new-a"), []byte("x"), 0o644); err != nil {
		t.Fatalf("cache dir not writable: %v", err)
	}
	// Missing seed and relative/empty GOCACHE are no-ops.
	if err := seedGoCache(filepath.Join(seed, "nope"), filepath.Join(t.TempDir(), "c")); err != nil {
		t.Fatal(err)
	}
	if err := seedGoCache(seed, ""); err != nil {
		t.Fatal(err)
	}
}
