package app

import (
	"fmt"
	"io"
	"os"

	"ai-adv-agent/internal/config"
	"ai-adv-agent/internal/port"
	"ai-adv-agent/internal/usecase/profilemap"
)

// ApplyProfiles fills every sub-agent role from its profile: the name, the
// composed system prompt (marker + shared DATA rules + profile body) and the
// tool policy derived by profilemap.Map. The YAML may not carry prompt/tools
// for a profiled role (config.Load refuses it), so the profile is the single
// source of truth.
//
// In chat mode a profile that may write gets cfg.Profiles.WriteRoot as its
// write root, narrowed by the profile's write globs. Without a configured
// write root such a profile is downgraded to read-only (and exec is dropped:
// commands run in the write root) — never widened.
func ApplyProfiles(cfg *config.Config, repo port.AgentProfileRepository, log io.Writer) error {
	if log == nil {
		log = os.Stderr
	}
	for i := range cfg.Orchestrator.SubAgents {
		sa := &cfg.Orchestrator.SubAgents[i]
		if sa.Profile == "" {
			continue
		}
		p, err := repo.Get(sa.Profile)
		if err != nil {
			return fmt.Errorf("orchestrator.subagents[%d]: %w", i, err)
		}
		g, err := profilemap.Map(p)
		if err != nil {
			return fmt.Errorf("orchestrator.subagents[%d]: %w", i, err)
		}
		for _, n := range g.Notes {
			fmt.Fprintf(log, "[profiles] %s: %s\n", p.Name, n)
		}
		tp := config.ToolPolicy{
			Allow:     g.AllowTools,
			ReadRoot:  sa.Tools.ReadRoot,
			DenyGlobs: g.DenyGlobs,
			// An empty allow list would otherwise mean "every tool of the
			// granted servers"; a profile that grants nothing gets nothing.
			AllowExhaustive: true,
		}
		if g.CanWrite() {
			if cfg.Profiles.WriteRoot == "" {
				fmt.Fprintf(log, "[profiles] %s: profiles.write_root не задан — роль работает только на чтение\n", p.Name)
			} else {
				tp.WriteRoot = cfg.Profiles.WriteRoot
				tp.WriteGlobs = g.WriteGlobs
				tp.ExecAllow = g.ExecAllow
				tp.ExecDeny = g.ExecDeny
				tp.ExecEnv = g.Env
			}
		}
		what := fmt.Sprintf("orchestrator.subagents[%d] (%s)", i, p.Name)
		if err := config.ValidateToolPolicy(tp, what); err != nil {
			return err
		}
		sa.Name = p.Name
		sa.Description = p.Description
		sa.Prompt = profilemap.SystemPrompt(p)
		sa.Tools = tp
	}
	return nil
}
