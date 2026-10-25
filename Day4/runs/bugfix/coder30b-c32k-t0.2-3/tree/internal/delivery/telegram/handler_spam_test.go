package telegram

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"weatherbot/internal/domain"
)

// Тест для проверки поведения при спам-всплеске
func TestHandlerSpam(t *testing.T) {
	// Создаем фейковый провайдер погоды, который всегда возвращает успешный результат
	fakeWeather := &fakeWeather{
		w: domain.Weather{
			City:        "Москва",
			Country:     "RU",
			TempC:       12.4,
			FeelsLikeC:  9.6,
			Description: "ясно",
			Humidity:    71,
			WindSpeed:   4.2,
			WindDeg:     315,
		},
	}

	fakeSender := &fakeSender{}
	handler := NewHandler(fakeWeather, fakeSender, slog.New(slog.DiscardHandler))

	// Отправляем 10 сообщений подряд в одном чате
	for i := 0; i < 10; i++ {
		handler.Handle(context.Background(), 1, "Москва")
	}

	// Проверяем результаты
	sentCount := len(fakeSender.sent)

	// В текущей реализации все 10 сообщений будут обработаны, что является багом
	// Ожидается:
	// 1. Одно сообщение с погодой
	// 2. 9 сообщений с "Не так быстро"

	// Подсчет типов сообщений
	weatherCount := 0
	tooFrequentCount := 0

	for _, msg := range fakeSender.sent {
		if strings.Contains(msg, "🌤") {
			weatherCount++
		} else if strings.Contains(msg, "Не так быстро") {
			tooFrequentCount++
		}
	}

	t.Logf("Отправлено сообщений: %d", sentCount)
	t.Logf("Сообщений с погодой: %d", weatherCount)
	t.Logf("Сообщений 'Не так быстро': %d", tooFrequentCount)

	// Проверяем, что проблема существует
	if tooFrequentCount == 10 {
		t.Errorf("БАГ: Все 10 сообщений получили 'Не так быстро' - это неправильно!")
		t.Logf("Это означает, что все сообщения в спам-всплеске обрабатываются, а не только первое")
	}

	// Проверяем, что есть хотя бы одно сообщение с погодой
	if weatherCount == 0 {
		t.Errorf("Ни одно сообщение не содержит погоды")
	}

	// Проверяем, что есть только одно сообщение с погодой (это и есть корректное поведение)
	if weatherCount != 1 {
		t.Errorf("Ожидалось только одно сообщение с погодой, получено %d", weatherCount)
	}

	// Проверяем, что остальные 9 - это "Не так быстро"
	if tooFrequentCount != 9 {
		t.Errorf("Ожидалось 9 сообщений 'Не так быстро', получено %d", tooFrequentCount)
	}
}
