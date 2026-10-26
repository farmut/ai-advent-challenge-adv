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
	for _, k := range []string{"TELEGRAM_BOT_TOKEN", "OPENWEATHER_API_KEY", "TELEGRAM_BASE_URL", "OPENWEATHER_BASE_URL", "LOG_LEVEL", "LOG_FORMAT"} {
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
		LogFormat:          config.LogFormatJSON,
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

func TestLoadLogFormat(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  config.LogFormat
	}{
		{name: "unset", value: "", want: config.LogFormatJSON},
		{name: "text", value: "text", want: config.LogFormatText},
		{name: "upper case", value: "TEXT", want: config.LogFormatText},
		{name: "spaces", value: " text ", want: config.LogFormatText},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, map[string]string{"TELEGRAM_BOT_TOKEN": testToken, "OPENWEATHER_API_KEY": "test-key", "LOG_FORMAT": tt.value})
			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("Load() err = %v", err)
			}
			if cfg.LogFormat != tt.want {
				t.Errorf("Load() LogFormat = %q, want %q", cfg.LogFormat, tt.want)
			}
		})
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
			name:     "unknown log format",
			vars:     map[string]string{"TELEGRAM_BOT_TOKEN": testToken, "OPENWEATHER_API_KEY": "test-key", "LOG_FORMAT": "xml"},
			wantErrs: []string{`LOG_FORMAT: unknown format "xml"`},
		},
		{
			name: "everything wrong at once",
			vars: map[string]string{
				"TELEGRAM_BASE_URL":    "ftp://example.com",
				"OPENWEATHER_BASE_URL": "https://example.com/?a=b",
				"LOG_LEVEL":            "verbose",
				"LOG_FORMAT":           "xml",
			},
			wantErrs: []string{
				"TELEGRAM_BOT_TOKEN is required", "OPENWEATHER_API_KEY is required",
				"TELEGRAM_BASE_URL", "OPENWEATHER_BASE_URL", "LOG_LEVEL", "LOG_FORMAT",
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

func TestLoadSingleError(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantErr string
		secret  bool // значение — токен, не должно попасть в текст ошибки
	}{
		{name: "token empty id", key: "TELEGRAM_BOT_TOKEN", value: ":abc", wantErr: "TELEGRAM_BOT_TOKEN has invalid format", secret: true},
		{name: "token empty secret", key: "TELEGRAM_BOT_TOKEN", value: "123:", wantErr: "TELEGRAM_BOT_TOKEN has invalid format", secret: true},
		{name: "token non-digit id", key: "TELEGRAM_BOT_TOKEN", value: "12a:abc", wantErr: "TELEGRAM_BOT_TOKEN has invalid format", secret: true},
		{name: "token slash in secret", key: "TELEGRAM_BOT_TOKEN", value: "123:a/b", wantErr: "TELEGRAM_BOT_TOKEN has invalid format", secret: true},
		{name: "token space in secret", key: "TELEGRAM_BOT_TOKEN", value: "123:a b", wantErr: "TELEGRAM_BOT_TOKEN has invalid format", secret: true},
		{name: "base url parse error", key: "TELEGRAM_BASE_URL", value: "http://[::1", wantErr: "TELEGRAM_BASE_URL: parse url"},
		{name: "base url empty host", key: "TELEGRAM_BASE_URL", value: "https://", wantErr: "TELEGRAM_BASE_URL: host is empty"},
		{name: "unknown log level", key: "LOG_LEVEL", value: "trace", wantErr: "LOG_LEVEL: unknown level"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vars := map[string]string{"TELEGRAM_BOT_TOKEN": testToken, "OPENWEATHER_API_KEY": "test-key"}
			vars[tt.key] = tt.value
			setEnv(t, vars)

			_, err := config.Load()
			if err == nil {
				t.Fatal("Load() err = nil, want error")
			}
			msg := err.Error()
			if !strings.Contains(msg, tt.wantErr) {
				t.Errorf("Load() err = %q, want it to contain %q", msg, tt.wantErr)
			}
			if n := strings.Count(msg, "\n"); n != 0 {
				t.Errorf("Load() err = %q, want exactly one error, got %d", msg, n+1)
			}
			if tt.secret && strings.Contains(msg, tt.value) {
				t.Errorf("Load() err = %q, must not contain the token", msg)
			}
		})
	}
}

func TestLoadLogLevel(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  slog.Level
	}{
		{name: "debug", value: "debug", want: slog.LevelDebug},
		{name: "warn", value: "warn", want: slog.LevelWarn},
		{name: "error upper case", value: "ERROR", want: slog.LevelError},
		{name: "debug upper case", value: "DEBUG", want: slog.LevelDebug},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setEnv(t, map[string]string{"TELEGRAM_BOT_TOKEN": testToken, "OPENWEATHER_API_KEY": "test-key", "LOG_LEVEL": tt.value})
			cfg, err := config.Load()
			if err != nil {
				t.Fatalf("Load() err = %v", err)
			}
			if cfg.LogLevel != tt.want {
				t.Errorf("Load() LogLevel = %v, want %v", cfg.LogLevel, tt.want)
			}
		})
	}
}

func TestLoadTrimsToken(t *testing.T) {
	setEnv(t, map[string]string{"TELEGRAM_BOT_TOKEN": "  123:abc  ", "OPENWEATHER_API_KEY": "test-key"})
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() err = %v", err)
	}
	if cfg.TelegramBotToken != "123:abc" {
		t.Errorf("Load() TelegramBotToken = %q, want %q", cfg.TelegramBotToken, "123:abc")
	}
}
