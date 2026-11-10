package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeYAML(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "agent.yaml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func validLLM() Config {
	c := Default()
	c.LLM.Provider = ProviderOpenAI
	c.LLM.APIKey = "sk-test"
	c.LLM.Model = "qwen2.5-7b-ollama"
	return c
}

func TestLoad_RejectsAPIKeyInYAML(t *testing.T) {
	_, err := Load(writeYAML(t, "llm:\n  provider: openai\n  api_key: sk-leaked\n"))
	if err == nil || !strings.Contains(err.Error(), "llm.api_key в yaml запрещён") {
		t.Fatalf("want api_key rejection, got %v", err)
	}
	// An explicitly empty key is harmless.
	if _, err := Load(writeYAML(t, "llm:\n  api_key: \"\"\n")); err != nil {
		t.Fatalf("empty api_key must load: %v", err)
	}
}

func TestResolveEnv_KeyOnlyFromEnv(t *testing.T) {
	t.Setenv("LLM_API_KEY", "sk-env")
	c := Default()
	c.LLM.APIKey = "set-by-code"
	ResolveEnv(&c)
	if c.LLM.APIKey != "sk-env" {
		t.Errorf("api key must come from LLM_API_KEY, got %q", c.LLM.APIKey)
	}
}

func TestValidate_ModelRequired(t *testing.T) {
	c := validLLM()
	c.LLM.Model = ""
	if err := Validate(c); err == nil || !strings.Contains(err.Error(), "llm.model") {
		t.Fatalf("empty model must fail, got %v", err)
	}
}

func TestValidateBaseURL(t *testing.T) {
	tests := []struct {
		url string
		ok  bool
	}{
		{"http://127.0.0.1:8090/v1", true},
		{"http://127.1.2.3:8090/v1", true},
		{"http://localhost:8090/v1", true},
		{"http://[::1]:8090/v1", true},
		{"https://api.openai.com/v1", true},
		{"http://10.0.0.5/v1", false},
		{"http://example.com/v1", false},
		{"https://user:pass@example.com/v1", false},
		{"https://example.com/v1?key=x", false},
		{"https://example.com/v1#frag", false},
		{"ftp://example.com", false},
		{"127.0.0.1:8090", false},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			err := validateBaseURL(tt.url)
			if (err == nil) != tt.ok {
				t.Errorf("validateBaseURL(%q) err=%v, want ok=%v", tt.url, err, tt.ok)
			}
		})
	}
}

func TestValidate_URLsChecked(t *testing.T) {
	c := validLLM()
	c.LLM.BaseURL = "http://10.0.0.5/v1"
	if err := Validate(c); err == nil {
		t.Error("non-loopback http base_url must fail")
	}
	c = validLLM()
	c.LLM.BaseURL = "http://127.0.0.1:8090/v1"
	if err := Validate(c); err != nil {
		t.Errorf("loopback proxy must pass: %v", err)
	}
	c = validLLM()
	c.RAG.Rerank.URL = "http://10.0.0.5/v1"
	if err := Validate(c); err == nil {
		t.Error("non-loopback http rerank url must fail")
	}
}

func TestValidate_OrchestratorLimits(t *testing.T) {
	c := validLLM()
	c.Orchestrator.JSONRetries = -1
	if err := Validate(c); err == nil {
		t.Error("negative json_retries must fail")
	}
	c = validLLM()
	c.Orchestrator.MaxRounds = 0
	if err := Validate(c); err == nil {
		t.Error("zero max_rounds must fail")
	}
}
