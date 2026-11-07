package benchmark

import (
	"agile-consultant/internal/config"
	"agile-consultant/internal/ports"
	"agile-consultant/internal/usecase"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

type BenchRunner struct {
	logger *slog.Logger
}

func NewBenchRunner(logger *slog.Logger) *BenchRunner {
	return &BenchRunner{logger: logger}
}

func (b *BenchRunner) RunFile(ctx context.Context, path string, client ports.ModelClient, cfg *config.Config, tmpl usecase.PromptTemplate, modelName string) ([]QueryResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open queries file: %w", err)
	}
	defer file.Close()

	if cfg == nil {
		cfg = &config.Config{}
	}
	consultant := usecase.NewConsultant(client, cfg, tmpl, b.logger)
	consultant.SetCurrentModel(modelName)

	var results []QueryResult
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var q struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal([]byte(line), &q); err != nil {
			return nil, fmt.Errorf("parse query line: %w", err)
		}
		resp, err := consultant.Ask(ctx, q.Query)
		if err != nil {
			return nil, fmt.Errorf("ask %q: %w", q.Query, err)
		}
		results = append(results, QueryResult{
			Query:        q.Query,
			Model:        modelName,
			Raw:          resp.Raw,
			Verdict:      resp.Verdict,
			TTFT:         resp.Metrics.TTFT,
			TokensPerSec: resp.Metrics.TokensPerSec,
			TotalTime:    resp.Metrics.TotalTime,
			Tokens:       resp.Metrics.Tokens,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan queries: %w", err)
	}
	return results, nil
}

func Summarize(results []QueryResult, modelName string, sizeGB, peakMemGB float64, loadTime time.Duration) ModelMetrics {
	if len(results) == 0 {
		return ModelMetrics{Name: modelName}
	}
	var totalTTFT time.Duration
	var totalTPS float64
	var totalTime time.Duration
	for _, r := range results {
		totalTTFT += r.TTFT
		totalTPS += r.TokensPerSec
		totalTime += r.TotalTime
	}
	n := float64(len(results))
	return ModelMetrics{
		Name:            modelName,
		SizeGB:          sizeGB,
		LoadTime:        loadTime,
		PeakMemoryGB:    peakMemGB,
		AvgTTFT:         time.Duration(int64(float64(totalTTFT) / n)),
		AvgTokensPerSec: totalTPS / n,
		AvgTotalTime:    time.Duration(int64(float64(totalTime) / n)),
	}
}

func ModelSizeGB(path string) (float64, error) {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return float64(size) / (1024 * 1024 * 1024), nil
}
