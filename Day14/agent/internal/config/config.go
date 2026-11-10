// Package config defines the declarative configuration for the LLM agent and
// its orchestrator. A single YAML file turns capabilities on and off: every
// memory layer is enabled by default and can be disabled per-layer, while RAG
// and MCP are opt-in. The orchestrator section declares the roster of sub-agent
// roles the orchestrator LLM may spawn and route between.
//
// Loading merges three sources, lowest priority first:
//  1. Default() — memory on, RAG/MCP off, orchestrator on.
//  2. the YAML file — only the fields it mentions override the defaults, so an
//     omitted `enabled:` keeps a layer on.
//  3. explicit CLI flags (via Overrides) — only flags the user actually set.
package config

import (
	"time"

	"ai-adv-agent/internal/domain"
)

// Config is the top-level agent configuration document.
type Config struct {
	LLM          LLMConfig          `yaml:"llm"`
	Memory       MemoryConfig       `yaml:"memory"`
	RAG          RAGConfig          `yaml:"rag"`
	MCP          MCPConfig          `yaml:"mcp"`
	Invariants   InvariantsConfig   `yaml:"invariants"`
	Profiles     ProfilesConfig     `yaml:"profiles"`
	Orchestrator OrchestratorConfig `yaml:"orchestrator"`
	Consultant   ConsultantConfig   `yaml:"consultant"`
	Sandbox      SandboxConfig      `yaml:"sandbox"`
	Pipeline     PipelineConfig     `yaml:"pipeline"`
	Loop         LoopConfig         `yaml:"loop"`
}

// LLMConfig holds the answer-generating model's connection, generation
// defaults and every limit the client enforces.
//
// The API key is deliberately not a YAML field: it is read from LLM_API_KEY
// only, and a config file that carries llm.api_key is rejected by Load so a
// secret never ends up in a file that gets committed or shared. The model is
// required in the config; there is no LLM_MODEL fallback in config mode.
type LLMConfig struct {
	Provider      string   `yaml:"provider"`
	Model         string   `yaml:"model"`
	BaseURL       string   `yaml:"base_url"`
	APIKey        string   `yaml:"-"`              // env LLM_API_KEY only
	Temperature   *float64 `yaml:"temperature"`    // nil = provider default
	MaxTokens     int      `yaml:"max_tokens"`     // 0 = no limit
	ContextWindow int      `yaml:"context_window"` // 0 = no client-side cap
	CACert        string   `yaml:"ca_cert"`        // PEM cert for a self-signed HTTPS endpoint

	// Timeout bounds one HTTP call to the model.
	Timeout time.Duration `yaml:"timeout"`
	// RequestMaxBytes caps the JSON request body; older history is trimmed to
	// fit (the guard proxy refuses bodies over 1 MiB).
	RequestMaxBytes int `yaml:"request_max_bytes"`
	// ResponseMaxBytes caps how much of a response body is read.
	ResponseMaxBytes int64           `yaml:"response_max_bytes"`
	RateLimit        RateLimitConfig `yaml:"rate_limit"`
	Retry            RetryConfig     `yaml:"retry"`
	Guard            LLMGuardConfig  `yaml:"guard"`
}

// RateLimitConfig is the client-side token bucket in front of the model.
// Disabling it is allowed: the guard proxy still enforces its own limit, and
// 429 answers are retried per RetryConfig.
type RateLimitConfig struct {
	Enabled   bool `yaml:"enabled"`
	PerMinute int  `yaml:"per_minute"`
	Burst     int  `yaml:"burst"`
}

// RetryConfig drives retries of 429 / 5xx / transport errors.
type RetryConfig struct {
	// MaxAttempts is the number of retries after the first call (0 = none).
	MaxAttempts int           `yaml:"max_attempts"`
	BaseDelay   time.Duration `yaml:"base_delay"`
	MaxDelay    time.Duration `yaml:"max_delay"`
	// Jitter in [0,1]: each back-off step is shortened by up to this fraction.
	Jitter float64 `yaml:"jitter"`
}

