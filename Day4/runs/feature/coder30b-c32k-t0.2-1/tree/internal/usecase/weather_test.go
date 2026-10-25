package usecase

import (
	"context"
	"errors"
	"testing"

	"weatherbot/internal/domain"
)

type mockProvider struct {
	weather domain.Weather
	err     error
}

func (m *mockProvider) Current(ctx context.Context, city string) (domain.Weather, error) {
	return m.weather, m.err
}

func TestWeatherService_Current(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		service := NewWeatherService(&mockProvider{
			weather: domain.Weather{City: "Москва", Country: "RU"},
			err:     nil,
		})

		w, err := service.Current(context.Background(), "Москва")
		if err != nil {
			t.Fatalf("Current() error = %v", err)
		}
		if w.City != "Москва" {
			t.Errorf("Current() City = %v, want Москва", w.City)
		}
		if w.Country != "RU" {
			t.Errorf("Current() Country = %v, want RU", w.Country)
		}
	})

	t.Run("invalid city - empty", func(t *testing.T) {
		service := NewWeatherService(&mockProvider{})

		w, err := service.Current(context.Background(), "")
		if err == nil {
			t.Fatalf("Current() error = nil, want error")
		}
		if !errors.Is(err, domain.ErrInvalidCity) {
			t.Errorf("Current() err = %v, want %v", err, domain.ErrInvalidCity)
		}
		if w != (domain.Weather{}) {
			t.Errorf("Current() weather = %v, want empty weather", w)
		}
	})

	t.Run("invalid city - too long", func(t *testing.T) {
		service := NewWeatherService(&mockProvider{})

		longCity := "a"
		for i := 0; i < 65; i++ {
			longCity += "a"
		}

		w, err := service.Current(context.Background(), longCity)
		if err == nil {
			t.Fatalf("Current() error = nil, want error")
		}
		if !errors.Is(err, domain.ErrInvalidCity) {
			t.Errorf("Current() err = %v, want %v", err, domain.ErrInvalidCity)
		}
		if w != (domain.Weather{}) {
			t.Errorf("Current() weather = %v, want empty weather", w)
		}
	})

	t.Run("invalid city - contains invalid chars", func(t *testing.T) {
		service := NewWeatherService(&mockProvider{})

		w, err := service.Current(context.Background(), "Москва123")
		if err == nil {
			t.Fatalf("Current() error = nil, want error")
		}
		if !errors.Is(err, domain.ErrInvalidCity) {
			t.Errorf("Current() err = %v, want %v", err, domain.ErrInvalidCity)
		}
		if w != (domain.Weather{}) {
			t.Errorf("Current() weather = %v, want empty weather", w)
		}
	})

	t.Run("provider error", func(t *testing.T) {
		service := NewWeatherService(&mockProvider{
			err: domain.ErrCityNotFound,
		})

		w, err := service.Current(context.Background(), "Москва")
		if err == nil {
			t.Fatalf("Current() error = nil, want error")
		}
		if !errors.Is(err, domain.ErrCityNotFound) {
			t.Errorf("Current() err = %v, want %v", err, domain.ErrCityNotFound)
		}
		if w != (domain.Weather{}) {
			t.Errorf("Current() weather = %v, want empty weather", w)
		}
	})
}