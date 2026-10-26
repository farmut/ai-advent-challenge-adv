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
	b.WriteString("\n💨 " + formatWind(w.WindSpeed, w.WindDeg))
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

// formatWind: 4.2 м/с, 315° → "Ветер 4 м/с, СЗ"; < 0.5 м/с → "Штиль" (направление при штиле бессмысленно).
func formatWind(speed float64, deg int) string {
	n := int(math.Round(speed))
	if n <= 0 {
		return msgCalm
	}
	return "Ветер " + strconv.Itoa(n) + " м/с, " + windDirection(deg)
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
