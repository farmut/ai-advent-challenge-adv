package htmlkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

func assertNoInvisible(t *testing.T, name, s string) {
	t.Helper()
	for i, r := range s {
		if r != '\n' && r != '\t' && (IsInvisible(r) || unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Bidi_Control, r)) {
			t.Errorf("%s: raw invisible U+%04X at byte %d", name, r, i)
			return
		}
	}
}

func TestRevealInvisible(t *testing.T) {
	tags := func(s string) string {
		var b strings.Builder
		for _, r := range s {
			b.WriteRune(0xE0000 + r)
		}
		return b.String()
	}
	tests := []struct {
		name, in, want string
	}{
		{"plain unchanged", "Вердикт: ок\n\tшаг", "Вердикт: ок\n\tшаг"},
		{"zwsp", "L\u200bY", "L[U+200B]Y"},
		{"bidi", "a\u202eb\u202cc", "a[U+202E]b[U+202C]c"},
		{"bom and soft hyphen", "\ufeffре\u00adтро", "[U+FEFF]ре[U+00AD]тро"},
		{"nbsp", "в\u00a0конце", "в[U+00A0]конце"},
		{"tags run", "ok" + tags("Hi \"x\"") + "!", `ok[TAGS:"Hi \"x\""]!`},
		{"tag cancel", "\U000E007F", "[U+E007F]"},
		{"vs supplement", "a\U000E0100", "a[U+E0100]"},
		{"control", "a\x1b[31m", "a[U+001B][31m"},
		{"emoji zwj is shown", "👨\u200d👩", "👨[U+200D]👩"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RevealInvisible(tc.in)
			if got != tc.want {
				t.Errorf("RevealInvisible(%q) = %q, want %q", tc.in, got, tc.want)
			}
			assertNoInvisible(t, tc.name, got)
		})
	}
}

func TestCopyDirKeepsOtherFiles(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "dst")
	_ = os.MkdirAll(filepath.Join(src, "cards"), 0o755)
	_ = os.WriteFile(filepath.Join(src, "cards", "a.html"), []byte("new"), 0o644)
	_ = os.WriteFile(filepath.Join(src, "same.txt"), []byte("new"), 0o644)
	_ = os.MkdirAll(dst, 0o755)
	_ = os.WriteFile(filepath.Join(dst, "other.md"), []byte("keep"), 0o644)
	_ = os.WriteFile(filepath.Join(dst, "same.txt"), []byte("old"), 0o644)
	if err := CopyDir(src, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "other.md")); string(b) != "keep" {
		t.Error("unrelated file must be kept")
	}
	for _, p := range []string{"same.txt", filepath.Join("cards", "a.html")} {
		if b, _ := os.ReadFile(filepath.Join(dst, p)); string(b) != "new" {
			t.Errorf("%s not overwritten", p)
		}
	}
}

func TestCopyFiles(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "out")
	p := filepath.Join(src, "report.md")
	_ = os.WriteFile(p, []byte("x"), 0o644)
	if err := CopyFiles(dst, map[string]string{p: "proxy-fake.md"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "proxy-fake.md")); string(b) != "x" {
		t.Error("not copied")
	}
	if err := CopyFiles(dst, map[string]string{filepath.Join(src, "missing"): "m"}); err == nil {
		t.Error("missing source must fail")
	}
}
