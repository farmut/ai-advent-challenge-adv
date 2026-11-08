package textnorm

import "testing"

func TestNormalizer(t *testing.T) {
	n := New()
	tests := []struct {
		name, in, nfc, nfkc string
	}{
		{"fullwidth special token", "\uFF1C\uFF5Cim_start\uFF5C\uFF1E", "\uFF1C\uFF5Cim_start\uFF5C\uFF1E", "<|im_start|>"},
		{"decomposed yo composes", "е\u0308", "ё", "ё"},
		{"ligature", "\uFB01", "\uFB01", "fi"},
		{"plain", "Спринт", "Спринт", "Спринт"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := n.NFC(tc.in); got != tc.nfc {
				t.Errorf("NFC = %q, want %q", got, tc.nfc)
			}
			if got := n.NFKC(tc.in); got != tc.nfkc {
				t.Errorf("NFKC = %q, want %q", got, tc.nfkc)
			}
		})
	}
}
