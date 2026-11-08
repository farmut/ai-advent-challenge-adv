package main

import (
	"agile-consultant/internal/adapters/mlx"
	"agile-consultant/internal/adapters/nonce"
	"agile-consultant/internal/adapters/promptfile"
	"agile-consultant/internal/adapters/textnorm"
	"agile-consultant/internal/benchmark"
	"agile-consultant/internal/config"
	"agile-consultant/internal/ui"
	"agile-consultant/internal/usecase"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
)

type app struct {
	cfg        *config.Config
	server     *mlx.Server
	client     *mlx.Client
	consultant *usecase.Consultant
	prompt     usecase.PromptTemplate
	guard      usecase.GuardOptions
	logger     *slog.Logger
	configPath string
}

func main() {
	if len(os.Args) < 2 {
		runCommand([]string{})
		return
	}

	switch os.Args[1] {
	case "bench":
		benchCommand(os.Args[2:])
	case "server":
		serverCommand(os.Args[2:])
	case "run", "help", "-h", "--help":
		runCommand(os.Args[2:])
	default:
		if os.Args[1][0] == '-' {
			runCommand(os.Args[1:])
		} else {
			fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
			os.Exit(1)
		}
	}
}

func serverCommand(args []string) {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	configPath := fs.String("config", "configs/app.yaml", "path to config")
	modelFlag := fs.String("model", "", "override default model (tuned|baseline)")
	_ = fs.Parse(args)

	_, _, app, err := setupApp(*configPath, *modelFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "setup: %v\n", err)
		os.Exit(1)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	app.handleSignals(sigCh)
}

func runCommand(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	configPath := fs.String("config", "configs/app.yaml", "path to config")
	modelFlag := fs.String("model", "", "override default model (tuned|baseline)")
	_ = fs.Parse(args)

	_, logger, app, err := setupApp(*configPath, *modelFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "setup: %v\n", err)
		os.Exit(1)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	go app.handleSignals(sigCh)

	model := ui.New(app.consultant, app)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		logger.Error("tui error", slog.String("error", err.Error()))
	}

	app.shutdown()
}

func benchCommand(args []string) {
	fs := flag.NewFlagSet("bench", flag.ExitOnError)
	configPath := fs.String("config", "configs/app.yaml", "path to config")
	modelFlag := fs.String("model", "tuned", "model to benchmark (tuned|baseline)")
	queriesPath := fs.String("queries", "testdata/agile_queries.jsonl", "path to queries file")
	_ = fs.Parse(args)

	_, _, app, err := setupApp(*configPath, *modelFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "setup: %v\n", err)
		os.Exit(1)
	}
	defer app.shutdown()

	runner := benchmark.NewBenchRunner(app.logger)
	results, err := runner.RunFile(context.Background(), *queriesPath, app.consultant, app.consultant.CurrentModel())
	if err != nil {
		fmt.Fprintf(os.Stderr, "benchmark: %v\n", err)
		os.Exit(1)
	}

	sizeGB, _ := benchmark.ModelSizeGB(app.cfg.DefaultModel().Path)
	metrics := benchmark.Summarize(results, app.consultant.CurrentModel(), sizeGB, 0, app.server.LoadTime())

	out := map[string]any{
		"metrics": metrics,
		"results": results,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

func setupApp(configPath, modelFlag string) (*config.Config, *slog.Logger, *app, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load config: %w", err)
	}
	cfg.SetPath(configPath)

	logger := setupLogger(cfg.Log.Path, cfg.Log.Level)

	if modelFlag != "" {
		target := modelFlag
		if m, ok := cfg.Models[modelFlag]; ok {
			target = m.Name
		}
		if err := cfg.SwitchDefault(target); err != nil {
			return nil, nil, nil, fmt.Errorf("invalid model flag: %w", err)
		}
	}

	tmpl, err := loadPrompt(cfg, logger)
	if err != nil {
		return nil, nil, nil, err
	}
	guardOpts, err := guardOptions(cfg, logger)
	if err != nil {
		return nil, nil, nil, err
	}

	app := &app{
		cfg:        cfg,
		prompt:     tmpl,
		guard:      guardOpts,
		logger:     logger,
		configPath: configPath,
	}

	if err := app.startServer(); err != nil {
		return nil, nil, nil, fmt.Errorf("start server: %w", err)
	}
	return cfg, logger, app, nil
}

func (a *app) startServer() error {
	model := a.cfg.DefaultModel()
	a.server = mlx.NewServer(a.cfg.Server.Bind, logDir(a.cfg.Log.Path), a.logger)
	if err := a.server.Start(model.Path); err != nil {
		return fmt.Errorf("start server with model %s: %w", model.Name, err)
	}
	a.client = mlx.NewClient("http://"+a.cfg.Server.Bind, model.Path, &a.cfg.Inference, a.logger)
	deps := usecase.GuardDeps{Normalizer: textnorm.New(), Nonce: nonce.New()}
	consultant, err := usecase.NewConsultant(a.client, a.prompt, deps, a.guard, a.logger)
	if err != nil {
		_ = a.server.Stop()
		return fmt.Errorf("create consultant: %w", err)
	}
	a.consultant = consultant
	a.consultant.SetCurrentModel(model.Name)
	a.logger.Info("service started", slog.String("model", model.Name), slog.String("bind", a.cfg.Server.Bind))
	return nil
}

