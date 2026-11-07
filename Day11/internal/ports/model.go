package ports

import (
	"context"
	"time"
)

type GenerationMetrics struct {
	TTFT         time.Duration
	TotalTime    time.Duration
	Tokens       int
	TokensPerSec float64
	PeakMemoryGB float64
}

type Response struct {
	Raw        string
	Verdict    string
	Principle  string
	Reasoning  string
	Suggestion string
	Metrics    GenerationMetrics
}

type ModelClient interface {
	Generate(ctx context.Context, prompt string) (Response, error)
}

type ModelServer interface {
	Start(modelPath string) error
	Stop() error
	HealthCheck(ctx context.Context) error
	LoadTime() time.Duration
}

type ModelSwitcher interface {
	Switch(ctx context.Context, modelName string) error
	CurrentModel() string
}
