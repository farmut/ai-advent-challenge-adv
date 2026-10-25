// Package config читает переменные окружения и валидирует конфигурацию.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// Config — настроенные параметры бота.
type Config struct {
	TelegramBotToken   string
	OpenWeatherAPIKey  string
	TelegramBaseURL    string
	OpenWeatherBaseURL string
	LogLevel           slog.Level
}

// Load читает переменные окружения и возвращает все проблемы разом.
func Load() (Config, error) {
	var errs []error

	cfg := Config{
		TelegramBotToken:   os.Getenv("TELEGRAM_BOT_TOKEN"),
		OpenWeatherAPIKey:  os.Getenv("OPENWEATHER_API_KEY"),
		TelegramBaseURL:    "https://api.telegram.org",
		OpenWeatherBaseURL: "https://api.openweathermap.org",
		LogLevel:           slog.LevelInfo,
	}

	if cfg.TelegramBotToken == "" {
		errs = append(errs, errors.New("TELEGRAM_BOT_TOKEN is required"))
	}
	if cfg.OpenWeatherAPIKey == "" {
		errs = append(errs, errors.New("OPENWEATHER_API_KEY is required"))
	}

	if v := os.Getenv("TELEGRAM_BASE_URL"); v != "" {
		cfg.TelegramBaseURL = v
	}
	if v := os.Getenv("OPENWEATHER_BASE_URL"); v != "" {
		cfg.OpenWeatherBaseURL = v
	}

	if v := os.Getenv("LOG_LEVEL"); v != "" {
		level, err := parseLogLevel(v)
		if err != nil {
			errs = append(errs, err)
		} else {
			cfg.LogLevel = level
		}
	}

	return cfg, errors.Join(errs...)
}

func parseLogLevel(v string) (slog.Level, error) {
	switch strings.ToLower(v) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("LOG_LEVEL must be one of debug/info/warn/error, got %q", v)
	}
}
