package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

const (
	backoffBase = time.Second
	backoffMax  = 30 * time.Second
)

// Updater — источник обновлений Telegram.
type Updater interface {
	GetUpdates(ctx context.Context, offset int64) ([]Update, error)
}

// MessageHandler обрабатывает одно сообщение.
type MessageHandler interface {
	Handle(ctx context.Context, chatID int64, text string)
}

// Poller — цикл long polling.
type Poller struct {
	updates Updater
	handler MessageHandler
	log     *slog.Logger
}

// NewPoller создаёт poller.
func NewPoller(updates Updater, handler MessageHandler, log *slog.Logger) *Poller {
	return &Poller{updates: updates, handler: handler, log: log}
}

// Run опрашивает getUpdates, пока ctx не отменён. Возвращает ошибку только при сбое,
// не связанном с отменой контекста.
func (p *Poller) Run(ctx context.Context) error {
	var offset int64
	backoff := backoffBase
	for {
		updates, err := p.updates.GetUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if d, ok := retryAfterFromError(err); ok {
				p.log.Warn("telegram rate limited, waiting retry_after", "retry_after", d)
				if err := sleepCtx(ctx, d); err != nil {
					return err
				}
				backoff = backoffBase
				continue
			}
			p.log.Warn("getUpdates failed, backing off", "err", err, "backoff", backoff)
			if err := sleepCtx(ctx, backoff); err != nil {
				return err
			}
			backoff = min(backoff*2, backoffMax)
			continue
		}
		backoff = backoffBase
		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			if u.Message != nil {
				p.handler.Handle(ctx, u.Message.Chat.ID, u.Message.Text)
			}
		}
	}
}

// rateLimitError — ошибка 429 с телом ответа, чтобы извлечь retry_after.
type rateLimitError struct {
	body []byte
}

func (e *rateLimitError) Error() string {
	return fmt.Sprintf("telegram rate limited: status 429, body: %s", truncate(string(e.body), 256))
}

// retryAfterFromError извлекает parameters.retry_after из ошибки 429.
func retryAfterFromError(err error) (time.Duration, bool) {
	var rle *rateLimitError
	if !asError(err, &rle) {
		return 0, false
	}
	var resp struct {
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	if json.Unmarshal(rle.body, &resp) != nil || resp.Parameters.RetryAfter <= 0 {
		return backoffBase, true
	}
	return time.Duration(resp.Parameters.RetryAfter) * time.Second, true
}

func asError(err error, target **rateLimitError) bool {
	for err != nil {
		if rle, ok := err.(*rateLimitError); ok {
			*target = rle
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
