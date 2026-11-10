package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"ai-adv-agent/internal/adapter/clock"
	"ai-adv-agent/internal/adapter/localtools"
	mcpadapter "ai-adv-agent/internal/adapter/mcp"
	"ai-adv-agent/internal/adapter/profilefs"
	ragadapter "ai-adv-agent/internal/adapter/rag"
	"ai-adv-agent/internal/adapter/storage"
	"ai-adv-agent/internal/config"
	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/policy"
	"ai-adv-agent/internal/port"
	"ai-adv-agent/internal/usecase"
)

// Toolbelt is the shared bundle of capabilities available to the orchestrator
// and, identically, to every sub-agent it spawns: the LLM client, the RAG
// pipeline, the MCP tool pool, and the memory factory. Sub-agents receive the
// same Toolbelt, so they have the same reach as the orchestrator — gated only by
// each role's config (which tools it may use).
type Toolbelt struct {
	Cfg    config.Config
	LLM    port.LLMClient
	Memory *MemoryFactory

	// RAG (nil when disabled)
	RAG    *usecase.RAGUseCase
	RAGCfg usecase.RAGConfig

	// MCP (nil/empty when disabled)
	MCPPool    port.MCPPool
	MCPTools   []domain.MCPTool
	MCPServers map[string]domain.MCPServerConfig
	// ToolRouting maps a tool name to the server that serves it, so an executor
	// can dispatch a tool call to the right MCP session.
	ToolRouting map[string]domain.MCPServerConfig

	// Runner executes run_command for the built-in "local" tools in the
	// docker sandbox. nil without sandbox.image: run_command then fails
	// closed (localtools.ErrNoRunner).
	Runner port.CommandRunner

	closers []func()
}

// Build wires a Toolbelt from a validated, env-resolved config. Call Close when
// done to release the RAG database handle and MCP subprocess/SSE sessions.
func Build(cfg config.Config) (*Toolbelt, error) {
	scope := os.Getenv("GIGACHAT_SCOPE")

	// Profiles first: a broken profile must stop the start before any MCP
	// subprocess is spawned.
	if err := ApplyProfiles(&cfg, profilefs.New(cfg.Profiles.Dir), os.Stderr); err != nil {
		return nil, fmt.Errorf("profiles: %w", err)
	}

	llmClient, err := buildLLMClient(cfg.LLM, scope, clock.System{})
	if err != nil {
		return nil, fmt.Errorf("LLM client: %w", err)
	}
	tb := &Toolbelt{
		Cfg:    cfg,
		LLM:    llmClient,
		Memory: NewMemoryFactory(cfg.Memory),
	}

	if err := tb.wireSandbox(context.Background(), cfg.Sandbox, os.Stderr); err != nil {
		return nil, err
	}

	if cfg.RAG.Enabled {
		if err := tb.buildRAG(cfg, scope); err != nil {
			tb.Close()
			return nil, err
		}
	}

	if cfg.MCP.Enabled {
		if err := tb.buildMCP(cfg.MCP); err != nil {
			tb.Close()
			return nil, err
		}
	}

	return tb, nil
}

func (tb *Toolbelt) buildRAG(cfg config.Config, scope string) error {
	retriever, err := ragadapter.NewRetriever(ragadapter.Config{
		DBPath:     cfg.RAG.DB,
		EmbedURL:   cfg.RAG.EmbedURL,
		EmbedModel: cfg.RAG.EmbedModel,
		EmbedKey:   cfg.RAG.EmbedKey,
	})
	if err != nil {
		return fmt.Errorf("RAG retrieval unavailable: %w", err)
	}
	tb.closers = append(tb.closers, func() { _ = retriever.Close() })

	reranker, useAPI, err := buildReranker(cfg.RAG.Rerank, cfg.LLM, scope)
	if err != nil {
		return err
	}
	if reranker != nil {
		mode := "chat scoring"
		if useAPI {
			mode = "dedicated /rerank endpoint"
		}
		model := cfg.RAG.Rerank.Model
		if model == "" {
			model = cfg.LLM.Model
		}
		fmt.Fprintf(os.Stderr, "[rag] rerank model: %s (%s)\n", model, mode)
	}

	tb.RAG = usecase.NewRAGUseCase(retriever, reranker)
	tb.RAGCfg = usecase.RAGConfig{
		TopKRetrieve: cfg.RAG.TopK,
		Rerank:       cfg.RAG.Rerank.Enabled,
		Threshold:    cfg.RAG.Threshold,
		TopKFinal:    cfg.RAG.TopKFinal,
	}
	return nil
}

