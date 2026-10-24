package telegram

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode"

	"weatherbot/internal/domain"
)

const (
	perChatInterval = 2 * time.Second
	maxTrackedChats = 10_000 // после этого из карты выкидываются устаревшие записи
)

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
	warned  map[int64]struct{}  // чаты, которым в текущем окне уже ушло предупреждение; ключи ⊆ lastReq
	now     func() time.Time    // часы; подменяются в тестах
}

// NewHandler создаёт обработчик сообщений.
func NewHandler(weather WeatherGetter, sender Sender, log *slog.Logger) *Handler {
	return &Handler{
		weather: weather, sender: sender, log: log,
		lastReq: make(map[int64]time.Time), warned: make(map[int64]struct{}), now: time.Now,
	}
}

// Handle обрабатывает одно сообщение. Паника не роняет бота.
func (h *Handler) Handle(ctx context.Context, chatID int64, text string) {
	defer func() {
		if r := recover(); r != nil {
			h.log.Error("handle message panicked", "chat_id", chatID, "panic", r)
		}
	}()
	var reply string
	switch {
	case h.allow(chatID, h.now()):
		reply = h.respond(ctx, text)
	case h.markWarned(chatID):
		reply = msgTooFrequent
	default:
		return // предупреждение в этом окне уже отправлено — молча игнорируем
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
		return msgCityNotFound(strings.Join(strings.Fields(city), " "))
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
	if len(h.lastReq) >= maxTrackedChats {
		for id, last := range h.lastReq {
			if now.Sub(last) >= perChatInterval {
				delete(h.lastReq, id)
				delete(h.warned, id)
			}
		}
	}
	h.lastReq[chatID] = now
	delete(h.warned, chatID) // новое окно — можно снова предупредить
	return true
}

// markWarned отмечает, что в текущем окне чату отправлено предупреждение.
// Возвращает false, если предупреждение в этом окне уже было.
func (h *Handler) markWarned(chatID int64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.warned[chatID]; ok {
		return false
	}
	h.warned[chatID] = struct{}{}
	return true
}

// parseCommand: "/weather@MyBot Казань" → ("/weather", "Казань"); "Казань" → ("", "Казань").
func parseCommand(text string) (cmd, arg string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", text
	}
	head, rest := text, ""
	if i := strings.IndexFunc(text, unicode.IsSpace); i >= 0 {
		head, rest = text[:i], text[i:]
	}
	head, _, _ = strings.Cut(head, "@")
	return strings.ToLower(head), strings.TrimSpace(rest)
}
