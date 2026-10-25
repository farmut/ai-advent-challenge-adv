// Package telegram — delivery-слой: long polling Telegram Bot API и обработка команд.
package telegram

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"weatherbot/internal/domain"
)

const perChatInterval = 2 * time.Second

// WeatherGetter — то, что handler'у нужно от usecase.
type WeatherGetter interface {
	Current(ctx context.Context, city string) (domain.Weather, error)
}

// Sender отправляет текст в чат.
type Sender interface {
	Send(ctx context.Context, chatID int64, text string) error
}

// Handler превращает входящее сообщение в ответ.
type Handler struct {
	weather WeatherGetter
	sender  Sender
	log     *slog.Logger
	mu      sync.Mutex
	lastReq map[int64]time.Time // token bucket ёмкостью 1 на чат
}

// NewHandler создаёт обработчик сообщений.
func NewHandler(weather WeatherGetter, sender Sender, log *slog.Logger) *Handler {
	return &Handler{weather: weather, sender: sender, log: log, lastReq: make(map[int64]time.Time)}
}

// Handle обрабатывает одно сообщение. Паника не роняет бота.
func (h *Handler) Handle(ctx context.Context, chatID int64, text string) {
	defer func() {
		if r := recover(); r != nil {
			h.log.Error("handle message panicked", "chat_id", chatID, "panic", r)
		}
	}()
	reply := msgTooFrequent
	if h.allow(chatID, time.Now()) {
		reply = h.respond(ctx, text)
	}
	if err := h.sender.Send(ctx, chatID, reply); err != nil {
		h.log.Warn("send message", "chat_id", chatID, "err", err) // Sender уже вернул redacted-ошибку
	}
}

func (h *Handler) respond(ctx context.Context, text string) string {
	cmd, city := parseCommand(text)
	switch {
	case cmd == "/start":
		return msgStart
	case cmd != "/weather" && cmd != "":
		return msgHelp
	case city == "":
		return msgEmptyCity
	}
	w, err := h.weather.Current(ctx, city)
	switch {
	case err == nil:
		return formatWeather(w)
	case errors.Is(err, domain.ErrInvalidCity):
		return msgInvalidCity
	case errors.Is(err, domain.ErrCityNotFound):
		return msgCityNotFound(city)
	default:
		h.log.Warn("weather request failed", "err", err)
		return msgUnavailable
	}
}

func (h *Handler) allow(chatID int64, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if last, ok := h.lastReq[chatID]; ok && now.Sub(last) < perChatInterval {
		return false
	}
	h.lastReq[chatID] = now
	return true
}

// parseCommand: "/weather@MyBot Казань" → ("/weather", "Казань"); "Казань" → ("", "Казань").
func parseCommand(text string) (cmd, arg string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", text
	}
	head, rest, _ := strings.Cut(text, " ")
	head, _, _ = strings.Cut(head, "@")
	return strings.ToLower(head), strings.TrimSpace(rest)
}