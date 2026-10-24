// Package config загружает настройки приложения из переменных окружения.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// Допустимые единицы измерения OpenWeatherMap.
var validUnits = map[string]bool{"metric": true, "imperial": true, "standard": true}

// Config — настройки бота.
type Config struct {
	TelegramToken  string
	WeatherAPIKey  string
	WeatherUnits   string
	WeatherLang    string
	RequestTimeout time.Duration
	LogLevel       string
}

// Load читает конфигурацию через getenv (обычно os.Getenv).
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		TelegramToken: strings.TrimSpace(getenv("TELEGRAM_BOT_TOKEN")),
		WeatherAPIKey: strings.TrimSpace(getenv("OPENWEATHER_API_KEY")),
		WeatherUnits:  valueOr(getenv("OPENWEATHER_UNITS"), "metric"),
		WeatherLang:   valueOr(getenv("OPENWEATHER_LANG"), "ru"),
		LogLevel:      valueOr(getenv("LOG_LEVEL"), "info"),
	}

	var errs []error
	if cfg.TelegramToken == "" {
		errs = append(errs, errors.New("TELEGRAM_BOT_TOKEN is required"))
	}
	if cfg.WeatherAPIKey == "" {
		errs = append(errs, errors.New("OPENWEATHER_API_KEY is required"))
	}
	if !validUnits[cfg.WeatherUnits] {
		errs = append(errs, fmt.Errorf("OPENWEATHER_UNITS must be metric, imperial or standard, got %q", cfg.WeatherUnits))
	}

	timeout, err := time.ParseDuration(valueOr(getenv("REQUEST_TIMEOUT"), "10s"))
	if err != nil || timeout <= 0 {
		errs = append(errs, fmt.Errorf("REQUEST_TIMEOUT must be a positive duration, got %q", getenv("REQUEST_TIMEOUT")))
	}
	cfg.RequestTimeout = timeout

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

// FromEnv загружает конфигурацию из окружения процесса.
func FromEnv() (Config, error) {
	return Load(os.Getenv)
}

func valueOr(v, def string) string {
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return def
}
