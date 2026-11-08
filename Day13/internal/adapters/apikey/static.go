// Package apikey authenticates clients of the guard proxy with a static
// set of API keys taken from the environment (PROXY_API_KEYS).
package apikey

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"

	"agile-consultant/internal/ports"
)

// Env names the env variable with comma-separated proxy API keys.
const Env = "PROXY_API_KEYS"

// Key constraints.
const (
	MinKeyLen      = 32
	MaxKeyLen      = 256
	MinDistinct    = 10
	MaxKeys        = 16
	MaxTokenLength = 512
	// FingerprintKind is the Fingerprinter kind used for key ids.
	FingerprintKind = "proxy_key"
)

// ErrNoKeys is returned when PROXY_API_KEYS is empty: the proxy must not
// start without authentication.
var ErrNoKeys = errors.New(Env + " is required: the proxy does not start without API keys")

// Static holds SHA-256 hashes of the keys and their precomputed key ids.
// Raw keys are not retained after New returns.
type Static struct {
	hashes [][32]byte
	ids    []string
}

var _ ports.Authenticator = (*Static)(nil)

// New parses raw (the value of PROXY_API_KEYS) and fingerprints every key
// with fp. Errors name only the key index, never the value.
func New(raw string, fp ports.Fingerprinter) (*Static, error) {
	if fp == nil {
		return nil, errors.New("apikey: fingerprinter is required")
	}
	keys, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	s := &Static{hashes: make([][32]byte, len(keys)), ids: make([]string, len(keys))}
	for i, k := range keys {
		s.hashes[i] = sha256.Sum256([]byte(k))
		s.ids[i] = fp.Fingerprint(FingerprintKind, k)
	}
	return s, nil
}

// Parse splits and validates PROXY_API_KEYS: comma-separated, trimmed, no
// empty elements or duplicates, 1..MaxKeys keys of MinKeyLen..MaxKeyLen
// characters from [A-Za-z0-9._~+/=-] with at least MinDistinct distinct
// characters.
func Parse(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, ErrNoKeys
	}
	parts := strings.Split(raw, ",")
	if len(parts) > MaxKeys {
		return nil, fmt.Errorf("%s: at most %d keys, got %d", Env, MaxKeys, len(parts))
	}
	seen := make(map[string]int, len(parts))
	keys := make([]string, 0, len(parts))
	for i, p := range parts {
		k := strings.TrimSpace(p)
		if err := validate(k); err != nil {
			return nil, fmt.Errorf("%s[%d]: %w", Env, i, err)
		}
		if j, dup := seen[k]; dup {
			return nil, fmt.Errorf("%s[%d]: duplicate of %s[%d]", Env, i, Env, j)
		}
		seen[k] = i
		keys = append(keys, k)
	}
	return keys, nil
}

func validate(k string) error {
	switch {
	case k == "":
		return errors.New("empty")
	case len(k) < MinKeyLen:
		return fmt.Errorf("too short (min %d)", MinKeyLen)
	case len(k) > MaxKeyLen:
		return fmt.Errorf("too long (max %d)", MaxKeyLen)
	}
	distinct := map[byte]bool{}
	for i := 0; i < len(k); i++ {
		c := k[i]
		if !allowed(c) {
			return fmt.Errorf("invalid character at position %d (allowed [A-Za-z0-9._~+/=-])", i)
		}
		distinct[c] = true
	}
	if len(distinct) < MinDistinct {
		return fmt.Errorf("low entropy (fewer than %d distinct characters)", MinDistinct)
	}
	return nil
}

func allowed(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("._~+/=-", c) >= 0
}

// Authenticate compares sha256(token) with every key hash in constant time
// (no early exit) and returns the key id of the match. Tokens longer than
// MaxTokenLength are rejected without hashing.
func (s *Static) Authenticate(token string) (string, bool) {
	if token == "" || len(token) > MaxTokenLength {
		return "", false
	}
	h := sha256.Sum256([]byte(token))
	found, idx := 0, 0
	for i := range s.hashes {
		eq := subtle.ConstantTimeCompare(h[:], s.hashes[i][:])
		idx = subtle.ConstantTimeSelect(eq, i, idx)
		found |= eq
	}
	if found != 1 {
		return "", false
	}
	return s.ids[idx], true
}

// Len returns the number of configured keys.
func (s *Static) Len() int { return len(s.hashes) }
