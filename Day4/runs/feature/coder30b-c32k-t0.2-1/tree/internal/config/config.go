// Package config читает переменные окружения и валидирует их.
package config

import (
	"errors"
	"log/slog"
	"os"
	"strings"
)

// Config содержит конфигурацию бота.
type Config struct {
	TelegramBotToken      string
	OpenWeatherAPIKey     string
	TelegramBaseURL       string
	OpenWeatherBaseURL    string
	LogLevel              slog.Level
}

// Load читает и валидирует конфигурацию.
func Load() (*Config, error) {
	var errs []error

	telegramBotToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if telegramBotToken == "" {
		errs = append(errs, errors.New("TELEGRAM_BOT_TOKEN is required"))
	}

	openWeatherAPIKey := os.Getenv("OPENWEATHER_API_KEY")
	if openWeatherAPIKey == "" {
		errs = append(errs, errors.New("OPENWEATHER_API_KEY is required"))
	}

	telegramBaseURL := os.Getenv("TELEGRAM_BASE_URL")
	if telegramBaseURL == "" {
		telegramBaseURL = "https://api.telegram.org"
	}

	openWeatherBaseURL := os.Getenv("OPENWEATHER_BASE_URL")
	if openWeatherBaseURL == "" {
		openWeatherBaseURL = "https://api.openweathermap.org"
	}

	logLevelStr := strings.ToLower(os.Getenv("LOG_LEVEL"))
	logLevel := slog.LevelInfo
	switch logLevelStr {
	case "debug":
		logLevel = slog.LevelDebug
	case "info":
		logLevel = slog.LevelInfo
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		if logLevelStr != "" {
			errs = append(errs, errors.New("invalid LOG_LEVEL: must be debug/info/warn/error"))
		}
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}

	return &Config{
		TelegramBotToken:   telegramBotToken,
		OpenWeatherAPIKey:  openWeatherAPIKey,
		TelegramBaseURL:    telegramBaseURL,
		OpenWeatherBaseURL: openWeatherBaseURL,
		LogLevel:           logLevel,
	}, nil
}