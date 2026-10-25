// Package config читает и валидирует конфигурацию из переменных окружения.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
)

const (
	defaultTelegramBaseURL    = "https://api.telegram.org"
	defaultOpenWeatherBaseURL = "https://api.openweathermap.org"
	defaultLogLevel           = "info"
)

// Config — настройки бота.
type Config struct {
	TelegramBotToken   string
	OpenWeatherAPIKey  string
	TelegramBaseURL    string
	OpenWeatherBaseURL string
	LogLevel           slog.Level
}

// Load читает окружение и возвращает все найденные проблемы разом.
// Значения секретов в тексты ошибок не попадают.
func Load() (Config, error) {
	var errs []error
	cfg := Config{
		TelegramBotToken:   env("TELEGRAM_BOT_TOKEN", ""),
		OpenWeatherAPIKey:  env("OPENWEATHER_API_KEY", ""),
		TelegramBaseURL:    env("TELEGRAM_BASE_URL", defaultTelegramBaseURL),
		OpenWeatherBaseURL: env("OPENWEATHER_BASE_URL", defaultOpenWeatherBaseURL),
	}

	switch {
	case cfg.TelegramBotToken == "":
		errs = append(errs, errors.New("TELEGRAM_BOT_TOKEN is required"))
	case !validBotToken(cfg.TelegramBotToken):
		errs = append(errs, errors.New("TELEGRAM_BOT_TOKEN has invalid format, want <digits>:<secret>"))
	}
	if cfg.OpenWeatherAPIKey == "" {
		errs = append(errs, errors.New("OPENWEATHER_API_KEY is required"))
	}
	if err := validateBaseURL(cfg.TelegramBaseURL); err != nil {
		errs = append(errs, fmt.Errorf("TELEGRAM_BASE_URL: %w", err))
	}
	if err := validateBaseURL(cfg.OpenWeatherBaseURL); err != nil {
		errs = append(errs, fmt.Errorf("OPENWEATHER_BASE_URL: %w", err))
	}
	level, err := parseLevel(env("LOG_LEVEL", defaultLogLevel))
	if err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL: %w", err))
	}
	cfg.LogLevel = level

	if err := errors.Join(errs...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// env возвращает значение переменной без пробелов по краям или def, если она пуста.
func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func validateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("host is empty")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return errors.New("query and fragment are not allowed")
	}
	return nil
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown level %q, want debug, info, warn or error", s)
	}
}

// validBotToken проверяет формат "123456:ABC-def_123": токен встаёт в путь URL,
// поэтому символы вроде "/" или "?" недопустимы.
func validBotToken(token string) bool {
	id, secret, ok := strings.Cut(token, ":")
	if !ok || id == "" || secret == "" {
		return false
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return false
		}
	}
	for _, r := range secret {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !isAlnum && r != '_' && r != '-' {
			return false
		}
	}
	return true
}
