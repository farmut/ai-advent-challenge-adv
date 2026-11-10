package main

import (
	"io"
	"strings"
	"testing"
	"time"

	"ai-adv-agent/internal/adapter/profilefs"
	"ai-adv-agent/internal/app"
	"ai-adv-agent/internal/config"
	"ai-adv-agent/internal/policy"
)

// gitToolNames are the tools served by ../git-mcp-server. They carry no file
// paths, so internal/policy has no spec for them — the allow-list guard below
// has to know them separately.
var gitToolNames = map[string]bool{
	"git_current_branch": true,
	"git_list_files":     true,
	"git_diff":           true,
}

// TestExampleConfigIsValid guards the shipped agent.config.yaml: it must parse,
// keep all memory layers enabled, expose the three-role roster, and pass
// validation once credentials are present.
func TestExampleConfigIsValid(t *testing.T) {
	cfg, err := config.Load("agent.config.yaml")
	if err != nil {
		t.Fatalf("example config failed to load: %v", err)
	}

	if !cfg.Memory.STM.Enabled || !cfg.Memory.WM.Enabled || !cfg.Memory.LTM.Enabled ||
		!cfg.Memory.Facts.Enabled || !cfg.Memory.Profile.Enabled || !cfg.Memory.TaskMemory.Enabled {
		t.Errorf("all memory layers must be enabled by default: %+v", cfg.Memory)
	}

	wantRoles := []string{"researcher", "bug-fixer", "tech-writer", "coder", "security-go", "security-vue"}
	if len(cfg.Orchestrator.SubAgents) != len(wantRoles) {
		t.Fatalf("expected %d sub-agents, got %d", len(wantRoles), len(cfg.Orchestrator.SubAgents))
	}
	for i, w := range wantRoles {
		if sa := cfg.Orchestrator.SubAgents[i]; sa.Profile != w || sa.Name != w {
			t.Errorf("sub-agent %d = %+v, want profile %q", i, sa, w)
		}
	}
	if !cfg.Orchestrator.SubAgents[0].RAG {
		t.Errorf("researcher role misconfigured: %+v", cfg.Orchestrator.SubAgents[0])
	}

	// --- file tooling: the two external MCP servers must be wired ---
	servers := map[string]bool{}
	for _, s := range cfg.MCP.Servers {
		servers[s.Name] = true
	}
	for _, want := range []string{"fs", "search"} {
		if !servers[want] {
			t.Errorf("MCP server %q is missing: %+v", want, cfg.MCP.Servers)
		}
	}

	// The file server's npm version must stay pinned: policy.DefaultSpecs is a
	// fail-closed table of that exact release's tools, so an unpinned upgrade
	// would make new tools vanish rather than slip through — a silent loss of
	// functionality that is hard to trace back here.
	for _, s := range cfg.MCP.Servers {
		if s.Name != "fs" {
			continue
		}
		pinned := false
		for _, a := range s.Args {
			if strings.Contains(a, "@modelcontextprotocol/server-filesystem@") {
				pinned = true
			}
		}
		if !pinned {
			t.Errorf("fs server must pin an explicit package version: %v", s.Args)
		}
	}

	// --- fs guard ---
	fg := cfg.MCP.FSGuard
	if !fg.Enabled {
		t.Error("mcp.fs_guard must stay enabled in the shipped config")
	}
	if fg.ReadRoot != ".." {
		t.Errorf("mcp.fs_guard.read_root = %q, want %q", fg.ReadRoot, "..")
	}
	// Only the file server needs path checks: fff-mcp's tools take no path
	// argument, so listing it here would gate nothing and mislead the reader.
	if len(fg.Servers) != 1 || fg.Servers[0] != "fs" {
		t.Errorf("mcp.fs_guard.servers = %v, want [fs]", fg.Servers)
	}
	for _, want := range []string{".env*", ".git/**"} {
		found := false
		for _, d := range fg.Deny {
			if d == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("mcp.fs_guard.deny is missing %q: %v", want, fg.Deny)
		}
	}

	// --- profiles: every role resolves, and only the writing profiles write ---
	pcfg := cfg
	pcfg.Orchestrator.SubAgents = append([]config.SubAgentConfig(nil), cfg.Orchestrator.SubAgents...)
	if err := config.ResolvePaths(&pcfg, "agent.config.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := app.ApplyProfiles(&pcfg, profilefs.New(pcfg.Profiles.Dir), io.Discard); err != nil {
		t.Fatalf("shipped profiles failed to apply: %v", err)
	}
	writers := map[string]bool{}
	for _, sa := range pcfg.Orchestrator.SubAgents {
		if sa.Tools.WriteRoot != "" {
			writers[sa.Name] = true
			if sa.Tools.WriteRoot != pcfg.Profiles.WriteRoot || len(sa.Tools.WriteGlobs) == 0 {
				t.Errorf("%s: writes must be confined to profiles.write_root by globs: %+v", sa.Name, sa.Tools)
			}
		}
		if !strings.HasPrefix(sa.Prompt, "# profile: "+sa.Name+"\n") {
			t.Errorf("%s: system prompt must start with the profile marker", sa.Name)
		}
	}
	if len(writers) != 3 || !writers["coder"] || !writers["bug-fixer"] || !writers["tech-writer"] {
		t.Errorf("exactly coder, bug-fixer and tech-writer may write, got %v", writers)
	}
	if cfg.Consultant.Tools.WriteRoot != "" {
		t.Errorf("the consultant must stay read-only")
	}

	// --- every granted tool name must actually exist ---
	// A typo in the YAML would silently strip the role of that tool (the
	// allow-list is matched by exact name), so the names are checked against
	// the fail-closed spec table plus the git server's tools.
	specs := policy.DefaultSpecs()
	checkAllow := func(role string, tp config.ToolPolicy) {
		for _, name := range append(append([]string{}, tp.Allow...), tp.Deny...) {
			if _, ok := specs[name]; ok {
				continue
			}
			if gitToolNames[name] {
				continue
			}
			t.Errorf("%s: unknown tool %q in tools.allow/deny (typo? not in policy.DefaultSpecs nor a git tool)", role, name)
		}
	}
	for _, sa := range pcfg.Orchestrator.SubAgents {
		checkAllow(sa.Name, sa.Tools)
	}
	checkAllow("consultant", cfg.Consultant.Tools)

	// --- LLM: through the Day13 proxy, model chosen by the user ---
	if cfg.LLM.Provider != config.ProviderOpenAI || cfg.LLM.BaseURL != "http://127.0.0.1:8090/v1" {
		t.Errorf("llm must target the local Day13 proxy, got provider=%q base_url=%q", cfg.LLM.Provider, cfg.LLM.BaseURL)
	}
	if !cfg.LLM.RateLimit.Enabled || cfg.LLM.RequestMaxBytes >= 1<<20 {
		t.Errorf("shipped limits must stay safe: %+v request_max_bytes=%d", cfg.LLM.RateLimit, cfg.LLM.RequestMaxBytes)
	}
	for _, want := range []string{"**/.git/**", "**/.ssh/**", "**/chat_history*.json", "**/agent-tui.log"} {
		found := false
		for _, d := range fg.Deny {
			found = found || d == want
		}
		if !found {
			t.Errorf("mcp.fs_guard.deny is missing %q", want)
		}
	}

	// The key comes from the environment; the model is required and is
	// supplied by the user (simulating --model).
	t.Setenv("LLM_API_KEY", "sk-test")
	t.Setenv("LLM_MODEL", "must-be-ignored")
	config.ResolveEnv(&cfg)
	if cfg.LLM.Model != "" {
		t.Fatalf("LLM_MODEL must not fill llm.model, got %q", cfg.LLM.Model)
	}
	if err := config.Validate(cfg); err == nil {
		t.Fatal("the shipped config without a model must not validate")
	}
	model := "qwen2.5-7b-ollama"
	config.Overrides{Model: &model}.Apply(&cfg)

	// The real startup path resolves roots and substitutes the ${READ_ROOT} /
	// ${WRITE_ROOT} tokens before validating. An unsubstituted token would be
	// handed to the server subprocess verbatim.
	if err := config.ResolvePaths(&cfg, "agent.config.yaml"); err != nil {
		t.Fatalf("ResolvePaths on the example config: %v", err)
	}
	for _, s := range cfg.MCP.Servers {
		for _, a := range s.Args {
			if strings.Contains(a, "${") {
				t.Errorf("server %q: unsubstituted token in args: %q", s.Name, a)
			}
		}
	}

	if err := config.Validate(cfg); err != nil {
		t.Fatalf("example config failed validation: %v", err)
	}
}

