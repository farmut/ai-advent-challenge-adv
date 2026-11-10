package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"ai-adv-agent/internal/policy"
)

// Load reads the YAML config at path, layered on top of Default(). When path is
// empty the defaults are returned unchanged. Because unmarshalling merges into
// the pre-filled defaults, any field the file omits keeps its default — so an
// omitted `enabled:` leaves a memory layer on.
func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}
	if err := rejectSecrets(data); err != nil {
		return Config{}, fmt.Errorf("config %q: %w", path, err)
	}
	if err := checkSubAgentFormat(data); err != nil {
		return Config{}, fmt.Errorf("config %q: %w", path, err)
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	for i := range cfg.Orchestrator.SubAgents {
		sa := &cfg.Orchestrator.SubAgents[i]
		sa.Name = sa.Profile
	}
	return cfg, nil
}

// checkSubAgentFormat rejects the pre-profile roster format. Roles used to
// carry their prompt and tool grants inline; now a role is a reference to a
// profile, and inline prompt/tools would be a second, silently diverging
// source of truth.
func checkSubAgentFormat(data []byte) error {
	var raw struct {
		Orchestrator struct {
			SubAgents []map[string]interface{} `yaml:"subagents"`
		} `yaml:"orchestrator"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for i, sa := range raw.Orchestrator.SubAgents {
		profile, _ := sa["profile"].(string)
		name, _ := sa["name"].(string)
		_, hasPrompt := sa["prompt"]
		_, hasTools := sa["tools"]
		label := name
		if label == "" {
			label = profile
		}
		if strings.TrimSpace(profile) == "" {
			if hasPrompt || hasTools || name != "" {
				return fmt.Errorf("orchestrator.subagents[%d] (%s): устаревший формат роли — поля name/prompt/tools заменены ссылкой на профиль. "+
					"Перенесите промпт в profiles/<имя>.md, права — в profiles/<имя>.settings.json и укажите роль так: "+
					"`- { profile: <имя>, mcp: [local] }` (пример — agent.config.yaml)", i, label)
			}
			return fmt.Errorf("orchestrator.subagents[%d]: profile обязателен", i)
		}
		if hasPrompt || hasTools {
			return fmt.Errorf("orchestrator.subagents[%d] (%s): prompt/tools задаются только в профиле profiles/%s.*, уберите их из YAML", i, profile, profile)
		}
		if name != "" && name != profile {
			return fmt.Errorf("orchestrator.subagents[%d]: name %q не совпадает с profile %q — имя роли берётся из профиля, уберите name", i, name, profile)
		}
	}
	return nil
}

// rejectSecrets refuses a config file that carries the LLM API key. LLMConfig
// does not even decode the field (yaml:"-"), so without this check a key put
// in the file would be silently ignored — and left lying in the file.
func rejectSecrets(data []byte) error {
	var raw struct {
		LLM struct {
			APIKey string `yaml:"api_key"`
		} `yaml:"llm"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if strings.TrimSpace(raw.LLM.APIKey) != "" {
		return fmt.Errorf("llm.api_key в yaml запрещён — используйте env LLM_API_KEY")
	}
	return nil
}

// ResolveEnv fills LLM fields from the LLM_* environment variables. The API
// key always comes from LLM_API_KEY; provider, base URL and CA cert fall back
// to env when the config leaves them empty. The model is NOT taken from env:
// in config mode it must be set in the file (or with --model). It does not
// error on missing values — Validate reports those.
func ResolveEnv(cfg *Config) {
	if cfg.LLM.Provider == "" {
		cfg.LLM.Provider = os.Getenv("LLM_PROVIDER")
	}
	cfg.LLM.APIKey = os.Getenv("LLM_API_KEY")
	if cfg.LLM.BaseURL == "" {
		cfg.LLM.BaseURL = strings.TrimRight(os.Getenv("LLM_BASE_URL"), "/")
	}
	if cfg.LLM.CACert == "" {
		cfg.LLM.CACert = os.Getenv("LLM_CA_CERT")
	}
}

// Path substitution tokens usable inside an MCP server's args. They are replaced
// by ResolvePaths with the absolute roots, so a server started as a subprocess
// receives a path that does not depend on the agent's working directory.
const (
	TokenReadRoot  = "${READ_ROOT}"
	TokenWriteRoot = "${WRITE_ROOT}"
)

// ResolvePaths turns every configured filesystem root into an absolute path and
// substitutes the ${READ_ROOT} / ${WRITE_ROOT} tokens in the MCP servers' args.
//
// Roots are resolved against the directory holding the config file, not against
// the process working directory: both file servers take their directory as a
// command-line argument and inherit the agent's cwd, so a relative ".." would
// mean something different depending on where the agent was launched from.
//
// It never checks that a directory exists — that would make config tests require
// a filesystem. EvalSymlinks is best-effort for the same reason: when the root is
// not there yet (workspace/ is created by the Makefile) the absolute path is kept
// as is, and the guard refuses the call at request time instead.
func ResolvePaths(cfg *Config, configPath string) error {
	base, err := configBase(configPath)
	if err != nil {
		return err
	}

	readRoot, err := resolveRoot(base, cfg.MCP.FSGuard.ReadRoot, base)
	if err != nil {
		return fmt.Errorf("mcp.fs_guard.read_root: %w", err)
	}
	cfg.MCP.FSGuard.ReadRoot = readRoot

	// The first non-empty role write root becomes ${WRITE_ROOT}. Roles share one
	// workspace by design; a server process gets a single directory argument, so
	// there is nothing per-role to substitute.
	writeRoot := ""
	resolveTools := func(tp *ToolPolicy, what string) error {
		r, err := resolveRoot(base, tp.ReadRoot, "")
		if err != nil {
			return fmt.Errorf("%s.read_root: %w", what, err)
		}
		tp.ReadRoot = r
		w, err := resolveRoot(base, tp.WriteRoot, "")
		if err != nil {
			return fmt.Errorf("%s.write_root: %w", what, err)
		}
		tp.WriteRoot = w
		if writeRoot == "" {
			writeRoot = w
		}
		return nil
	}

	pdir, err := resolveRoot(base, cfg.Profiles.Dir, "")
	if err != nil {
		return fmt.Errorf("profiles.dir: %w", err)
	}
	cfg.Profiles.Dir = pdir
	pwr, err := resolveRoot(base, cfg.Profiles.WriteRoot, "")
	if err != nil {
		return fmt.Errorf("profiles.write_root: %w", err)
	}
	cfg.Profiles.WriteRoot = pwr
	writeRoot = pwr

	// The sandbox mount boundary defaults to the chat-mode write root: that is
	// where run_command executes.
	swr, err := resolveRoot(base, cfg.Sandbox.WorkspaceRoot, pwr)
	if err != nil {
		return fmt.Errorf("sandbox.workspace_root: %w", err)
	}
	cfg.Sandbox.WorkspaceRoot = swr
	for stack, dir := range cfg.Sandbox.Templates {
		td, err := resolveRoot(base, dir, "")
		if err != nil {
			return fmt.Errorf("sandbox.templates[%s]: %w", stack, err)
		}
		cfg.Sandbox.Templates[stack] = td
	}

	for i := range cfg.Orchestrator.SubAgents {
		sa := &cfg.Orchestrator.SubAgents[i]
		if err := resolveTools(&sa.Tools, fmt.Sprintf("orchestrator.subagents[%d].tools", i)); err != nil {
			return err
		}
	}
	if err := resolveTools(&cfg.Consultant.Tools, "consultant.tools"); err != nil {
		return err
	}

	for i := range cfg.MCP.Servers {
		args := cfg.MCP.Servers[i].Args
		for j, a := range args {
			a = strings.ReplaceAll(a, TokenReadRoot, readRoot)
			a = strings.ReplaceAll(a, TokenWriteRoot, writeRoot)
			args[j] = a
		}
	}
	return nil
}

// configBase returns the directory holding the config file, or the working
// directory when no config path was given.
func configBase(configPath string) (string, error) {
	if strings.TrimSpace(configPath) == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("working directory: %w", err)
		}
		return wd, nil
	}
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return "", fmt.Errorf("resolve config path %q: %w", configPath, err)
	}
	dir := filepath.Dir(abs)
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return real, nil
	}
	return dir, nil
}

