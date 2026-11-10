//go:build integration

package main

import "ai-adv-agent/internal/adapter/llm"

// mustLLMClient builds an LLM client for the integration tests; a bad CA file
// is a broken test environment, so it panics instead of being threaded through.
func mustLLMClient(cfg llm.Config) *llm.Client {
	c, err := llm.NewClient(cfg)
	if err != nil {
		panic(err)
	}
	return c
}
