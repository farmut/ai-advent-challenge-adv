package policy

import (
	"io"
	"strings"
	"testing"
)

func outputGuard() FS {
	return FS{
		ReadRoot: "/repo/Day14",
		GitRoot:  "/repo",
		Deny:     DefaultDenyGlobs(),
		Specs:    DefaultSpecs(),
		Audit:    io.Discard,
	}
}

func TestDenied_NestedSecretsAndAgentState(t *testing.T) {
	p := FS{Deny: DefaultDenyGlobs()}
	for _, rel := range []string{
		"sub/.git/config", "a/b/.git", "a/.ssh/id_rsa", "x/.ssh",
		"x/chat_history.wm.json", "chat_history.json", "deep/chat_history.ltm.json",
		"work/run.loop.json", "run.loop.state.json", "a/run.loop.lock",
		"agent/agent-tui.log", "nested/.env.local",
	} {
		if _, hit := p.denied(rel); !hit {
			t.Errorf("%q must be denied", rel)
		}
	}
	for _, rel := range []string{"internal/app/toolbelt.go", "gitignore.md", "history.json"} {
		if pat, hit := p.denied(rel); hit {
			t.Errorf("%q must be allowed, matched %q", rel, pat)
		}
	}
}

// Golden samples below are verbatim fff-mcp 0.10.0 output.
func TestFilterOutput_FFF(t *testing.T) {
	p := outputGuard()
	tests := []struct {
		name       string
		tool       string
		in         string
		want       string
		wantHidden int
	}{
		{
			name: "grep drops a denied file block and its hint",
			tool: "grep",
			in:   "→ Read .env (best match)\n.env\n 3: KEY=secret\na/code.go\n 1: hello secret",
			want: "a/code.go\n 1: hello secret", wantHidden: 1,
		},
		{
			name: "grep keeps meta lines",
			tool: "grep",
			in:   "→ Read a/many.go (only match)\n2/9 matches shown\na/many.go\n 1: x\n 2: y",
			want: "→ Read a/many.go (only match)\n2/9 matches shown\na/many.go\n 1: x\n 2: y",
		},
		{
			name: "grep nested agent state hidden",
			tool: "grep",
			in:   "a/chat_history.wm.json\n 1: secret\na/code.go\n  1: hello\n  2| ctx",
			want: "a/code.go\n  1: hello\n  2| ctx", wantHidden: 1,
		},
		{
			name: "multi_grep with context and separators",
			tool: "multi_grep",
			in:   "sub/.git/config\n 1: token\n 2-x\n--\n 1-y\nok.go\n 1: token",
			want: "ok.go\n 1: token", wantHidden: 1,
		},
		{
			name: "count mode",
			tool: "grep",
			in:   "a/many.go: 6\n.env.local: 1\na/code.go: 2",
			want: "a/many.go: 6\na/code.go: 2", wantHidden: 1,
		},
		{
			name: "find_files with git status and cursor",
			tool: "find_files",
			in:   "→ Read a/code.go (best match — Read this file directly)\n1/3 matches\na/code.go git:clean\nx/.ssh/id_rsa git:clean\ncursor: 1",
			want: "→ Read a/code.go (best match — Read this file directly)\n1/3 matches\na/code.go git:clean\ncursor: 1", wantHidden: 1,
		},
		{name: "empty grep", tool: "grep", in: "0 matches.", want: "0 matches."},
		{name: "empty find", tool: "find_files", in: "0 results (2 indexed)", want: "0 results (2 indexed)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, hidden, err := p.FilterOutput(tt.tool, tt.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want || hidden != tt.wantHidden {
				t.Errorf("got (%q, %d), want (%q, %d)", got, hidden, tt.want, tt.wantHidden)
			}
		})
	}
}

func TestFilterOutput_UnrecognisedFailsClosed(t *testing.T) {
	p := outputGuard()
	for name, in := range map[string]string{
		"content before any file": " 1: KEY=secret",
		"unknown indented line":   "a.go\n    something odd",
	} {
		t.Run(name, func(t *testing.T) {
			got, _, err := p.FilterOutput("grep", in)
			if CodeOf(err) != CodeDeniedByPolicy {
				t.Fatalf("want %s, got err=%v out=%q", CodeDeniedByPolicy, err, got)
			}
			if got != "" {
				t.Errorf("no output may leak on failure, got %q", got)
			}
		})
	}
	if _, _, err := p.FilterOutput("git_diff", "warning: something\ndiff --git a/x b/x"); CodeOf(err) != CodeDeniedByPolicy {
		t.Errorf("git_diff preamble must fail closed, got %v", err)
	}
}