// resolveRoot makes p absolute relative to base and resolves symlinks. An empty
// p yields fallback (itself possibly empty, which means "not granted").
func resolveRoot(base, p, fallback string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return fallback, nil
	}
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(base, abs)
	}
	abs = filepath.Clean(abs)
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real, nil
	}
	return abs, nil
}

// Supported LLM providers (kept here to avoid importing the llm adapter, which
// would create an import cycle for callers of the config package).
const (
	ProviderOpenAI     = "openai"
	ProviderOpenRouter = "openrouter"
	ProviderGigaChat   = "gigachat"
)

// Validate checks that the resolved configuration is internally consistent and
// has the credentials needed for the enabled features. Call after ResolveEnv and
// any flag overrides.
func Validate(cfg Config) error {
	if cfg.LLM.Provider == "" {
		return fmt.Errorf("llm.provider is required (set it in the config or LLM_PROVIDER)")
	}
	switch cfg.LLM.Provider {
	case ProviderOpenAI, ProviderOpenRouter, ProviderGigaChat:
	default:
		return fmt.Errorf("unsupported llm.provider %q (supported: openai, openrouter, gigachat)", cfg.LLM.Provider)
	}
	if cfg.LLM.APIKey == "" {
		return fmt.Errorf("API-ключ LLM не задан: установите env LLM_API_KEY")
	}
	if strings.TrimSpace(cfg.LLM.Model) == "" {
		return fmt.Errorf("llm.model обязателен: укажите модель (алиас прокси) в конфиге или флагом --model")
	}
	if cfg.LLM.BaseURL != "" {
		if err := validateBaseURL(cfg.LLM.BaseURL); err != nil {
			return fmt.Errorf("llm.base_url: %w", err)
		}
	}
	if err := validateLLMLimits(cfg.LLM); err != nil {
		return err
	}
	if s := cfg.Memory.STM.Strategy; s != "" &&
		s != "sliding-window" && s != "sticky-facts" && s != "branching" {
		return fmt.Errorf("invalid memory.stm.strategy %q (want: sliding-window, sticky-facts, branching)", s)
	}
	if cfg.RAG.Rerank.URL != "" {
		if err := validateBaseURL(cfg.RAG.Rerank.URL); err != nil {
			return fmt.Errorf("rag.rerank.url: %w", err)
		}
	}
	if cfg.Consultant.RAG.Rerank.URL != "" {
		if err := validateBaseURL(cfg.Consultant.RAG.Rerank.URL); err != nil {
			return fmt.Errorf("consultant.rag.rerank.url: %w", err)
		}
	}
	if cfg.RAG.Rerank.Enabled {
		switch strings.ToLower(cfg.RAG.Rerank.Mode) {
		case "", "api", "chat", "auto":
		default:
			return fmt.Errorf("invalid rag.rerank.mode %q (want: api, chat, auto)", cfg.RAG.Rerank.Mode)
		}
	}
	if cfg.Orchestrator.Enabled {
		if cfg.Orchestrator.MaxRounds <= 0 {
			return fmt.Errorf("orchestrator.max_rounds должен быть > 0, получено %d", cfg.Orchestrator.MaxRounds)
		}
		if cfg.Orchestrator.JSONRetries < 0 {
			return fmt.Errorf("orchestrator.json_retries должен быть >= 0, получено %d", cfg.Orchestrator.JSONRetries)
		}
		seen := map[string]bool{}
		for i, sa := range cfg.Orchestrator.SubAgents {
			if strings.TrimSpace(sa.Profile) == "" {
				return fmt.Errorf("orchestrator.subagents[%d]: profile обязателен (роль — ссылка на profiles/<имя>.md)", i)
			}
			if sa.Name == "" {
				sa.Name = sa.Profile
			}
			if seen[sa.Name] {
				return fmt.Errorf("orchestrator.subagents: duplicate name %q", sa.Name)
			}
			seen[sa.Name] = true
			if err := validateToolPolicy(sa.Tools, fmt.Sprintf("orchestrator.subagents[%d] (%s)", i, sa.Name)); err != nil {
				return err
			}
		}
	}
	if err := validateToolPolicy(cfg.Consultant.Tools, "consultant"); err != nil {
		return err
	}
	if err := validateSandbox(cfg.Sandbox); err != nil {
		return err
	}
	if err := validatePipeline(cfg.Pipeline, cfg.Sandbox); err != nil {
		return err
	}
	if err := validateLoop(cfg.Loop); err != nil {
		return err
	}
	return nil
}

