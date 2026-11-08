// Package textnorm implements ports.TextNormalizer with golang.org/x/text.
package textnorm

import (
	"golang.org/x/text/unicode/norm"

	"agile-consultant/internal/ports"
)

// Normalizer applies Unicode normalisation forms. The zero value is ready.
type Normalizer struct{}

var _ ports.TextNormalizer = Normalizer{}

// New returns a Normalizer.
func New() Normalizer { return Normalizer{} }

// NFC returns the canonical composition of s.
func (Normalizer) NFC(s string) string { return norm.NFC.String(s) }

// NFKC returns the compatibility composition of s.
func (Normalizer) NFKC(s string) string { return norm.NFKC.String(s) }