// LLMGuardConfig describes the guard proxy's behaviour the client must know.
type LLMGuardConfig struct {
	// RefusalText is the proxy's replacement answer for a blocked output.
	RefusalText string `yaml:"refusal_text"`
}

// DefaultRefusalText matches guard.output.refusal_text in Day13/configs/proxy.yaml.
const DefaultRefusalText = "Ответ модели скрыт политикой безопасности прокси."

// MemoryConfig toggles the memory layers. Dir is the base path from which each
// layer's file path is derived (equivalent to the historical --history value).
type MemoryConfig struct {
	Dir        string     `yaml:"dir"`
	STM        STMConfig  `yaml:"stm"`
	WM         Toggle     `yaml:"wm"`
	LTM        Toggle     `yaml:"ltm"`
	Facts      Toggle     `yaml:"facts"`
	Profile    ProfileCfg `yaml:"profile"`
	TaskMemory Toggle     `yaml:"task_memory"`
	// AutoUpdate refreshes WM and LTM with an extra LLM call after each turn.
	AutoUpdate bool `yaml:"auto_update"`
}

// Toggle is a simple on/off switch for a capability.
type Toggle struct {
	Enabled bool `yaml:"enabled"`
}

// STMConfig configures Layer 1 short-term memory (dialogue history).
type STMConfig struct {
	Enabled    bool   `yaml:"enabled"`
	History    string `yaml:"history"`     // explicit path; empty = derived from Memory.Dir
	Limit      int    `yaml:"limit"`       // max messages kept (0 = unlimited)
	Summary    bool   `yaml:"summary"`     // summarize older turns past Limit
	Strategy   string `yaml:"strategy"`    // "" | sliding-window | sticky-facts | branching
	WindowSize int    `yaml:"window_size"` // recent messages in prompt for window strategies
}

// ProfileCfg configures the explicit user profile layer.
type ProfileCfg struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"` // empty = derived from Memory.Dir
}

// RAGConfig configures the retrieval-augmented pipeline (disabled by default).
type RAGConfig struct {
	Enabled    bool         `yaml:"enabled"`
	DB         string       `yaml:"db"`
	EmbedURL   string       `yaml:"embed_url"`
	EmbedModel string       `yaml:"embed_model"`
	EmbedKey   string       `yaml:"embed_key"`
	TopK       int          `yaml:"top_k"`       // retrieval pool size before filtering
	Threshold  float64      `yaml:"threshold"`   // drop chunks scoring below this (0 disables)
	TopKFinal  int          `yaml:"top_k_final"` // chunks kept after rerank/filter (0 = all)
	Rerank     RerankConfig `yaml:"rerank"`
}

// RerankConfig configures the optional rerank stage. Empty Model/Provider/URL/Key
// fall back to the main LLM at wiring time.
type RerankConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Model    string `yaml:"model"`
	Mode     string `yaml:"mode"` // api | chat | auto
	Provider string `yaml:"provider"`
	URL      string `yaml:"url"`
	Key      string `yaml:"key"`
}

// MCPConfig configures Model Context Protocol tool servers (disabled by default).
// Servers may be listed inline or loaded from an external YAML file via File.
type MCPConfig struct {
	Enabled bool                     `yaml:"enabled"`
	File    string                   `yaml:"file"` // optional path to an external MCP servers YAML
	Servers []domain.MCPServerConfig `yaml:"servers"`
	FSGuard FSGuardConfig            `yaml:"fs_guard"`
}

// FSGuardConfig configures the client-side file-access guard applied to the MCP
// servers named in Servers. It is enabled by default: forgetting to configure it
// must mean "forbidden", never "allowed".
type FSGuardConfig struct {
	Enabled bool `yaml:"enabled"`
	// ReadRoot is the root every relative path is addressed against. Empty
	// resolves to the config file's directory (see ResolvePaths).
	ReadRoot string `yaml:"read_root"`
	// Deny holds glob patterns; empty means policy.DefaultDenyGlobs().
	Deny []string `yaml:"deny"`
	// Servers lists the MCP server names whose tools the guard applies to.
	// A tool from one of these servers with no entry in the policy table is
	// refused (fail-closed). Empty means the guard governs no server.
	Servers []string `yaml:"servers"`
}

