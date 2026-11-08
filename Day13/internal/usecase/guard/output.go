package guard

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	"agile-consultant/internal/ports"
)

// Output validation reasons.
const (
	ReasonCanary      = "canary"
	ReasonNonce       = "nonce"
	ReasonSystemLeak  = "system_leak"
	ReasonSpecial     = "special_token"
	ReasonPseudoTag   = "pseudo_tag"
	ReasonInvisible   = "invisible"
	ReasonANSI        = "ansi"
	ReasonFormat      = "format"
	ReasonLanguage    = "language"
	ReasonImage       = "md_image"
	ReasonURLQuery    = "url_query"
	ReasonURLScheme   = "url_scheme"
	ReasonHiddenEcho  = "hidden_echo"
	leakNGram         = 8
	minCyrillicShare  = 0.5
	minLongWordEchoes = 2
	minCodeWordRunes  = 5
)

// OutputCheck is the per-request context for output validation.
type OutputCheck struct {
	// Canaries are secret markers of the system prompt.
	Canaries []string
	// Nonce is the boundary nonce ("" for the static boundary).
	Nonce string
	// SystemText is the system prompt text.
	SystemText string
	// Hidden is content removed from the input (see Result.HiddenContent).
	Hidden []string
	// Visible is the sanitised question and document the user can see.
	Visible string
}

// OutputValidator checks model answers before delivery.
type OutputValidator struct {
	n ports.TextNormalizer
}

// NewOutputValidator returns a validator using n for NFKC.
func NewOutputValidator(n ports.TextNormalizer) *OutputValidator { return &OutputValidator{n: n} }

var (
	fieldHeaderRe = regexp.MustCompile(`(?im)^[\s>*_#-]*(вердикт|принцип|обоснование|рекомендация)[*_\s]*:`)
	mdImageRe     = regexp.MustCompile(`!\[[^\]]*\]\s*\(`)
	urlQueryRe    = regexp.MustCompile(`(?i)\b(https?|ftp)://[^\s)]*\?[^\s)]*=`)
	urlSchemeRe   = regexp.MustCompile(`(?i)\b(javascript|vbscript):|\bdata:[a-z]+/`)
	wordRe        = regexp.MustCompile(`[\p{L}\p{N}]+`)
	codeWordRe    = regexp.MustCompile(`[\p{L}\p{N}]+(?:-[\p{L}\p{N}]+)+`)
	longWordRe    = regexp.MustCompile(`\p{L}{7,}`)
)

