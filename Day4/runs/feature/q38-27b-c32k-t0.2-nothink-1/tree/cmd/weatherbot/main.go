package main

import (
	"context"
	"errors"
	"fmt"
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
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	weather := openweather.NewClient(cfg.OpenWeatherBaseURL, cfg.OpenWeatherAPIKey, &http.Client{Timeout: 10 * time.Second})
	service := usecase.NewWeatherService(weather)
	bot := telegram.NewClient(cfg.TelegramBaseURL, cfg.TelegramBotToken, &http.Client{Timeout: 40 * time.Second})
	handler := telegram.NewHandler(service, bot, logger)
	poller := telegram.NewPoller(bot, handler, logger)

	logger.Info("weatherbot started")
	if err := poller.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("run poller: %w", err)
	}
	logger.Info("weatherbot stopped")
	return nil
}
