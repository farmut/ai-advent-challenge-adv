package bot

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"weatherbot/internal/weather"
)

// unitLabels — подписи единиц измерения для режимов OpenWeatherMap.
type unitLabels struct {
	temp string
	wind string
}

var labelsByUnits = map[string]unitLabels{
	"metric":   {temp: "°C", wind: "м/с"},
	"imperial": {temp: "°F", wind: "миль/ч"},
	"standard": {temp: "K", wind: "м/с"},
}

func labelsFor(units string) unitLabels {
	if l, ok := labelsByUnits[units]; ok {
		return l
	}
	return labelsByUnits["metric"]
}

// formatWeather превращает данные о погоде в текст ответа.
func formatWeather(w weather.Weather, units string) string {
	l := labelsFor(units)

	place := w.City
	if w.Country != "" {
		place += ", " + w.Country
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Погода: %s\n", place)
	if w.Description != "" {
		fmt.Fprintf(&sb, "%s\n", capitalize(w.Description))
	}
	fmt.Fprintf(&sb, "🌡 Температура: %.1f%s (ощущается как %.1f%s)\n", w.Temp, l.temp, w.FeelsLike, l.temp)
	fmt.Fprintf(&sb, "💧 Влажность: %d%%\n", w.Humidity)
	fmt.Fprintf(&sb, "💨 Ветер: %.1f %s\n", w.WindSpeed, l.wind)
	fmt.Fprintf(&sb, "🧭 Давление: %d гПа", w.Pressure)
	return sb.String()
}

func capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}
