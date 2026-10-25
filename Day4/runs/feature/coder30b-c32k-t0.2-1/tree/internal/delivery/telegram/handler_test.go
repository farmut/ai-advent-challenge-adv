package telegram_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"weatherbot/internal/delivery/telegram"
	"weatherbot/internal/domain"
)

type mockWeatherGetter struct {
	mock.Mock
}

func (m *mockWeatherGetter) Current(ctx context.Context, city string) (domain.Weather, error) {
	args := m.Called(ctx, city)
	return args.Get(0).(domain.Weather), args.Error(1)
}

type mockSender struct {
	mock.Mock
}

func (m *mockSender) Send(ctx context.Context, chatID int64, text string) error {
	args := m.Called(ctx, chatID, text)
	return args.Error(0)
}

func TestHandler_Handle(t *testing.T) {
	t.Run("success /start", func(t *testing.T) {
		sender := new(mockSender)
		sender.On("Send", mock.Anything, int64(123456789), "Привет! Я подскажу погоду. Просто напиши название города, например: Казань").Return(nil)

		handler := telegram.NewHandler(nil, sender, nil)
		handler.Handle(context.Background(), 123456789, "/start")
		sender.AssertExpectations(t)
	})

	t.Run("success /help", func(t *testing.T) {
		sender := new(mockSender)
		sender.On("Send", mock.Anything, int64(123456789), "Доступные команды:\n/weather <город> — показать погоду\n/help — показать помощь\n/start — приветствие").Return(nil)

		handler := telegram.NewHandler(nil, sender, nil)
		handler.Handle(context.Background(), 123456789, "/help")
		sender.AssertExpectations(t)
	})

	t.Run("success /weather with city", func(t *testing.T) {
		weather := new(mockWeatherGetter)
		weather.On("Current", mock.Anything, "Москва").Return(domain.Weather{
			City:        "Москва",
			Country:     "RU",
			TempC:       12.4,
			FeelsLikeC:  9.6,
			Description: "переменная облачность",
			Humidity:    71,
			WindSpeed:   4.2,
			WindDeg:     315,
		}, nil)

		sender := new(mockSender)
		sender.On("Send", mock.Anything, int64(123456789), "🌤 Москва, RU\n🌡 +12 °C, ощущается как +10 °C\n☁️ Переменная облачность\n💧 Влажность 71 %\n💨 Ветер 4 м/с, СЗ").Return(nil)

		handler := telegram.NewHandler(weather, sender, nil)
		handler.Handle(context.Background(), 123456789, "/weather Москва")
		weather.AssertExpectations(t)
		sender.AssertExpectations(t)
	})

	t.Run("success city name without /", func(t *testing.T) {
		weather := new(mockWeatherGetter)
		weather.On("Current", mock.Anything, "Москва").Return(domain.Weather{
			City:        "Москва",
			Country:     "RU",
			TempC:       12.4,
			FeelsLikeC:  9.6,
			Description: "переменная облачность",
			Humidity:    71,
			WindSpeed:   4.2,
			WindDeg:     315,
		}, nil)

		sender := new(mockSender)
		sender.On("Send", mock.Anything, int64(123456789), "🌤 Москва, RU\n🌡 +12 °C, ощущается как +10 °C\n☁️ Переменная облачность\n💧 Влажность 71 %\n💨 Ветер 4 м/с, СЗ").Return(nil)

		handler := telegram.NewHandler(weather, sender, nil)
		handler.Handle(context.Background(), 123456789, "Москва")
		weather.AssertExpectations(t)
		sender.AssertExpectations(t)
	})

	t.Run("invalid city", func(t *testing.T) {
		weather := new(mockWeatherGetter)
		weather.On("Current", mock.Anything, "Москва123").Return(domain.Weather{}, domain.ErrInvalidCity)

		sender := new(mockSender)
		sender.On("Send", mock.Anything, int64(123456789), "Название города может содержать только буквы, пробел, дефис, точку и запятую").Return(nil)

		handler := telegram.NewHandler(weather, sender, nil)
		handler.Handle(context.Background(), 123456789, "/weather Москва123")
		weather.AssertExpectations(t)
		sender.AssertExpectations(t)
	})

	t.Run("city not found", func(t *testing.T) {
		weather := new(mockWeatherGetter)
		weather.On("Current", mock.Anything, "Москва").Return(domain.Weather{}, domain.ErrCityNotFound)

		sender := new(mockSender)
		sender.On("Send", mock.Anything, int64(123456789), "🤔 Не нашёл город «Москва». Проверь написание или попробуй по-английски").Return(nil)

		handler := telegram.NewHandler(weather, sender, nil)
		handler.Handle(context.Background(), 123456789, "/weather Москва")
		weather.AssertExpectations(t)
		sender.AssertExpectations(t)
	})

	t.Run("too frequent", func(t *testing.T) {
		// Сначала разрешаем запрос, потом запрещаем из-за rate limit
		weather := new(mockWeatherGetter)
		weather.On("Current", mock.Anything, "Москва").Return(domain.Weather{
			City:        "Москва",
			Country:     "RU",
			TempC:       12.4,
			FeelsLikeC:  9.6,
			Description: "переменная облачность",
			Humidity:    71,
			WindSpeed:   4.2,
			WindDeg:     315,
		}, nil)

		sender := new(mockSender)
		sender.On("Send", mock.Anything, int64(123456789), "🌤 Москва, RU\n🌡 +12 °C, ощущается как +10 °C\n☁️ Переменная облачность\n💧 Влажность 71 %\n💨 Ветер 4 м/с, СЗ").Return(nil)

		handler := telegram.NewHandler(weather, sender, nil)
		// Первый запрос разрешен
		handler.Handle(context.Background(), 123456789, "/weather Москва")
		// Второй запрос заблокирован из-за rate limit
		handler.Handle(context.Background(), 123456789, "/weather Москва")
		// Проверим, что первый запрос был обработан
		weather.AssertExpectations(t)
		sender.AssertExpectations(t)
	})
}

func TestHandler_respond(t *testing.T) {
	t.Run("empty command", func(t *testing.T) {
		weather := new(mockWeatherGetter)
		weather.On("Current", mock.Anything, "").Return(domain.Weather{}, nil)

		handler := telegram.NewHandler(weather, nil, nil)
		result := handler.respond(context.Background(), "")
		assert.Equal(t, "Напиши город после команды, например: /weather Пермь", result)
	})

	t.Run("unknown command", func(t *testing.T) {
		handler := telegram.NewHandler(nil, nil, nil)
		result := handler.respond(context.Background(), "/unknown")
		assert.Equal(t, "Доступные команды:\n/weather <город> — показать погоду\n/help — показать помощь\n/start — приветствие", result)
	})
}

func TestHandler_parseCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantCmd  string
		wantCity string
	}{
		{"with @", "/weather@MyBot Казань", "/weather", "Казань"},
		{"without @", "/weather Казань", "/weather", "Казань"},
		{"without command", "Казань", "", "Казань"},
		{"empty", "", "", ""},
		{"with extra spaces", "  /weather   Москва  ", "/weather", "Москва"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd, city := telegram.ParseCommand(tt.input)
			assert.Equal(t, tt.wantCmd, cmd)
			assert.Equal(t, tt.wantCity, city)
		})
	}
}