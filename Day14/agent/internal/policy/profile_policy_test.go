package policy

import (
	"os"
	"path/filepath"
	"testing"
)

// profileGuard is a loop-style guard: read root == write root, narrowed by
// write globs.
func profileGuard(t *testing.T, globs []string) (FS, string) {
	t.Helper()
	root, _ := fixture(t)
	for _, d := range []string{"cmd", "internal/app"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	p := guard(t, root, root)
	p.WriteGlobs = globs
	return p, root
}

func TestCheck_WriteGlobs(t *testing.T) {
	p, _ := profileGuard(t, []string{"internal/**", "!internal/app/secret.go", "README.md"})
	cases := []struct {
		tool, path, want string
	}{
		{"write_file", "internal/app/x.go", ""},
		{"write_file", "cmd/x.go", CodeReadOnlyPath},
		{"write_file", "internal/app/secret.go", CodeReadOnlyPath}, // deny beats glob
		{"write_file", "README.md", ""},
		{"write_file", "workspace/README.md", CodeReadOnlyPath}, // positive globs are anchored
		{"create_directory", "internal/new", ""},
		{"create_directory", "internal", ""},
		{"create_directory", "cmd/new", CodeReadOnlyPath},
		{"write_file", ".env.local", CodeDeniedByPolicy}, // global deny still first
	}
	for _, c := range cases {
		t.Run(c.tool+" "+c.path, func(t *testing.T) {
			err := p.Check("fs", c.tool, map[string]interface{}{"path": c.path, "content": "x"})
			if CodeOf(err) != c.want {
				t.Fatalf("want %q, got %v", c.want, err)
			}
		})
	}
}

func TestCheck_WriteGlobsDoubleStar(t *testing.T) {
	p, _ := profileGuard(t, []string{"**/*.go", "docs/**/*.md", "!go.mod"})
	if err := os.MkdirAll(filepath.Join(p.WriteRoot, "docs", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"main.go":            "",
		"internal/app/x.go":  "",
		"docs/x.md":          "",
		"docs/a/x.md":        "",
		"docs/x.txt":         CodeReadOnlyPath,
		"go.mod":             CodeReadOnlyPath,
		"internal/app/x.txt": CodeReadOnlyPath,
	} {
		err := p.Check("fs", "write_file", map[string]interface{}{"path": path})
		if CodeOf(err) != want {
			t.Errorf("%s: want %q, got %v", path, want, err)
		}
	}
	// "**/*.go" lets the coder create any package directory.
	if err := p.Check("fs", "create_directory", map[string]interface{}{"path": "internal/domain"}); err != nil {
		t.Errorf("create_directory for a Go package must pass: %v", err)
	}
}

func TestValidateWriteGlob(t *testing.T) {
	for _, g := range []string{"/etc/**", "../x", "a/../b", "~/x", "!", ""} {
		if ValidateWriteGlob(g) == nil {
			t.Errorf("%q must be rejected", g)
		}
	}
	for _, g := range []string{"internal/**", "!go.mod", "**/*.go", "docs/**/*.md"} {
		if err := ValidateWriteGlob(g); err != nil {
			t.Errorf("%q: %v", g, err)
		}
	}
	if NormalizeGlob("./internal/**") != "internal/**" || NormalizeGlob("!./go.mod") != "!go.mod" || NormalizeGlob("./") != "**" {
		t.Error("NormalizeGlob")
	}
}

func execGuard(t *testing.T, allow, deny []string) FS {
	t.Helper()
	e, err := ParseExec(allow, deny)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := profileGuard(t, []string{"**"})
	p.Exec = e
	return p
}

func TestCheckExec(t *testing.T) {
	p := execGuard(t,
		[]string{"go test:*", "go vet:*", "gofmt -l:*", "go build ./...", "git diff:*"},
		[]string{"git diff --output*", "rm:*"})
	cases := []struct {
		argv []string
		want string
	}{
		{[]string{"go", "test", "./..."}, ""},
		{[]string{"go", "test", "-run", "TestX", "./internal/..."}, ""},
		{[]string{"go", "build", "./..."}, ""},
		{[]string{"go", "build", "./...", "-race"}, CodePermissionDenied}, // exact only
		{[]string{"go", "test", "-exec", "x", "./..."}, CodeDeniedByPolicy},
		{[]string{"go", "test", "--exec=x"}, CodeDeniedByPolicy},
		{[]string{"go", "test", "-toolexec=x"}, CodeDeniedByPolicy},
		{[]string{"go", "vet", "-vettool=/tmp/x"}, CodeDeniedByPolicy},
		{[]string{"go", "test", "-o", "bin"}, CodeDeniedByPolicy},
		{[]string{"gofmt", "-l", "-w", "."}, CodeDeniedByPolicy},
		{[]string{"go", "run", "."}, CodeDeniedByPolicy},
		{[]string{"go", "mod", "tidy"}, CodeDeniedByPolicy},
		{[]string{"npx", "x"}, CodeDeniedByPolicy},
		{[]string{"go", "test", "./...;rm", "-rf"}, CodeDeniedByPolicy},
		{[]string{"go", "test", "$(id)"}, CodeDeniedByPolicy},
		{[]string{"/usr/bin/go", "test"}, CodeDeniedByPolicy},
		{[]string{"git", "diff", "--output=/tmp/x"}, CodeDeniedByPolicy},
		{[]string{"rmdir", "x"}, CodeDeniedByPolicy},
		{[]string{"curl", "http://x"}, CodePermissionDenied},
		{[]string{}, CodeBadArgument},
	}
	for _, c := range cases {
		err := p.CheckExec(c.argv)
		if CodeOf(err) != c.want {
			t.Errorf("%v: want %q, got %v", c.argv, c.want, err)
		}
	}
}

func TestCheck_RunCommandTool(t *testing.T) {
	p := execGuard(t, []string{"go test:*"}, nil)
	if !p.AllowsTool("run_command") {
		t.Fatal("run_command must be offered when exec is allowed")
	}
	if err := p.Check("local", "run_command", map[string]interface{}{"argv": []interface{}{"go", "test", "./..."}}); err != nil {
		t.Fatalf("allowed command refused: %v", err)
	}
	if err := p.Check("local", "run_command", map[string]interface{}{"argv": "go test"}); CodeOf(err) != CodeBadArgument {
		t.Errorf("string argv: want %s, got %v", CodeBadArgument, err)
	}
	if err := p.Check("local", "run_command", map[string]interface{}{"argv": []interface{}{"go", 1}}); CodeOf(err) != CodeBadArgument {
		t.Errorf("non-string element: want %s, got %v", CodeBadArgument, err)
	}

	denyAll := execGuard(t, []string{"go test:*"}, []string{""})
	if denyAll.AllowsTool("run_command") {
		t.Error("a bare Bash deny must hide run_command")
	}
	if err := denyAll.CheckExec([]string{"go", "test"}); CodeOf(err) != CodePermissionDenied {
		t.Errorf("deny-all: got %v", err)
	}
	none := execGuard(t, nil, nil)
	if none.AllowsTool("run_command") {
		t.Error("empty exec allow must hide run_command")
	}
	readOnly := none
	readOnly.WriteRoot = ""
	readOnly.Exec, _ = ParseExec([]string{"go test:*"}, nil)
	if err := readOnly.Check("local", "run_command", map[string]interface{}{"argv": []interface{}{"go", "test"}}); CodeOf(err) != CodePermissionDenied {
		t.Errorf("exec without write root: got %v", err)
	}
}

func TestParseExec_RejectsWildcardAllow(t *testing.T) {
	for _, a := range []string{"go *", "", ":*", "go test ./*:*"} {
		if _, err := ParseExec([]string{a}, nil); err == nil {
			t.Errorf("allow %q must be rejected", a)
		}
	}
}
