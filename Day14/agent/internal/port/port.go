package port

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"ai-adv-agent/internal/domain"
)

// LLMRequest bundles all parameters for a single chat completion call.
type LLMRequest struct {
	Model       string
	Messages    []domain.Message
	MaxTokens   int
	Stop        []string
	Temperature float64 // negative = use provider default
	Debug       bool
	Tools       []domain.MCPTool // optional; enables tool-calling when non-empty
}

// LLMResponse is the structured result returned by an LLM call.
type LLMResponse struct {
	Content      string
	Usage        domain.Usage
	FinishReason string
	Elapsed      time.Duration
	ToolCalls    []domain.ToolCall // non-nil when finish_reason == "tool_calls"
	// Guard carries the guard proxy's verdict (X-Guard-* headers). Zero when
	// the endpoint is not a guard proxy.
	Guard GuardMeta
	// UpstreamModel is the real model the guard proxy routed to
	// (X-Upstream-Model header). Empty when the endpoint is not a guard
	// proxy (or the request never reached an upstream).
	UpstreamModel string
}

// Guard verdicts as reported in X-Guard-Input / X-Guard-Output.
const (
	GuardPass     = "pass"
	GuardMasked   = "mask"
	GuardBlocked  = "blocked"
	GuardRedacted = "redacted"
)

// GuardMeta is what the guard proxy reported about one call.
//
// Output == GuardBlocked means the proxy replaced the model's answer with its
// refusal text (or the model stopped with finish_reason=content_filter). The
// client does not turn that into an error — the HTTP call succeeded — but the
// caller must treat the step as failed: the content is not the model's answer.
type GuardMeta struct {
	Input, Output                 string
	InputFindings, OutputFindings string
	UsageSource                   string
	CostUSD                       float64
	HasCost                       bool
}

// Blocked reports whether the answer was replaced by the guard.
func (g GuardMeta) Blocked() bool { return g.Output == GuardBlocked }

// Sentinel errors for errors.Is on LLM failures.
var (
	// ErrGuardBlocked: the guard proxy refused the request (input guard).
	ErrGuardBlocked = errors.New("llm: request blocked by guard")
	// ErrFatalLLM: an error retrying cannot fix (auth, bad request, unknown
	// model, misconfigured upstream).
	ErrFatalLLM = errors.New("llm: fatal error")
	// ErrBudgetExceeded: a configured call/token/cost/time budget is spent.
	ErrBudgetExceeded = errors.New("llm: budget exceeded")
)

// LLMError is a non-2xx answer from an OpenAI-compatible endpoint, decoded
// from {"error":{"message","type","code","param"}} plus the response headers.
type LLMError struct {
	Status                                int
	Code, Type, Param, Message, RequestID string
	// RetryAfter is the server's Retry-After hint; 0 when absent.
	RetryAfter time.Duration
}

