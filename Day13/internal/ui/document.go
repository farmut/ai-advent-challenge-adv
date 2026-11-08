package ui

import (
	"errors"
	"fmt"
	"io"
	"os"
	"unicode/utf8"
)

// Errors returned by LoadDocument.
var (
	ErrDocumentTooLarge = errors.New("document too large")
	ErrDocumentEncoding = errors.New("document is not valid UTF-8")
)

// LoadDocument reads a UTF-8 text document of at most maxBytes bytes and
// maxRunes runes. Content is returned as is; sanitisation is the guard's job.
func LoadDocument(path string, maxBytes int64, maxRunes int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open document: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return "", fmt.Errorf("read document: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return "", fmt.Errorf("%w: more than %d bytes", ErrDocumentTooLarge, maxBytes)
	}
	if !utf8.Valid(data) {
		return "", ErrDocumentEncoding
	}
	if n := utf8.RuneCount(data); n > maxRunes {
		return "", fmt.Errorf("%w: %d > %d characters", ErrDocumentTooLarge, n, maxRunes)
	}
	return string(data), nil
}
