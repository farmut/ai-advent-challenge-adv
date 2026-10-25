package telegram

import (
	"testing"

	"weatherbot/internal/domain"
)

func TestFormatWeather(t *testing.T) {
	w := domain.Weather{
		City: "Москва", Country: "RU", TempC: 12.4, FeelsLikeC: 9.6,
		Description: "переменная облачность", Humidity: 71, WindSpeed: 4.2, WindDeg: 315,
	}
	want := "🌤 Москва, RU\n" +
		"🌡 +12 °C, ощущается как +10 °C\n" +
		"☁️ Переменная облачность\n" +
		"💧 Влажность 71 %\n" +
		"💨 Ветер 4 м/с, СЗ"
	if got := formatWeather(w); got != want {
		t.Errorf("formatWeather() =\n%s\nwant\n%s", got, want)
	}
}

func TestFormatWeatherWithoutOptionalFields(t *testing.T) {
	got := formatWeather(domain.Weather{City: "Нигде", TempC: -0.3, FeelsLikeC: -2.6})
	want := "🌤 Нигде\n🌡 0 °C, ощущается как -3 °C\n💧 Влажность 0 %\n💨 Ветер 0 м/с, С"
	if got != want {
		t.Errorf("formatWeather() =\n%s\nwant\n%s", got, want)
	}
}

func TestFormatTemp(t *testing.T) {
	tests := map[float64]string{12.4: "+12", 0.5: "+1", 0.4: "0", 0: "0", -0.4: "0", -0.5: "-1", -2.6: "-3", 30: "+30"}
	for in, want := range tests {
		if got := formatTemp(in); got != want {
			t.Errorf("formatTemp(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestWindDirection(t *testing.T) {
	tests := map[int]string{
		0: "С", 22: "С", 23: "СВ", 45: "СВ", 90: "В", 135: "ЮВ", 180: "Ю",
		225: "ЮЗ", 270: "З", 315: "СЗ", 337: "СЗ", 338: "С", 360: "С", 405: "СВ", -45: "СЗ",
	}
	for deg, want := range tests {
		if got := windDirection(deg); got != want {
			t.Errorf("windDirection(%d) = %q, want %q", deg, got, want)
		}
	}
}

func TestCapitalize(t *testing.T) {
	tests := map[string]string{"ясно": "Ясно", "clear sky": "Clear sky", "": "", "Дождь": "Дождь", "ёлки": "Ёлки"}
	for in, want := range tests {
		if got := capitalize(in); got != want {
			t.Errorf("capitalize(%q) = %q, want %q", in, got, want)
		}
	}
}
