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
	ackTimeout = 5 * time.Second
)

// pollState: offset — следующий ожидаемый update_id, acked — offset последнего успешного getUpdates.
type pollState struct {
	offset int64
	acked  int64
}

// UpdatesGetter — источник апдейтов для long polling.
type UpdatesGetter interface {
	GetUpdates(ctx context.Context, offset int64) ([]Update, error)
	Ack(ctx context.Context, offset int64) error // подтверждает апдейты < offset без ожидания
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
// При отмене подтверждает обработанные апдейты, чтобы после рестарта они не пришли снова.
func (p *Poller) Run(ctx context.Context) error {
	var st pollState
	err := p.poll(ctx, &st)
	if ctx.Err() != nil && st.offset > st.acked {
		ackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), ackTimeout)
		defer cancel()
		if err := p.updates.Ack(ackCtx, st.offset); err != nil {
			p.log.Warn("ack updates", "offset", st.offset, "err", err)
		}
	}
	return err
}

func (p *Poller) poll(ctx context.Context, st *pollState) error {
	backoff := minBackoff
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		updates, err := p.updates.GetUpdates(ctx, st.offset)
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
		st.acked = st.offset
		backoff = minBackoff
		for _, u := range updates {
			st.offset = max(st.offset, u.UpdateID+1)
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
