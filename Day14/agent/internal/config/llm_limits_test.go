package config

import (
	"strings"
	"testing"
	"time"
)

// Limits are validated at load time: a zero or negative value that does not
// mean "off" is a configuration error, never "unlimited".
func TestValidateLLMLimits(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*LLMConfig)
		wantErr string
	}{
		{"defaults are valid", func(*LLMConfig) {}, ""},
		{"limiter may be disabled", func(c *LLMConfig) { c.RateLimit = RateLimitConfig{Enabled: false} }, ""},
		{"zero retries allowed", func(c *LLMConfig) { c.Retry.MaxAttempts = 0 }, ""},
		{"zero timeout", func(c *LLMConfig) { c.Timeout = 0 }, "llm.timeout"},
		{"zero request cap", func(c *LLMConfig) { c.RequestMaxBytes = 0 }, "llm.request_max_bytes"},
		{"negative response cap", func(c *LLMConfig) { c.ResponseMaxBytes = -1 }, "llm.response_max_bytes"},
		{"zero per_minute when enabled", func(c *LLMConfig) { c.RateLimit.PerMinute = 0 }, "llm.rate_limit.per_minute"},
		{"zero burst when enabled", func(c *LLMConfig) { c.RateLimit.Burst = 0 }, "llm.rate_limit.burst"},
		{"negative retries", func(c *LLMConfig) { c.Retry.MaxAttempts = -1 }, "llm.retry.max_attempts"},
		{"zero base delay", func(c *LLMConfig) { c.Retry.BaseDelay = 0 }, "llm.retry.base_delay"},
		{"max below base", func(c *LLMConfig) { c.Retry.MaxDelay = c.Retry.BaseDelay / 2 }, "llm.retry.max_delay"},
		{"jitter above one", func(c *LLMConfig) { c.Retry.Jitter = 1.5 }, "llm.retry.jitter"},
		{"negative jitter", func(c *LLMConfig) { c.Retry.Jitter = -0.1 }, "llm.retry.jitter"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Default().LLM
			tt.mutate(&c)
			err := validateLLMLimits(c)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("want error mentioning %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestDefault_LLMLimitsAreSafe(t *testing.T) {
	c := Default().LLM
	if !c.RateLimit.Enabled || c.RateLimit.PerMinute != 18 || c.RateLimit.Burst != 2 {
		t.Errorf("rate limit default = %+v, want enabled 18/min burst 2", c.RateLimit)
	}
	if c.Retry.MaxAttempts != 3 || c.Retry.BaseDelay != time.Second || c.Retry.MaxDelay != 30*time.Second {
		t.Errorf("retry default = %+v", c.Retry)
	}
	if c.RequestMaxBytes >= 1<<20 {
		t.Errorf("request_max_bytes %d must stay below the proxy's 1 MiB cap", c.RequestMaxBytes)
	}
}

// Durations and the enabled toggle decode from YAML onto the defaults.
func TestLoad_LLMLimitsFromYAML(t *testing.T) {
	cfg, err := Load(writeYAML(t, `
llm:
  timeout: 45s
  request_max_bytes: 1000
  rate_limit: { enabled: false }
  retry: { max_attempts: 2, base_delay: 1ms, max_delay: 5ms, jitter: 0 }
`))
	if err != nil {
		t.Fatal(err)
	}
	l := cfg.LLM
	if l.Timeout != 45*time.Second || l.RequestMaxBytes != 1000 || l.RateLimit.Enabled {
		t.Errorf("decoded %+v", l)
	}
	if l.RateLimit.PerMinute != 18 {
		t.Errorf("unmentioned per_minute must keep its default, got %d", l.RateLimit.PerMinute)
	}
	if l.Retry != (RetryConfig{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}) {
		t.Errorf("retry = %+v", l.Retry)
	}
	if err := validateLLMLimits(l); err != nil {
		t.Errorf("fast test limits must validate: %v", err)
	}
}
