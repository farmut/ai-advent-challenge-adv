package config

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// validateLLMLimits checks every LLM limit. Zero is rejected wherever it would
// not mean "off": a zero timeout or body cap is a misconfiguration, not an
// unlimited setting. The client-side rate limiter may be disabled explicitly
// (enabled: false) — the guard proxy keeps its own limit — but when enabled
// its values must be positive.
func validateLLMLimits(c LLMConfig) error {
	if c.Timeout <= 0 {
		return fmt.Errorf("llm.timeout должен быть > 0, получено %s", c.Timeout)
	}
	if c.RequestMaxBytes <= 0 {
		return fmt.Errorf("llm.request_max_bytes должен быть > 0, получено %d", c.RequestMaxBytes)
	}
	if c.ResponseMaxBytes <= 0 {
		return fmt.Errorf("llm.response_max_bytes должен быть > 0, получено %d", c.ResponseMaxBytes)
	}
	if c.RateLimit.Enabled {
		if c.RateLimit.PerMinute <= 0 {
			return fmt.Errorf("llm.rate_limit.per_minute должен быть > 0 (или rate_limit.enabled: false), получено %d", c.RateLimit.PerMinute)
		}
		if c.RateLimit.Burst <= 0 {
			return fmt.Errorf("llm.rate_limit.burst должен быть > 0, получено %d", c.RateLimit.Burst)
		}
	}
	r := c.Retry
	if r.MaxAttempts < 0 {
		return fmt.Errorf("llm.retry.max_attempts должен быть >= 0, получено %d", r.MaxAttempts)
	}
	if r.BaseDelay <= 0 {
		return fmt.Errorf("llm.retry.base_delay должен быть > 0, получено %s", r.BaseDelay)
	}
	if r.MaxDelay < r.BaseDelay {
		return fmt.Errorf("llm.retry.max_delay (%s) должен быть >= base_delay (%s)", r.MaxDelay, r.BaseDelay)
	}
	if r.Jitter < 0 || r.Jitter > 1 {
		return fmt.Errorf("llm.retry.jitter должен быть в [0,1], получено %g", r.Jitter)
	}
	return nil
}

// validateBaseURL accepts https URLs, and plain http only to a loopback host
// (the local guard proxy): a key sent over http to any other host travels in
// clear text. User info, query and fragment are refused — credentials and
// parameters do not belong in a base URL.
func validateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("некорректный URL %q: %w", raw, err)
	}
	if u.Host == "" {
		return fmt.Errorf("в URL %q нет хоста", raw)
	}
	if u.User != nil {
		return fmt.Errorf("URL %q не должен содержать учётные данные (user:pass@)", raw)
	}
	if u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(raw, "#") {
		return fmt.Errorf("URL %q не должен содержать query или fragment", raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		if isLoopback(u.Hostname()) {
			return nil
		}
		return fmt.Errorf("http допустим только для loopback (127.0.0.0/8, ::1, localhost), а не для %q — используйте https", u.Hostname())
	default:
		return fmt.Errorf("схема %q не поддерживается (нужен https или http на loopback)", u.Scheme)
	}
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
