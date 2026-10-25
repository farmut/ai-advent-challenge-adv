package telegram_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"weatherbot/internal/delivery/telegram"
)

func TestFormatWeather(t *testing.T) {
	tests := []struct {
		name     string
		weather  telegram.Weather
		expected string
	}{
		{
			name: "success",
			weather: telegram.Weather{
				City:        "Москва",
				Country:     "RU",
				TempC:       12.4,
				FeelsLikeC:  9.6,
				Description: "переменная облачность",
				Humidity:    71,
				WindSpeed:   4.2,
				WindDeg:     315,
			},
			expected: "🌤 Москва, RU\n🌡 +12 °C, ощущается как +10 °C\n☁️ Переменная облачность\n💧 Влажность 71 %\n💨 Ветер 4 м/с, СЗ",
		},
		{
			name: "negative temperature",
			weather: telegram.Weather{
				City:        "Санкт-Петербург",
				Country:     "RU",
				TempC:       -5.7,
				FeelsLikeC:  -8.2,
				Description: "облачно",
				Humidity:    80,
				WindSpeed:   3.1,
				WindDeg:     90,
			},
			expected: "🌤 Санкт-Петербург, RU\n🌡 -6 °C, ощущается как -8 °C\n☁️ Облачно\n💧 Влажность 80 %\n💨 Ветер 3 м/с, В",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := telegram.FormatWeather(tt.weather)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatTemp(t *testing.T) {
	tests := []struct {
		name     string
		temp     float64
		expected string
	}{
		{"positive", 12.4, "+12"},
		{"negative", -5.7, "-6"},
		{"zero", 0, "+0"},
		{"negative zero", -0.1, "-0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := telegram.FormatTemp(tt.temp)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatWindSpeed(t *testing.T) {
	tests := []struct {
		name     string
		speed    float64
		expected string
	}{
		{"integer", 4.0, "4"},
		{"rounded up", 4.7, "5"},
		{"rounded down", 4.2, "4"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := telegram.FormatWindSpeed(tt.speed)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestWindDirection(t *testing.T) {
	tests := []struct {
		name     string
		deg      int
		expected string
	}{
		{"north", 0, "С"},
		{"north-east", 45, "СВ"},
		{"east", 90, "В"},
		{"south-east", 135, "ЮВ"},
		{"south", 180, "Ю"},
		{"south-west", 225, "ЮЗ"},
		{"west", 270, "З"},
		{"north-west", 315, "СЗ"},
		{"between north and north-east", 22, "С"},
		{"between north-east and east", 67, "СВ"},
		{"between east and south-east", 112, "В"},
		{"between south-east and south", 157, "ЮВ"},
		{"between south and south-west", 202, "Ю"},
		{"between south-west and west", 247, "ЮЗ"},
		{"between west and north-west", 292, "З"},
		{"between north-west and north", 337, "СЗ"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := telegram.WindDirection(tt.deg)
			assert.Equal(t, tt.expected, result)
		})
	}
}