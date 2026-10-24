package telegram

import (
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"weatherbot/internal/domain"
)

// formatWeather собирает plain-text ответ с погодой.
func formatWeather(w domain.Weather) string {
	var b strings.Builder
	b.WriteString("🌤 " + w.City)
	if w.Country != "" {
		b.WriteString(", " + w.Country)
	}
	b.WriteString("\n🌡 " + formatTemp(w.TempC) + " °C, ощущается как " + formatTemp(w.FeelsLikeC) + " °C")
	if w.Description != "" {
		b.WriteString("\n☁️ " + capitalize(w.Description))
	}
	b.WriteString("\n💧 Влажность " + strconv.Itoa(w.Humidity) + " %")
	b.WriteString("\n💨 Ветер " + strconv.Itoa(int(math.Round(w.WindSpeed))) + " м/с, " + windDirection(w.WindDeg))
	return b.String()
}

// formatTemp: 12.4 → "+12", -2.6 → "-3", -0.4 → "0".
func formatTemp(t float64) string {
	n := int(math.Round(t))
	if n > 0 {
		return "+" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// windDirection переводит градусы в один из 8 румбов; 0 = север.
func windDirection(deg int) string {
	dirs := [...]string{"С", "СВ", "В", "ЮВ", "Ю", "ЮЗ", "З", "СЗ"} // по часовой, от севера
	deg = (deg%360 + 360) % 360
	return dirs[int(math.Round(float64(deg)/45))%len(dirs)]
}

// capitalize делает первую руну заглавной.
func capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}
