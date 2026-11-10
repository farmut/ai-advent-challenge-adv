package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"ai-adv-agent/internal/app"
	"ai-adv-agent/internal/config"
	"ai-adv-agent/internal/domain"
)

// Exit codes of the --loop/--loop-resume run (see the plan, PR8).
const (
	exitDone               = 0 // the run finished: all subtasks done
	exitFailed             = 2 // the run finished with failed subtasks
	exitAborted            = 3 // paused (SIGINT) or aborted (budget/deadline)
	exitSandboxUnavailable = 4 // no sandbox image or docker unreachable
	exitConfigError        = 5 // bad flags or config, or the run cannot start
)

// validateLoopFlags rejects flag combinations the loop mode cannot serve:
// the loop runs to completion on its own, so it cannot share the process
// with a one-shot query or an interactive session, and a fresh goal cannot
// start while a resume of the paused run was requested.
func validateLoopFlags(loopGoal string, loopResume, querySet, interactive bool) error {
	if loopGoal == "" && !loopResume {
		return nil
	}
	if querySet {
		return fmt.Errorf("--loop/--loop-resume несовместимы с --query")
	}
	if interactive {
		return fmt.Errorf("--loop/--loop-resume несовместимы с --interactive")
	}
	if loopGoal != "" && loopResume {
		return fmt.Errorf("--loop и --loop-resume несовместимы: resume продолжает существующий запуск, ему не нужна цель")
	}
	return nil
}

// loopExitCode maps the loop's outcome to the process exit code. A run that
// never started (zero status: lock conflict, load/save error) is a usage or
// config error, not a loop outcome.
func loopExitCode(run domain.LoopRun, err error) int {
	if err != nil && run.Status == "" {
		return exitConfigError
	}
	switch run.Status {
	case domain.LoopDone:
		return exitDone
	case domain.LoopFailed:
		return exitFailed
	case domain.LoopPaused, domain.LoopAborted:
		return exitAborted
	default:
		return exitAborted
	}
}

// cliLoopEvents prints loop progress to stderr; the final report goes to
// stdout in runLoop.
type cliLoopEvents struct{ logf func(string, ...any) }

func (e cliLoopEvents) Stage(taskID string, to domain.SubtaskStatus, msg string) {
	e.logf("[loop] %s → %s: %s", taskID, to, msg)
}

func (e cliLoopEvents) Findings(taskID string, fs []domain.Finding) {
	e.logf("[security] %s: %d находок, %d критичных/высоких", taskID, len(fs), len(domain.Blocking(fs)))
}

func (e cliLoopEvents) Budget(u domain.BudgetUsage) {
	e.logf("[loop] бюджет: вызовов %d · токенов %d · $%.4f", u.LLMCalls, u.Tokens, u.CostUSD)
}

// runLoop executes one loop run in CLI mode and returns the process exit
// code. Order matters: the sandbox is checked fail-closed BEFORE any LLM
// client exists, so a broken docker can never leak a call upstream. SIGINT
// cancels the context: the loop pauses (the run stays resumable with
// --loop-resume) and the exit code reflects that.
func runLoop(configPath string, f *cliFlags) (int, error) {
	if err := validateLoopFlags(*f.loopGoal, *f.loopResume, *f.query != "", *f.interactive); err != nil {
		return exitConfigError, err
	}

	cfg, err := loadLoopConfig(configPath, f)
	if err != nil {
		return exitConfigError, err
	}

	// Preflight the sandbox before the toolbelt (and its LLM client) exists:
	// no image or no docker → refuse before any network call could happen.
	runner, err := app.NewSandboxRunner(cfg.Sandbox)
	if err != nil {
		return exitSandboxUnavailable, err
	}
	preflightCtx, preflightCancel := context.WithTimeout(context.Background(), cfg.Sandbox.KillTimeout*3)
	err = runner.Preflight(preflightCtx)
	preflightCancel()
	if err != nil {
		return exitSandboxUnavailable, err
	}

	tb, err := app.Build(cfg)
	if err != nil {
		return exitConfigError, err
	}
	defer tb.Close()

	ev := cliLoopEvents{logf: func(format string, a ...any) { fmt.Fprintf(os.Stderr, format+"\n", a...) }}
	lp, err := app.BuildLoop(cfg, tb, ev, ev.logf)
	if err != nil {
		return exitConfigError, err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	var run domain.LoopRun
	if *f.loopResume {
		fmt.Fprintf(os.Stderr, "[loop] продолжаю остановленный запуск\n")
		run, err = lp.Resume(ctx)
	} else {
		goal, gerr := goalFromArg(*f.loopGoal)
		if gerr != nil {
			return exitConfigError, gerr
		}
		fmt.Fprintf(os.Stderr, "[loop] новый запуск: %s\n", firstLine(goal.Text))
		run, err = lp.Start(ctx, goal)
	}
	if err != nil && run.Status == "" {
		return exitConfigError, err
	}

	// The report is the answer: stdout for pipes and files, and a hint on
	// stderr about how to continue a paused run.
	if run.Report != "" {
		fmt.Println(run.Report)
	}
	code := loopExitCode(run, err)
	switch run.Status {
	case domain.LoopPaused:
		fmt.Fprintln(os.Stderr, "[loop] запуск остановлен — продолжение: --loop-resume")
	case domain.LoopAborted:
		fmt.Fprintln(os.Stderr, "[loop] запуск прерван (бюджет или дедлайн)")
	case domain.LoopFailed:
		if err != nil && !errors.Is(err, context.Canceled) {
			fmt.Fprintf(os.Stderr, "[loop] ошибка: %v\n", err)
		}
	}
	return code, nil
}

// loadLoopConfig loads and validates the config exactly like the
// orchestrator path, so --loop behaves like the rest of the app.
func loadLoopConfig(configPath string, f *cliFlags) (config.Config, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return config.Config{}, err
	}
	config.ResolveEnv(&cfg)
	buildOverrides(f).Apply(&cfg)
	if err := config.ResolvePaths(&cfg, configPath); err != nil {
		return config.Config{}, err
	}
	if err := config.Validate(cfg); err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

// firstLine shortens a goal for the one-line stderr announcement.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + "…"
	}
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}