// Validate returns the reasons the answer must not be delivered; an empty
// result means the answer passed. Checks run on lower(NFKC(StripInvisible))
// except the invisible/ANSI checks, which inspect the raw answer.
func (v *OutputValidator) Validate(answer string, chk OutputCheck) []string {
	reasons := map[string]bool{}
	if InvisibleCount(answer) > 0 {
		reasons[ReasonInvisible] = true
	}
	if strings.Contains(answer, "\x1b[") || strings.ContainsRune(answer, 0x9B) {
		reasons[ReasonANSI] = true
	}
	norm := strings.ToLower(v.n.NFKC(StripInvisible(answer)))
	squashed := squash(norm)

	for _, c := range chk.Canaries {
		if c != "" && strings.Contains(squashed, squash(strings.ToLower(c))) {
			reasons[ReasonCanary] = true
		}
	}
	if chk.Nonce != "" && strings.Contains(squashed, chk.Nonce) {
		reasons[ReasonNonce] = true
	}
	if leaksSystem(norm, strings.ToLower(v.n.NFKC(chk.SystemText))) {
		reasons[ReasonSystemLeak] = true
	}
	if specialTokenRe.MatchString(norm) {
		reasons[ReasonSpecial] = true
	}
	if pseudoTagRe.MatchString(norm) || boundaryCloseRe.MatchString(norm) {
		reasons[ReasonPseudoTag] = true
	}
	if !hasFourFields(norm) {
		reasons[ReasonFormat] = true
	}
	if cyrillicShare(norm) < minCyrillicShare {
		reasons[ReasonLanguage] = true
	}
	if mdImageRe.MatchString(norm) {
		reasons[ReasonImage] = true
	}
	if urlQueryRe.MatchString(norm) {
		reasons[ReasonURLQuery] = true
	}
	if urlSchemeRe.MatchString(norm) {
		reasons[ReasonURLScheme] = true
	}
	if v.echoesHidden(norm, squashed, chk) {
		reasons[ReasonHiddenEcho] = true
	}

	out := make([]string, 0, len(reasons))
	for r := range reasons {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// squash keeps only letters and digits so separators inserted between
// characters ("C A N A R Y", "ФЛАМИНГО 4417") do not evade matching.
func squash(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func leaksSystem(answer, system string) bool {
	sys := wordRe.FindAllString(system, -1)
	if len(sys) < leakNGram {
		return false
	}
	grams := make(map[string]struct{}, len(sys))
	for i := 0; i+leakNGram <= len(sys); i++ {
		grams[strings.Join(sys[i:i+leakNGram], " ")] = struct{}{}
	}
	ans := wordRe.FindAllString(answer, -1)
	for i := 0; i+leakNGram <= len(ans); i++ {
		if _, ok := grams[strings.Join(ans[i:i+leakNGram], " ")]; ok {
			return true
		}
	}
	return false
}

// hasFourFields requires all four fields with "Вердикт" first.
func hasFourFields(norm string) bool {
	ms := fieldHeaderRe.FindAllStringSubmatch(norm, -1)
	if len(ms) == 0 || ms[0][1] != "вердикт" {
		return false
	}
	seen := map[string]bool{}
	for _, m := range ms {
		seen[m[1]] = true
	}
	return len(seen) == 4
}

func cyrillicShare(s string) float64 {
	var letters, cyr int
	for _, r := range s {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.Is(unicode.Cyrillic, r) {
			cyr++
		}
	}
	if letters == 0 {
		return 0
	}
	return float64(cyr) / float64(letters)
}

// echoesHidden reports whether the answer repeats tokens that were only
// present in hidden input content. A single code word (letters-digits or an
// upper-case hyphenated token) is enough; plain long words need at least
// minLongWordEchoes distinct matches to limit false positives.
func (v *OutputValidator) echoesHidden(norm, squashed string, chk OutputCheck) bool {
	if len(chk.Hidden) == 0 {
		return false
	}
	known := map[string]bool{}
	for _, src := range []string{chk.Visible, chk.SystemText} {
		for _, w := range wordRe.FindAllString(strings.ToLower(v.n.NFKC(src)), -1) {
			known[w] = true
		}
	}
	answerWords := map[string]bool{}
	for _, w := range wordRe.FindAllString(norm, -1) {
		answerWords[w] = true
	}
	visibleSquashed := squash(strings.ToLower(v.n.NFKC(chk.Visible)))
	longEchoes := map[string]bool{}
	for _, h := range chk.Hidden {
		h = v.n.NFKC(h)
		for _, cw := range codeWordRe.FindAllString(h, -1) {
			if !isCodeWord(cw) {
				continue
			}
			sq := squash(strings.ToLower(cw))
			if len([]rune(sq)) >= minCodeWordRunes && !strings.Contains(visibleSquashed, sq) && strings.Contains(squashed, sq) {
				return true
			}
		}
		for _, w := range longWordRe.FindAllString(strings.ToLower(h), -1) {
			if !known[w] && answerWords[w] {
				longEchoes[w] = true
			}
		}
	}
	return len(longEchoes) >= minLongWordEchoes
}

// isCodeWord accepts hyphenated tokens that contain a digit or are fully
// upper case (ФЛАМИНГО-4417, PWNED-OTTER), not ordinary compounds.
func isCodeWord(s string) bool {
	hasDigit, hasLower := false, false
	for _, r := range s {
		if unicode.IsDigit(r) {
			hasDigit = true
		}
		if unicode.IsLower(r) {
			hasLower = true
		}
	}
	return hasDigit || !hasLower
}
