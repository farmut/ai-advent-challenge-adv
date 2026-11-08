package injection

import (
	"fmt"
	"strings"
	"unicode"
)

// Unicode Tags block (ASCII smuggling).
const (
	tagFirstPrintable = 0xE0020
	tagLastPrintable  = 0xE007E
	tagBase           = 0xE0000
)

// IsInvisible reports whether r renders as nothing (or as a plain space) and
// must therefore be shown as a placeholder in reports: format characters
// (zero-width, bidi controls, soft hyphen, BOM, Unicode Tags), controls
// other than newline and tab, variation selectors, non-breaking spaces and
// filler characters.
func IsInvisible(r rune) bool {
	switch {
	case r == '\n' || r == '\t':
		return false
	case unicode.Is(unicode.Cf, r), unicode.Is(unicode.Cc, r):
		return true
	case r >= 0xFE00 && r <= 0xFE0F, r >= 0xE0100 && r <= 0xE01EF:
		return true
	case r == 0x00A0, r == 0x202F, r == 0x2007, r == 0x034F, r == 0x180E,
		r == 0x115F, r == 0x1160, r == 0x3164, r == 0xFFA0:
		return true
	}
	return false
}

// RevealInvisible makes invisible characters visible for reports: each one
// becomes "[U+XXXX]" and a run of printable Unicode Tags characters becomes
// `[TAGS:"<decoded ASCII>"]`. The result contains no invisible or bidi
// characters, so reports cannot be visually spoofed by the payloads they
// display.
func RevealInvisible(s string) string {
	if !strings.ContainsFunc(s, IsInvisible) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 16)
	var tags strings.Builder
	flush := func() {
		if tags.Len() > 0 {
			fmt.Fprintf(&b, "[TAGS:%q]", tags.String())
			tags.Reset()
		}
	}
	for _, r := range s {
		if r >= tagFirstPrintable && r <= tagLastPrintable {
			tags.WriteRune(r - tagBase)
			continue
		}
		flush()
		if IsInvisible(r) {
			fmt.Fprintf(&b, "[U+%04X]", r)
			continue
		}
		b.WriteRune(r)
	}
	flush()
	return b.String()
}