// validateToolPolicy rejects a role whose tool grants contradict themselves: a
// tool listed in both allow and deny, or a write tool granted with no write root
// (which would be a permission the guard could never honour).
func validateToolPolicy(tp ToolPolicy, what string) error {
	if len(tp.WriteGlobs) > 0 && tp.WriteRoot == "" {
		return fmt.Errorf("%s.tools: write_globs заданы без write_root", what)
	}
	for _, g := range tp.WriteGlobs {
		if err := policy.ValidateWriteGlob(g); err != nil {
			return fmt.Errorf("%s.tools: %w", what, err)
		}
	}
	for _, g := range tp.DenyGlobs {
		if err := policy.ValidateWriteGlob(g); err != nil {
			return fmt.Errorf("%s.tools.deny_globs: %w", what, err)
		}
	}
	if _, err := policy.ParseExec(tp.ExecAllow, tp.ExecDeny); err != nil {
		return fmt.Errorf("%s.tools: %w", what, err)
	}
	if len(tp.ExecAllow) > 0 && tp.WriteRoot == "" {
		return fmt.Errorf("%s.tools: exec_allow задан без write_root (команды выполняются в каталоге записи)", what)
	}
	denied := make(map[string]bool, len(tp.Deny))
	for _, d := range tp.Deny {
		denied[d] = true
	}
	specs := policy.DefaultSpecs()
	for _, a := range tp.Allow {
		if denied[a] {
			return fmt.Errorf("%s.tools: tool %q is in both allow and deny", what, a)
		}
		if tp.WriteRoot == "" && specs[a].Kind == policy.KindWrite {
			return fmt.Errorf("%s.tools: write tool %q is allowed but tools.write_root is empty (writes are forbidden)", what, a)
		}
	}
	return nil
}

// ValidateToolPolicy is validateToolPolicy for callers that derive a policy
// after Validate ran (app.ApplyProfiles builds role policies from profiles).
func ValidateToolPolicy(tp ToolPolicy, what string) error { return validateToolPolicy(tp, what) }
