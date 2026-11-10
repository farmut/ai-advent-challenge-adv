package localtools

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-adv-agent/internal/policy"
	"ai-adv-agent/internal/port"
)

func setup(t *testing.T) (*Server, policy.FS, string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "task")
	files := map[string]string{
		"main.go":          "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n",
		"internal/a/a.go":  "package a\n// SECRET_TOKEN usage\n",
		".env":             "SECRET_TOKEN=xyz\n",
		"sub/.env.local":   "SECRET_TOKEN=abc\n",
		"go.mod":           "module x\n",
		"../outside/x.txt": "SECRET_TOKEN outside\n",
	}
	for p, c := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pol := policy.FS{
		ReadRoot: root, WriteRoot: root,
		WriteGlobs: []string{"**/*.go", "!go.mod"},
		Deny:       policy.DefaultDenyGlobs(),
		Specs:      policy.DefaultSpecs(),
		FSServers:  []string{ServerName},
		Audit:      io.Discard,
	}
	s := &Server{Root: root, Hide: pol.Denied}
	return s, pol, root
}

// call runs a tool the way the app does: policy check first, then the server.
func call(t *testing.T, s *Server, pol policy.FS, name string, args map[string]interface{}) (string, error) {
	t.Helper()
	if err := pol.Check(ServerName, name, args); err != nil {
		return "", err
	}
	out, err := s.Call(context.Background(), name, args)
	if err != nil {
		return "", err
	}
	filtered, _, err := pol.FilterOutput(name, out)
	return filtered, err
}

func TestEditFile(t *testing.T) {
	s, pol, root := setup(t)
	_, err := call(t, s, pol, "edit_file", map[string]interface{}{
		"path":  "main.go",
		"edits": []interface{}{map[string]interface{}{"oldText": "println(\"bye\")", "newText": "x"}},
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unmatched oldText must fail, got %v", err)
	}
	out, err := call(t, s, pol, "edit_file", map[string]interface{}{
		"path":   "main.go",
		"edits":  []interface{}{map[string]interface{}{"oldText": "\"hello\"", "newText": "\"hi\""}},
		"dryRun": true,
	})
	if err != nil || !strings.Contains(out, "dry run") {
		t.Fatalf("dry run: %q %v", out, err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "main.go")); strings.Contains(string(b), "hi") {
		t.Fatal("dry run must not write")
	}
	if _, err := call(t, s, pol, "edit_file", map[string]interface{}{
		"path":  "main.go",
		"edits": []interface{}{map[string]interface{}{"oldText": "\"hello\"", "newText": "\"hi\""}},
	}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(root, "main.go")); !strings.Contains(string(b), "\"hi\"") {
		t.Fatal("edit not applied")
	}
	// Ambiguous oldText is refused.
	if err := os.WriteFile(filepath.Join(root, "dup.go"), []byte("x\nx\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := call(t, s, pol, "edit_file", map[string]interface{}{
		"path": "dup.go", "edits": []interface{}{map[string]interface{}{"oldText": "x", "newText": "y"}},
	}); err == nil || !strings.Contains(err.Error(), "2 times") {
		t.Fatalf("ambiguous edit: %v", err)
	}
}

func TestWriteFile(t *testing.T) {
	s, pol, root := setup(t)
	if _, err := call(t, s, pol, "write_file", map[string]interface{}{"path": "internal/a/b.go", "content": "package a\n"}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(root, "internal/a/b.go"))
	if err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("written file: %v %v", fi, err)
	}
	for _, c := range []struct{ path, code string }{
		{"../outside/x.go", policy.CodeReadOnlyPath},
		{"go.mod", policy.CodeReadOnlyPath},
		{"notes.md", policy.CodeReadOnlyPath},
		{"/etc/x.go", policy.CodeBadArgument},
	} {
		_, err := call(t, s, pol, "write_file", map[string]interface{}{"path": c.path, "content": "x"})
		if policy.CodeOf(err) != c.code {
			t.Errorf("%s: want %s, got %v", c.path, c.code, err)
		}
	}
	// Defence in depth: the server alone also refuses paths outside Root.
	if _, err := s.Call(context.Background(), "write_file", map[string]interface{}{"path": "../outside/y.go", "content": "x"}); err == nil {
		t.Error("server must refuse a path outside its root even without the policy")
	}
}

