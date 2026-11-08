package nonce

import (
	"errors"
	"regexp"
	"testing"
)

var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

func TestNonceFormatAndUniqueness(t *testing.T) {
	src := New()
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		n, err := src.Nonce()
		if err != nil {
			t.Fatal(err)
		}
		if !hex32.MatchString(n) {
			t.Fatalf("nonce %q is not 32 hex chars", n)
		}
		if seen[n] {
			t.Fatalf("duplicate nonce %q", n)
		}
		seen[n] = true
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("entropy unavailable") }

func TestNonceReaderError(t *testing.T) {
	if _, err := (Source{r: failReader{}}).Nonce(); err == nil {
		t.Fatal("expected error from failing reader")
	}
}
