// Package telegram — delivery-слой: long polling Telegram Bot API.
package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"weatherbot/internal/domain"
)

// Poller выполняет long polling для получения обновлений от Telegram.
type Poller struct {
	client   *Client
	handler  *Handler
	log      *slog.Logger
	lastUpdateID int64
}

// NewPoller создаёт poller для работы с Telegram Bot API.
func NewPoller(client *Client, handler *Handler, log *slog.Logger) *Poller {
	return &Poller{client: client, handler: handler, log: log}
}

// Run запускает long polling.
func (p *Poller) Run(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			updates, err := p.client.GetUpdates(ctx, p.lastUpdateID)
			if err != nil {
				// Обработка rate limiting от Telegram
				if errors.Is(err, domain.ErrProviderUnavailable) {
					p.log.Warn("rate limited by Telegram", "err", err)
					// Ждем 30 секунд перед следующей попыткой
					select {
					case <-time.After(30 * time.Second):
						continue
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				p.log.Error("get updates failed", "err", err)
				return fmt.Errorf("get updates: %w", err)
			}

			for _, update := range updates {
				p.lastUpdateID = update.ID
				if update.Message.Text != "" {
					p.handler.Handle(ctx, update.Message.Chat.ID, update.Message.Text)
				}
			}

			// Небольшая задержка между опросами, чтобы не перегружать API
			select {
			case <-time.After(100 * time.Millisecond):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}