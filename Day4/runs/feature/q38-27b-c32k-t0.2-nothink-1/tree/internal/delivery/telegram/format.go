package telegram

import (
	"fmt"
	"math"
	"strings"
	"unicode"

	"weatherbot/internal/domain"
)

// formatWeather превращает погоду в текст для пользователя (plain text).
func formatWeather(w domain.Weather) string {
	return fmt.Sprintf("🌤 %s, %s\n🌡 %s, ощущается как %s\n☁️ %s\n💧 Влажность %d %%\n💨 Ветер %d м/с, %s",
		w.City, w.Country,
		formatTemp(w.TempC), formatTemp(w.FeelsLikeC),
		capitalizeFirst(w.Description),
		w.Humidity,
		int(math.Round(w.WindSpeed)),
		compass(w.WindDeg),
	)
}

// formatTemp: +12, -3, 0 без знака.
func formatTemp(c float64) string {
	v := int(math.Round(c))
	switch {
	case v > 0:
		return fmt.Sprintf("+%d °C", v)
	case v < 0:
		return fmt.Sprintf("%d °C", v)
	default:
		return "0 °C"
	}
}

var compassPoints = [8]string{"С", "СВ", "В", "ЮВ", "Ю", "ЮЗ", "З", "СЗ"}

// compass: градусы → 8 румбов.
func compass(deg int) string {
	idx := ((deg % 360) + 360) % 360
	return compassPoints[(idx+22)/45%8]
}

// capitalizeFirst — первая буква описания с заглавной (по рунам).
func capitalizeFirst(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	r := []rune(s)
	if !unicode.IsUpper(r[0]) {
		r[0] = unicode.ToUpper(r[0])
	}
	return string(r)
}
