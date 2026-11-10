package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// sandbox.templates maps a stack to its seed template directory: only known
// stacks with non-empty paths pass validation, and ResolvePaths makes the
// template paths absolute against the config directory.
func TestSandboxTemplates(t *testing.T) {
	base := t.TempDir()

	withImage := DefaultSandbox()
	withImage.Image = "img:1"
	withImage.WorkspaceRoot = "ws"

	c := withImage
	c.Templates = map[string]string{"go": "../sandbox/templates/go"}
	if err := validateSandbox(c); err != nil {
		t.Fatalf("valid templates rejected: %v", err)
	}
	cfg := Config{Sandbox: c}
	if err := ResolvePaths(&cfg, filepath.Join(base, "agent.yaml")); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(base, "../sandbox/templates/go")
	if real, err := filepath.EvalSymlinks(base); err == nil {
		// ResolvePaths resolves the config dir's symlinks (macOS /var →
		// /private/var); the not-yet-existing template dir stays as joined.
		want = filepath.Join(real, "../sandbox/templates/go")
	}
	if got := cfg.Sandbox.Templates["go"]; got != want {
		t.Fatalf("template path = %q, want %q", got, want)
	}

	for name, mutate := range map[string]func(*SandboxConfig){
		"unknown stack": func(c *SandboxConfig) { c.Templates = map[string]string{"rust": "x"} },
		"empty path":    func(c *SandboxConfig) { c.Templates = map[string]string{"go": "  "} },
	} {
		c := withImage
		mutate(&c)
		if err := validateSandbox(c); err == nil || !strings.Contains(err.Error(), "sandbox.templates") {
			t.Errorf("%s: want a sandbox.templates error, got %v", name, err)
		}
	}
}
