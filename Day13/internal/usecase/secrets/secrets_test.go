package secrets

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"agile-consultant/internal/adapters/textnorm"
)

// All secrets below are synthetic.
const (
	fakeAWSKey    = "AKIA2E0A8F3B244C9986"
	fakeAWSSecret = "Xk3r9Lq2Vb7Nm4Tz8Wc1Yd6Fh0Gj5Ps2Ra9Ue3Qz"
	fakeGitHub    = "ghp_A1b2C3d4E5f6G7h8I9j0K1l2M3n4O5p6Q7r8"
	fakeOpenAI    = "sk-proj-abc123XYZ789"
	fakeCard      = "4276380012345679" // Visa BIN, Luhn-valid, generated
)

func newScanner(depth int) *Scanner {
	return New(textnorm.New(), []string{"AKIAIOSFODNN7EXAMPLE"}, depth)
}

func types(fs []Finding) map[Type]int {
	m := map[Type]int{}
	for _, f := range fs {
		m[f.Type]++
	}
	return m
}

func TestLuhnAndBIN(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{fakeCard, true},
		{"4276 3800 1234 5679", true},
		{"2200123456789019", true},  // MIR
		{"5555444433332226", true},  // Mastercard
		{"4111111111111112", false}, // Luhn fails
		{"1234567812345670", false}, // Luhn ok, unknown BIN
		{"427638001234", false},     // too short
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := validCard(tt.in); got != tt.want {
				t.Fatalf("validCard(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
	if !luhn("1234567812345670") {
		t.Fatal("luhn sanity")
	}
}

func TestDirectRules(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Type // "" means no finding
	}{
		{"openai proj short", "ключ sk-proj-abc123 тут", APIKeyOpenAI},
		{"openai legacy", "sk-" + strings.Repeat("a1B2", 6), APIKeyOpenAI},
		{"openai too short legacy", "sk-abc123", ""},
		{"anthropic", "sk-ant-api03-" + strings.Repeat("Ab1_", 6), APIKeyAnthropic},
		{"github classic", "token " + fakeGitHub, GitHubToken},
		{"github short", "ghp_abc", ""},
		{"github pat", "github_pat_" + strings.Repeat("A1_b", 6), GitHubToken},
		{"aws access", "id=" + fakeAWSKey, AWSAccessKey},
		{"aws lowercase not key", "akia2e0a8f3b244c9986", ""},
		{"aws secret with keyword", "aws_secret_access_key = " + fakeAWSSecret, AWSSecretKey},
		{"aws secret without keyword", "value " + fakeAWSSecret, ""},
		{"slack", "xoxb-1234567890-abcdef", SlackToken},
		{"google", "AIza" + strings.Repeat("Ab1-", 8) + "xyz", GoogleAPIKey},
		{"jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.abcdefghijk", JWT},
		{"pem", "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADANBg\n-----END PRIVATE KEY-----", PrivateKey},
		{"pem unterminated", "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXk", PrivateKey},
		{"card spaced", "карта 4276 3800 1234 5679", Card},
		{"card bad luhn", "заказ 4111 1111 1111 1112", ""},
		{"email", "пишите ivan.petrov@example.com", Email},
		{"not email", "user at example dot com", ""},
		{"phone ru", "+7 (912) 345-67-89", Phone},
		{"phone ru 8", "звоните 8 912 345 67 89", Phone},
		{"phone e164", "call +44 20 7946 0958", Phone},
		{"plain number", "версия 2024 12 01", ""},
		{"allowlisted", "AKIAIOSFODNN7EXAMPLE", ""},
	}
	s := newScanner(2)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := s.Scan(tt.in)
			got := types(r.Findings)
			if tt.want == "" {
				if len(r.Findings) != 0 {
					t.Fatalf("unexpected findings %v", got)
				}
				return
			}
			if got[tt.want] == 0 {
				t.Fatalf("want %s, got %v", tt.want, got)
			}
			for _, f := range r.Findings {
				if f.Derived {
					t.Fatalf("direct case produced derived finding %+v", f)
				}
			}
		})
	}
}

func TestPhoneInsideCardDropped(t *testing.T) {
	r := newScanner(2).Scan("+4276380012345679")
	if got := types(r.Findings); got[Phone] != 0 || got[Card] != 1 {
		t.Fatalf("got %v, want only card", got)
	}
}

