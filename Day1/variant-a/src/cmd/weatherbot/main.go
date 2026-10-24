// Команда weatherbot запускает Telegram-бота погоды.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"weatherbot/internal/bot"
	"weatherbot/internal/config"
	"weatherbot/internal/telegram"
	"weatherbot/internal/weather"
)

const pollTimeout = 30 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	// HTTP-таймаут Telegram больше таймаута long polling, иначе getUpdates будет обрываться.
	tg := telegram.NewClient(&http.Client{Timeout: pollTimeout + 10*time.Second}, "", cfg.TelegramToken)
	owm := weather.NewClient(&http.Client{Timeout: cfg.RequestTimeout}, "", cfg.WeatherAPIKey, cfg.WeatherUnits, cfg.WeatherLang)

	b := bot.New(tg, tg, owm, bot.Options{
		Units:          cfg.WeatherUnits,
		RequestTimeout: cfg.RequestTimeout,
		PollTimeout:    pollTimeout,
		Logger:         logger,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return b.Run(ctx)
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
