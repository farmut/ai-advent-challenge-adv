// Package domain содержит сущности и ошибки предметной области.
package domain

import "errors"

// Weather — текущая погода в городе.
type Weather struct {
	City        string  // название из ответа провайдера
	Country     string  // ISO 3166-1 alpha-2, например "RU"
	TempC       float64 // температура, °C
	FeelsLikeC  float64 // ощущается как, °C
	Description string  // описание на русском, как прислал провайдер
	Humidity    int     // влажность, %
	WindSpeed   float64 // скорость ветра, м/с
	WindDeg     int     // направление ветра, градусы, 0 = север
}

var (
	ErrInvalidCity         = errors.New("invalid city")
	ErrCityNotFound        = errors.New("city not found")
	ErrProviderAuth        = errors.New("weather provider auth failed")
	ErrRateLimited         = errors.New("weather provider rate limited")
	ErrProviderUnavailable = errors.New("weather provider unavailable")
)