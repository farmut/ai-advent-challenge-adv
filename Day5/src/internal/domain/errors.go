package domain

import "errors"

// Sentinel-ошибки предметной области. Сравнивать через errors.Is: выше они приходят обёрнутыми.
var (
	// ErrInvalidCity — город не прошёл валидацию в usecase (пустой, >64 рун, недопустимые символы,
	// нет букв, невалидный UTF-8); провайдер не вызывается. Handler отвечает msgInvalidCity, без лога.
	ErrInvalidCity = errors.New("invalid city")
	// ErrCityNotFound — провайдер ответил 404. Usecase оборачивает её в *CityNotFoundError
	// с нормализованным городом; handler отвечает msgCityNotFound(город), без лога.
	ErrCityNotFound = errors.New("city not found")
	// ErrProviderAuth — провайдер ответил 401 (неверный API-ключ). Пользователю не раскрывается:
	// handler логирует WARN и отвечает msgUnavailable.
	ErrProviderAuth = errors.New("weather provider auth failed")
	// ErrRateLimited — провайдер ответил 429 (исчерпана квота). Не путать с per-chat лимитом бота
	// (msgTooFrequent). Handler логирует WARN и отвечает msgUnavailable.
	ErrRateLimited = errors.New("weather provider rate limited")
	// ErrProviderUnavailable — 5xx или иной неожиданный статус, сбой сети или таймаут, битый или
	// обрезанный (>1 MiB) JSON. Handler логирует WARN и отвечает msgUnavailable.
	ErrProviderUnavailable = errors.New("weather provider unavailable")
)

// CityNotFoundError — провайдер не нашёл город. City — нормализованное имя, ушедшее провайдеру.
//
// Тип ошибки, а не экспорт нормализации: usecase — единственный источник истины о том, что
// отправлено провайдеру, а handler зависит только от порта WeatherGetter и не дублирует правило.
// Совместим с errors.Is(err, ErrCityNotFound) через Unwrap.
type CityNotFoundError struct {
	City string
}

// Error не содержит города: ошибка может попасть в логи, а ввод пользователя туда не пишем.
func (e *CityNotFoundError) Error() string { return "city not found" }

// Unwrap связывает тип с sentinel ErrCityNotFound.
func (e *CityNotFoundError) Unwrap() error { return ErrCityNotFound }