func (tb *Toolbelt) buildMCP(mc config.MCPConfig) error {
	servers := append([]domain.MCPServerConfig(nil), mc.Servers...)

	// Merge servers from an external MCP YAML file, if configured.
	if mc.File != "" {
		fileCfg, err := storage.NewMCPConfigFile(mc.File).Load()
		if err != nil {
			return fmt.Errorf("load MCP file %q: %w", mc.File, err)
		}
		servers = append(servers, fileCfg.Servers...)
	}

	if len(servers) == 0 {
		fmt.Fprintln(os.Stderr, "[mcp] enabled but no servers configured")
		return nil
	}

	pool, errs := mcpadapter.NewClient().OpenPool(servers)
	for _, e := range errs {
		fmt.Fprintf(os.Stderr, "[mcp] warning: %v\n", e)
	}
	tb.MCPPool = pool
	tb.closers = append(tb.closers, pool.Close)

	tb.MCPServers = make(map[string]domain.MCPServerConfig, len(servers))
	tb.ToolRouting = make(map[string]domain.MCPServerConfig)
	for _, s := range servers {
		tb.MCPServers[s.Name] = s
	}

	// List each server's tools once so sub-agents can advertise them and calls
	// can be routed back to the serving session.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for _, s := range servers {
		tools, err := pool.ListTools(ctx, s)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[mcp] warning: list tools for %q: %v\n", s.Name, err)
			continue
		}
		for _, t := range tools {
			tb.MCPTools = append(tb.MCPTools, t)
			tb.ToolRouting[t.Name] = s
		}
	}
	fmt.Fprintf(os.Stderr, "[mcp] %d server(s), %d tool(s) available\n", len(servers), len(tb.MCPTools))
	return nil
}

// serverAllowed reports whether a server name is in the allow list. A list
// containing "*" grants every server; an empty list grants none (a role must opt
// in to MCP explicitly).
func serverAllowed(allow []string, name string) bool {
	for _, a := range allow {
		if a == "*" || a == name {
			return true
		}
	}
	return false
}

// Grant is everything a role is allowed to reach: which MCP servers, which of
// their tools, and where it may write. The zero Grant grants nothing.
type Grant struct {
	// Servers is the MCP server allow list ("*" = all, empty = none).
	Servers []string
	// Tools narrows the servers' tools by name and carries the write root.
	Tools config.ToolPolicy
	// Role labels the guard's audit lines.
	Role string
}

// policyFor builds the file guard for one role. The roots are made absolute and
// symlink-resolved here, once per grant — never per tool call — so the executor
// closure captures a ready policy.
func (tb *Toolbelt) policyFor(g Grant) policy.FS {
	return tb.policyForServer(g, false)
}

// policyForServer builds the guard. force=true ignores mcp.fs_guard.enabled:
// the built-in local tools are always guarded — there is no external server
// whose own sandboxing could stand in for the policy.
func (tb *Toolbelt) policyForServer(g Grant, force bool) policy.FS {
	fg := tb.Cfg.MCP.FSGuard
	if !fg.Enabled && !force {
		// Explicitly disabled: an empty policy has no specs and no file
		// servers, so every tool is neutral and passes through.
		return policy.FS{}
	}

	readRoot := fg.ReadRoot
	if g.Tools.ReadRoot != "" {
		readRoot = g.Tools.ReadRoot
	}
	deny := fg.Deny
	if len(deny) == 0 {
		deny = policy.DefaultDenyGlobs()
	}
	deny = append(append([]string{}, deny...), g.Tools.DenyGlobs...)

	exec, err := policy.ParseExec(g.Tools.ExecAllow, g.Tools.ExecDeny)
	if err != nil {
		// Validated at load time; a policy that no longer parses allows nothing.
		exec = policy.ExecPolicy{DenyAll: true}
	}

	return policy.FS{
		ReadRoot:   absReal(readRoot),
		WriteRoot:  absReal(g.Tools.WriteRoot), // empty stays empty: no writes
		WriteGlobs: g.Tools.WriteGlobs,
		Exec:       exec,
		Deny:       deny,
		Specs:      policy.DefaultSpecs(),
		FSServers:  append(append([]string{}, fg.Servers...), localtools.ServerName),
		Role:       g.Role,
	}
}

