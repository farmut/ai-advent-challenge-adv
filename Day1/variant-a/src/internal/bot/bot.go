// Package bot связывает Telegram и сервис погоды.
package bot

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"weatherbot/internal/telegram"
	"weatherbot/internal/weather"
)

// Updater получает новые события из Telegram.
type Updater interface {
	GetUpdates(ctx context.Context, offset int64, timeoutSec int) ([]telegram.Update, error)
}

// Sender отправляет сообщения в Telegram.
type Sender interface {
	SendMessage(ctx context.Context, chatID int64, text string) error
}

// WeatherProvider возвращает текущую погоду.
type WeatherProvider interface {
	Current(ctx context.Context, city string) (weather.Weather, error)
}

// Options — необязательные настройки бота.
type Options struct {
	Units          string        // metric | imperial | standard
	RequestTimeout time.Duration // таймаут обработки одного сообщения
	PollTimeout    time.Duration // таймаут long polling
	MaxConcurrency int           // сколько сообщений обрабатывается параллельно
	Logger         *slog.Logger
}

// Bot обрабатывает входящие сообщения.
type Bot struct {
	updater        Updater
	sender         Sender
	weather        WeatherProvider
	units          string
	requestTimeout time.Duration
	pollTimeoutSec int
	sem            chan struct{}
	log            *slog.Logger

	// Пауза между повторами при ошибках getUpdates (экспоненциальная).
	minBackoff time.Duration
	maxBackoff time.Duration
}

// New создаёт бота, подставляя значения по умолчанию для пустых опций.
func New(updater Updater, sender Sender, provider WeatherProvider, opts Options) *Bot {
	if opts.Units == "" {
		opts.Units = "metric"
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = 10 * time.Second
	}
	if opts.PollTimeout <= 0 {
		opts.PollTimeout = 30 * time.Second
	}
	if opts.MaxConcurrency <= 0 {
		opts.MaxConcurrency = 16
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Bot{
		updater:        updater,
		sender:         sender,
		weather:        provider,
		units:          opts.Units,
		requestTimeout: opts.RequestTimeout,
		pollTimeoutSec: int(opts.PollTimeout / time.Second),
		sem:            make(chan struct{}, opts.MaxConcurrency),
		log:            opts.Logger,
		minBackoff:     time.Second,
		maxBackoff:     30 * time.Second,
	}
}

// Run запускает long polling и блокируется до отмены ctx.
// После отмены дожидается завершения уже начатых обработчиков.
func (b *Bot) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	defer wg.Wait()

	// Обработчики не должны обрываться на середине при остановке:
	// их время жизни ограничено requestTimeout, а не ctx.
	handlerCtx := context.WithoutCancel(ctx)

	var offset int64
	backoff := b.minBackoff
	b.log.Info("bot started")

	for ctx.Err() == nil {
		updates, err := b.updater.GetUpdates(ctx, offset, b.pollTimeoutSec)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			b.log.Warn("getUpdates failed, retrying", "error", err, "backoff", backoff)
			if !sleep(ctx, backoff) {
				break
			}
			backoff = min(backoff*2, b.maxBackoff)
			continue
		}
		backoff = b.minBackoff

		for _, upd := range updates {
			offset = upd.UpdateID + 1
			msg := upd.Message
			if msg == nil || msg.Text == "" {
				continue
			}
			select {
			case b.sem <- struct{}{}:
			case <-ctx.Done():
				return nil
			}
			wg.Go(func() {
				defer func() { <-b.sem }()
				b.handleMessage(handlerCtx, msg)
			})
		}
	}

	b.log.Info("bot stopping")
	return nil
}

func (b *Bot) handleMessage(ctx context.Context, msg *telegram.Message) {
	ctx, cancel := context.WithTimeout(ctx, b.requestTimeout)
	defer cancel()

	b.log.Info("message received", "chat_id", msg.Chat.ID, "text", msg.Text)
	reply := b.Reply(ctx, msg.Text)
	if err := b.sender.SendMessage(ctx, msg.Chat.ID, reply); err != nil {
		b.log.Error("send message failed", "chat_id", msg.Chat.ID, "error", err)
	}
}

// sleep ждёт d или отмены ctx; возвращает false, если ctx отменён.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