func TestGrepHidesDeniedFiles(t *testing.T) {
	s, pol, _ := setup(t)
	out, err := call(t, s, pol, "grep", map[string]interface{}{"pattern": "SECRET_TOKEN"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, ".env") || strings.Contains(out, "xyz") || strings.Contains(out, "outside") {
		t.Fatalf("grep leaked denied/outside content:\n%s", out)
	}
	if !strings.Contains(out, "internal/a/a.go\n 2: // SECRET_TOKEN usage") {
		t.Fatalf("grep output format:\n%s", out)
	}
	if out, _ := call(t, s, pol, "grep", map[string]interface{}{"pattern": "nothing-here-zzz"}); out != "0 matches." {
		t.Errorf("empty grep: %q", out)
	}
	if _, err := call(t, s, pol, "grep", map[string]interface{}{"pattern": "("}); err == nil {
		t.Error("invalid regexp must fail")
	}
}

func TestReadsAndListings(t *testing.T) {
	s, pol, _ := setup(t)
	if out, err := call(t, s, pol, "read_text_file", map[string]interface{}{"path": "main.go", "head": float64(1)}); err != nil || out != "package main\n" {
		t.Errorf("read head: %q %v", out, err)
	}
	if _, err := call(t, s, pol, "read_text_file", map[string]interface{}{"path": ".env"}); policy.CodeOf(err) != policy.CodeDeniedByPolicy {
		t.Errorf(".env read: %v", err)
	}
	out, err := call(t, s, pol, "list_directory", map[string]interface{}{"path": "."})
	if err != nil || strings.Contains(out, ".env") || !strings.Contains(out, "[FILE] main.go") || !strings.Contains(out, "[DIR] internal") {
		t.Errorf("list_directory: %q %v", out, err)
	}
	out, err = call(t, s, pol, "directory_tree", map[string]interface{}{"path": "."})
	if err != nil || strings.Contains(out, ".env") || !strings.Contains(out, "\"a.go\"") {
		t.Errorf("directory_tree: %q %v", out, err)
	}
	out, err = call(t, s, pol, "search_files", map[string]interface{}{"path": ".", "pattern": "env"})
	if err != nil || out != "No matches found" {
		t.Errorf("search_files must not find denied files: %q %v", out, err)
	}
	out, err = call(t, s, pol, "read_multiple_files", map[string]interface{}{"paths": []interface{}{"main.go", "go.mod"}})
	if err != nil || !strings.Contains(out, "module x") {
		t.Errorf("read_multiple_files: %q %v", out, err)
	}
	if _, err := call(t, s, pol, "create_directory", map[string]interface{}{"path": "internal/b"}); err != nil {
		t.Errorf("create_directory: %v", err)
	}
}

type fakeRunner struct {
	got port.CommandSpec
	err error
}

func (f *fakeRunner) Run(_ context.Context, s port.CommandSpec) (port.CommandResult, error) {
	f.got = s
	return port.CommandResult{ExitCode: 1, Stdout: "FAIL x", Stderr: "boom"}, f.err
}
func (f *fakeRunner) Preflight(context.Context) error { return nil }

func TestRunCommand(t *testing.T) {
	s, pol, root := setup(t)
	pol.Exec, _ = policy.ParseExec([]string{"go test:*"}, nil)

	if _, err := call(t, s, pol, "run_command", map[string]interface{}{"argv": []interface{}{"go", "test", "./..."}}); !errors.Is(err, ErrNoRunner) {
		t.Fatalf("no runner must fail closed, got %v", err)
	}
	r := &fakeRunner{}
	s.Runner = r
	s.Command = port.CommandSpec{Env: map[string]string{"GOPROXY": "off"}, Stack: "go"}
	out, err := call(t, s, pol, "run_command", map[string]interface{}{"argv": []interface{}{"go", "test", "./..."}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "exit_code: 1") || !strings.Contains(out, "boom") {
		t.Errorf("output: %q", out)
	}
	if r.got.Workdir != root || strings.Join(r.got.Argv, " ") != "go test ./..." || r.got.Env["GOPROXY"] != "off" {
		t.Errorf("runner got %+v", r.got)
	}
	if _, err := call(t, s, pol, "run_command", map[string]interface{}{"argv": []interface{}{"go", "run", "."}}); policy.CodeOf(err) != policy.CodeDeniedByPolicy {
		t.Errorf("go run must be denied by policy before the runner, got %v", err)
	}
}

func TestToolsMatchPolicyTable(t *testing.T) {
	specs := policy.DefaultSpecs()
	names := map[string]bool{}
	for _, tool := range Tools() {
		names[tool.Name] = true
		spec, ok := specs[tool.Name]
		if !ok {
			t.Errorf("local tool %q has no policy spec (would be denied)", tool.Name)
			continue
		}
		schema := tool.InputSchema.(map[string]interface{})
		props := schema["properties"].(map[string]interface{})
		for _, a := range append(append([]string{}, spec.PathArgs...), spec.ListArgs...) {
			if _, ok := props[a]; !ok {
				t.Errorf("%s: schema lacks guarded argument %q", tool.Name, a)
			}
		}
	}
	for _, n := range policy.LocalToolNames() {
		if !names[n] {
			t.Errorf("LocalToolNames lists %q but the server does not serve it", n)
		}
	}
	if len(names) != len(policy.LocalToolNames()) {
		t.Errorf("tool count %d != LocalToolNames %d", len(names), len(policy.LocalToolNames()))
	}
}
