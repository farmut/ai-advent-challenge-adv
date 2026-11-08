package ports

// TextNormalizer applies Unicode normalisation forms.
type TextNormalizer interface {
	// NFC returns the canonical composition of s.
	NFC(s string) string
	// NFKC returns the compatibility composition of s (folds fullwidth
	// forms, ligatures and similar look-alikes).
	NFKC(s string) string
}

// NonceSource produces unpredictable per-request nonces.
type NonceSource interface {
	// Nonce returns 32 lowercase hex characters (128 bits) or an error.
	// Callers must fail closed on error.
	Nonce() (string, error)
}
