package usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"unicode/utf8"

	"agile-consultant/internal/ports"
	"agile-consultant/internal/usecase/guard"
)

// Consultant answers Agile questions through a model using a prompt template
// and an optional input/output guard. It is safe for concurrent use.
type Consultant struct {
	client    ports.ModelClient
	logger    *slog.Logger
	sanitizer *guard.Sanitizer
	policy    guard.Policy
	nonceB    guard.NonceBoundary
	validator *guard.OutputValidator

	mu      sync.RWMutex
	current string
	tmpl    PromptTemplate
	opts    GuardOptions
}

// NewConsultant creates a Consultant. The template must be valid and both
// guard dependencies must be set (the guard can be enabled at runtime).
func NewConsultant(client ports.ModelClient, tmpl PromptTemplate, deps GuardDeps, opts GuardOptions, logger *slog.Logger) (*Consultant, error) {
	if client == nil {
		return nil, errors.New("new consultant: nil model client")
	}
	if err := tmpl.Validate(); err != nil {
		return nil, fmt.Errorf("new consultant: %w", err)
	}
	if deps.Normalizer == nil || deps.Nonce == nil {
		return nil, errors.New("new consultant: guard dependencies (normalizer, nonce) are required")
	}
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Consultant{
		client:    client,
		logger:    logger,
		sanitizer: guard.NewSanitizer(deps.Normalizer),
		policy:    guard.DefaultPolicy(),
		nonceB:    guard.NewNonceBoundary(deps.Nonce),
		validator: guard.NewOutputValidator(deps.Normalizer),
		tmpl:      tmpl,
		opts:      opts.withDefaults(),
	}, nil
}

// Ask answers one request. With the guard off the input is wrapped in static
// boundary tags and sent unchanged. With the guard on the input is sanitised,
// checked by the policy, wrapped in nonce tags and the answer is validated;
// any guard failure yields RefusalAnswer with Blocked set (fail-closed).
func (c *Consultant) Ask(ctx context.Context, req Request) (ports.Response, error) {
	c.mu.RLock()
	tmpl, current, opts := c.tmpl, c.current, c.opts
	c.mu.RUnlock()

	if strings.TrimSpace(req.Question) == "" {
		return ports.Response{}, ErrEmptyQuestion
	}
	qLen, ctxLen := utf8.RuneCountInString(req.Question), utf8.RuneCountInString(req.Context)
	if qLen > opts.MaxQuestionRunes {
		return ports.Response{}, fmt.Errorf("%w: question %d > %d runes", ErrInputTooLong, qLen, opts.MaxQuestionRunes)
	}
	if ctxLen > opts.MaxContextRunes {
		return ports.Response{}, fmt.Errorf("%w: context %d > %d runes", ErrInputTooLong, ctxLen, opts.MaxContextRunes)
	}
	log := c.logger.With(
		slog.String("model", current),
		slog.String("prompt", tmpl.Name),
		slog.Int("q_len", qLen),
		slog.Int("ctx_len", ctxLen),
		slog.String("q_sha", shortSHA(req.Question)),
	)
	if !opts.Enabled {
		return c.askUnguarded(ctx, tmpl, req, log.With(slog.String("guard", "off")))
	}
	return c.askGuarded(ctx, tmpl, req, log.With(slog.String("guard", "on")))
}

func (c *Consultant) askUnguarded(ctx context.Context, tmpl PromptTemplate, req Request, log *slog.Logger) (ports.Response, error) {
	w, _ := guard.StaticBoundary{}.Wrap(req.Question, req.Context)
	resp, err := c.generate(ctx, tmpl, w)
	if err != nil {
		return ports.Response{}, err
	}
	resp.RawModelAnswer = resp.Raw
	fillFields(&resp)
	logResponse(log, resp, nil)
	return resp, nil
}

