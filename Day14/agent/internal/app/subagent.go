package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"ai-adv-agent/internal/config"
	"ai-adv-agent/internal/usecase"
)

// SubAgent is one worker the orchestrator spawns to carry out a delegated task.
// It reuses the same execution engine as the rest of the agent — ChatUseCase
// with the MCP tool-calling loop — but runs on its own ephemeral memory (in-memory
// STM+WM seeded with the orchestrator's context, shared read-only LTM/profile) and
// with tool access scoped to what its role config permits. It has the same reach
// as the orchestrator: RAG grounding and MCP tools, gated per role.
type SubAgent struct {
	role  config.SubAgentConfig
	tb    *Toolbelt
	debug bool
	out   io.Writer // progress sink; default os.Stderr, redirected by Orchestrator.SetOutput
}

// ErrGuardBlockedAnswer reports that the guard proxy replaced the sub-agent's
// final answer with its refusal text. The text is not the model's answer, so
// the run is a failure, never a result to route onward.
var ErrGuardBlockedAnswer = errors.New("ответ саб-агента заблокирован guard'ом прокси")

// NewSubAgent binds a role definition to the shared toolbelt.
func NewSubAgent(role config.SubAgentConfig, tb *Toolbelt, debug bool) *SubAgent {
	return &SubAgent{role: role, tb: tb, debug: debug, out: os.Stderr}
}

// Run carries out task using the orchestrator-supplied context. It returns the
// sub-agent's final text answer, which the orchestrator routes onward.
func (s *SubAgent) Run(ctx context.Context, task, orchestratorContext string) (string, error) {
	chat := s.tb.Memory.EphemeralChat(s.tb.LLM, nil, s.tb.Memory.WMEnabledFor(s.role.Memory.WM))

	// The orchestrator context seeds the sub-agent's working input; the role
	// prompt is the system instruction.
	// The context routes earlier agents' results, which may quote files or
	// tool output: it is untrusted data, wrapped in nonce markers.
	query := task
	if strings.TrimSpace(orchestratorContext) != "" {
		query = "Контекст от оркестратора:\n" + usecase.WrapUntrusted("orchestrator-context", orchestratorContext) +
			"\n\nЗадача:\n" + task
	}

	// RAG grounding: retrieve → [rerank] → filter, then prepend the context to the
	// query. Composes with MCP tools below (the grounded prompt is still the input
	// to the tool-calling loop).
	if s.role.RAG && s.tb.RAG != nil {
		res, err := s.tb.RAG.BuildPrompt(ctx, query, s.tb.RAGCfg)
		if err != nil {
			fmt.Fprintf(s.out, "[subagent %s] RAG retrieval failed: %v\n", s.role.Name, err)
		} else {
			if res.RerankErr != nil {
				fmt.Fprintf(s.out, "[subagent %s] rerank failed, using similarity order: %v\n", s.role.Name, res.RerankErr)
			}
			fmt.Fprintf(s.out, "[subagent %s] RAG grounded on %d chunk(s)\n", s.role.Name, len(res.Final))
			query = res.Prompt
		}
	}

	// The grant carries the role's servers, its per-tool gating and its write
	// root, so the file guard knows what this role may touch and whose name to
	// record in the audit log.
	grant := Grant{Servers: s.role.MCP, Tools: s.role.Tools, Role: s.role.Name}
	tools := s.tb.MCPToolsForGrant(grant)
	executor := s.tb.ToolExecutorForGrant(grant)

	cfg := usecase.ChatConfig{
		Model:         s.tb.Cfg.LLM.Model,
		FullQuery:     query,
		SystemMessage: systemPromptFor(s.role),
		MaxTokens:     s.tb.Cfg.LLM.MaxTokens,
		Temperature:   temperatureOrDefault(s.tb.Cfg.LLM.Temperature),
		Strategy:      usecase.StrategyNone,
		Debug:         s.debug,
	}

	res, err := chat.ExecuteWithTools(ctx, cfg, tools, executor)
	if err != nil {
		return "", fmt.Errorf("subagent %q: %w", s.role.Name, err)
	}
	if res.RedactedToolCalls > 0 {
		fmt.Fprintf(s.out, "[subagent %s] %d tool call(s) refused: arguments redacted by the guard proxy\n", s.role.Name, res.RedactedToolCalls)
	}
	if res.GuardBlocked {
		return "", fmt.Errorf("subagent %q: %w (%s)", s.role.Name, ErrGuardBlockedAnswer, res.GuardReason)
	}
	if usecase.CanaryLeak(res.Content) {
		return "", fmt.Errorf("subagent %q: %w", s.role.Name, usecase.ErrCanaryLeak)
	}
	return res.Content, nil
}

// systemPromptFor returns the role prompt with the shared DATA-marker rule.
// Profile prompts already carry it (profilemap.SystemPrompt); a hand-built
// role gets it appended, because the orchestrator context is always wrapped.
func systemPromptFor(role config.SubAgentConfig) string {
	if strings.Contains(role.Prompt, usecase.UntrustedDataRule) {
		return role.Prompt
	}
	if strings.TrimSpace(role.Prompt) == "" {
		return usecase.UntrustedDataRule
	}
	return role.Prompt + "\n\n" + usecase.UntrustedDataRule
}

// temperatureOrDefault maps a *float64 config temperature to the ChatConfig
// convention (negative = provider default).
func temperatureOrDefault(t *float64) float64 {
	if t == nil {
		return -1
	}
	return *t
}
