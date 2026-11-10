package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"ai-adv-agent/internal/adapter/localtools"
	"ai-adv-agent/internal/config"
	"ai-adv-agent/internal/port"
	"ai-adv-agent/internal/usecase"
	"ai-adv-agent/internal/usecase/profilemap"
)

// ProfileRoleRunner implements port.RoleRunner on top of the existing
// SubAgent machinery: a profile file (profiles/<name>.md) is mapped to a
// permission grant by profilemap.Map, narrowed to the requested workspace
// roots, and the role runs on an ephemeral chat with the built-in local
// tools. The first line of the system prompt is the "# profile: <name>"
// marker the fake upstream recognises a role by.
//
// The pipeline (usecase/devpipeline) drives this runner: the generator gets
// read+write roots of the task directory, reviewers are read-only
// (Write == "" drops the write globs and exec rights, whatever the profile
// grants).
type ProfileRoleRunner struct {
	tb    *Toolbelt
	repo  port.AgentProfileRepository
	debug bool
	out   io.Writer // progress sink; default os.Stderr
}

// NewProfileRoleRunner binds the runner to a toolbelt and a profile
// repository (usually adapter/profilefs rooted at cfg.Profiles.Dir).
func NewProfileRoleRunner(tb *Toolbelt, repo port.AgentProfileRepository, debug bool) *ProfileRoleRunner {
	return &ProfileRoleRunner{tb: tb, repo: repo, debug: debug, out: os.Stderr}
}

// Run carries out one role request.
func (r *ProfileRoleRunner) Run(ctx context.Context, req port.RoleRequest) (port.RoleResult, error) {
	if strings.TrimSpace(req.Profile) == "" {
		return port.RoleResult{}, fmt.Errorf("role runner: профиль не задан")
	}
	p, err := r.repo.Get(req.Profile)
	if err != nil {
		return port.RoleResult{}, fmt.Errorf("role %q: %w", req.Profile, err)
	}
	g, err := profilemap.Map(p)
	if err != nil {
		return port.RoleResult{}, fmt.Errorf("role %q: %w", req.Profile, err)
	}

	// The grant comes from the profile; the roots come from the caller and
	// may only narrow it. Write == "" makes the role read-only: write globs
	// and exec are dropped, never widened.
	tp := config.ToolPolicy{
		Allow:           g.AllowTools,
		AllowExhaustive: true,
		ReadRoot:        req.Roots.Read,
		DenyGlobs:       g.DenyGlobs,
	}
	if req.Roots.Write != "" {
		tp.WriteRoot = req.Roots.Write
		tp.WriteGlobs = g.WriteGlobs
		tp.ExecAllow = g.ExecAllow
		tp.ExecDeny = g.ExecDeny
		tp.ExecEnv = g.Env
	}
	if err := config.ValidateToolPolicy(tp, "role "+req.Profile); err != nil {
		return port.RoleResult{}, err
	}

	grant := Grant{Servers: []string{localtools.ServerName}, Tools: tp, Role: req.Profile}
	tools := r.tb.MCPToolsForGrant(grant)
	executor := r.tb.ToolExecutorForGrant(grant)

	query := req.Task
	for _, b := range req.DataBlocks {
		query += "\n\n" + usecase.WrapUntrusted(b.Label, b.Content)
	}

	// The guard proxy reports the call cost per response; prefer it over the
	// pricing-table estimate. OnResponse sees every round of the tool loop.
	var guardCost float64
	var hasGuardCost bool
	cfg := usecase.ChatConfig{
		Model:         r.tb.Cfg.LLM.Model,
		FullQuery:     query,
		SystemMessage: profilemap.SystemPrompt(p),
		MaxTokens:     r.tb.Cfg.LLM.MaxTokens,
		Temperature:   temperatureOrDefault(r.tb.Cfg.LLM.Temperature),
		Strategy:      usecase.StrategyNone,
		Debug:         r.debug,
		MaxToolRounds: req.MaxToolRounds,
		OnResponse: func(resp port.LLMResponse) error {
			if resp.Guard.HasCost {
				guardCost += resp.Guard.CostUSD
				hasGuardCost = true
			}
			return nil
		},
	}

	chat := r.tb.Memory.EphemeralChat(r.tb.LLM, nil, false)
	res, err := chat.ExecuteWithTools(ctx, cfg, tools, executor)
	if err != nil {
		// errors.Is(err, port.ErrGuardBlocked) stays true through the wrap:
		// the caller decides that an input-guard refusal blocks the subtask.
		return port.RoleResult{}, fmt.Errorf("role %q: %w", req.Profile, err)
	}
	if res.RedactedToolCalls > 0 {
		// Refused calls were already answered with a tool error inside the
		// loop (usecase.RedactedToolCallResult); nothing executed them.
		fmt.Fprintf(r.out, "[role %s] %d tool call(s) refused: arguments redacted by the guard proxy\n", req.Profile, res.RedactedToolCalls)
	}

	cost := res.CostUSD
	if hasGuardCost {
		cost = guardCost
	}
	out := port.RoleResult{
		Content:   res.Content,
		Usage:     res.Usage,
		CostUSD:   cost,
		ToolCalls: res.ToolCalls,
	}
	if res.GuardBlocked {
		out.GuardBlocked = true
		out.GuardReason = res.GuardReason
	}
	return out, nil
}

// Compile-time check that ProfileRoleRunner is the pipeline's RoleRunner.
var _ port.RoleRunner = (*ProfileRoleRunner)(nil)
