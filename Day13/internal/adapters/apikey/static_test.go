package apikey

import (
	"strings"
	"testing"
)

// Synthetic keys only.
const (
	keyA = "test-proxy-key-A-0123456789abcdefghijKLMN"
	keyB = "test-proxy-key-B-ZYXWVUTSRQ9876543210qrst"
)

type fakeFP struct{}

func (fakeFP) Fingerprint(kind, v string) string { return kind + "#" + v[len(v)-4:] }

func TestParse(t *testing.T) {
	tests := []struct {
		name, raw, err string
		n              int
	}{
		{"one", keyA, "", 1},
		{"two trimmed", " " + keyA + " ,\t" + keyB + " ", "", 2},
		{"empty env", "  ", "is required", 0},
		{"empty element", keyA + ",,", "PROXY_API_KEYS[1]: empty", 0},
		{"trailing comma", keyA + ",", "PROXY_API_KEYS[1]: empty", 0},
		{"short", keyA + ",short-key-0123456789", "PROXY_API_KEYS[1]: too short", 0},
		{"long", strings.Repeat("abcdefghij", 26), "PROXY_API_KEYS[0]: too long", 0},
		{"bad char", "test-proxy-key-0123456789abcdef ghijk", "PROXY_API_KEYS[0]: invalid character", 0},
		{"unicode", "test-proxy-key-0123456789abcdefghijké", "PROXY_API_KEYS[0]: invalid character", 0},
		{"low entropy", strings.Repeat("ab", 20), "PROXY_API_KEYS[0]: low entropy", 0},
		{"duplicate", keyA + "," + keyB + "," + keyA, "PROXY_API_KEYS[2]: duplicate of PROXY_API_KEYS[0]", 0},
		{"too many", strings.Repeat(keyA+",", 16) + keyA, "at most 16 keys", 0},
		{"allowed specials", "AbCdEfGhIj0123456789._~+/=-xyzXYZ", "", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, err := Parse(tt.raw)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("err = %v, want %q", err, tt.err)
				}
				for _, k := range []string{keyA, keyB, "short-key-0123456789"} {
					if strings.Contains(err.Error(), k) {
						t.Fatal("error must not contain the key value")
					}
				}
				return
			}
			if err != nil || len(keys) != tt.n {
				t.Fatalf("keys %d err %v", len(keys), err)
			}
		})
	}
}

func TestAuthenticate(t *testing.T) {
	s, err := New(keyA+","+keyB, fakeFP{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, token, id string
		ok              bool
	}{
		{"first", keyA, "proxy_key#KLMN", true},
		{"second", keyB, "proxy_key#qrst", true},
		{"wrong", keyA[:len(keyA)-1] + "X", "", false},
		{"prefix", keyA[:20], "", false},
		{"empty", "", "", false},
		{"too long", strings.Repeat("a", MaxTokenLength+1), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := s.Authenticate(tt.token)
			if id != tt.id || ok != tt.ok {
				t.Fatalf("got (%q, %v), want (%q, %v)", id, ok, tt.id, tt.ok)
			}
		})
	}
	if s.Len() != 2 {
		t.Fatal("len")
	}
}

func TestNewRequiresFingerprinter(t *testing.T) {
	if _, err := New(keyA, nil); err == nil {
		t.Fatal("nil fingerprinter must fail")
	}
}

func BenchmarkAuthenticate(b *testing.B) {
	var keys []string
	for i := 0; i < MaxKeys; i++ {
		keys = append(keys, keyA[:len(keyA)-2]+string(rune('a'+i))+"Z")
	}
	s, err := New(strings.Join(keys, ","), fakeFP{})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		s.Authenticate(keyB)
	}
}
