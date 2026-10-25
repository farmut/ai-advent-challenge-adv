package telegram

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

const (
	minBackoff = time.Second
	maxBackoff = 30 * time.Second
)

// UpdatesGetter — источник апдейтов для long polling.
type UpdatesGetter interface {
	GetUpdates(ctx context.Context, offset int64) ([]Update, error)
}

// MessageHandler обрабатывает одно текстовое сообщение.
type MessageHandler interface {
	Handle(ctx context.Context, chatID int64, text string)
}

// Poller крутит цикл getUpdates и раздаёт сообщения обработчику.
type Poller struct {
	updates UpdatesGetter
	handler MessageHandler
	log     *slog.Logger
	wait    func(ctx context.Context, d time.Duration) error // подменяется в тестах
}

// NewPoller создаёт poller.
func NewPoller(updates UpdatesGetter, handler MessageHandler, log *slog.Logger) *Poller {
	return &Poller{updates: updates, handler: handler, log: log, wait: sleep}
}

// Run работает до отмены ctx (возвращает ctx.Err()) или до отказа Telegram в токене.
func (p *Poller) Run(ctx context.Context) error {
	var offset int64
	backoff := minBackoff
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		updates, err := p.updates.GetUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, ErrUnauthorized) {
				return err
			}
			delay := backoff
			var ra *retryAfterError
			if errors.As(err, &ra) && ra.after > 0 {
				delay = ra.after
			} else {
				backoff = min(backoff*2, maxBackoff)
			}
			p.log.Warn("get updates", "err", err, "retry_in", delay.String())
			if err := p.wait(ctx, delay); err != nil {
				return err
			}
			continue
		}
		backoff = minBackoff
		for _, u := range updates {
			offset = max(offset, u.UpdateID+1)
			if u.Message == nil || u.Message.Text == "" {
				continue
			}
			p.handler.Handle(ctx, u.Message.Chat.ID, u.Message.Text)
		}
	}
}

// sleep ждёт d или отмены ctx.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
