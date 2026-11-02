package usecase

import (
	"context"
	"fmt"
	"log/slog"

	"agile-consultant/internal/config"
	"agile-consultant/internal/ports"
)

type Consultant struct {
	client     ports.ModelClient
	cfg        *config.Config
	logger     *slog.Logger
	current    string
}

func NewConsultant(client ports.ModelClient, cfg *config.Config, logger *slog.Logger) *Consultant {
	return &Consultant{
		client:  client,
		cfg:     cfg,
		logger:  logger,
		current: cfg.DefaultModel().Name,
	}
}

func (c *Consultant) Ask(ctx context.Context, question string) (ports.Response, error) {
	prompt := buildQwen25InstructPrompt(question)
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
		slog.String("model", c.current),
		slog.Duration("ttft", resp.Metrics.TTFT),
		slog.Float64("tokens_per_sec", resp.Metrics.TokensPerSec),
		slog.Int("tokens", resp.Metrics.Tokens),
	)
	return resp, nil
}

func (c *Consultant) CurrentModel() string {
	return c.current
}

func (c *Consultant) SetCurrentModel(name string) {
	c.current = name
}

func buildQwen25InstructPrompt(question string) string {
	return fmt.Sprintf("<|im_start|>system\nТы — Agile-консультант. Отвечай структурированно, используя поля: Вердикт, Принцип, Обоснование, Рекомендация. Отвечай на русском языке.\n<|im_end|>\n<|im_start|>user\n%s\n<|im_end|>\n<|im_start|>assistant\n", question)
}