// TestReviewConfigIsValid guards the CI review config (agent.review.yaml, used
// by .github/workflows/pr-agent-review.yml): it must parse, disable every
// persistent memory layer (CI runs are ephemeral), wire the github MCP server,
// expose the five-role review roster with correct tool gating, and validate.
func TestReviewConfigIsValid(t *testing.T) {
	cfg, err := config.Load("agent.review.yaml")
	if err != nil {
		t.Fatalf("review config failed to load: %v", err)
	}

	if cfg.Memory.STM.Enabled || cfg.Memory.WM.Enabled || cfg.Memory.LTM.Enabled ||
		cfg.Memory.Facts.Enabled || cfg.Memory.Profile.Enabled || cfg.Memory.TaskMemory.Enabled {
		t.Errorf("CI review config must disable all memory layers: %+v", cfg.Memory)
	}
	if cfg.RAG.Enabled || cfg.Consultant.Enabled {
		t.Error("RAG and consultant must be disabled in the CI review config")
	}

	if !cfg.MCP.Enabled || len(cfg.MCP.Servers) != 1 || cfg.MCP.Servers[0].Name != "github" {
		t.Fatalf("expected the single github MCP server, got %+v", cfg.MCP)
	}

	want := map[string][]string{
		"diff-reader":   {"github"},
		"bug-hunter":    {},
		"architect":     {},
		"code-reviewer": {},
		"publisher":     {"github"},
	}
	if len(cfg.Orchestrator.SubAgents) != len(want) {
		t.Fatalf("expected %d sub-agents, got %d", len(want), len(cfg.Orchestrator.SubAgents))
	}
	for _, sa := range cfg.Orchestrator.SubAgents {
		allow, ok := want[sa.Name]
		if !ok {
			t.Errorf("unexpected sub-agent %q", sa.Name)
			continue
		}
		if len(sa.MCP) != len(allow) {
			t.Errorf("%s: MCP allow-list = %v, want %v", sa.Name, sa.MCP, allow)
		}
		if sa.RAG {
			t.Errorf("%s: RAG must be off in the review roster", sa.Name)
		}
	}

	if err := config.ResolvePaths(&cfg, "agent.review.yaml"); err != nil {
		t.Fatal(err)
	}
	if err := app.ApplyProfiles(&cfg, profilefs.New(cfg.Profiles.Dir), io.Discard); err != nil {
		t.Fatalf("review profiles failed to apply: %v", err)
	}
	for _, sa := range cfg.Orchestrator.SubAgents {
		if sa.Tools.WriteRoot != "" || len(sa.Tools.Allow) != 0 || !sa.Tools.AllowExhaustive {
			t.Errorf("%s: review roles get no file tools, got %+v", sa.Name, sa.Tools)
		}
		if !strings.Contains(sa.Prompt, "<<<DATA-") {
			t.Errorf("%s: prompt must carry the DATA rule", sa.Name)
		}
	}

	t.Setenv("LLM_PROVIDER", "openrouter")
	t.Setenv("LLM_API_KEY", "sk-test")
	config.ResolveEnv(&cfg)
	model := "openai/gpt-4o" // CI passes --model
	config.Overrides{Model: &model}.Apply(&cfg)
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("review config failed validation: %v", err)
	}
}

// TestE2EConfigIsFast guards testdata/config/agent.e2e.yaml: functional runs
// must not wait on production limits, and the fast values must still pass the
// same validation as production ones.
func TestE2EConfigIsFast(t *testing.T) {
	cfg, err := config.Load("testdata/config/agent.e2e.yaml")
	if err != nil {
		t.Fatalf("e2e config failed to load: %v", err)
	}
	if cfg.LLM.RateLimit.Enabled {
		t.Error("e2e config must disable the client-side limiter")
	}
	if cfg.LLM.Retry.BaseDelay > 10*time.Millisecond || cfg.LLM.Retry.MaxDelay > 10*time.Millisecond {
		t.Errorf("e2e retry delays must be tiny, got %+v", cfg.LLM.Retry)
	}
	t.Setenv("LLM_API_KEY", "sk-test")
	config.ResolveEnv(&cfg)
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("e2e config failed validation: %v", err)
	}
}
