package app

import (
	"fmt"
	"time"

	"ai-adv-agent/internal/adapter/profilefs"
	sastadapter "ai-adv-agent/internal/adapter/sast"
	"ai-adv-agent/internal/adapter/storage"
	"ai-adv-agent/internal/adapter/workspace"
	"ai-adv-agent/internal/config"
	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/usecase/devpipeline"
	"ai-adv-agent/internal/usecase/execloop"
)

// BuildLoop assembles the execution loop (internal/usecase/execloop) from the
// loaded config and an already-built toolbelt. It wires:
//
//   - roles: the profile role runner over the profile catalogue;
//   - workspace: the FS adapter rooted at sandbox.workspace_root, seeded from
//     sandbox.templates;
//   - sast: the scanner adapter over the sandbox runner, from pipeline.sast;
//   - repo: the LoopFile store derived from the memory history path;
//   - meter: a MeteredLLM over the toolbelt LLM client, budgeted from loop:.
//
// ev receives stage/security/budget progress (TUI and CLI pass their own
// adapters); logf is the loop's internal diagnostic sink (nil = silent).
// The meter becomes tb.LLM, so every role call of the loop is budgeted.
func BuildLoop(cfg config.Config, tb *Toolbelt, ev execloop.Events, logf func(string, ...any)) (*execloop.Loop, error) {
	if tb == nil {
		return nil, fmt.Errorf("loop: toolbelt is nil")
	}
	lc := cfg.Loop

	// Planner/verifier run as the "researcher" profile; the catalogue comes
	// from the profile directory. A missing researcher fails here, closed.
	profiles := profilefs.New(cfg.Profiles.Dir)
	catalogue, err := profiles.List()
	if err != nil {
		return nil, fmt.Errorf("loop: каталог профилей: %w", err)
	}
	catalogueCfg := make([]execloop.Profile, 0, len(catalogue))
	for _, p := range catalogue {
		catalogueCfg = append(catalogueCfg, execloop.Profile{Name: p.Name, Description: p.Description})
	}

	budget := domain.Budget{
		MaxSubtasks:           lc.MaxSubtasks,
		MaxAttemptsPerSubtask: lc.MaxAttemptsPerSubtask,
		MaxLLMCalls:           lc.MaxLLMCalls,
		MaxTokens:             lc.MaxTokens,
		MaxCostUSD:            lc.MaxCostUSD,
		MaxReplans:            lc.MaxReplans,
		Deadline:              time.Now().Add(lc.Deadline),
	}
	meter, err := execloop.NewMeteredLLM(tb.LLM, budget, nil, ev, 10)
	if err != nil {
		return nil, fmt.Errorf("loop: счётчик бюджета: %w", err)
	}
	tb.LLM = meter // every role chat of the loop goes through the meter

	roles := NewProfileRoleRunner(tb, profiles, false)

	scanner, err := sastadapter.New(tb.Runner, sastadapter.Config{
		Commands:  cfg.Pipeline.SAST,
		Timeout:   cfg.Pipeline.CommandTimeout,
		MaxOutput: cfg.Sandbox.MaxOutputBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("loop: SAST: %w", err)
	}

	ws, err := workspace.New(workspace.Config{
		Root:      cfg.Sandbox.WorkspaceRoot,
		Templates: cfg.Sandbox.Templates,
		MaxFiles:  cfg.Sandbox.CopyMaxFiles,
		MaxBytes:  cfg.Sandbox.CopyMaxBytes,
	})
	if err != nil {
		return nil, fmt.Errorf("loop: рабочая область: %w", err)
	}

	repo := storage.NewLoopFile(loopHistoryPath(cfg))

	loopCfg := execloop.Config{
		MaxSubtasks:           lc.MaxSubtasks,
		MaxAttemptsPerSubtask: lc.MaxAttemptsPerSubtask,
		MaxLLMCalls:           lc.MaxLLMCalls,
		MaxTokens:             lc.MaxTokens,
		MaxCostUSD:            lc.MaxCostUSD,
		Deadline:              lc.Deadline,
		MaxReplans:            lc.MaxReplans,
		OnAskUser:             lc.OnAskUser,
		Profiles:              catalogueCfg,
		JSONRetries:           cfg.Pipeline.JSONRetries,
		BudgetNotifyEvery:     10,
		VerifyFileMaxBytes:    cfg.Pipeline.ReviewFileMaxBytes,
		VerifyTotalMaxBytes:   cfg.Pipeline.ReviewTotalMaxBytes,
		WorkspaceRoot:         cfg.Sandbox.WorkspaceRoot,
	}
	pipeCfg := devpipeline.Config{
		MaxSecurityIterations: cfg.Pipeline.MaxSecurityIterations,
		JSONRetries:           cfg.Pipeline.JSONRetries,
		Generator:             cfg.Pipeline.GeneratorProfile,
		Reviewers:             cfg.Pipeline.Reviewers,
		TestCommands:          cfg.Pipeline.TestCommands,
		CommandTimeout:        cfg.Pipeline.CommandTimeout,
		TestOutputMaxBytes:    cfg.Pipeline.TestOutputMaxBytes,
		ReviewFileMaxBytes:    cfg.Pipeline.ReviewFileMaxBytes,
		ReviewTotalMaxBytes:   cfg.Pipeline.ReviewTotalMaxBytes,
		MaxToolRounds:         cfg.Pipeline.MaxToolRounds,
	}
	return execloop.New(roles, tb.Runner, scanner, meter, repo, ws, ev, loopCfg, pipeCfg, nil, logf)
}

// loopHistoryPath mirrors MemoryFactory: the STM history path when set,
// otherwise the memory dir. The loop store derives its file next to it.
func loopHistoryPath(cfg config.Config) string {
	if cfg.Memory.STM.History != "" {
		return cfg.Memory.STM.History
	}
	return cfg.Memory.Dir
}
