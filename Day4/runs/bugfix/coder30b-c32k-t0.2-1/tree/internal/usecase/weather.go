// Package usecase содержит бизнес-логику бота.
package usecase

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"weatherbot/internal/domain"
)

const maxCityRunes = 64

// WeatherProvider — источник текущей погоды.
type WeatherProvider interface {
	Current(ctx context.Context, city string) (domain.Weather, error)
}

// WeatherService отдаёт погоду по названию города.
type WeatherService struct {
	provider WeatherProvider
}

// NewWeatherService создаёт сервис поверх провайдера погоды.
func NewWeatherService(provider WeatherProvider) *WeatherService {
	return &WeatherService{provider: provider}
}

// Current валидирует город и запрашивает погоду у провайдера.
func (s *WeatherService) Current(ctx context.Context, rawCity string) (domain.Weather, error) {
	city, err := normalizeCity(rawCity)
	if err != nil {
		return domain.Weather{}, err
	}
	w, err := s.provider.Current(ctx, city)
	if err != nil {
		return domain.Weather{}, fmt.Errorf("get current weather: %w", err)
	}
	return w, nil
}

func normalizeCity(raw string) (string, error) {
	if !utf8.ValidString(raw) {
		return "", domain.ErrInvalidCity
	}
	city := strings.Join(strings.Fields(raw), " ")
	if n := utf8.RuneCountInString(city); n == 0 || n > maxCityRunes {
		return "", domain.ErrInvalidCity
	}
	for _, r := range city {
		if !unicode.IsLetter(r) && !strings.ContainsRune(" -.,", r) {
			return "", domain.ErrInvalidCity // заодно отсекает управляющие символы
		}
	}
	return city, nil
}
