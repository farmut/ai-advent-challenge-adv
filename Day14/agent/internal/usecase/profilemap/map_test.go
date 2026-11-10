package profilemap

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"ai-adv-agent/internal/adapter/profilefs"
	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/policy"
)

func load(t *testing.T, name string) domain.AgentProfile {
	t.Helper()
	p, err := (&profilefs.Repo{Dir: "../../../profiles", Log: &bytes.Buffer{}}).Get(name)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustMap(t *testing.T, name string) Grant {
	t.Helper()
	g, err := Map(load(t, name))
	if err != nil {
		t.Fatalf("Map(%s): %v", name, err)
	}
	return g
}

func has(list []string, s string) bool { return slices.Contains(list, s) }

func guardFor(g Grant) policy.FS {
	return policy.FS{Exec: g.Exec, WriteRoot: "/w"}
}

func TestMap_Profiles(t *testing.T) {
	writeTools := []string{"write_file", "edit_file", "create_directory", "run_command"}
	tests := []struct {
		name       string
		canWrite   bool
		wantTools  []string
		denyTools  []string
		wantGlobs  []string
		wantExecOK [][]string
		execDenied [][]string
	}{
		{
			name:      "researcher",
			wantTools: []string{"read_text_file", "grep", "search_files"},
			denyTools: append(append([]string{}, writeTools...), "git_diff"),
		},
		{
			name:       "bug-fixer",
			canWrite:   true,
			wantTools:  []string{"edit_file", "run_command", "git_diff", "git_list_files"},
			denyTools:  []string{"write_file"},
			wantGlobs:  []string{"cmd/**", "internal/**", "!go.mod", "!go.sum"},
			wantExecOK: [][]string{{"go", "test", "./..."}, {"go", "vet", "./..."}, {"gofmt", "-l", "."}, {"go", "build", "./..."}},
			execDenied: [][]string{{"go", "test", "-exec", "x"}, {"go", "build", "-o", "x"}, {"curl", "x"}, {"gofmt", "-w", "."}},
		},
		{
			name:      "tech-writer",
			canWrite:  true,
			wantTools: []string{"edit_file", "write_file", "create_directory"},
			denyTools: []string{"run_command", "git_diff"},
			wantGlobs: []string{"README.md", "docs/**/*.md", "!**/*.go"},
		},
		{
			name:       "coder",
			canWrite:   true,
			wantTools:  []string{"edit_file", "write_file", "run_command"},
			wantGlobs:  []string{"**/*.go", "src/**", "!go.mod", "!package.json", "!**/.env*"},
			wantExecOK: [][]string{{"go", "test", "./..."}, {"vitest", "run"}, {"vue-tsc", "--noEmit"}},
			execDenied: [][]string{{"vue-tsc"}, {"npm", "install"}, {"go", "run", "."}},
		},
		{
			name:      "security-go",
			wantTools: []string{"read_text_file", "grep"},
			denyTools: writeTools,
		},
		{
			name:      "security-vue",
			wantTools: []string{"read_text_file", "grep"},
			denyTools: writeTools,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := mustMap(t, tt.name)
			if g.CanWrite() != tt.canWrite {
				t.Errorf("CanWrite = %v, want %v (globs %v)", g.CanWrite(), tt.canWrite, g.WriteGlobs)
			}
			for _, n := range tt.wantTools {
				if !has(g.AllowTools, n) {
					t.Errorf("tool %q missing: %v", n, g.AllowTools)
				}
			}
			for _, n := range tt.denyTools {
				if has(g.AllowTools, n) {
					t.Errorf("tool %q must not be granted: %v", n, g.AllowTools)
				}
			}
			for _, w := range tt.wantGlobs {
				if !has(g.WriteGlobs, w) {
					t.Errorf("write glob %q missing: %v", w, g.WriteGlobs)
				}
			}
			p := guardFor(g)
			for _, argv := range tt.wantExecOK {
				if err := p.CheckExec(argv); err != nil {
					t.Errorf("%v must be allowed: %v", argv, err)
				}
			}
			for _, argv := range tt.execDenied {
				if err := p.CheckExec(argv); err == nil {
					t.Errorf("%v must be denied", argv)
				}
			}
			if !tt.canWrite && (len(g.WriteGlobs) != 0 || g.Exec.Usable()) {
				t.Errorf("read-only profile got write/exec: %v %+v", g.WriteGlobs, g.Exec)
			}
		})
	}
}

