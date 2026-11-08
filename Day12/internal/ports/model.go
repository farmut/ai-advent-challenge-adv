// Package ports declares the interfaces between the use cases and the
// adapters (model server, text normalisation, randomness).
package ports

import (
	"context"
	"time"
)

// Chat roles used in Message.Role.
const (
	RoleSystem = "system"
	RoleUser   = "user"
)

// Block stages reported in Response.BlockStage.
const (
	BlockStageInput  = "input"
	BlockStageOutput = "output"
)

// Message is one chat message sent to the model. System and user content are
// always transported as separate messages so the model's chat template, not
// the user, controls the turn structure.
type Message struct {
	Role    string
	Content string
}

// GenerationMetrics are timing and throughput figures of one generation.
type GenerationMetrics struct {
	TTFT         time.Duration
	TotalTime    time.Duration
	Tokens       int
	TokensPerSec float64
	PeakMemoryGB float64
}

// GuardTrace describes what the input guard did to a request. It is filled
// only when the guard is enabled and never contains the system prompt.
type GuardTrace struct {
	// Findings are sanitizer findings, e.g. "question:html_comment=2".
	Findings []string
	// SanitizedQuestion and SanitizedContext are the texts actually placed
	// inside the boundary markers.
	SanitizedQuestion string
	SanitizedContext  string
	// Hidden is content cut from the input (HTML comments, hidden elements,
	// bidi-overridden text, decoded Unicode Tags).
	Hidden []string
}

// Response is the consultant answer delivered to the caller.
type Response struct {
	// Raw is the delivered answer: the model output, or RefusalAnswer when
	// the guard blocked the request at input or output.
	Raw        string
	Verdict    string
	Principle  string
	Reasoning  string
	Suggestion string
	Metrics    GenerationMetrics

	// Blocked reports that the guard replaced the answer with a refusal.
	Blocked bool
	// BlockStage is "", BlockStageInput or BlockStageOutput.
	BlockStage string
	// Reasons explain why the request was blocked.
	Reasons []string
	// RawModelAnswer is the unmodified model output ("" when the model was
	// not called). It is for diagnostics and must not be shown to end users.
	RawModelAnswer string
	Trace          GuardTrace
}

// ModelClient generates a completion for a chat conversation.
type ModelClient interface {
	Generate(ctx context.Context, messages []Message) (Response, error)
}

// ModelServer manages the lifecycle of a local model server.
type ModelServer interface {
	Start(modelPath string) error
	Stop() error
	HealthCheck(ctx context.Context) error
	LoadTime() time.Duration
}

// ModelSwitcher switches the active model.
type ModelSwitcher interface {
	Switch(ctx context.Context, modelName string) error
	CurrentModel() string
}
