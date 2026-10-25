package config

import (
	"log/slog"
	"strings"
	"testing"
)

func setEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, map[string]string{
		"TELEGRAM_BOT_TOKEN":  "tg-token",
		"OPENWEATHER_API_KEY": "owm-key",
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() err = %v", err)
	}
	if cfg.TelegramBaseURL != "https://api.telegram.org" {
		t.Errorf("TelegramBaseURL = %q", cfg.TelegramBaseURL)
	}
	if cfg.OpenWeatherBaseURL != "https://api.openweathermap.org" {
		t.Errorf("OpenWeatherBaseURL = %q", cfg.OpenWeatherBaseURL)
	}
	if cfg.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v", cfg.LogLevel)
	}
}

func TestLoadMissingRequired(t *testing.T) {
	setEnv(t, map[string]string{
		"TELEGRAM_BOT_TOKEN":  "",
		"OPENWEATHER_API_KEY": "",
	})
	_, err := Load()
	if err == nil {
		t.Fatal("Load() err = nil, want error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "TELEGRAM_BOT_TOKEN") || !strings.Contains(msg, "OPENWEATHER_API_KEY") {
		t.Errorf("err = %q, want both missing vars", msg)
	}
}

func TestLoadCustomValues(t *testing.T) {
	setEnv(t, map[string]string{
		"TELEGRAM_BOT_TOKEN":   "tg-token",
		"OPENWEATHER_API_KEY":  "owm-key",
		"TELEGRAM_BASE_URL":    "http://127.0.0.1:8080",
		"OPENWEATHER_BASE_URL": "http://127.0.0.1:8081",
		"LOG_LEVEL":            "debug",
	})
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() err = %v", err)
	}
	if cfg.TelegramBaseURL != "http://127.0.0.1:8080" {
		t.Errorf("TelegramBaseURL = %q", cfg.TelegramBaseURL)
	}
	if cfg.OpenWeatherBaseURL != "http://127.0.0.1:8081" {
		t.Errorf("OpenWeatherBaseURL = %q", cfg.OpenWeatherBaseURL)
	}
	if cfg.LogLevel != slog.LevelDebug {
		t.Errorf("LogLevel = %v", cfg.LogLevel)
	}
}

func TestLoadInvalidLogLevel(t *testing.T) {
	setEnv(t, map[string]string{
		"TELEGRAM_BOT_TOKEN":  "tg-token",
		"OPENWEATHER_API_KEY": "owm-key",
		"LOG_LEVEL":           "verbose",
	})
	_, err := Load()
	if err == nil {
		t.Fatal("Load() err = nil, want error for invalid LOG_LEVEL")
	}
}
