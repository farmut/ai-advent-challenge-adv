package domain_test

import (
	"errors"
	"fmt"
	"testing"

	"weatherbot/internal/domain"
)

func TestCityNotFoundError(t *testing.T) {
	err := fmt.Errorf("get current weather: %w", &domain.CityNotFoundError{City: "Нью Йорк"})
	if !errors.Is(err, domain.ErrCityNotFound) {
		t.Errorf("errors.Is(%v, ErrCityNotFound) = false", err)
	}
	var nf *domain.CityNotFoundError
	if !errors.As(err, &nf) || nf.City != "Нью Йорк" {
		t.Fatalf("errors.As() = %v, city %v, want city %q", errors.As(err, &nf), nf, "Нью Йорк")
	}
	if got := nf.Error(); got != "city not found" {
		t.Errorf("Error() = %q, want %q (без города)", got, "city not found")
	}
}
