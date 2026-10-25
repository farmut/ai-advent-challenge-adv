package config_test

import (
	"log/slog"
	"strings"
	"testing"

	"weatherbot/internal/config"
)

const testToken = "123456:test-token_ABC"

func setEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	for _, k := range []string{"TELEGRAM_BOT_TOKEN", "OPENWEATHER_API_KEY", "TELEGRAM_BASE_URL", "OPENWEATHER_BASE_URL", "LOG_LEVEL"} {
		t.Setenv(k, vars[k])
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, map[string]string{"TELEGRAM_BOT_TOKEN": testToken, "OPENWEATHER_API_KEY": "test-key"})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() err = %v", err)
	}
	want := config.Config{
		TelegramBotToken:   testToken,
		OpenWeatherAPIKey:  "test-key",
		TelegramBaseURL:    "https://api.telegram.org",
		OpenWeatherBaseURL: "https://api.openweathermap.org",
		LogLevel:           slog.LevelInfo,
	}
	if cfg != want {
		t.Errorf("Load() = %+v, want %+v", cfg, want)
	}
}

func TestLoadOverrides(t *testing.T) {
	setEnv(t, map[string]string{
		"TELEGRAM_BOT_TOKEN":   testToken,
		"OPENWEATHER_API_KEY":  " test-key ",
		"TELEGRAM_BASE_URL":    "http://localhost:8081",
		"OPENWEATHER_BASE_URL": "http://localhost:8082",
		"LOG_LEVEL":            "DEBUG",
	})

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() err = %v", err)
	}
	if cfg.OpenWeatherAPIKey != "test-key" || cfg.TelegramBaseURL != "http://localhost:8081" ||
		cfg.OpenWeatherBaseURL != "http://localhost:8082" || cfg.LogLevel != slog.LevelDebug {
		t.Errorf("Load() = %+v", cfg)
	}
}

func TestLoadErrors(t *testing.T) {
	tests := []struct {
		name     string
		vars     map[string]string
		wantErrs []string
	}{
		{
			name:     "all required missing",
			vars:     map[string]string{},
			wantErrs: []string{"TELEGRAM_BOT_TOKEN is required", "OPENWEATHER_API_KEY is required"},
		},
		{
			name:     "bad token format",
			vars:     map[string]string{"TELEGRAM_BOT_TOKEN": "abc/../x", "OPENWEATHER_API_KEY": "test-key"},
			wantErrs: []string{"TELEGRAM_BOT_TOKEN has invalid format"},
		},
		{
			name: "everything wrong at once",
			vars: map[string]string{
				"TELEGRAM_BASE_URL":    "ftp://example.com",
				"OPENWEATHER_BASE_URL": "https://example.com/?a=b",
				"LOG_LEVEL":            "verbose",
			},
			wantErrs: []string{
				"TELEGRAM_BOT_TOKEN is required", "OPENWEATHER_API_KEY is required",
				"TELEGRAM_BASE_URL", "OPENWEATHER_BASE_URL", "LOG_LEVEL",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, tt.vars)
			_, err := config.Load()
			if err == nil {
				t.Fatal("Load() err = nil, want error")
			}
			for _, want := range tt.wantErrs {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Load() err = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

func TestLoadDoesNotLeakSecrets(t *testing.T) {
	setEnv(t, map[string]string{"TELEGRAM_BOT_TOKEN": "secret/token", "LOG_LEVEL": "nope"})
	_, err := config.Load()
	if err == nil || strings.Contains(err.Error(), "secret/token") {
		t.Errorf("Load() err = %v, must be non-nil and must not contain the token", err)
	}
}