func TestMap_BugFixerEnvAndDeny(t *testing.T) {
	g := mustMap(t, "bug-fixer")
	if g.Env["GOPROXY"] != "off" {
		t.Errorf("env not carried: %v", g.Env)
	}
	if !has(g.DenyGlobs, ".env") || !has(g.DenyGlobs, "**/.env.local") {
		t.Errorf("read deny globs: %v", g.DenyGlobs)
	}
	for _, d := range g.DenyGlobs {
		if strings.HasPrefix(d, "~") {
			t.Errorf("home path must be ignored, got %q", d)
		}
	}
}

func TestMap_Errors(t *testing.T) {
	base := domain.AgentProfile{Name: "x", Tools: []domain.PermKind{domain.PermRead, domain.PermEdit, domain.PermBash}}
	cases := map[string][]domain.PermRule{
		"WebFetch in allow":   {{Kind: domain.PermWebFetch}},
		"Task in allow":       {{Kind: domain.PermTask}},
		"bare Bash allow":     {{Kind: domain.PermBash}},
		"narrow Read":         {{Kind: domain.PermRead, Pattern: "./src/**"}},
		"absolute Edit":       {{Kind: domain.PermEdit, Pattern: "/etc/**"}},
		"dotdot Edit":         {{Kind: domain.PermEdit, Pattern: "../x/**"}},
		"wildcard exec":       {{Kind: domain.PermEdit, Pattern: "./**"}, {Kind: domain.PermBash, Pattern: "go *"}},
		"exec without writes": {{Kind: domain.PermBash, Pattern: "go test:*"}},
		"unknown kind":        {{Kind: "Shell"}},
	}
	for name, allow := range cases {
		t.Run(name, func(t *testing.T) {
			p := base
			p.Allow = allow
			if _, err := Map(p); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestMap_FrontmatterToolsIntersect(t *testing.T) {
	p := domain.AgentProfile{
		Name:  "x",
		Tools: []domain.PermKind{domain.PermRead},
		Allow: []domain.PermRule{{Kind: domain.PermRead}, {Kind: domain.PermEdit, Pattern: "./**"}},
	}
	g, err := Map(p)
	if err != nil {
		t.Fatal(err)
	}
	if g.CanWrite() || has(g.AllowTools, "edit_file") {
		t.Errorf("Edit without Edit in tools: must not be granted: %+v", g)
	}
	if len(g.Notes) == 0 {
		t.Error("skipped rule must be noted")
	}
}

func TestMap_DenyIgnoresUnsupportedKinds(t *testing.T) {
	p := domain.AgentProfile{
		Name:  "x",
		Tools: []domain.PermKind{domain.PermRead},
		Allow: []domain.PermRule{{Kind: domain.PermRead}},
		Deny:  []domain.PermRule{{Kind: domain.PermWebFetch}, {Kind: domain.PermAgent}, {Kind: domain.PermRead, Pattern: "~/.ssh/**"}},
	}
	if _, err := Map(p); err != nil {
		t.Fatalf("deny of tools that do not exist must be ignored: %v", err)
	}
}

func TestSystemPrompt(t *testing.T) {
	p := load(t, "coder")
	s := SystemPrompt(p)
	first, _, _ := strings.Cut(s, "\n")
	if first != "# profile: coder" {
		t.Errorf("first line = %q", first)
	}
	if strings.Count(s, "# profile: coder") != 1 {
		t.Error("marker must appear exactly once")
	}
	if !strings.Contains(s, "<<<DATA-") || strings.Index(s, "Общие правила") > strings.Index(s, "# Роль") {
		t.Error("common DATA rules must precede the profile body")
	}
}
