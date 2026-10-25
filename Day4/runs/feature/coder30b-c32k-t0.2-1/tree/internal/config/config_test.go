package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	// Сохраняем оригинальные значения
	oldTelegramBotToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	oldOpenWeatherAPIKey := os.Getenv("OPENWEATHER_API_KEY")
	oldTelegramBaseURL := os.Getenv("TELEGRAM_BASE_URL")
	oldOpenWeatherBaseURL := os.Getenv("OPENWEATHER_BASE_URL")
	oldLogLevel := os.Getenv("LOG_LEVEL")

	t.Cleanup(func() {
		os.Setenv("TELEGRAM_BOT_TOKEN", oldTelegramBotToken)
		os.Setenv("OPENWEATHER_API_KEY", oldOpenWeatherAPIKey)
		os.Setenv("TELEGRAM_BASE_URL", oldTelegramBaseURL)
		os.Setenv("OPENWEATHER_BASE_URL", oldOpenWeatherBaseURL)
		os.Setenv("LOG_LEVEL", oldLogLevel)
	})

	t.Run("success with all env vars", func(t *testing.T) {
		os.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
		os.Setenv("OPENWEATHER_API_KEY", "test-key")
		os.Setenv("TELEGRAM_BASE_URL", "https://test.telegram.org")
		os.Setenv("OPENWEATHER_BASE_URL", "https://test.openweathermap.org")
		os.Setenv("LOG_LEVEL", "debug")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.TelegramBotToken != "test-token" {
			t.Errorf("Load() TelegramBotToken = %v, want %v", cfg.TelegramBotToken, "test-token")
		}
		if cfg.OpenWeatherAPIKey != "test-key" {
			t.Errorf("Load() OpenWeatherAPIKey = %v, want %v", cfg.OpenWeatherAPIKey, "test-key")
		}
		if cfg.TelegramBaseURL != "https://test.telegram.org" {
			t.Errorf("Load() TelegramBaseURL = %v, want %v", cfg.TelegramBaseURL, "https://test.telegram.org")
		}
		if cfg.OpenWeatherBaseURL != "https://test.openweathermap.org" {
			t.Errorf("Load() OpenWeatherBaseURL = %v, want %v", cfg.OpenWeatherBaseURL, "https://test.openweathermap.org")
		}
		if os.Getenv("LOG_LEVEL") != "debug" {
			t.Errorf("Load() LOG_LEVEL = %v, want debug", os.Getenv("LOG_LEVEL"))
		}
	})

	t.Run("success with minimal env vars", func(t *testing.T) {
		os.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
		os.Setenv("OPENWEATHER_API_KEY", "test-key")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.TelegramBotToken != "test-token" {
			t.Errorf("Load() TelegramBotToken = %v, want %v", cfg.TelegramBotToken, "test-token")
		}
		if cfg.OpenWeatherAPIKey != "test-key" {
			t.Errorf("Load() OpenWeatherAPIKey = %v, want %v", cfg.OpenWeatherAPIKey, "test-key")
		}
		if cfg.TelegramBaseURL != "https://api.telegram.org" {
			t.Errorf("Load() TelegramBaseURL = %v, want %v", cfg.TelegramBaseURL, "https://api.telegram.org")
		}
		if cfg.OpenWeatherBaseURL != "https://api.openweathermap.org" {
			t.Errorf("Load() OpenWeatherBaseURL = %v, want %v", cfg.OpenWeatherBaseURL, "https://api.openweathermap.org")
		}
	})

	t.Run("fail with missing required vars", func(t *testing.T) {
		os.Setenv("TELEGRAM_BOT_TOKEN", "")
		os.Setenv("OPENWEATHER_API_KEY", "")

		cfg, err := Load()
		if err == nil {
			t.Fatalf("Load() error = nil, want error")
		}
		if cfg != nil {
			t.Errorf("Load() cfg = %v, want nil", cfg)
		}
	})

	t.Run("fail with invalid log level", func(t *testing.T) {
		os.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
		os.Setenv("OPENWEATHER_API_KEY", "test-key")
		os.Setenv("LOG_LEVEL", "invalid")

		cfg, err := Load()
		if err == nil {
			t.Fatalf("Load() error = nil, want error")
		}
		if cfg != nil {
			t.Errorf("Load() cfg = %v, want nil", cfg)
		}
	})
}