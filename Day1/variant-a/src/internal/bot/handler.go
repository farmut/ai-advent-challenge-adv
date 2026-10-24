package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"weatherbot/internal/weather"
)

// Самое длинное название населённого пункта в мире ~85 символов; всё длиннее — мусор.
const maxCityLength = 100

const (
	helpText = "Я присылаю текущую погоду.\n\n" +
		"Отправьте название города, например: Москва\n" +
		"или команду: /weather Санкт-Петербург"
	askCityText     = "Укажите город: /weather Москва"
	unknownCmdText  = "Неизвестная команда. Наберите /help"
	cityTooLongText = "Слишком длинное название города."
	rateLimitedText = "Сервис погоды перегружен, попробуйте через минуту."
	failureText     = "Не удалось получить погоду, попробуйте позже."
)

// Reply формирует ответ на текст сообщения пользователя.
func (b *Bot) Reply(ctx context.Context, text string) string {
	cmd, arg := parseCommand(text)
	switch cmd {
	case "":
		return b.weatherReply(ctx, arg)
	case "start", "help":
		return helpText
	case "weather":
		if arg == "" {
			return askCityText
		}
		return b.weatherReply(ctx, arg)
	default:
		return unknownCmdText
	}
}

// parseCommand разбирает "/cmd@BotName arg" на ("cmd", "arg").
// Для обычного текста возвращает пустую команду и весь текст как аргумент.
func parseCommand(text string) (cmd, arg string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", text
	}
	head, rest, _ := strings.Cut(text, " ")
	head, _, _ = strings.Cut(strings.TrimPrefix(head, "/"), "@")
	return strings.ToLower(head), strings.TrimSpace(rest)
}

func (b *Bot) weatherReply(ctx context.Context, city string) string {
	if utf8.RuneCountInString(city) > maxCityLength {
		return cityTooLongText
	}

	ctx, cancel := context.WithTimeout(ctx, b.requestTimeout)
	defer cancel()

	w, err := b.weather.Current(ctx, city)
	switch {
	case err == nil:
		return formatWeather(w, b.units)
	case errors.Is(err, weather.ErrCityNotFound):
		return fmt.Sprintf("Город «%s» не найден. Проверьте название.", city)
	case errors.Is(err, weather.ErrRateLimited):
		b.log.Warn("weather rate limited", "city", city)
		return rateLimitedText
	default:
		b.log.Error("weather request failed", "city", city, "error", err)
		return failureText
	}
}