func TestFilterOutput_SearchFilesAbsolutePaths(t *testing.T) {
	p := outputGuard()
	in := "/repo/Day14/a/chat_history.wm.json\n/repo/Day14/a/code.go\n/repo/Day14/sub/.git/config"
	got, hidden, err := p.FilterOutput("search_files", in)
	if err != nil || hidden != 2 || got != "/repo/Day14/a/code.go" {
		t.Fatalf("got (%q, %d, %v)", got, hidden, err)
	}
	if got, _, _ := p.FilterOutput("search_files", "No matches found"); got != "No matches found" {
		t.Errorf("empty marker changed: %q", got)
	}
}

func TestFilterOutput_GitListFiles(t *testing.T) {
	p := outputGuard()
	tests := []struct {
		name, in, want string
		hidden         int
	}{
		{"porcelain", " M Day14/agent/main.go\n?? Day14/agent/.env\nR  Day14/old.go -> Day14/.ssh/id_rsa", " M Day14/agent/main.go", 2},
		{"name-status", "M\tDay14/agent/main.go\nA\tDay14/agent/chat_history.json\nR100\tDay14/a.go\tDay14/b/.git/x", "M\tDay14/agent/main.go", 2},
		{"ls-files", "Day14/agent/main.go\nDay14/agent/agent.config.yaml\n\"Day14/sp ace/.env\"", "Day14/agent/main.go", 2},
		{"clean marker", "working tree clean — no changed files", "working tree clean — no changed files", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, hidden, err := p.FilterOutput("git_list_files", tt.in)
			if err != nil || got != tt.want || hidden != tt.hidden {
				t.Errorf("got (%q, %d, %v), want (%q, %d)", got, hidden, err, tt.want, tt.hidden)
			}
		})
	}
}

func TestFilterOutput_GitDiffDropsDeniedBlocks(t *testing.T) {
	p := outputGuard()
	in := strings.Join([]string{
		"diff --git a/Day14/.env b/Day14/.env",
		"index 1..2 100644",
		"--- a/Day14/.env",
		"+++ b/Day14/.env",
		"@@ -1 +1 @@",
		"-KEY=old",
		"+KEY=new",
		"diff --git a/Day14/main.go b/Day14/main.go",
		"--- a/Day14/main.go",
		"+++ b/Day14/main.go",
		"@@ -1 +1 @@",
		"+package main",
		"diff --git a/Day14/notes.txt b/Day14/sub/.git/notes.txt",
		"similarity index 100%",
		"rename from Day14/notes.txt",
		"rename to Day14/sub/.git/notes.txt",
		"diff --git \"a/Day14/x y/.env\" \"b/Day14/x y/.env\"",
		"+SECRET",
	}, "\n")
	got, hidden, err := p.FilterOutput("git_diff", in)
	if err != nil {
		t.Fatal(err)
	}
	if hidden != 3 {
		t.Errorf("hidden = %d, want 3", hidden)
	}
	if strings.Contains(got, "KEY=") || strings.Contains(got, "SECRET") || strings.Contains(got, ".git/") {
		t.Errorf("denied content leaked:\n%s", got)
	}
	if !strings.Contains(got, "+package main") {
		t.Errorf("allowed block lost:\n%s", got)
	}
	if got, _, _ := p.FilterOutput("git_diff", "no unstaged changes"); got != "no unstaged changes" {
		t.Errorf("empty marker changed: %q", got)
	}
}

func TestFilterOutput_PassThrough(t *testing.T) {
	// Tools without OutputPaths and a disabled guard leave the text alone.
	p := outputGuard()
	if got, _, _ := p.FilterOutput("read_text_file", ".env\n 1: x"); got != ".env\n 1: x" {
		t.Errorf("read_text_file output altered: %q", got)
	}
	if got, _, _ := (FS{}).FilterOutput("grep", ".env\n 1: KEY"); got != ".env\n 1: KEY" {
		t.Errorf("disabled guard altered output: %q", got)
	}
}

