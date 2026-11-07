package benchmark

import "time"

type ModelMetrics struct {
	Name            string        `json:"name"`
	SizeGB          float64       `json:"size_gb"`
	LoadTime        time.Duration `json:"load_time"`
	PeakMemoryGB    float64       `json:"peak_memory_gb"`
	AvgTTFT         time.Duration `json:"avg_ttft"`
	AvgTokensPerSec float64       `json:"avg_tokens_per_sec"`
	AvgTotalTime    time.Duration `json:"avg_total_time"`
}

type QueryResult struct {
	Query        string        `json:"query"`
	Model        string        `json:"model"`
	Raw          string        `json:"raw"`
	Verdict      string        `json:"verdict"`
	TTFT         time.Duration `json:"ttft"`
	TokensPerSec float64       `json:"tokens_per_sec"`
	TotalTime    time.Duration `json:"total_time"`
	Tokens       int           `json:"tokens"`
}

type Comparison struct {
	Tuned    ModelMetrics  `json:"tuned"`
	Baseline ModelMetrics  `json:"baseline"`
	Results  []QueryResult `json:"results"`
}
