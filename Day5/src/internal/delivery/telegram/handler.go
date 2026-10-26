package telegram

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"weatherbot/internal/domain"
)

const (
	maxTrackedChats = 10_000 // ёмкость карт лимитера и последних городов

	// Сырой ввод безопасен: usecase нормализует и валидирует его при каждом вызове.
	// Лимит лишь не даёт держать в памяти мегабайтные строки.
	maxStoredCityBytes = 512
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
	weather     WeatherGetter
	sender      Sender
	botUsername string // без '@'; команды с чужим упоминанием игнорируются
	log         *slog.Logger
	limiter     *chatLimiter // per-chat rate limit запросов к провайдеру погоды
	last        *lastCities  // город для /weather без аргумента
}

// NewHandler создаёт обработчик сообщений. botUsername — username своего бота без '@'.
func NewHandler(weather WeatherGetter, sender Sender, botUsername string, log *slog.Logger) *Handler {
	return &Handler{
		weather: weather, sender: sender, botUsername: botUsername, log: log,
		limiter: newChatLimiter(perChatInterval, maxTrackedChats, time.Now),
		last:    newLastCities(maxTrackedChats),
	}
}

// Handle обрабатывает одно сообщение. Паника не роняет бота.
func (h *Handler) Handle(ctx context.Context, chatID int64, text string) {
	defer func() {
		if r := recover(); r != nil {
			h.log.Error("handle message panicked", "chat_id", chatID, "panic", r)
		}
	}()
	cmd, mention, arg := parseCommand(text)
	if mention != "" && !strings.EqualFold(mention, h.botUsername) {
		h.log.Debug("command for another bot ignored", "chat_id", chatID, "mention", mention)
		return // молчим и не тратим rate limit: в группе ответит адресат
	}
	reply := h.respond(ctx, chatID, cmd, arg)
	if err := h.sender.Send(ctx, chatID, reply); err != nil {
		h.log.Warn("send message", "chat_id", chatID, "err", err) // Sender уже вернул redacted-ошибку
	}
}

// respond выбирает ответ. Rate limit защищает только провайдера погоды, поэтому проверяется
// непосредственно перед Current: команды без похода во внешний API отвечают всегда.
// /weather без города повторяет последний успешный город чата.
func (h *Handler) respond(ctx context.Context, chatID int64, cmd, city string) string {
	switch {
	case cmd == "/start":
		return msgStart
	case cmd != "/weather" && cmd != "":
		return msgHelp
	}
	if city == "" {
		last, ok := h.last.Get(chatID)
		if !ok {
			return msgEmptyCity
		}
		city = last
	}
	if !h.limiter.Allow(chatID) {
		return msgTooFrequent
	}
	w, err := h.weather.Current(ctx, city)
	switch {
	case err == nil:
		if len(city) <= maxStoredCityBytes {
			h.last.Set(chatID, city)
		}
		return formatWeather(w)
	case errors.Is(err, domain.ErrInvalidCity):
		return msgInvalidCity
	case errors.Is(err, domain.ErrCityNotFound):
		name := city // fallback, если ошибка пришла без нормализованного имени
		var nf *domain.CityNotFoundError
		if errors.As(err, &nf) {
			name = nf.City
		}
		return msgCityNotFound(name)
	default:
		h.log.Warn("weather request failed", "err", err)
		return msgUnavailable
	}
}

// parseCommand: "/weather@MyBot Казань" → ("/weather", "MyBot", "Казань"); "Казань" → ("", "", "Казань").
// Упоминание ищется только в первом слове команды; текст без '/' — всегда город.
func parseCommand(text string) (cmd, mention, arg string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", "", text
	}
	head, rest := text, ""
	if i := strings.IndexFunc(text, unicode.IsSpace); i >= 0 {
		head, rest = text[:i], text[i:]
	}
	head, mention, _ = strings.Cut(head, "@")
	return strings.ToLower(head), mention, strings.TrimSpace(rest)
}