// localFor builds the built-in tool server for one grant: rooted at the
// grant's read root (or write root when it has none), running commands in the
// write root, hiding what the deny globs hide.
func (tb *Toolbelt) localFor(g Grant, pol policy.FS) *localtools.Server {
	root := pol.ReadRoot
	if root == "" {
		root = pol.WriteRoot
	}
	return &localtools.Server{
		Root:    root,
		WorkDir: pol.WriteRoot,
		Runner:  tb.Runner,
		Command: port.CommandSpec{Env: g.Tools.ExecEnv},
		Hide:    pol.Denied,
	}
}

// localGranted reports whether the grant includes the built-in local server
// and the tool is one of its tools.
func localGranted(g Grant, name string) bool {
	if !serverAllowed(g.Servers, localtools.ServerName) {
		return false
	}
	for _, n := range policy.LocalToolNames() {
		if n == name {
			return true
		}
	}
	return false
}

// absReal makes a path absolute and resolves symlinks. An empty path stays
// empty (it means "not granted"). Config.ResolvePaths normally did this at load
// time; repeating it here keeps policyFor correct for hand-built Toolbelts.
func absReal(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real
	}
	return abs
}

// toolNameAllowed applies the role's per-tool gating: deny always wins, and an
// empty allow list means every tool of the granted servers.
//
// For a profiled role (AllowExhaustive) the allow list is exhaustive for the
// tools the policy table knows — file, search, git and run_command — because
// the profile's permissions are what grants them. Tools of other servers
// (github, petstore, …) are outside a profile's vocabulary and are granted by
// the role's `mcp:` server list alone.
func toolNameAllowed(tp config.ToolPolicy, name string) bool {
	for _, d := range tp.Deny {
		if d == name {
			return false
		}
	}
	listed := false
	for _, a := range tp.Allow {
		if a == name {
			listed = true
			break
		}
	}
	if tp.AllowExhaustive {
		_, known := policy.DefaultSpecs()[name]
		return listed || !known
	}
	return len(tp.Allow) == 0 || listed
}

// MCPToolsForGrant returns the tools a role may see: its granted servers, minus
// the names its policy denies, minus the ones the file guard would always
// refuse (a write tool with no write root, or an unknown tool from a file
// server). This is a UX filter — do not offer what is bound to fail. The
// executor below is the actual security boundary.
func (tb *Toolbelt) MCPToolsForGrant(g Grant) []domain.MCPTool {
	if len(g.Servers) == 0 {
		return nil
	}
	var out []domain.MCPTool
	// Local tools win over same-named MCP tools for a grant that includes
	// "local": a role gets one read_text_file, not two.
	taken := map[string]bool{}
	if lpol := tb.policyForServer(g, true); serverAllowed(g.Servers, localtools.ServerName) &&
		(lpol.ReadRoot != "" || lpol.WriteRoot != "") {
		// Without any root the local server has nothing to serve.
		for _, t := range localtools.Tools() {
			if !toolNameAllowed(g.Tools, t.Name) {
				continue
			}
			if len(lpol.FilterServer(localtools.ServerName, []domain.MCPTool{t})) == 0 {
				continue
			}
			out = append(out, t)
			taken[t.Name] = true
		}
	}
	if tb.MCPPool == nil {
		return out
	}
	pol := tb.policyFor(g)
	for _, t := range tb.MCPTools {
		if taken[t.Name] {
			continue
		}
		srv, ok := tb.ToolRouting[t.Name]
		if !ok || !serverAllowed(g.Servers, srv.Name) {
			continue
		}
		if !toolNameAllowed(g.Tools, t.Name) {
			continue
		}
		if len(pol.FilterServer(srv.Name, []domain.MCPTool{t})) == 0 {
			continue
		}
		out = append(out, t)
	}
	return out
}

