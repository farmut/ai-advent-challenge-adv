package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateSandbox(t *testing.T) {
	withImage := func(c *SandboxConfig) { c.Image = "ai-adv-sandbox:1"; c.WorkspaceRoot = "/abs/workspace" }
	tests := []struct {
		name    string
		mutate  func(*SandboxConfig)
		wantErr string
	}{
		{"defaults are valid (no image)", func(*SandboxConfig) {}, ""},
		{"image with root", withImage, ""},
		{"digest image", func(c *SandboxConfig) {
			withImage(c)
			c.Image = "ai-adv-sandbox@sha256:0123456789abcdef"
		}, ""},
		{"narrowed binaries", func(c *SandboxConfig) { c.AllowedBinaries = []string{"go", "gofmt"} }, ""},
		{"fractional cpus", func(c *SandboxConfig) { c.CPUs = "1.5" }, ""},
		{"platform", func(c *SandboxConfig) { c.Platform = "linux/amd64" }, ""},
		{"bad platform", func(c *SandboxConfig) { c.Platform = "--privileged" }, "sandbox.platform"},
		{"other runtime", func(c *SandboxConfig) { c.Runtime = "podman" }, "sandbox.runtime"},
		{"empty runtime", func(c *SandboxConfig) { c.Runtime = "" }, "sandbox.runtime"},
		{"docker_bin flag", func(c *SandboxConfig) { c.DockerBin = "--host=x" }, "sandbox.docker_bin"},
		{"docker_bin relative path", func(c *SandboxConfig) { c.DockerBin = "bin/docker" }, "sandbox.docker_bin"},
		{"image as flag", func(c *SandboxConfig) { c.Image = "--privileged" }, "sandbox.image"},
		{"image with space", func(c *SandboxConfig) { c.Image = "a b" }, "sandbox.image"},
		{"image without root", func(c *SandboxConfig) { c.Image = "x:1"; c.WorkspaceRoot = "" }, "sandbox.workspace_root"},
		{"root is /", func(c *SandboxConfig) { c.Image = "x:1"; c.WorkspaceRoot = "/" }, "sandbox.workspace_root"},
		{"zero timeout", func(c *SandboxConfig) { c.Timeout = 0 }, "sandbox.timeout"},
		{"negative kill timeout", func(c *SandboxConfig) { c.KillTimeout = -time.Second }, "sandbox.kill_timeout"},
		{"zero output", func(c *SandboxConfig) { c.MaxOutputBytes = 0 }, "sandbox.max_output_bytes"},
		{"bad memory", func(c *SandboxConfig) { c.Memory = "lots" }, "sandbox.memory"},
		{"zero memory", func(c *SandboxConfig) { c.Memory = "0" }, "sandbox.memory"},
		{"bad tmpfs", func(c *SandboxConfig) { c.TmpfsSize = "1g,exec" }, "sandbox.tmpfs_size"},
		{"zero cpus", func(c *SandboxConfig) { c.CPUs = "0" }, "sandbox.cpus"},
		{"cpus not a number", func(c *SandboxConfig) { c.CPUs = "two" }, "sandbox.cpus"},
		{"zero pids", func(c *SandboxConfig) { c.Pids = 0 }, "sandbox.pids"},
		{"zero copy files", func(c *SandboxConfig) { c.CopyMaxFiles = 0 }, "sandbox.copy_max_files"},
		{"zero copy bytes", func(c *SandboxConfig) { c.CopyMaxBytes = 0 }, "sandbox.copy_max_bytes"},
		{"empty binaries", func(c *SandboxConfig) { c.AllowedBinaries = nil }, "sandbox.allowed_binaries"},
		{"widened binaries", func(c *SandboxConfig) { c.AllowedBinaries = []string{"go", "sh"} }, "sandbox.allowed_binaries"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := DefaultSandbox()
			tt.mutate(&c)
			err := validateSandbox(c)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error mentioning %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestDefaultSandboxIsSafe(t *testing.T) {
	c := DefaultSandbox()
	if c.Enabled() {
		t.Error("no image by default: the sandbox must be configured explicitly")
	}
	if err := c.RequireImage(); err == nil {
		t.Error("RequireImage must fail without an image")
	}
	if c.Runtime != "docker" || c.Timeout != 5*time.Minute || c.MaxOutputBytes != 256<<10 ||
		c.Memory != "1g" || c.CPUs != "2" || c.Pids != 256 || c.TmpfsSize != "1g" {
		t.Errorf("defaults = %+v", c)
	}
}

func TestLoadSandboxSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.yaml")
	yaml := `
profiles:
  write_root: ws
sandbox:
  image: ai-adv-sandbox:1
  timeout: 30s
  max_output_bytes: 4096
  memory: 512m
  cpus: "1"
  pids: 64
  tmpfs_size: 256m
  allowed_binaries: [go, gofmt]
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ResolvePaths(&cfg, path); err != nil {
		t.Fatal(err)
	}
	s := cfg.Sandbox
	if s.Timeout != 30*time.Second || s.MaxOutputBytes != 4096 || s.Memory != "512m" || s.Pids != 64 ||
		len(s.AllowedBinaries) != 2 || s.DockerBin != "docker" || s.KillTimeout != 10*time.Second {
		t.Fatalf("sandbox = %+v", s)
	}
	real, _ := filepath.EvalSymlinks(dir)
	if s.WorkspaceRoot != filepath.Join(real, "ws") {
		t.Fatalf("workspace_root must default to profiles.write_root, got %q", s.WorkspaceRoot)
	}
	if err := validateSandbox(s); err != nil {
		t.Fatal(err)
	}
}