func (a *app) reload() error {
	a.logger.Info("reloading configuration")
	newCfg, err := config.Load(a.configPath)
	if err != nil {
		return fmt.Errorf("reload config: %w", err)
	}
	newCfg.SetPath(a.configPath)

	tmpl, err := loadPrompt(newCfg, a.logger)
	if err != nil {
		return fmt.Errorf("reload prompt: %w", err)
	}
	guardOpts, err := guardOptions(newCfg, a.logger)
	if err != nil {
		return fmt.Errorf("reload guard: %w", err)
	}

	model := newCfg.DefaultModel()
	if model.Path == "" {
		return fmt.Errorf("default model %s has empty path", model.Name)
	}

	if err := a.server.Start(model.Path); err != nil {
		a.logger.Error("failed to start new server, falling back to baseline",
			slog.String("error", err.Error()))
		fallback := newCfg.Models["baseline"]
		_ = a.server.Start(fallback.Path)
		a.client.SetModelPath(fallback.Path)
		if err := a.applyPrompt(tmpl, guardOpts); err != nil {
			return err
		}
		a.consultant.SetCurrentModel(fallback.Name)
		a.cfg = newCfg
		return nil
	}

	a.client.SetModelPath(model.Path)
	if err := a.applyPrompt(tmpl, guardOpts); err != nil {
		return err
	}
	a.consultant.SetCurrentModel(model.Name)
	a.cfg = newCfg
	a.logger.Info("configuration reloaded", slog.String("model", model.Name))
	return nil
}

// applyPrompt installs a reloaded template and guard options.
func (a *app) applyPrompt(tmpl usecase.PromptTemplate, opts usecase.GuardOptions) error {
	if err := a.consultant.SetTemplate(tmpl); err != nil {
		return fmt.Errorf("reload prompt: %w", err)
	}
	a.prompt = tmpl
	a.consultant.SetGuardOptions(opts)
	a.guard = opts
	return nil
}

func (a *app) shutdown() {
	a.logger.Info("shutting down")
	if a.server != nil {
		_ = a.server.Stop()
	}
}

func (a *app) handleSignals(ch chan os.Signal) {
	for sig := range ch {
		a.logger.Info("signal received", slog.String("signal", sig.String()))
		switch sig {
		case syscall.SIGHUP:
			a.logger.Info("handling SIGHUP reload")
			if err := a.reload(); err != nil {
				a.logger.Error("reload failed", slog.String("error", err.Error()))
			} else {
				a.logger.Info("SIGHUP reload completed")
			}
		case os.Interrupt, syscall.SIGTERM:
			a.shutdown()
			os.Exit(0)
		}
	}
	a.logger.Info("signal handler loop exited")
}

func (a *app) Switch(ctx context.Context, modelName string) (string, error) {
	current := a.cfg.DefaultModel().Name
	target := modelName
	if target == "" {
		target = a.nextModelName(current)
	}
	if target == current || target == "" {
		return target, nil
	}
	if err := a.cfg.SwitchDefault(target); err != nil {
		return "", err
	}
	if err := a.cfg.Save(); err != nil {
		return "", err
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGHUP); err != nil {
		return "", err
	}
	return target, nil
}

func (a *app) nextModelName(current string) string {
	if len(a.cfg.Models) == 0 {
		return ""
	}
	names := make([]string, 0, len(a.cfg.Models))
	for _, m := range a.cfg.Models {
		names = append(names, m.Name)
	}
	sort.Strings(names)
	for i, name := range names {
		if name == current {
			return names[(i+1)%len(names)]
		}
	}
	return names[0]
}

func setupLogger(path, level string) *slog.Logger {
	lvl := slog.LevelInfo
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	}

	var handler slog.Handler
	if path != "" {
		_ = os.MkdirAll(filepath.Dir(path), 0755)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			handler = slog.NewJSONHandler(f, &slog.HandlerOptions{Level: lvl})
		}
	}
	if handler == nil {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	}
	return slog.New(handler)
}

// loadPrompt loads the prompt template (PROMPT_FILE env > prompt.file) and
// logs its name and SHA-256. There is no built-in fallback prompt.
func loadPrompt(cfg *config.Config, logger *slog.Logger) (usecase.PromptTemplate, error) {
	path := cfg.PromptFile()
	if path == "" {
		return usecase.PromptTemplate{}, errors.New("load prompt: prompt.file или PROMPT_FILE обязателен")
	}
	tmpl, err := promptfile.Load(path)
	if err != nil {
		return usecase.PromptTemplate{}, fmt.Errorf("load prompt: %w", err)
	}
	logger.Info("prompt template loaded",
		slog.String("name", tmpl.Name),
		slog.String("file", path),
		slog.String("sha256", tmpl.SHA256()),
	)
	return tmpl, nil
}

// guardOptions maps the configuration to usecase guard options
// (GUARD env > guard.enabled > on).
func guardOptions(cfg *config.Config, logger *slog.Logger) (usecase.GuardOptions, error) {
	enabled, err := cfg.GuardEnabled()
	if err != nil {
		return usecase.GuardOptions{}, fmt.Errorf("guard config: %w", err)
	}
	opts := usecase.DefaultGuardOptions()
	opts.Enabled = enabled
	logger.Info("guard configured",
		slog.Bool("enabled", opts.Enabled),
		slog.Int("max_question_runes", opts.MaxQuestionRunes),
		slog.Int("max_context_runes", opts.MaxContextRunes),
	)
	return opts, nil
}

// logDir returns the absolute directory of the application log file; the
// mlx_lm.server log is written next to it.
func logDir(logPath string) string {
	if logPath == "" {
		logPath = "logs/agile-consultant.log"
	}
	dir := filepath.Dir(logPath)
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}
