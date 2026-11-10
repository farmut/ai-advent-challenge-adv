package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadYAML(t *testing.T, body string) (Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

// The pre-profile roster format must fail with a message that says how to
// migrate, not with a generic parse error or — worse — load silently.
func TestLoad_OldSubAgentFormatMigrationError(t *testing.T) {
	cases := map[string]string{
		"name+prompt": "orchestrator:\n  subagents:\n    - name: coder\n      prompt: write code\n",
		"name+tools":  "orchestrator:\n  subagents:\n    - name: coder\n      tools: { allow: [write_file] }\n",
		"name only":   "orchestrator:\n  subagents:\n    - name: coder\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := loadYAML(t, body)
			if err == nil || !strings.Contains(err.Error(), "устаревший формат") || !strings.Contains(err.Error(), "profile:") {
				t.Fatalf("want a migration error, got %v", err)
			}
		})
	}
}

func TestLoad_ProfileRoleRules(t *testing.T) {
	cases := map[string]string{
		"profile+prompt": "orchestrator:\n  subagents:\n    - { profile: coder, prompt: x }\n",
		"profile+tools":  "orchestrator:\n  subagents:\n    - { profile: coder, tools: { allow: [x] } }\n",
		"name mismatch":  "orchestrator:\n  subagents:\n    - { profile: coder, name: other }\n",
		"no profile":     "orchestrator:\n  subagents:\n    - { mcp: [local] }\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := loadYAML(t, body); err == nil {
				t.Fatal("want error")
			}
		})
	}
	c, err := loadYAML(t, "profiles: { dir: p, write_root: ../ws }\norchestrator:\n  subagents:\n    - { profile: coder, name: coder, mcp: [local] }\n")
	if err != nil {
		t.Fatal(err)
	}
	if c.Orchestrator.SubAgents[0].Name != "coder" || c.Profiles.Dir != "p" || c.Profiles.WriteRoot != "../ws" {
		t.Errorf("parsed: %+v %+v", c.Orchestrator.SubAgents[0], c.Profiles)
	}
}

func TestResolvePaths_Profiles(t *testing.T) {
	tmp := t.TempDir()
	if real, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = real
	}
	cfg := Default()
	cfg.Profiles.WriteRoot = "../ws"
	if err := ResolvePaths(&cfg, filepath.Join(tmp, "a", "agent.yaml")); err != nil {
		t.Fatal(err)
	}
	if cfg.Profiles.Dir != filepath.Join(tmp, "a", "profiles") || cfg.Profiles.WriteRoot != filepath.Join(tmp, "ws") {
		t.Errorf("profiles not resolved: %+v", cfg.Profiles)
	}
}

func TestValidateToolPolicy_GlobsAndExec(t *testing.T) {
	bad := map[string]ToolPolicy{
		"globs without root": {WriteGlobs: []string{"src/**"}},
		"absolute glob":      {WriteRoot: "/w", WriteGlobs: []string{"/etc/**"}},
		"dotdot glob":        {WriteRoot: "/w", WriteGlobs: []string{"../x"}},
		"dotdot deny glob":   {DenyGlobs: []string{"../x"}},
		"wildcard exec":      {WriteRoot: "/w", ExecAllow: []string{"go *"}},
		"exec without root":  {ExecAllow: []string{"go test:*"}},
	}
	for name, tp := range bad {
		if err := ValidateToolPolicy(tp, name); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	ok := ToolPolicy{WriteRoot: "/w", WriteGlobs: []string{"src/**", "!src/x"}, ExecAllow: []string{"go test:*"}, ExecDeny: []string{"*"}}
	if err := ValidateToolPolicy(ok, "ok"); err != nil {
		t.Errorf("valid policy rejected: %v", err)
	}
}
