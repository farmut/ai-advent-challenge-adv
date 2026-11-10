package app

import (
	"context"
	"fmt"
	"io"

	"ai-adv-agent/internal/adapter/sandbox"
	"ai-adv-agent/internal/config"
)

// NewSandboxRunner builds the docker CommandRunner from a resolved config
// (ResolvePaths made workspace_root absolute). It does not contact docker.
func NewSandboxRunner(sc config.SandboxConfig) (*sandbox.Docker, error) {
	if err := sc.RequireImage(); err != nil {
		return nil, err
	}
	return sandbox.New(sandbox.Config{
		DockerBin:       sc.DockerBin,
		Image:           sc.Image,
		Platform:        sc.Platform,
		WorkspaceRoot:   sc.WorkspaceRoot,
		Timeout:         sc.Timeout,
		KillTimeout:     sc.KillTimeout,
		MaxOutputBytes:  sc.MaxOutputBytes,
		Memory:          sc.Memory,
		CPUs:            sc.CPUs,
		Pids:            sc.Pids,
		TmpfsSize:       sc.TmpfsSize,
		CopyMaxFiles:    sc.CopyMaxFiles,
		CopyMaxBytes:    sc.CopyMaxBytes,
		AllowedBinaries: sc.AllowedBinaries,
	})
}

// wireSandbox attaches the sandbox runner for run_command in chat mode.
//
// Without sandbox.image there is no runner and run_command fails closed. With
// an image the runner is attached even if docker is down right now: every
// command then fails with sandbox.ErrUnavailable — never runs outside the
// sandbox. Preflight (which also removes containers left by dead agent
// processes) is best-effort here and only reported; the loop calls it itself
// and refuses to start on failure.
func (tb *Toolbelt) wireSandbox(ctx context.Context, sc config.SandboxConfig, log io.Writer) error {
	if !sc.Enabled() {
		return nil
	}
	r, err := NewSandboxRunner(sc)
	if err != nil {
		return fmt.Errorf("sandbox: %w", err)
	}
	tb.Runner = r
	if err := r.Preflight(ctx); err != nil {
		fmt.Fprintf(log, "[sandbox] %v — run_command будет отклоняться, пока песочница недоступна\n", err)
	}
	return nil
}
