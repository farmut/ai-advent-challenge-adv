// Package app is the composition root: it wires the declarative config.Config
// into concrete adapters and use cases (the shared Toolbelt) and the memory
// factory that gives the orchestrator and every sub-agent their memory. Keeping
// this wiring here leaves internal/usecase free of adapter imports.
package app

import (
	"fmt"
	"strings"

	"ai-adv-agent/internal/adapter/llm"
	ragadapter "ai-adv-agent/internal/adapter/rag"
	"ai-adv-agent/internal/config"
	"ai-adv-agent/internal/port"
)

const (
	defaultOpenAIBaseURL     = "https://api.openai.com/v1"
	defaultOpenRouterBaseURL = "https://openrouter.ai/api/v1"
)

// providerBaseURL returns the default API base URL for a provider.
func providerBaseURL(provider string) string {
	switch provider {
	case config.ProviderOpenRouter:
		return defaultOpenRouterBaseURL
	case config.ProviderGigaChat:
		return llm.DefaultGigaChatBaseURL
	default:
		return defaultOpenAIBaseURL
	}
}

// resolveBaseURL returns the configured base URL or the provider default.
func resolveBaseURL(provider, baseURL string) string {
	if b := strings.TrimRight(baseURL, "/"); b != "" {
		return b
	}
	return providerBaseURL(provider)
}

// buildLLMClient constructs the answer-generating LLM client from the config:
// the HTTP client, wrapped in the retry decorator, which also owns the
// client-side token bucket (nil when llm.rate_limit.enabled is false). clk
// drives every wait, so tests can pass a virtual clock.
func buildLLMClient(c config.LLMConfig, scope string, clk port.Clock) (port.LLMClient, error) {
	base, err := llm.NewClient(llm.Config{
		Provider:         c.Provider,
		APIKey:           c.APIKey,
		BaseURL:          resolveBaseURL(c.Provider, c.BaseURL),
		GigaChatScope:    scope,
		CACertFile:       c.CACert,
		Temperature:      c.Temperature,
		MaxTokens:        c.MaxTokens,
		ContextWindow:    c.ContextWindow,
		Timeout:          c.Timeout,
		RequestMaxBytes:  c.RequestMaxBytes,
		ResponseMaxBytes: c.ResponseMaxBytes,
		RefusalText:      c.Guard.RefusalText,
	})
	if err != nil {
		return nil, err
	}
	var bucket *llm.TokenBucket
	if c.RateLimit.Enabled {
		if bucket, err = llm.NewTokenBucket(c.RateLimit.PerMinute, c.RateLimit.Burst, clk); err != nil {
			return nil, err
		}
	}
	return llm.NewRetryingClient(base, llm.RetryPolicy{
		MaxAttempts: c.Retry.MaxAttempts,
		BaseDelay:   c.Retry.BaseDelay,
		MaxDelay:    c.Retry.MaxDelay,
		Jitter:      c.Retry.Jitter,
	}, bucket, clk)
}

// buildReranker constructs the optional rerank stage. Empty rerank fields fall
// back to the main LLM's provider/URL/key/model. Returns nil when reranking is
// disabled. The bool reports whether the dedicated /rerank API transport was
// chosen (vs chat scoring), for logging.
func buildReranker(rc config.RerankConfig, main config.LLMConfig, scope string) (port.Reranker, bool, error) {
	if !rc.Enabled {
		return nil, false, nil
	}
	provider := rc.Provider
	if provider == "" {
		provider = main.Provider
	}
	key := rc.Key
	if key == "" {
		key = main.APIKey
	}
	model := rc.Model
	if model == "" {
		model = main.Model
	}
	baseURL := strings.TrimRight(rc.URL, "/")
	if baseURL == "" {
		if provider == main.Provider {
			baseURL = resolveBaseURL(main.Provider, main.BaseURL)
		} else {
			baseURL = providerBaseURL(provider)
		}
	}

	// Dedicated rerank models (cohere/rerank-*, jina-reranker-*) are cross-encoders
	// served on a Cohere-style /rerank endpoint, not /chat/completions. Pick the
	// transport by mode; 'auto' detects a rerank model by its name.
	useAPI := false
	switch strings.ToLower(rc.Mode) {
	case "api":
		useAPI = true
	case "chat":
		useAPI = false
	default: // "auto" or empty
		useAPI = strings.Contains(strings.ToLower(model), "rerank")
	}

	if useAPI {
		return ragadapter.NewAPIReranker(baseURL, model, key), true, nil
	}
	rerankClient, err := llm.NewClient(llm.Config{
		Provider:         provider,
		APIKey:           key,
		BaseURL:          baseURL,
		GigaChatScope:    scope,
		CACertFile:       main.CACert,
		Timeout:          main.Timeout,
		RequestMaxBytes:  main.RequestMaxBytes,
		ResponseMaxBytes: main.ResponseMaxBytes,
		RefusalText:      main.Guard.RefusalText,
	})
	if err != nil {
		return nil, false, fmt.Errorf("rerank client: %w", err)
	}
	return ragadapter.NewLLMReranker(rerankClient, model), false, nil
}
