// Package nonce implements ports.NonceSource with crypto/rand.
package nonce

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"

	"agile-consultant/internal/ports"
)

// Size is the nonce size in bytes (128 bits).
const Size = 16

// Source reads nonces from a cryptographically secure reader.
type Source struct {
	r io.Reader
}

var _ ports.NonceSource = Source{}

// New returns a Source backed by crypto/rand.
func New() Source { return Source{r: rand.Reader} }

// Nonce returns 32 lowercase hex characters.
func (s Source) Nonce() (string, error) {
	r := s.r
	if r == nil {
		r = rand.Reader
	}
	var b [Size]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return "", fmt.Errorf("read nonce: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
