package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"weatherbot/internal/domain"
)

type fakeProvider struct {
	lastCity string
	weather  domain.Weather
	err      error
}

func (f *fakeProvider) Current(_ context.Context, city string) (domain.Weather, error) {
	f.lastCity = city
	return f.weather, f.err
}

func TestCurrentNormalizesCity(t *testing.T) {
	f := &fakeProvider{weather: domain.Weather{City: "Москва"}}
	s := NewWeatherService(f)
	if _, err := s.Current(context.Background(), "  Москва   "); err != nil {
		t.Fatalf("Current() err = %v", err)
	}
	if f.lastCity != "Москва" {
		t.Errorf("provider got city %q, want %q", f.lastCity, "Москва")
	}
}

func TestCurrentInvalidCity(t *testing.T) {
	f := &fakeProvider{}
	s := NewWeatherService(f)
	tests := []string{
		"",
		"   ",
		"city 123",
		"city/with/slash",
	}
	for _, in := range tests {
		if _, err := s.Current(context.Background(), in); !errors.Is(err, domain.ErrInvalidCity) {
			t.Errorf("Current(%q) err = %v, want ErrInvalidCity", in, err)
		}
	}
	if f.lastCity != "" {
		t.Errorf("provider called with %q for invalid input", f.lastCity)
	}
}

func TestCurrentAllowsLettersAndPunctuation(t *testing.T) {
	f := &fakeProvider{weather: domain.Weather{City: "Saint-Petersburg"}}
	s := NewWeatherService(f)
	for _, in := range []string{"Москва", "Saint-Petersburg", "New York", "St. Petersburg", "A,B", "café"} {
		if _, err := s.Current(context.Background(), in); err != nil {
			t.Errorf("Current(%q) err = %v, want nil", in, err)
		}
	}
}

func TestCurrentTooLong(t *testing.T) {
	f := &fakeProvider{}
	s := NewWeatherService(f)
	long := strings.Repeat("bcdefghi", 9) // 72 буквы
	if _, err := s.Current(context.Background(), long); !errors.Is(err, domain.ErrInvalidCity) {
		t.Errorf("Current(long) err = %v, want ErrInvalidCity", err)
	}
}

func TestCurrentWrapsProviderError(t *testing.T) {
	f := &fakeProvider{err: domain.ErrCityNotFound}
	s := NewWeatherService(f)
	_, err := s.Current(context.Background(), "Нигдеград")
	if !errors.Is(err, domain.ErrCityNotFound) {
		t.Fatalf("Current() err = %v, want ErrCityNotFound", err)
	}
	if !strings.Contains(err.Error(), "get current weather") {
		t.Errorf("err = %q, want wrapped with context", err)
	}
}