// Error renders status, code and message, plus the request id for support.
func (e *LLMError) Error() string {
	msg := fmt.Sprintf("LLM API error %d", e.Status)
	if e.Code != "" {
		msg += " (" + e.Code + ")"
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if e.Param != "" {
		msg += " [param " + e.Param + "]"
	}
	if e.RequestID != "" {
		msg += " request_id=" + e.RequestID
	}
	return msg
}

// Codes that are never worth retrying even under a retryable status.
const (
	CodeGuardInputBlocked     = "guard_input_blocked"
	CodeUpstreamNotConfigured = "upstream_not_configured"
	CodeUpstreamModelNotFound = "upstream_model_not_found"
	// CodeRequestTooLargeClient is produced locally when the request cannot be
	// shrunk under llm.request_max_bytes.
	CodeRequestTooLargeClient = "request_too_large_client"
)

// Retryable reports whether repeating the same request may succeed: rate
// limiting (429) and transient upstream failures (500/502/503/504), except
// the upstream codes that mean "misconfigured", which no retry can fix.
func (e *LLMError) Retryable() bool {
	switch e.Code {
	case CodeUpstreamNotConfigured, CodeUpstreamModelNotFound, CodeGuardInputBlocked, CodeRequestTooLargeClient:
		return false
	}
	switch e.Status {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// Is maps the error onto the sentinels: a guard refusal is ErrGuardBlocked,
// and every non-retryable error is ErrFatalLLM.
func (e *LLMError) Is(target error) bool {
	switch target {
	case ErrGuardBlocked:
		return e.Code == CodeGuardInputBlocked
	case ErrFatalLLM:
		return !e.Retryable()
	}
	return false
}

// Clock abstracts time for everything that waits: rate limiters, retry
// back-off and budgets. Production code uses the wall clock; tests inject a
// fake whose Sleep advances virtual time instantly, so limits are exercised
// without real waiting.
type Clock interface {
	Now() time.Time
	// Sleep waits for d or until ctx is done, returning ctx.Err() then.
	Sleep(ctx context.Context, d time.Duration) error
}

// LLMClient is the outbound port for any OpenAI-compatible chat-completion provider.
type LLMClient interface {
	Chat(ctx context.Context, req LLMRequest) (LLMResponse, error)
}

// HistoryRepository manages the Layer 1 (STM) conversation log.
type HistoryRepository interface {
	Load() ([]domain.Message, error)
	Save(messages []domain.Message) error
}

// StatsRepository persists per-session token / cost statistics.
type StatsRepository interface {
	Load() (domain.SessionStats, error)
	Save(stats domain.SessionStats) error
}

// SummaryRepository stores the rolling text summary of older conversation turns.
type SummaryRepository interface {
	Load() (string, error)
	Save(content string) error
}

// FactsRepository manages the KV facts store used by the sticky-facts strategy.
type FactsRepository interface {
	Load() (domain.FactsStore, error)
	Save(facts domain.FactsStore) error
}

// WorkingMemoryRepository manages Layer 2 (task facts).
type WorkingMemoryRepository interface {
	Load() (domain.WorkingMemory, error)
	Save(wm domain.WorkingMemory) error
}

// LongTermMemoryRepository manages Layer 3 (profile / knowledge).
type LongTermMemoryRepository interface {
	Load() (domain.LongTermMemory, error)
	Save(ltm domain.LongTermMemory) error
}

// TaskMemoryRepository persists the dialogue task memory (goal, clarified points,
// fixed constraints/terms) across the turns of a RAG conversation.
type TaskMemoryRepository interface {
	Load() (domain.TaskMemory, error)
	Save(tm domain.TaskMemory) error
}

// BranchRepository manages branching state and per-branch conversation histories.
type BranchRepository interface {
	LoadState() (domain.BranchState, error)
	SaveState(bs domain.BranchState) error
	LoadHistory(branchName string) ([]domain.Message, error)
	SaveHistory(branchName string, messages []domain.Message) error
}

// UserProfileRepository manages the explicit user profile (name + preferences).
type UserProfileRepository interface {
	Load() (domain.UserProfile, error)
	Save(profile domain.UserProfile) error
}

// TaskRepository persists the active task state across the 4-phase state machine.
// Load returns (state, true, nil) when a task is in progress, or (zero, false, nil) when none.
type TaskRepository interface {
	Load() (domain.TaskState, bool, error)
	Save(ts domain.TaskState) error
	Clear() error
}

// InvariantsRepository loads the user-defined invariants document (Markdown).
// Invariants are absolute constraints the agent must never violate.
// The repository is read-only: the file is managed by the user directly.
// Load returns an empty string when no invariants file exists (no-op, not an error).
type InvariantsRepository interface {
	Load() (string, error)
}

// MCPRepository persists MCP server configurations to a YAML file.
type MCPRepository interface {
	Load() (domain.MCPConfig, error)
	Save(cfg domain.MCPConfig) error
}

// MCPToolLister connects to an MCP server and retrieves its tool list.
type MCPToolLister interface {
	ListTools(ctx context.Context, cfg domain.MCPServerConfig) ([]domain.MCPTool, error)
}

// MCPToolCaller connects to an MCP server and executes a named tool.
type MCPToolCaller interface {
	CallTool(ctx context.Context, cfg domain.MCPServerConfig, name string, args map[string]interface{}) (string, error)
}

// MCPPool is a set of persistent MCP server sessions that can list tools and call
// tools without spawning a new subprocess per request.  Close must be called when
// the pool is no longer needed.
type MCPPool interface {
	MCPToolLister
	MCPToolCaller
	Close()
}

// MCPPoolOpener creates a new MCPPool for the given server configurations.
// Errors from individual servers are returned alongside a valid (partial) pool.
type MCPPoolOpener interface {
	OpenPool(cfgs []domain.MCPServerConfig) (MCPPool, []error)
}

// ToolExecutor is the callback invoked by ChatUseCase.ExecuteWithTools for each tool call.
// name is the tool name; argsJSON is the JSON-encoded arguments string from the LLM.
type ToolExecutor func(ctx context.Context, name, argsJSON string) (string, error)

// Retriever performs semantic search over an indexed knowledge base and returns
// the chunks most relevant to a query. It is the retrieval step of the RAG
// pipeline (question → retrieve relevant chunks → combine with question → LLM).
type Retriever interface {
	Retrieve(ctx context.Context, query string, topK int) ([]domain.RetrievedChunk, error)
}

// Reranker is the optional second stage of the RAG pipeline: it re-scores the
// chunks returned by the Retriever for their relevance to the query and returns
// them sorted best-first with domain.RetrievedChunk.RerankScore populated. Unlike
// the embedding similarity used for retrieval, a reranker judges the query and
// each chunk jointly, so it can promote chunks that are topically on-point and
// demote ones that merely share vocabulary.
type Reranker interface {
	Rerank(ctx context.Context, query string, chunks []domain.RetrievedChunk) ([]domain.RetrievedChunk, error)
}

// AgentProfileRepository loads sub-agent role profiles (Day2 format:
// <name>.md + <name>.settings.json).
type AgentProfileRepository interface {
	List() ([]domain.AgentProfile, error)
	Get(name string) (domain.AgentProfile, error)
}

// CommandSpec is one command to run in the sandbox. Argv is executed directly,
// never through a shell.
type CommandSpec struct {
	Argv []string
	// Workdir is absolute and must lie inside the sandbox workspace root.
	Workdir   string
	Timeout   time.Duration
	MaxOutput int
	Env       map[string]string
	Stack     string
}

// CommandResult is the outcome of a sandboxed command. A non-zero ExitCode is
// not an error: the error return is reserved for "could not run at all".
type CommandResult struct {
	ExitCode       int
	Stdout, Stderr string
	Truncated      bool
	TimedOut       bool
	Duration       time.Duration
}

// CommandRunner executes commands in an isolated sandbox (Docker, PR5).
// Preflight reports whether the sandbox is usable; callers that require it
// must fail closed when it is not.
type CommandRunner interface {
	Run(ctx context.Context, s CommandSpec) (CommandResult, error)
	Preflight(ctx context.Context) error
}

// RoleRequest is one run of a profiled role (generator, reviewer, judge).
type RoleRequest struct {
	// Profile names the agent profile (profiles/<name>.md).
	Profile string
	// Task is the trusted instruction, built by code.
	Task string
	// DataBlocks are untrusted inputs; the runner wraps each in nonce
	// delimiters.
	DataBlocks []domain.DataBlock
	// Roots bound the role's file access. Write "" makes it read-only
	// whatever the profile says.
	Roots domain.WorkRoots
	// MaxToolRounds caps tool-calling rounds (0 = the runner's default).
	MaxToolRounds int
}

// RoleResult is what a role produced.
type RoleResult struct {
	Content string
	Usage   domain.Usage
	CostUSD float64
	// GuardBlocked: the guard proxy replaced the final answer; Content is the
	// refusal text, not the model's answer.
	GuardBlocked bool
	GuardReason  string
	ToolCalls    int
}

// RoleRunner runs a profiled role with its permissions narrowed to Roots.
// An input-guard refusal is returned as an error matching ErrGuardBlocked.
type RoleRunner interface {
	Run(ctx context.Context, r RoleRequest) (RoleResult, error)
}

// ErrScanOutput marks SAST output that cannot be trusted (invalid or
// truncated JSON, unexpected exit). The review then fails closed. Errors that
// do not match it mean the scanner could not run at all (sandbox down).
var ErrScanOutput = errors.New("sast: unusable scanner output")

// SecurityScanner runs the static analysers of a stack ("go", "vue") over a
// task directory. File paths in the findings are relative to workdir.
type SecurityScanner interface {
	Scan(ctx context.Context, workdir, stack string) ([]domain.Finding, error)
}

// Workspace manages the per-run task directories:
// <root>/<runID>/base (accepted state) and <root>/<runID>/<taskID>.
type Workspace interface {
	// Seed fills the run's base from the stacks' templates. It is a no-op
	// when base already exists (a resumed run keeps its accepted state).
	// Called after planning, before the first Prepare.
	Seed(runID string, stacks []string) error
	// Prepare replaces the task directory with a fresh copy of base and
	// returns its absolute path.
	Prepare(runID, taskID string) (dir string, err error)
	// TaskDir resolves the task directory's absolute path without touching
	// it (unlike Prepare, which resets it to a copy of base).
	TaskDir(runID, taskID string) (dir string, err error)
	// Snapshot maps every regular file under dir (slash-separated relative
	// path) to its SHA-256.
	Snapshot(dir string) (map[string]string, error)
	// Promote makes the task directory the new base.
	Promote(runID, taskID string) error
	// ReadNumbered renders a file with line numbers ("  12| code"), cut at
	// maxBytes of source text. lines is the file's full line count.
	ReadNumbered(dir, rel string, maxBytes int) (text string, lines int, err error)
}

// LoopRepository persists the execution-loop run state (the active run plus
// archived finished runs) and serialises concurrent loop processes through a
// lock file. LoadActive returns (run, true, nil) when a run is in progress,
// or (zero, false, nil) when none; a present but invalid active file is an
// error, never a silent reset.
type LoopRepository interface {
	LoadActive() (domain.LoopRun, bool, error)
	Save(run domain.LoopRun) error
	Archive(id string) error
	Lock() (unlock func(), err error)
}
