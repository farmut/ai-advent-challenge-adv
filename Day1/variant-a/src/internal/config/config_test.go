package config

import (
	"strings"
	"testing"
	"time"
)

func envFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load(envFrom(map[string]string{
		"TELEGRAM_BOT_TOKEN":  "tg",
		"OPENWEATHER_API_KEY": "owm",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.WeatherUnits != "metric" || cfg.WeatherLang != "ru" || cfg.LogLevel != "info" {
		t.Errorf("unexpected defaults: %+v", cfg)
	}
	if cfg.RequestTimeout != 10*time.Second {
		t.Errorf("timeout = %v, want 10s", cfg.RequestTimeout)
	}
}

func TestLoad_Overrides(t *testing.T) {
	cfg, err := Load(envFrom(map[string]string{
		"TELEGRAM_BOT_TOKEN":  " tg ",
		"OPENWEATHER_API_KEY": "owm",
		"OPENWEATHER_UNITS":   "imperial",
		"OPENWEATHER_LANG":    "en",
		"REQUEST_TIMEOUT":     "3s",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TelegramToken != "tg" || cfg.WeatherUnits != "imperial" || cfg.WeatherLang != "en" || cfg.RequestTimeout != 3*time.Second {
		t.Errorf("unexpected config: %+v", cfg)
	}
}

func TestLoad_Errors(t *testing.T) {
	_, err := Load(envFrom(map[string]string{
		"OPENWEATHER_UNITS": "kelvin",
		"REQUEST_TIMEOUT":   "abc",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"TELEGRAM_BOT_TOKEN", "OPENWEATHER_API_KEY", "OPENWEATHER_UNITS", "REQUEST_TIMEOUT"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}
