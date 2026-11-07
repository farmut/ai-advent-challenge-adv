package mlx

import (
	"agile-consultant/internal/config"
	"agile-consultant/internal/ports"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	baseURL   string
	cfg       *config.Inference
	logger    *slog.Logger
	modelPath string
}

func NewClient(baseURL, modelPath string, cfg *config.Inference, logger *slog.Logger) *Client {
	return &Client{
		baseURL:   strings.TrimRight(baseURL, "/"),
		cfg:       cfg,
		logger:    logger,
		modelPath: modelPath,
	}
}

func (c *Client) SetModelPath(path string) {
	c.modelPath = path
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	MaxTokens   int           `json:"max_tokens"`
	Temperature float64       `json:"temperature"`
	TopP        float64       `json:"top_p"`
	Stream      bool          `json:"stream"`
}

type delta struct {
	Content string `json:"content"`
}

type streamChoice struct {
	Delta delta `json:"delta"`
}

type streamResponse struct {
	Choices []streamChoice `json:"choices"`
}

type nonStreamChoice struct {
	Message chatMessage `json:"message"`
}

type nonStreamResponse struct {
	Choices []nonStreamChoice `json:"choices"`
	Usage   struct {
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (c *Client) Generate(ctx context.Context, prompt string) (ports.Response, error) {
	start := time.Now()
	reqBody := chatRequest{
		Model:       c.modelPath,
		Messages:    []chatMessage{{Role: "user", Content: prompt}},
		MaxTokens:   c.cfg.MaxTokens,
		Temperature: c.cfg.Temperature,
		TopP:        c.cfg.TopP,
		Stream:      true,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return ports.Response{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/chat/completions", bytes.NewReader(data))
	if err != nil {
		return ports.Response{}, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ports.Response{}, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return ports.Response{}, fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var raw strings.Builder
	var ttft time.Duration
	first := true
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			break
		}
		if first {
			ttft = time.Since(start)
			first = false
		}
		var chunk streamResponse
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		for _, ch := range chunk.Choices {
			raw.WriteString(ch.Delta.Content)
		}
	}
	if err := scanner.Err(); err != nil {
		return ports.Response{}, fmt.Errorf("scan stream: %w", err)
	}

	total := time.Since(start)
	genTime := total - ttft
	tokens := estimateTokens(raw.String())
	tokensPerSec := 0.0
	if genTime > 0 {
		tokensPerSec = float64(tokens) / genTime.Seconds()
	}

	c.logger.Debug("generation complete",
		slog.String("model_path", c.modelPath),
		slog.Duration("ttft", ttft),
		slog.Duration("total", total),
		slog.Int("tokens", tokens),
		slog.Float64("tokens_per_sec", tokensPerSec),
	)

	return ports.Response{
		Raw: raw.String(),
		Metrics: ports.GenerationMetrics{
			TTFT:         ttft,
			TotalTime:    total,
			Tokens:       tokens,
			TokensPerSec: tokensPerSec,
		},
	}, nil
}

func estimateTokens(text string) int {
	words := strings.Fields(text)
	chars := len(text)
	return max(len(words), chars/4)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