func TestCheck_GitDiffPathArgument(t *testing.T) {
	readRoot, writeRoot := fixture(t)
	p := guard(t, readRoot, writeRoot)

	args := map[string]interface{}{"path": ".env"}
	if err := p.Check("git", "git_diff", args); CodeOf(err) != CodeDeniedByPolicy {
		t.Errorf("git_diff on .env: want %s, got %v", CodeDeniedByPolicy, err)
	}
	args = map[string]interface{}{"path": "../outside"}
	if err := p.Check("git", "git_diff", args); err == nil {
		t.Error("git_diff path escaping the root must be refused")
	}
	args = map[string]interface{}{"path": 5}
	if err := p.Check("git", "git_diff", args); CodeOf(err) != CodeBadArgument {
		t.Errorf("non-string path: want %s, got %v", CodeBadArgument, err)
	}
	args = map[string]interface{}{}
	if err := p.Check("git", "git_diff", args); err != nil {
		t.Errorf("git_diff without path must pass: %v", err)
	}
	args = map[string]interface{}{"path": "internal/app/toolbelt.go"}
	if err := p.Check("git", "git_diff", args); err != nil {
		t.Fatalf("git_diff on a normal file must pass: %v", err)
	}
	if s, _ := args["path"].(string); !strings.HasPrefix(s, readRoot) {
		t.Errorf("approved path must be rewritten to absolute, got %q", args["path"])
	}
}

// Git tools print paths against the repository root, which sits above the
// read root. Paths outside the read root must not leak through them.
func TestFilterOutput_GitOutsideReadRootHidden(t *testing.T) {
	p := outputGuard()
	tests := []struct {
		name, in, want string
		hidden         int
	}{
		{"porcelain", " M Day14/agent/main.go\n M Day13/secret_plan.go\n?? Day2/notes.md\nR  Day13/x.go -> Day14/x.go", " M Day14/agent/main.go", 3},
		{"ls-files", "Day14/a.go\nREADME.md\nDay140/a.go\n../escape", "Day14/a.go", 3},
		{"name-status", "M\tDay14/a.go\nM\tDay13/b.go", "M\tDay14/a.go", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, hidden, err := p.FilterOutput("git_list_files", tt.in)
			if err != nil || got != tt.want || hidden != tt.hidden {
				t.Errorf("got (%q, %d, %v), want (%q, %d)", got, hidden, err, tt.want, tt.hidden)
			}
		})
	}

	diff := strings.Join([]string{
		"diff --git a/Day13/main.go b/Day13/main.go",
		"--- a/Day13/main.go",
		"+++ b/Day13/main.go",
		"+outside",
		"diff --git a/Day14/main.go b/Day14/main.go",
		"--- a/Day14/main.go",
		"+++ b/Day14/main.go",
		"+inside",
		"diff --git a/Day13/moved.go b/Day14/moved.go",
		"rename from Day13/moved.go",
		"rename to Day14/moved.go",
	}, "\n")
	got, hidden, err := p.FilterOutput("git_diff", diff)
	if err != nil || hidden != 2 || strings.Contains(got, "Day13") || !strings.Contains(got, "+inside") {
		t.Fatalf("got (%q, %d, %v)", got, hidden, err)
	}
}

// Without a locatable repository the git tool output fails closed.
func TestFilterOutput_GitNoRepoFailsClosed(t *testing.T) {
	p := FS{ReadRoot: t.TempDir(), Deny: DefaultDenyGlobs(), Specs: DefaultSpecs(), Audit: io.Discard}
	if _, ok := DetectGitRoot(p.ReadRoot); ok {
		t.Skip("temp dir is inside a git repository")
	}
	if _, _, err := p.FilterOutput("git_list_files", "a.go"); CodeOf(err) != CodeDeniedByPolicy {
		t.Errorf("want %s, got %v", CodeDeniedByPolicy, err)
	}
	if got, _, err := p.FilterOutput("git_list_files", "no untracked files"); err != nil || got != "no untracked files" {
		t.Errorf("marker must pass: %q %v", got, err)
	}
}
