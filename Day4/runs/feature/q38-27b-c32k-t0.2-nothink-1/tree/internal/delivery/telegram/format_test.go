package telegram

import (
	"testing"

	"weatherbot/internal/domain"
)

func TestFormatWeatherReference(t *testing.T) {
	w := domain.Weather{
		City: "Москва", Country: "RU",
		TempC: 12.4, FeelsLikeC: 9.6,
		Description: "переменная облачность",
		Humidity:    71, WindSpeed: 4.2, WindDeg: 315,
	}
	want := "🌤 Москва, RU\n🌡 +12 °C, ощущается как +10 °C\n☁️ Переменная облачность\n💧 Влажность 71 %\n💨 Ветер 4 м/с, СЗ"
	if got := formatWeather(w); got != want {
		t.Errorf("formatWeather() =\n%q\nwant\n%q", got, want)
	}
}

func TestFormatTemp(t *testing.T) {
	tests := []struct {
		in   float64
		want string
	}{
		{12.4, "+12 °C"},
		{12.5, "+13 °C"},
		{-3.2, "-3 °C"},
		{-3.5, "-4 °C"},
		{0, "0 °C"},
		{0.4, "0 °C"},
		{-0.4, "0 °C"},
	}
	for _, tt := range tests {
		if got := formatTemp(tt.in); got != tt.want {
			t.Errorf("formatTemp(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCompass(t *testing.T) {
	tests := []struct {
		deg  int
		want string
	}{
		{0, "С"},
		{22, "С"},
		{45, "СВ"},
		{90, "В"},
		{135, "ЮВ"},
		{180, "Ю"},
		{225, "ЮЗ"},
		{270, "З"},
		{315, "СЗ"},
		{359, "С"},
		{360, "С"},
		{-45, "СЗ"},
	}
	for _, tt := range tests {
		if got := compass(tt.deg); got != tt.want {
			t.Errorf("compass(%d) = %q, want %q", tt.deg, got, tt.want)
		}
	}
}

func TestCapitalizeFirst(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"ясно", "Ясно"},
		{"Ясно", "Ясно"},
		{"  дождь  ", "Дождь"},
		{"", ""},
		{"облачно", "Облачно"},
	}
	for _, tt := range tests {
		if got := capitalizeFirst(tt.in); got != tt.want {
			t.Errorf("capitalizeFirst(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
