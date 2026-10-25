package domain

import "errors"

var (
	ErrInvalidCity         = errors.New("invalid city")
	ErrCityNotFound        = errors.New("city not found")
	ErrProviderAuth        = errors.New("weather provider auth failed")
	ErrRateLimited         = errors.New("weather provider rate limited")
	ErrProviderUnavailable = errors.New("weather provider unavailable")
)
