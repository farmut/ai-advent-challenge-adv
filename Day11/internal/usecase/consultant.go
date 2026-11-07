package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"agile-consultant/internal/config"
	"agile-consultant/internal/ports"
)

// Consultant answers Agile questions through a model using a prompt template.
// It is safe for concurrent use.
type Consultant struct {
	client ports.ModelClient
	cfg    *config.Config
	logger *slog.Logger

	mu      sync.RWMutex
	current string
	tmpl    PromptTemplate
}

// NewConsultant creates a Consultant that renders questions with tmpl.
// An empty template falls back to LegacyTemplate.
func NewConsultant(client ports.ModelClient, cfg *config.Config, tmpl PromptTemplate, logger *slog.Logger) *Consultant {
	if tmpl.Text == "" {
		tmpl = LegacyTemplate()
	}
	current := ""
	if cfg != nil {
		if m := cfg.DefaultModel(); m != nil {
			current = m.Name
		}
	}
	return &Consultant{
		client:  client,
		cfg:     cfg,
		logger:  logger,
		current: current,
		tmpl:    tmpl,
	}
}

func (c *Consultant) Ask(ctx context.Context, question string) (ports.Response, error) {
	c.mu.RLock()
	tmpl, current := c.tmpl, c.current
	c.mu.RUnlock()

	prompt := tmpl.Render(question)
	resp, err := c.client.Generate(ctx, prompt)
	if err != nil {
		return ports.Response{}, fmt.Errorf("generate: %w", err)
	}
	parsed := ParseAgileAnswer(resp.Raw)
	resp.Verdict = parsed.Verdict
	resp.Principle = parsed.Principle
	resp.Reasoning = parsed.Reasoning
	resp.Suggestion = parsed.Suggestion
	c.logger.Info("consultant response",
		slog.String("model", current),
		slog.String("prompt", tmpl.Name),
		slog.Duration("ttft", resp.Metrics.TTFT),
		slog.Float64("tokens_per_sec", resp.Metrics.TokensPerSec),
		slog.Int("tokens", resp.Metrics.Tokens),
	)
	return resp, nil
}

func (c *Consultant) CurrentModel() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

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

// SetTemplate replaces the prompt template (e.g. on SIGHUP reload). The
// template is expected to be validated by the loader.
func (c *Consultant) SetTemplate(tmpl PromptTemplate) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tmpl = tmpl
}
