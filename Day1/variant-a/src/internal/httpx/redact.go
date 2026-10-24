// Package httpx содержит общие HTTP-утилиты.
package httpx

import (
	"errors"
	"net/url"
)

// RedactURLError убирает URL из ошибки транспорта.
//
// net/http оборачивает сетевые ошибки в *url.Error, который включает полный URL
// запроса. У Telegram токен лежит в пути, у OpenWeatherMap ключ — в query,
// поэтому без редактирования секреты попали бы в логи.
func RedactURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return &url.Error{Op: urlErr.Op, URL: "[redacted]", Err: urlErr.Err}
	}
	return err
}