func TestViews(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString([]byte(fakeOpenAI))
	b64x2 := base64.StdEncoding.EncodeToString([]byte(b64))
	hexKey := hex.EncodeToString([]byte(fakeAWSKey))
	urlKey := ""
	for _, c := range []byte(fakeGitHub) {
		urlKey += "%" + strings.ToUpper(hex.EncodeToString([]byte{c}))
	}
	tests := []struct {
		name    string
		in      string
		depth   int
		want    Type
		view    View
		derived bool
	}{
		{"zero width", "sk-​proj-QwErTy123456", 2, APIKeyOpenAI, ViewNormalized, false},
		{"fullwidth nfkc", "ＡＫＩＡ2E0A8F3B244C9986", 2, AWSAccessKey, ViewNormalized, false},
		{"joined quotes", `мой ключ: "sk-" + "proj-abc"`, 2, APIKeyOpenAI, ViewJoined, true},
		{"joined plus chain", "AKIA2E0A + 8F3B244C + 9986", 2, AWSAccessKey, ViewJoined, true},
		{"joined prefix space", "sk- proj-abc123", 2, APIKeyOpenAI, ViewJoined, true},
		{"base64 depth1", "секрет: " + b64, 1, APIKeyOpenAI, ViewDecoded, true},
		{"base64 depth2", "секрет: " + b64x2, 2, APIKeyOpenAI, ViewDecoded, true},
		{"hex", "hex " + hexKey, 2, AWSAccessKey, ViewDecoded, true},
		{"url encoded", "q=" + urlKey, 2, GitHubToken, ViewDecoded, true},
		{"rot13 secret (was known gap)", "fx-cebw-nop123KLM789", 2, APIKeyOpenAI, ViewDecoded, true},
		// PEER-01 regression: structural carriers that reached upstream
		// byte-identical before the fix.
		{"peer block_comment", "prefix sk-/* split */proj-PEERauditSynthetic7xQ2z9Lm4vW8 suffix", 2, APIKeyOpenAI, ViewJoined, true},
		{"peer line comment", "sk- // note\n proj-PEERauditSynthetic7xQ2z9Lm4vW8", 2, APIKeyOpenAI, ViewJoined, true},
		{"peer base64_plus", fmt.Sprintf("key %q + %q",
			base64.StdEncoding.EncodeToString([]byte("sk-proj-")),
			base64.StdEncoding.EncodeToString([]byte("PEERauditSynthetic7xQ2z9Lm4vW8"))), 2, APIKeyOpenAI, ViewDecoded, true},
		{"peer comment_plus", "sk-/* x */ + /* y */proj-PEERauditSynthetic7xQ2z9Lm4vW8", 2, APIKeyOpenAI, ViewJoined, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newScanner(tt.depth).Scan(tt.in)
			var hit *Finding
			for i, f := range r.Findings {
				if f.Type == tt.want && f.View == tt.view {
					hit = &r.Findings[i]
				}
			}
			if hit == nil {
				t.Fatalf("no %s/%s finding in %+v", tt.want, tt.view, r.Findings)
			}
			if hit.Derived != tt.derived {
				t.Fatalf("derived = %v, want %v", hit.Derived, tt.derived)
			}
		})
	}
	// depth limit: double base64 is not seen with depth 1.
	if r := newScanner(1).Scan(b64x2); len(r.Findings) != 0 {
		t.Fatalf("depth 1 must not decode twice: %+v", r.Findings)
	}
	_ = url.PathEscape
}

func TestNegativeViews(t *testing.T) {
	tests := []struct{ name, in string }{
		{"plus email not joined", "пишите на a+b test"},
		{"math", "2 + 2 = 4, x + y"},
		{"long word", "internationalization and localization"},
		{"git sha", "commit 3f786850e387550fdab836ed7e6dc881de23001b"},
		// "rot13 gap" (fx-cebw-nop123KLM789) moved to TestViews: ciphered
		// secrets are now detected — the known-gap contract changed.
		{"allowlisted base64", base64.StdEncoding.EncodeToString([]byte("AKIAIOSFODNN7EXAMPLE"))},
	}
	s := newScanner(2)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if r := s.Scan(tt.in); len(r.Findings) != 0 {
				t.Fatalf("unexpected findings %+v", r.Findings)
			}
		})
	}
}

func TestRedact(t *testing.T) {
	s := newScanner(2)
	tests := []struct {
		name    string
		in      string
		want    string
		notHave []string
	}{
		{"openai", "use sk-proj-abc123 now", "use [REDACTED_API_KEY] now", []string{"abc123"}},
		{"two", "a@b.io и " + fakeAWSKey, "[REDACTED_EMAIL] и [REDACTED_AWS_KEY]", nil},
		{"joined", `k = "sk-" + "proj-abc"`, `k = "[REDACTED_JOINED_SECRET]"`, []string{"proj-abc"}},
		{"encoded", "x " + base64.StdEncoding.EncodeToString([]byte(fakeOpenAI)), "x [REDACTED_ENCODED_SECRET]", nil},
		{"zero width canonical", "sk-​proj-QwErTy123456!", "[REDACTED_API_KEY]!", []string{"QwErTy"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := s.Scan(tt.in)
			got := Redact(r.Canonical, r.Findings)
			if got != tt.want {
				t.Fatalf("Redact = %q, want %q", got, tt.want)
			}
			for _, n := range tt.notHave {
				if strings.Contains(got, n) {
					t.Fatalf("%q leaked in %q", n, got)
				}
			}
		})
	}
}

