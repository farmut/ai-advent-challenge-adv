// Package telegram содержит функции форматирования погоды.
package telegram

import (
	"fmt"
	"math"
	"strings"

	"weatherbot/internal/domain"
)

const (
	windDirections = "С, СВ, В, ЮВ, Ю, ЮЗ, З, СЗ"
)

// formatWeather форматирует погоду в читаемый вид.
func formatWeather(w domain.Weather) string {
	temp := formatTemp(w.TempC)
	feelsLike := formatTemp(w.FeelsLikeC)
	windDir := windDirection(w.WindDeg)
	windSpeed := formatWindSpeed(w.WindSpeed)

	var sb strings.Builder
	sb.WriteString("🌤 ")
	sb.WriteString(w.City)
	sb.WriteString(", ")
	sb.WriteString(w.Country)
	sb.WriteString("\n")
	sb.WriteString("🌡 ")
	sb.WriteString(temp)
	sb.WriteString(" °C, ощущается как ")
	sb.WriteString(feelsLike)
	sb.WriteString(" °C\n")
	sb.WriteString("☁️ ")
	sb.WriteString(strings.Title(w.Description))
	sb.WriteString("\n")
	sb.WriteString("💧 Влажность ")
	sb.WriteString(fmt.Sprintf("%d", w.Humidity))
	sb.WriteString(" %\n")
	sb.WriteString("💨 Ветер ")
	sb.WriteString(windSpeed)
	sb.WriteString(" м/с, ")
	sb.WriteString(windDir)

	return sb.String()
}

func formatTemp(temp float64) string {
	sign := "+"
	if temp < 0 {
		sign = "-"
	}
	return fmt.Sprintf("%s%.0f", sign, math.Round(temp))
}

func formatWindSpeed(speed float64) string {
	return fmt.Sprintf("%.0f", math.Round(speed))
}

func windDirection(degrees int) string {
	directions := strings.Split(windDirections, ", ")
	index := (degrees + 22) / 45 % 8
	return directions[index]
}