// ToolPolicy gates a role's tools by name and declares its write root. The zero
// value is read-only: no allow list restriction, no denials, and — because
// WriteRoot is empty — no write access at all (fail-closed).
type ToolPolicy struct {
	// Allow lists tool names; empty means every tool of the granted servers.
	Allow []string `yaml:"allow"`
	// Deny always beats Allow.
	Deny []string `yaml:"deny"`
	// WriteRoot is the only subtree the role may write into; empty forbids writes.
	WriteRoot string `yaml:"write_root"`
	// ReadRoot overrides mcp.fs_guard.read_root for this role; empty inherits it.
	ReadRoot string `yaml:"read_root"`
	// WriteGlobs narrow WriteRoot (relative to it). "!glob" is a negative
	// glob: deny beats allow. Empty means the whole WriteRoot. Requires
	// WriteRoot.
	WriteGlobs []string `yaml:"write_globs"`
	// DenyGlobs are extra read/write deny globs (relative to the read root),
	// added to mcp.fs_guard.deny.
	DenyGlobs []string `yaml:"deny_globs"`
	// ExecAllow / ExecDeny gate run_command, in Claude Bash syntax:
	// "go test:*" (argv prefix), "go build ./..." (exact argv); in deny
	// "*" forbids every command. Empty ExecAllow = no run_command.
	ExecAllow []string `yaml:"exec_allow"`
	ExecDeny  []string `yaml:"exec_deny"`
	// ExecEnv is the allowlisted environment for run_command. Profiles only.
	ExecEnv map[string]string `yaml:"-"`
	// AllowExhaustive makes Allow the complete list even when it is empty
	// (a profile that grants no tools gets none, not "all"). Profiles only.
	AllowExhaustive bool `yaml:"-"`
}

// ProfilesConfig points at the sub-agent profile directory (Day2 format:
// <name>.md + <name>.settings.json).
type ProfilesConfig struct {
	// Dir is resolved against the config file's directory.
	Dir string `yaml:"dir"`
	// WriteRoot is the chat-mode write root for profiles that may write
	// (their Edit/Write globs apply inside it). Empty: such profiles stay
	// read-only. In loop mode the task directory is used instead.
	WriteRoot string `yaml:"write_root"`
}

// InvariantsConfig loads absolute constraints the agent must never violate.
type InvariantsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Path    string `yaml:"path"` // empty = derived from Memory.Dir
}

// OrchestratorConfig declares the orchestrator behaviour and its sub-agent roster.
type OrchestratorConfig struct {
	Enabled   bool `yaml:"enabled"`
	MaxRounds int  `yaml:"max_rounds"` // safety cap on spawn/route cycles
	// JSONRetries is how many consecutive protocol violations (prose instead
	// of a JSON action, finish without an answer) are answered with a repair
	// prompt before Handle fails with ErrProtocol. 0 = fail on the first one.
	JSONRetries int              `yaml:"json_retries"`
	SubAgents   []SubAgentConfig `yaml:"subagents"`
}

// SubAgentConfig defines one sub-agent role the orchestrator may spawn.
//
// In YAML a role is a reference to a profile (`profile: coder`) plus the
// capabilities the profile cannot express (rag, mcp servers, memory). Name,
// Prompt and Tools are filled from the profile by app.ApplyProfiles; writing
// `prompt:` or `tools:` in YAML is a load error (the profile is the single
// source of truth). MCP lists the server names visible to the role ("*" = all,
// empty = none).
type SubAgentConfig struct {
	Profile string `yaml:"profile"`
	// Name equals Profile; set by Load.
	Name   string         `yaml:"name"`
	Prompt string         `yaml:"prompt"`
	RAG    bool           `yaml:"rag"`
	MCP    []string       `yaml:"mcp"`
	Tools  ToolPolicy     `yaml:"tools"`
	Memory SubAgentMemory `yaml:"memory"`
	// Description is the profile's one-line description (for /agents and the
	// orchestrator's roster); filled by app.ApplyProfiles.
	Description string `yaml:"-"`
}