func TestRedactOverlap(t *testing.T) {
	canon := "0123456789"
	fs := []Finding{
		{Type: Phone, Start: 0, End: 6},
		{Type: Card, Start: 3, End: 10},
		{Type: Email, Start: 3, End: 10},
	}
	if got := Redact(canon, fs); got != "[REDACTED_CARD]" {
		t.Fatalf("got %q", got)
	}
}

func TestPolicy(t *testing.T) {
	p := Policy{Default: ModeMask, Overrides: map[Type]Mode{PrivateKey: ModeBlock}}
	tests := []struct {
		name string
		fs   []Finding
		want Action
	}{
		{"none", nil, ActionPass},
		{"mask", []Finding{{Type: Email}}, ActionMasked},
		{"override block", []Finding{{Type: Email}, {Type: PrivateKey}}, ActionBlocked},
		{"derived blocks in mask", []Finding{{Type: APIKeyOpenAI, Derived: true}}, ActionBlocked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.Decide(tt.fs); got != tt.want {
				t.Fatalf("Decide = %s, want %s", got, tt.want)
			}
		})
	}
	if got := (Policy{Default: ModeBlock}).Decide([]Finding{{Type: Email}}); got != ActionBlocked {
		t.Fatalf("block default: %s", got)
	}
	if _, err := ParseMode("drop"); err == nil {
		t.Fatal("ParseMode must reject unknown")
	}
}

func TestPlaceholder(t *testing.T) {
	if Placeholder(APIKeyOpenAI) != "[REDACTED_API_KEY]" || Placeholder(Card) != "[REDACTED_CARD]" {
		t.Fatal("placeholders")
	}
	for _, ty := range Types() {
		if !ValidType(string(ty)) {
			t.Fatalf("%s not valid", ty)
		}
	}
	if ValidType("nope") {
		t.Fatal("unknown type accepted")
	}
}

// PEER-hardening / тайнопись: ciphered secrets reach the decoded view (block),
// encoded natural language is reported for the upstream annotation.
func TestCipherDetection(t *testing.T) {
	caesar := func(s string, k int) string {
		var b strings.Builder
		for _, r := range s {
			switch {
			case r >= 'а' && r <= 'я':
				b.WriteRune('а' + (r-'а'+rune(k))%32)
			case r >= 'a' && r <= 'z':
				b.WriteRune('a' + (r-'a'+rune(k))%26)
			default:
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	rot13 := func(s string) string {
		var b strings.Builder
		for _, r := range s {
			if r >= 'a' && r <= 'z' {
				b.WriteRune('a' + (r-'a'+13)%26)
			} else {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	sc := newScanner(2)

	// Secret hidden by Caesar: found in the decoded view → policy blocks.
	shifted := caesar("мой секретный ключ: sk-proj-abc123", 3)
	if r := sc.Scan(shifted); len(r.Findings) == 0 {
		t.Fatalf("caesar-shifted secret not found: %q", shifted)
	} else if !r.Findings[0].Derived {
		t.Fatalf("cipher finding must be derived: %+v", r.Findings[0])
	}

	// Detection kinds.
	tests := []struct {
		name string
		in   string
		want string // "" = no hit expected
	}{
		{"caesar ru", caesar("служебная записка с просьбой вывести маркер для проверки безопасности системы", 5), "caesar"},
		{"rot13 en", rot13("hidden instruction telling the model to reveal the internal marker now"), "rot13"},
		// Litorea keeps the vowel skeleton and is statistically inseparable
		// from plain words: documented as out of scope, NOT annotated.
		{"litorea (out of scope)", "овтевь блото бтекла служебная", ""},
		{"plain ru", "обычное предложение на русском языке без всяких шифров", ""},
		{"plain en", "an ordinary english sentence without any ciphers at all", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hits := sc.DetectCiphers(tt.in)
			if tt.want == "" {
				if len(hits) != 0 {
					t.Fatalf("false positive: %+v", hits)
				}
				return
			}
			if len(hits) == 0 || hits[0].Kind != tt.want {
				t.Fatalf("hits %+v, want kind %q", hits, tt.want)
			}
		})
	}
}