func (c *Consultant) askGuarded(ctx context.Context, tmpl PromptTemplate, req Request, log *slog.Logger) (ports.Response, error) {
	q := c.sanitizer.Sanitize(req.Question)
	doc := c.sanitizer.Sanitize(req.Context)
	trace := ports.GuardTrace{
		Findings:          findings(q, doc),
		SanitizedQuestion: q.Clean,
		SanitizedContext:  doc.Clean,
		Hidden:            append(q.HiddenContent(), doc.HiddenContent()...),
	}

	if d := c.policy.Decide(q, doc); d.Block {
		resp := refusal(ports.BlockStageInput, d.Reasons, trace)
		logResponse(log, resp, trace.Findings)
		return resp, nil
	}

	w, err := c.nonceB.Wrap(q.Clean, doc.Clean)
	switch {
	case errors.Is(err, guard.ErrBoundaryCollision):
		resp := refusal(ports.BlockStageInput, []string{"boundary_collision"}, trace)
		logResponse(log, resp, trace.Findings)
		return resp, nil
	case err != nil:
		log.Error("guard boundary failed", slog.String("error", err.Error()))
		return ports.Response{}, fmt.Errorf("guard boundary: %w", err)
	}

	resp, err := c.generate(ctx, tmpl, w)
	if err != nil {
		return ports.Response{}, err
	}
	resp.RawModelAnswer = resp.Raw
	resp.Trace = trace
	reasons := c.validator.Validate(resp.Raw, guard.OutputCheck{
		Canaries:   tmpl.Canaries(),
		Nonce:      w.Nonce,
		SystemText: tmpl.SystemText(),
		Hidden:     trace.Hidden,
		Visible:    q.Clean + "\n" + doc.Clean,
	})
	if len(reasons) > 0 {
		blocked := refusal(ports.BlockStageOutput, reasons, trace)
		blocked.RawModelAnswer = resp.RawModelAnswer
		blocked.Metrics = resp.Metrics
		resp = blocked
	} else {
		fillFields(&resp)
	}
	logResponse(log, resp, trace.Findings)
	return resp, nil
}

func (c *Consultant) generate(ctx context.Context, tmpl PromptTemplate, w guard.Wrapped) (ports.Response, error) {
	msgs := tmpl.Render(RenderInput{QuestionTag: w.QuestionTag, ContextTag: w.ContextTag, UserInput: w.UserInput})
	resp, err := c.client.Generate(ctx, msgs)
	if err != nil {
		return ports.Response{}, fmt.Errorf("generate: %w", err)
	}
	return resp, nil
}

func refusal(stage string, reasons []string, trace ports.GuardTrace) ports.Response {
	resp := ports.Response{
		Raw:        RefusalAnswer,
		Blocked:    true,
		BlockStage: stage,
		Reasons:    reasons,
		Trace:      trace,
	}
	fillFields(&resp)
	return resp
}

func fillFields(resp *ports.Response) {
	p := ParseAgileAnswer(resp.Raw)
	resp.Verdict, resp.Principle, resp.Reasoning, resp.Suggestion = p.Verdict, p.Principle, p.Reasoning, p.Suggestion
}

// findings formats sanitiser findings as "<part>:<kind>=<count>".
func findings(q, doc guard.Result) []string {
	var out []string
	for _, part := range []struct {
		name string
		r    guard.Result
	}{{"question", q}, {"document", doc}} {
		for _, f := range part.r.Findings {
			out = append(out, fmt.Sprintf("%s:%s=%d", part.name, f.Kind, f.Count))
		}
	}
	return out
}

// logResponse logs metadata only: never the payload, the sanitised text or
// the model answer.
func logResponse(log *slog.Logger, resp ports.Response, findings []string) {
	stage := resp.BlockStage
	if stage == "" {
		stage = "delivered"
	}
	log.Info("consultant response",
		slog.String("stage", stage),
		slog.Bool("blocked", resp.Blocked),
		slog.Any("reasons", resp.Reasons),
		slog.Any("findings", findings),
		slog.Duration("ttft", resp.Metrics.TTFT),
		slog.Float64("tokens_per_sec", resp.Metrics.TokensPerSec),
		slog.Int("tokens", resp.Metrics.Tokens),
	)
}

func shortSHA(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}

// CurrentModel returns the active model name.
func (c *Consultant) CurrentModel() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

// SetCurrentModel sets the active model name (for logs and the UI).
func (c *Consultant) SetCurrentModel(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = name
}

// Template returns the prompt template in use.
func (c *Consultant) Template() PromptTemplate {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tmpl
}

// SetTemplate replaces the prompt template (e.g. on SIGHUP reload).
func (c *Consultant) SetTemplate(tmpl PromptTemplate) error {
	if err := tmpl.Validate(); err != nil {
		return fmt.Errorf("set template: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tmpl = tmpl
	return nil
}

// GuardOptions returns the current guard options.
func (c *Consultant) GuardOptions() GuardOptions {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.opts
}

// SetGuardOptions replaces the guard options (e.g. on SIGHUP reload).
func (c *Consultant) SetGuardOptions(opts GuardOptions) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.opts = opts.withDefaults()
}
