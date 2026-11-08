package secrets

import (
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// View names the text representation a finding was made in.
type View string

// Views.
const (
	ViewRaw        View = "raw"
	ViewNormalized View = "normalized"
	ViewJoined     View = "joined"
	ViewDecoded    View = "decoded"
)

// mapped is a view derived from the canonical text with a byte offset map:
// toCanon[i] is the canonical offset of byte i of text.
type mapped struct {
	text    string
	toCanon []int
}

// span converts a [start, end) range of the view to canonical offsets.
func (m mapped) span(start, end int) (int, int) {
	if end <= start {
		return m.toCanon[start], m.toCanon[start]
	}
	return m.toCanon[start], m.toCanon[end-1] + 1
}

var (
	// "sk-" + "proj-abc", 'a' + 'b', `a` + `b`
	quoteConcatRe = regexp.MustCompile("[\"'`]\\s*\\+\\s*[\"'`]")
	// "sk- proj-abc": whitespace right after a well-known prefix.
	prefixSpaceRe = regexp.MustCompile(`\b(?:sk-|ghp_|github_pat_|AKIA|xox[a-z]-)(\s+)[A-Za-z0-9]`)
	// abc + def between identifier characters.
	plusConcatRe = regexp.MustCompile(`[A-Za-z0-9_-](\s*\+\s*)[A-Za-z0-9_-]`)
)

// joinedView removes concatenation syntax so that fragments of a secret are
// glued together. ok is false when nothing was removed.
func joinedView(canon string) (m mapped, ok bool) {
	type cut struct{ a, b int }
	var cuts []cut
	for _, loc := range quoteConcatRe.FindAllStringIndex(canon, -1) {
		cuts = append(cuts, cut{loc[0], loc[1]})
	}
	for _, loc := range prefixSpaceRe.FindAllStringSubmatchIndex(canon, -1) {
		cuts = append(cuts, cut{loc[2], loc[3]})
	}
	// Overlapping search: "a + b + c" must drop both operators.
	for pos := 0; pos < len(canon); {
		loc := plusConcatRe.FindStringSubmatchIndex(canon[pos:])
		if loc == nil {
			break
		}
		cuts = append(cuts, cut{pos + loc[2], pos + loc[3]})
		pos += loc[3]
	}
	if len(cuts) == 0 {
		return mapped{}, false
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].a < cuts[j].a })
	var b strings.Builder
	b.Grow(len(canon))
	to := make([]int, 0, len(canon)+1)
	pos := 0
	for _, c := range cuts {
		if c.a < pos {
			c.a = pos
		}
		if c.b <= c.a {
			continue
		}
		for i := pos; i < c.a; i++ {
			to = append(to, i)
		}
		b.WriteString(canon[pos:c.a])
		pos = c.b
	}
	for i := pos; i < len(canon); i++ {
		to = append(to, i)
	}
	b.WriteString(canon[pos:])
	to = append(to, len(canon))
	return mapped{text: b.String(), toCanon: to}, true
}

var (
	base64CandRe = regexp.MustCompile(`[A-Za-z0-9+/_-]{16,}={0,2}`)
	hexCandRe    = regexp.MustCompile(`(?:[0-9a-fA-F]{2}){10,}`)
	// At least four %XX escapes, possibly interleaved with unreserved chars.
	urlCandRe = regexp.MustCompile(`[A-Za-z0-9._~-]*(?:%[0-9A-Fa-f]{2}[A-Za-z0-9._~-]*){4,}`)
)

// candidate is an encoded fragment of the canonical text and its decoding.
type candidate struct {
	start, end int
	decoded    string
}

// decodeCandidates finds base64, hex and URL-encoded fragments in s that
// decode to mostly printable UTF-8 text.
func decodeCandidates(s string) []candidate {
	var out []candidate
	seen := map[[2]int]map[string]bool{}
	add := func(a, b int, d string) {
		k := [2]int{a, b}
		if seen[k] == nil {
			seen[k] = map[string]bool{}
		}
		if seen[k][d] {
			return
		}
		seen[k][d] = true
		out = append(out, candidate{a, b, d})
	}
	for _, loc := range base64CandRe.FindAllStringIndex(s, -1) {
		for _, d := range decodeBase64(s[loc[0]:loc[1]]) {
			add(loc[0], loc[1], d)
		}
	}
	for _, loc := range hexCandRe.FindAllStringIndex(s, -1) {
		if b, err := hex.DecodeString(s[loc[0]:loc[1]]); err == nil && printable(string(b)) {
			add(loc[0], loc[1], string(b))
		}
	}
	for _, loc := range urlCandRe.FindAllStringIndex(s, -1) {
		if d, err := url.PathUnescape(s[loc[0]:loc[1]]); err == nil && d != s[loc[0]:loc[1]] && printable(d) {
			add(loc[0], loc[1], d)
		}
	}
	return out
}

var base64Encodings = []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding}

func decodeBase64(s string) []string {
	var out []string
	trimmed := strings.TrimRight(s, "=")
	for _, enc := range base64Encodings {
		in := s
		if enc == base64.RawStdEncoding || enc == base64.RawURLEncoding {
			in = trimmed
		}
		b, err := enc.DecodeString(in)
		if err != nil || len(b) < 8 {
			continue
		}
		if d := string(b); printable(d) {
			out = append(out, d)
		}
	}
	return out
}

// printable requires valid UTF-8 with at least 90% printable runes.
func printable(s string) bool {
	if s == "" || !utf8.ValidString(s) {
		return false
	}
	total, ok := 0, 0
	for _, r := range s {
		total++
		if unicode.IsPrint(r) || r == '\n' || r == '\t' || r == '\r' {
			ok++
		}
	}
	return ok*10 >= total*9
}
