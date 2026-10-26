package telegram

import (
	"strings"
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
	want := "🌤 Нигде\n🌡 0 °C, ощущается как -3 °C\n💧 Влажность 0 %\n💨 Штиль"
	if got != want {
		t.Errorf("formatWeather() =\n%s\nwant\n%s", got, want)
	}
}

func TestFormatWind(t *testing.T) {
	tests := []struct {
		name  string
		speed float64
		deg   int
		want  string
	}{
		{name: "zero", speed: 0, deg: 0, want: "Штиль"},
		{name: "rounds to zero", speed: 0.4, deg: 180, want: "Штиль"},
		{name: "just below boundary", speed: 0.49, deg: 90, want: "Штиль"},
		{name: "boundary rounds up", speed: 0.5, deg: 90, want: "Ветер 1 м/с, В"},
		{name: "negative small", speed: -0.4, deg: 0, want: "Штиль"},
		{name: "negative", speed: -3, deg: 45, want: "Штиль"},
		{name: "normal", speed: 4.2, deg: 315, want: "Ветер 4 м/с, СЗ"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatWind(tt.speed, tt.deg); got != tt.want {
				t.Errorf("formatWind(%v, %d) = %q, want %q", tt.speed, tt.deg, got, tt.want)
			}
		})
	}
}

func TestFormatWeatherCalm(t *testing.T) {
	got := formatWeather(domain.Weather{City: "Казань", WindSpeed: 0.4, WindDeg: 0})
	if !strings.HasSuffix(got, "\n💨 Штиль") {
		t.Errorf("formatWeather() =\n%s\nwant suffix \"💨 Штиль\"", got)
	}
	if strings.Contains(got, "м/с") || strings.Contains(got, ", С") {
		t.Errorf("formatWeather() = %q, calm must not contain speed or direction", got)
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
