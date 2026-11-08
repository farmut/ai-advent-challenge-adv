package audit

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"

	"agile-consultant/internal/ports"
)

// PepperEnv names the env variable with the HMAC key.
const PepperEnv = "AUDIT_PEPPER"

// MinPepperLen is the minimal pepper length.
const MinPepperLen = 16

// HMAC fingerprints values with HMAC-SHA256 and keeps 12 hex characters.
type HMAC struct{ key []byte }

var _ ports.Fingerprinter = (*HMAC)(nil)

// NewHMAC returns a fingerprinter keyed with pepper. An empty pepper gets a
// random per-process key (fingerprints are then not comparable across
// restarts) and a WARN; a pepper shorter than MinPepperLen is an error.
func NewHMAC(pepper string, log *slog.Logger) (*HMAC, error) {
	if pepper == "" {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("audit: random pepper: %w", err)
		}
		if log != nil {
			log.Warn("AUDIT_PEPPER is not set; using a random key, fingerprints will not match across restarts")
		}
		return &HMAC{key: key}, nil
	}
	if len(pepper) < MinPepperLen {
		return nil, fmt.Errorf("audit: %s must be at least %d characters", PepperEnv, MinPepperLen)
	}
	return &HMAC{key: []byte(pepper)}, nil
}

// Fingerprint returns hex(HMAC-SHA256(key, kind+":"+value))[:12].
func (h *HMAC) Fingerprint(kind, value string) string {
	m := hmac.New(sha256.New, h.key)
	m.Write([]byte(kind + ":" + value))
	return hex.EncodeToString(m.Sum(nil))[:12]
}
