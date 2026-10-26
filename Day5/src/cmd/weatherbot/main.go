// Command weatherbot — Telegram-бот, присылающий текущую погоду из OpenWeatherMap.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"weatherbot/internal/config"
	"weatherbot/internal/delivery/telegram"
	"weatherbot/internal/repository/openweather"
	"weatherbot/internal/usecase"
)

func main() {
	if err := run(); err != nil {
		slog.Error("weatherbot stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := slog.New(newLogHandler(os.Stdout, cfg.LogFormat, &slog.HandlerOptions{Level: cfg.LogLevel}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	weather := openweather.NewClient(cfg.OpenWeatherBaseURL, cfg.OpenWeatherAPIKey, &http.Client{Timeout: 10 * time.Second})
	service := usecase.NewWeatherService(weather)
	bot := telegram.NewClient(cfg.TelegramBaseURL, cfg.TelegramBotToken, &http.Client{Timeout: 40 * time.Second})
	// Username берём у Telegram (getMe), а не из env: это единственный источник правды,
	// env может разойтись с реальным именем после переименования бота в @BotFather.
	// Без ретраев: 401 — ошибка конфигурации, сетевой сбой лечится перезапуском супервизором.
	me, err := bot.Me(ctx)
	if err != nil {
		return fmt.Errorf("get bot identity: %w", err)
	}
	if me.Username == "" {
		return errors.New("get bot identity: empty username")
	}
	handler := telegram.NewHandler(service, bot, me.Username, logger)
	poller := telegram.NewPoller(bot, handler, logger)

	logger.Info("weatherbot started", "username", me.Username)
	if err := poller.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("run poller: %w", err)
	}
	logger.Info("weatherbot stopped")
	return nil
}

// newLogHandler выбирает формат логов по конфигу; неизвестных значений нет — их отсекает config.Load.
func newLogHandler(w io.Writer, f config.LogFormat, opts *slog.HandlerOptions) slog.Handler {
	switch f {
	case config.LogFormatText:
		return slog.NewTextHandler(w, opts)
	default:
		return slog.NewJSONHandler(w, opts)
	}
}
