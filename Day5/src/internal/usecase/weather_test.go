package usecase_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"weatherbot/internal/domain"
	"weatherbot/internal/usecase"
)

type fakeProvider struct {
	gotCity string
	calls   int
	err     error
}

func (f *fakeProvider) Current(_ context.Context, city string) (domain.Weather, error) {
	f.calls++
	f.gotCity = city
	if f.err != nil {
		return domain.Weather{}, f.err
	}
	return domain.Weather{City: city}, nil
}

func TestWeatherServiceCurrentValid(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{raw: "Москва", want: "Москва"},
		{raw: "  Нижний   Новгород \n", want: "Нижний Новгород"},
		{raw: "Санкт-Петербург", want: "Санкт-Петербург"},
		{raw: "St. Louis", want: "St. Louis"},
		{raw: "Paris, FR", want: "Paris, FR"},
		{raw: "Ёшкар-Ола", want: "Ёшкар-Ола"},
		{raw: "Ростов-на-Дону", want: "Ростов-на-Дону"},
		{raw: "St. Petersburg", want: "St. Petersburg"},
		{raw: "N'Djamena", want: "N'Djamena"},
		{raw: "Val-d'Or", want: "Val-d'Or"},
		{raw: "Sant'Agata", want: "Sant'Agata"},
		{raw: "N’Djamena", want: "N’Djamena"},
		{raw: strings.Repeat("я", 64), want: strings.Repeat("я", 64)},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			p := &fakeProvider{}
			got, err := usecase.NewWeatherService(p).Current(t.Context(), tt.raw)
			if err != nil {
				t.Fatalf("Current(%q) err = %v", tt.raw, err)
			}
			if p.gotCity != tt.want || got.City != tt.want {
				t.Errorf("provider got %q, result %q, want %q", p.gotCity, got.City, tt.want)
			}
		})
	}
}

func TestWeatherServiceCurrentInvalid(t *testing.T) {
	tests := map[string]string{
		"empty":                        "",
		"only spaces":                  "   \t ",
		"too long":                     strings.Repeat("я", 65),
		"digits":                       "Moscow1",
		"query inject":                 "a&appid=x",
		"slash":                        "../etc",
		"control char":                 "Ka\azan",
		"nul":                          "Kazan\x00",
		"invalid utf-8":                "Kaz\xffan",
		"emoji":                        "Москва😀",
		"only dashes":                  "---",
		"only dot":                     ".",
		"only punct":                   " , . ",
		"only ascii apostrophes":       "''",
		"only typographic apostrophes": "’’",
		"apostrophe with digit":        "N'1",
		"apostrophe query inject":      "a'&appid=x",
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			p := &fakeProvider{}
			_, err := usecase.NewWeatherService(p).Current(t.Context(), raw)
			if !errors.Is(err, domain.ErrInvalidCity) {
				t.Errorf("Current(%q) err = %v, want ErrInvalidCity", raw, err)
			}
			if p.calls != 0 {
				t.Errorf("provider called %d times, want 0", p.calls)
			}
		})
	}
}

func TestWeatherServiceCurrentProviderError(t *testing.T) {
	p := &fakeProvider{err: domain.ErrCityNotFound}
	_, err := usecase.NewWeatherService(p).Current(t.Context(), "Нигдебург")
	if !errors.Is(err, domain.ErrCityNotFound) {
		t.Errorf("Current() err = %v, want wrapped ErrCityNotFound", err)
	}
}

func TestWeatherServiceCurrentCityNotFoundNormalized(t *testing.T) {
	p := &fakeProvider{err: domain.ErrCityNotFound}
	_, err := usecase.NewWeatherService(p).Current(t.Context(), "  Нью   Йорк  ")
	if !errors.Is(err, domain.ErrCityNotFound) {
		t.Fatalf("Current() err = %v, want wrapped ErrCityNotFound", err)
	}
	var nf *domain.CityNotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("Current() err = %v, want *domain.CityNotFoundError", err)
	}
	if nf.City != "Нью Йорк" {
		t.Errorf("CityNotFoundError.City = %q, want %q", nf.City, "Нью Йорк")
	}
}
