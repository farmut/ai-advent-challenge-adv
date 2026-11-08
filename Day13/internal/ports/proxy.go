package ports

import (
	"context"
	"errors"
	"time"
)

// Day13 guard-proxy ports. They depend only on the standard library.

// ChatMessage is one OpenAI-style chat message.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatParams are the generation parameters forwarded upstream (whitelist).
type ChatParams struct {
	MaxTokens   *int
	Temperature *float64
	TopP        *float64
	Stop        []string
}

// UpstreamRequest is a non-streaming chat completion request.
type UpstreamRequest struct {
	Model    string
	Messages []ChatMessage
	Params   ChatParams
}

// Usage sources.
const (
	UsageSourceUpstream  = "upstream"
	UsageSourceEstimated = "estimated"
	UsageSourceNone      = "none"
)

// Usage is the token accounting of a request.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	Source           string
}

// UpstreamResult is the upstream answer. HasUsage is false when the upstream
// did not report token usage.
type UpstreamResult struct {
	Content      string
	FinishReason string
	Usage        Usage
	HasUsage     bool
}

// Upstream is an LLM API.
type Upstream interface {
	Chat(ctx context.Context, r UpstreamRequest) (UpstreamResult, error)
}

// Upstream errors. Adapters wrap them; the use case maps them to outcomes.
var (
	ErrUpstreamTimeout       = errors.New("upstream timeout")
	ErrUpstreamUnavailable   = errors.New("upstream unavailable")
	ErrUpstreamBadResponse   = errors.New("upstream bad response")
	ErrUpstreamNotConfigured = errors.New("upstream not configured")
	// ErrUpstreamModelNotFound is an upstream 404: the model is not
	// pulled/loaded in Ollama or LM Studio, or the model id is wrong.
	ErrUpstreamModelNotFound = errors.New("upstream model not found")
)

// Price is the model price in micro-USD per one million tokens. Ref names
// the source of the price (e.g. a pricing page snapshot).
type Price struct {
	InputMicroUSDPer1M  int64
	OutputMicroUSDPer1M int64
	Ref                 string
}

// Route binds a public model alias to an upstream model. Ready is false when
// the upstream lacks a required API key.
type Route struct {
	Alias         string
	UpstreamName  string
	UpstreamModel string
	// AuditModel is the upstream model as written to the audit; for local
	// model paths it is relative to the module root so that audits and
	// reports do not reveal the absolute path. "" means UpstreamModel.
	AuditModel string
	Upstream   Upstream
	Price      Price
	Ready      bool
}

// UpstreamRouter resolves model aliases (the allowlist of models).
type UpstreamRouter interface {
	Resolve(alias string) (Route, bool)
	Aliases() []string
}

// Fingerprinter returns a keyed, non-reversible short fingerprint of a
// value (HMAC hex[:12]).
type Fingerprinter interface {
	Fingerprint(kind, value string) string
}

// AuditLogger persists audit records.
type AuditLogger interface {
	Write(ctx context.Context, r AuditRecord) error
	Close() error
}

// RateDecision is the rate limiter verdict.
type RateDecision struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
	ResetAt    time.Time
}

// RateLimiter limits requests per key.
type RateLimiter interface {
	Allow(key string) RateDecision
}

// Clock returns the current time.
type Clock interface {
	Now() time.Time
}

// Authenticator checks proxy API keys. It returns a stable, non-secret key
// id (a keyed fingerprint) for a valid token. Implementations must compare
// in constant time and never retain or log the token.
type Authenticator interface {
	Authenticate(token string) (keyID string, ok bool)
}

// AuditVersion is the audit schema version. v2 adds key_id, reason and the
// unauthorized status; system prompts are stored as a hash by default.
const AuditVersion = 2

// Audit statuses.
const (
	AuditStatusOK            = "ok"
	AuditStatusInputBlocked  = "input_blocked"
	AuditStatusOutputBlocked = "output_blocked"
	AuditStatusRateLimited   = "rate_limited"
	AuditStatusInvalid       = "invalid"
	AuditStatusModelNotFound = "model_not_found"
	AuditStatusUpstreamError = "upstream_error"
	AuditStatusUnauthorized  = "unauthorized"
)

// Reasons of unauthorized requests (AuditRecord.Reason).
const (
	AuthReasonMissing = "missing"
	AuthReasonInvalid = "invalid"
	AuthReasonScheme  = "scheme"
	AuthReasonQuery   = "query"
	AuthReasonTooLong = "too_long"
	// AuthReasonFailLimit marks requests refused by the per-IP limit of
	// authentication failures (status rate_limited).
	AuthReasonFailLimit = "auth_fail_limit"
)

// AuditRecord is one line of the audit log (schema v2; v1 records lack
// key_id and reason). It never contains raw secrets: prompts and responses
// are masked, findings carry only fingerprints, and neither the proxy API
// key nor its HMAC is written (only key_id of an authenticated key).
type AuditRecord struct {
	V              int           `json:"v"`
	TS             time.Time     `json:"ts"`
	RequestID      string        `json:"request_id"`
	ClientIPHash   string        `json:"client_ip_hash"`
	KeyID          string        `json:"key_id"`
	Model          string        `json:"model"`
	Upstream       string        `json:"upstream"`
	UpstreamModel  string        `json:"upstream_model"`
	Status         string        `json:"status"`
	HTTPStatus     int           `json:"http_status"`
	ErrorCode      string        `json:"error_code"`
	Reason         string        `json:"reason,omitempty"`
	Input          AuditInput    `json:"input"`
	Output         AuditOutput   `json:"output"`
	PromptMasked   []ChatMessage `json:"prompt_masked"`
	ResponseMasked string        `json:"response_masked"`
	Usage          AuditUsage    `json:"usage"`
	CostMicroUSD   int64         `json:"cost_micro_usd"`
	PriceRef       string        `json:"price_ref"`
	LatencyMS      AuditLatency  `json:"latency_ms"`
}

// AuditInput is the input guard section.
type AuditInput struct {
	Mode     string         `json:"mode"`
	Action   string         `json:"action"`
	Findings []AuditFinding `json:"findings"`
}

// AuditOutput is the output guard section.
type AuditOutput struct {
	Action   string         `json:"action"`
	Findings []AuditFinding `json:"findings"`
}

// AuditFinding aggregates findings of one type/view. Placeholder is the
// mask that replaced the value in the masked text ("" when nothing was
// replaced, e.g. a blocked system leak).
type AuditFinding struct {
	Type         string   `json:"type"`
	View         string   `json:"view,omitempty"`
	Count        int      `json:"count"`
	Action       string   `json:"action"`
	Derived      bool     `json:"derived"`
	Fingerprints []string `json:"fingerprints"`
	Placeholder  string   `json:"placeholder,omitempty"`
}

// AuditUsage is the token accounting section.
type AuditUsage struct {
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
	TotalTokens      int    `json:"total_tokens"`
	Source           string `json:"source"`
}

// AuditLatency holds latencies in milliseconds.
type AuditLatency struct {
	Total    int64 `json:"total"`
	Upstream int64 `json:"upstream"`
}