// SubAgentMemory toggles a sub-agent's own ephemeral working memory. STM+WM are
// scoped to the sub-agent run; LTM/profile are shared read-only from the session.
type SubAgentMemory struct {
	WM bool `yaml:"wm"`
}

// ConsultantConfig configures the documentation-consultant mode, entered with
// /help in the interactive orchestrator and left with /end. The consultant
// answers questions about the project grounded on the documentation RAG index
// (an independent pipeline from the main `rag:` section — it points at docs.db)
// and may call MCP tools from its allow-list (e.g. the git server).
type ConsultantConfig struct {
	Enabled bool       `yaml:"enabled"`
	RAG     RAGConfig  `yaml:"rag"`    // docs index; its Enabled gates retrieval
	MCP     []string   `yaml:"mcp"`    // MCP server allow-list, e.g. ["git"]
	Tools   ToolPolicy `yaml:"tools"`  // per-tool gating; zero value = read-only
	Prompt  string     `yaml:"prompt"` // optional system-prompt override
}

// Default returns a Config with every memory layer enabled, RAG and MCP disabled,
// and the orchestrator enabled with an empty roster. YAML and flags layer on top.
func Default() Config {
	return Config{
		// Safe production defaults: 18 req/min stays under the guard proxy's
		// 20/min, 900 KB stays under its 1 MiB body cap. Fast values live only
		// in test configs (testdata/config/*.yaml).
		LLM: LLMConfig{
			Timeout:          300 * time.Second,
			RequestMaxBytes:  900_000,
			ResponseMaxBytes: 8 << 20,
			RateLimit:        RateLimitConfig{Enabled: true, PerMinute: 18, Burst: 2},
			Retry:            RetryConfig{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: 30 * time.Second, Jitter: 0.2},
			Guard:            LLMGuardConfig{RefusalText: DefaultRefusalText},
		},
		Memory: MemoryConfig{
			Dir:        "chat_history.json",
			STM:        STMConfig{Enabled: true, Limit: 10, WindowSize: 5},
			WM:         Toggle{Enabled: true},
			LTM:        Toggle{Enabled: true},
			Facts:      Toggle{Enabled: true},
			Profile:    ProfileCfg{Enabled: true},
			TaskMemory: Toggle{Enabled: true},
			AutoUpdate: false,
		},
		RAG: RAGConfig{
			Enabled:    false,
			DB:         "rag.db",
			EmbedURL:   "http://localhost:11434",
			EmbedModel: "nomic-embed-text",
			TopK:       10,
			TopKFinal:  5,
			Rerank:     RerankConfig{Mode: "auto"},
		},
		// The guard defaults to on: an unconfigured fs_guard must forbid, not permit.
		MCP:        MCPConfig{Enabled: false, FSGuard: FSGuardConfig{Enabled: true}},
		Invariants: InvariantsConfig{Enabled: false},
		Profiles:   ProfilesConfig{Dir: "profiles"},
		Orchestrator: OrchestratorConfig{
			Enabled:     true,
			MaxRounds:   8,
			JSONRetries: 2,
		},
		Sandbox:  DefaultSandbox(),
		Pipeline: DefaultPipeline(),
		Loop:     DefaultLoop(),
		Consultant: ConsultantConfig{
			Enabled: true,
			RAG: RAGConfig{
				Enabled:    true,
				DB:         "../rag/docs.db",
				EmbedURL:   "http://localhost:11434",
				EmbedModel: "nomic-embed-text",
				TopK:       12,
				Threshold:  0.35,
				TopKFinal:  6,
				Rerank:     RerankConfig{Mode: "auto"},
			},
			MCP: []string{"git"},
		},
	}
}