// ToolExecutorForGrant returns a port.ToolExecutor that dispatches a call to the
// serving MCP session after the grant and the file guard both approve it.
//
// The order is: resolve the server → server allow list → tool name gate → parse
// arguments → policy check on the resolved paths → call. The name gate repeats
// what MCPToolsForGrant already filtered, and that duplication is deliberate: the
// filter only shapes what the model is offered, while this function is the
// boundary a model that invents a tool name has to get past.
func (tb *Toolbelt) ToolExecutorForGrant(g Grant) port.ToolExecutor {
	pol := tb.policyFor(g)
	lpol := tb.policyForServer(g, true)
	local := tb.localFor(g, lpol)
	return func(ctx context.Context, name, argsJSON string) (string, error) {
		if localGranted(g, name) {
			return callGuarded(ctx, g, lpol, localtools.ServerName, name, argsJSON,
				func(ctx context.Context, args map[string]interface{}) (string, error) {
					return local.Call(ctx, name, args)
				})
		}
		srv, ok := tb.ToolRouting[name]
		if !ok {
			return "", fmt.Errorf("unknown tool %q", name)
		}
		if !serverAllowed(g.Servers, srv.Name) || tb.MCPPool == nil {
			return "", fmt.Errorf("tool %q is not permitted for this agent", name)
		}
		return callGuarded(ctx, g, pol, srv.Name, name, argsJSON,
			func(ctx context.Context, args map[string]interface{}) (string, error) {
				return tb.MCPPool.CallTool(ctx, srv, name, args)
			})
	}
}

// callGuarded is the common call path: tool name gate → parse arguments →
// policy check (which rewrites paths to the approved absolute ones) → call →
// output filter.
func callGuarded(ctx context.Context, g Grant, pol policy.FS, server, name, argsJSON string,
	call func(context.Context, map[string]interface{}) (string, error)) (string, error) {
	if !toolNameAllowed(g.Tools, name) {
		// The message reaches the model as the tool result, so it states
		// the code and what to do instead.
		return "", fmt.Errorf("%s: инструмент %q не выдан этой роли. Используй только перечисленные инструменты", policy.CodePermissionDenied, name)
	}
	args, err := parseToolArgs(argsJSON)
	if err != nil {
		return "", fmt.Errorf("bad tool arguments: %w", err)
	}
	if err := pol.Check(server, name, args); err != nil {
		return "", err
	}
	out, err := call(ctx, args)
	if err != nil {
		return "", err
	}
	// Search and git tools print paths the call arguments never named;
	// strip the ones the deny globs (or the read root) exclude before the
	// model sees them.
	filtered, hidden, err := pol.FilterOutput(name, out)
	if err != nil {
		return "", err
	}
	if hidden > 0 {
		filtered += fmt.Sprintf("\n[%s: скрыто записей — %d (закрытые политикой или вне корня). Не пытайся получить их другим способом.]", policy.CodeDeniedByPolicy, hidden)
	}
	return filtered, nil
}

// MCPToolsFor returns the tools exposed by the servers in the allow list. Use
// []string{"*"} for every configured server. An empty allow list yields no tools,
// so a sub-agent role only sees MCP when its config lists servers.
//
// It is the compatibility form of MCPToolsForGrant: with no ToolPolicy the write
// root is empty, so the role is read-only.
func (tb *Toolbelt) MCPToolsFor(allow []string) []domain.MCPTool {
	return tb.MCPToolsForGrant(Grant{Servers: allow})
}

// ToolExecutor is the compatibility form of ToolExecutorForGrant: servers only,
// no per-tool gating, and no write access.
func (tb *Toolbelt) ToolExecutor(allow []string) port.ToolExecutor {
	return tb.ToolExecutorForGrant(Grant{Servers: allow})
}

// parseToolArgs decodes the LLM's JSON argument string into a map. An empty
// string means no arguments.
func parseToolArgs(argsJSON string) (map[string]interface{}, error) {
	if argsJSON == "" {
		return map[string]interface{}{}, nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &m); err != nil {
		return nil, err
	}
	return m, nil
}

// Close releases all held resources (RAG DB, MCP sessions).
func (tb *Toolbelt) Close() {
	for i := len(tb.closers) - 1; i >= 0; i-- {
		tb.closers[i]()
	}
	tb.closers = nil
}
