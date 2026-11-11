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
	// Whole block comments (PEER-01 block_comment carrier) and line comments
	// (PEER-01 comment carrier). A scan view may drop comment text: findings
	// map back to original spans, never to the view.
	blockCommentRe = regexp.MustCompile(`(?s)/[*].*?[*]/`)
)

// lineCommentCuts returns cut ranges of whole line comments without a URL
// scheme colon. The regexp above captures the preceding char when it is not
// ':'; cutting must keep that char, so only the comment body is cut here and
// the pattern is applied manually.
func lineCommentCuts(canon string) [][2]int {
	var cuts [][2]int
	for i := 0; i+1 < len(canon); i++ {
		if canon[i] != '/' || canon[i+1] != '/' {
			continue
		}
		if i > 0 && canon[i-1] == ':' { // https:// — not a comment
			i++
			continue
		}
		end := len(canon)
		if j := strings.IndexByte(canon[i:], '\n'); j >= 0 {
			end = i + j
		}
		cuts = append(cuts, [2]int{i, end})
		i = end
	}
	return cuts
}

// encodedPart is a decoded quote-joined fragment chain: the concatenation of
// the decodings and its span in the canonical text.
type encodedPart struct {
	text       string
	start, end int
}

var (
	quotedPartRe = regexp.MustCompile(`"([^"]*)"`)
	chainSepRe   = regexp.MustCompile(`^\s*\+\s*$`)
)

// decodeFragment decodes one quoted fragment as base64, hex or URL-encoding.
// Only printable decodings are returned; the empty string means "not encoded".
func decodeFragment(body string) []string {
	if body == "" {
		return nil
	}
	var out []string
	for _, d := range decodeBase64(body) {
		if printable(d) {
			out = append(out, d)
		}
	}
	if h, err := hex.DecodeString(body); err == nil && printable(string(h)) {
		out = append(out, string(h))
	}
	if u, err := url.QueryUnescape(body); err == nil && u != body && printable(u) {
		out = append(out, u)
	}
	return out
}

// encodedPartsViews models PEER-01 base64_plus: "b64(A)" + "b64(B)" decodes to
// A+B. Each maximal chain of double-quoted parts joined by '+' is decoded per
// part; if at least two parts decode, the concatenation of the decodings is a
// scan view mapped to the whole chain span.
func encodedPartsViews(canon string) []encodedPart {
	parts := quotedPartRe.FindAllStringSubmatchIndex(canon, -1)
	var out []encodedPart
	for i := 0; i < len(parts); {
		j := i
		for j+1 < len(parts) {
			if !chainSepRe.MatchString(canon[parts[j][1]:parts[j+1][0]]) {
				break
			}
			j++
		}
		if j > i {
			var b strings.Builder
			ok := true
			for k := i; k <= j && ok; k++ {
				decs := decodeFragment(canon[parts[k][2]:parts[k][3]])
				if len(decs) == 0 {
					ok = false
					break
				}
				b.WriteString(decs[0])
			}
			if ok && b.Len() > 0 {
				out = append(out, encodedPart{text: b.String(), start: parts[i][0], end: parts[j][1]})
			}
		}
		if j == i {
			i++
		} else {
			i = j + 1
		}
	}
	return out
}

// joinedView removes concatenation and comment syntax so that fragments of a
// secret are glued together. Passes repeat to a fixed point (bounded): a cut
// may expose a new prefix-space or plus join ("sk- // note\n proj-x" needs the
// comment cut before the prefix-space cut is visible). ok is false when
// nothing was removed.
func joinedView(canon string) (m mapped, ok bool) {
	text := canon
	to := identityMap(len(canon))
	changedAny := false
	for pass := 0; pass < 3; pass++ {
		cuts := findCuts(text)
		if len(cuts) == 0 {
			break
		}
		changedAny = true
		text, to = applyCuts(text, to, cuts)
	}
	if !changedAny {
		return mapped{}, false
	}
	return mapped{text: text, toCanon: to}, true
}

// identityMap maps every byte offset to itself.
func identityMap(n int) []int {
	to := make([]int, n+1)
	for i := range to {
		to[i] = i
	}
	return to
}

// findCuts locates all removable separators in one pass over text.
func findCuts(text string) []struct{ a, b int } {
	type cut = struct{ a, b int }
	var cuts []cut
	for _, loc := range quoteConcatRe.FindAllStringIndex(text, -1) {
		cuts = append(cuts, cut{loc[0], loc[1]})
	}
	for _, loc := range prefixSpaceRe.FindAllStringSubmatchIndex(text, -1) {
		cuts = append(cuts, cut{loc[2], loc[3]})
	}
	for pos := 0; pos < len(text); {
		loc := plusConcatRe.FindStringSubmatchIndex(text[pos:])
		if loc == nil {
			break
		}
		cuts = append(cuts, cut{pos + loc[2], pos + loc[3]})
		pos += loc[3]
	}
	for _, loc := range blockCommentRe.FindAllStringIndex(text, -1) {
		cuts = append(cuts, cut{loc[0], loc[1]})
	}
	for _, loc := range lineCommentCuts(text) {
		cuts = append(cuts, cut{loc[0], loc[1]})
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].a < cuts[j].a })
	return cuts
}

// applyCuts removes the given ranges from text, mapping surviving bytes
// through the previous offset map to canonical offsets.
func applyCuts(text string, to []int, cuts []struct{ a, b int }) (string, []int) {
	var b strings.Builder
	b.Grow(len(text))
	var nt []int
	pos := 0
	emit := func(i int) {
		nt = append(nt, to[i])
	}
	for _, c := range cuts {
		if c.a < pos {
			c.a = pos
		}
		if c.b <= c.a {
			continue
		}
		for i := pos; i < c.a; i++ {
			emit(i)
		}
		b.WriteString(text[pos:c.a])
		pos = c.b
	}
	for i := pos; i < len(text); i++ {
		emit(i)
	}
	b.WriteString(text[pos:])
	nt = append(nt, to[len(text)])
	return b.String(), nt
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
	for _, c := range cipherCandidates(s) {
		add(c.start, c.end, c.decoded)
	}
	return out
}

// cipherHit describes an encoded-natural-language fragment found in the text.
// Detection (not decoding of substitution ciphers) drives the [guard]
// annotation that rides the upstream request, so a model cannot mistake a
// decoded hidden instruction for a legitimate one.
type cipherHit struct {
	Kind    string // "caesar", "rot13", "base64_text", "substitution"
	Preview string // decoded text (caesar/rot13/base64) or the raw fragment
}

// common-word presence separates plain/decoded text from scrambled text far
// more reliably than bigram coverage on short notes.
var (
	ruWords = map[string]bool{"что": true, "это": true, "не": true, "на": true, "я": true, "ты": true, "он": true, "она": true, "мы": true, "вы": true, "они": true, "и": true, "в": true, "с": true, "к": true, "по": true, "за": true, "из": true, "у": true, "о": true, "от": true, "до": true, "да": true, "нет": true, "все": true, "уже": true, "еще": true, "очень": true, "можно": true, "нужно": true, "нужна": true, "слово": true, "слова": true, "ответ": true, "ответь": true, "ответа": true, "текст": true, "задача": true, "задачу": true, "записка": true, "служебная": true, "служебной": true, "служебную": true, "мар": true, "маркер": true, "ключ": true, "ключа": true, "секрет": true, "секретный": true, "мой": true, "моя": true, "как": true, "так": true, "чем": true, "где": true, "когда": true, "который": true, "которая": true, "которые": true, "чтобы": true, "его": true, "ее": true, "их": true, "свой": true, "своя": true, "просьба": true, "просьбой": true, "вывести": true, "проверки": true, "проверка": true, "безопасности": true, "системы": true, "система": true, "инструкция": true, "инструкции": true, "выполни": true, "расшифруй": true, "передай": true, "выведи": true}
	enWords = map[string]bool{"the": true, "and": true, "with": true, "this": true, "that": true, "word": true, "words": true, "reply": true, "answer": true, "print": true, "output": true, "hidden": true, "instruction": true, "instructions": true, "model": true, "text": true, "note": true, "secret": true, "marker": true, "follow": true, "decode": true, "read": true, "write": true, "reveal": true, "system": true, "prompt": true, "you": true, "your": true, "task": true, "please": true, "now": true, "must": true}
)

// wordHits counts words of s present in the dictionary.
func wordHits(s string, dict map[string]bool) int {
	n := 0
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r)
	}) {
		if dict[w] {
			n++
		}
	}
	return n
}

// naturalText reports whether s looks like natural language of the table's
// language: either decent bigram coverage or at least one common word.
func naturalText(s string, table map[string]bool, dict map[string]bool) bool {
	return bigramCoverage(s, table) >= 0.35 || wordHits(s, dict) >= 1
}

var (
	ruBigrams = map[string]bool{"ав": true, "ае": true, "ал": true, "ан": true, "ас": true, "ащ": true, "ая": true, "ве": true, "ви": true, "вь": true, "го": true, "да": true, "де": true, "ди": true, "ее": true, "ем": true, "ен": true, "ер": true, "ес": true, "ет": true, "ещ": true, "за": true, "ие": true, "ий": true, "им": true, "ир": true, "ис": true, "ищ": true, "ия": true, "ка": true, "ке": true, "ко": true, "ла": true, "ле": true, "ли": true, "ло": true, "ль": true, "ма": true, "ме": true, "ми": true, "мо": true, "му": true, "на": true, "не": true, "ни": true, "но": true, "ня": true, "об": true, "ов": true, "ог": true, "од": true, "ое": true, "ол": true, "ом": true, "он": true, "оп": true, "ор": true, "ос": true, "от": true, "пе": true, "пи": true, "по": true, "пр": true, "ра": true, "ре": true, "ро": true, "са": true, "св": true, "се": true, "си": true, "см": true, "ст": true, "су": true, "та": true, "те": true, "то": true, "ум": true, "ун": true, "ус": true, "уч": true, "уы": true, "ую": true, "фи": true, "ха": true, "че": true, "ше": true, "ще": true, "ые": true, "ым": true, "ьк": true, "ют": true, "юч": true, "ющ": true, "юю": true, "як": true, "яц": true}
	enBigrams = map[string]bool{"ad": true, "ai": true, "al": true, "an": true, "ar": true, "as": true, "at": true, "ay": true, "ba": true, "be": true, "bu": true, "ce": true, "ch": true, "co": true, "de": true, "ea": true, "ed": true, "el": true, "en": true, "er": true, "es": true, "fo": true, "ge": true, "ha": true, "he": true, "hi": true, "ic": true, "id": true, "in": true, "io": true, "is": true, "it": true, "le": true, "li": true, "ll": true, "ly": true, "ma": true, "me": true, "mo": true, "na": true, "nd": true, "ng": true, "nt": true, "of": true, "om": true, "on": true, "or": true, "ot": true, "ou": true, "ow": true, "pa": true, "pp": true, "pr": true, "qu": true, "ra": true, "re": true, "ri": true, "ro": true, "se": true, "si": true, "so": true, "st": true, "te": true, "th": true, "ti": true, "to": true, "tr": true, "un": true, "ur": true, "us": true, "ve": true, "vi": true, "wh": true, "wi": true, "wo": true}
)

// bigramCoverage returns the fraction of adjacent letter pairs of s present
// in the language bigram table. Non-letters break pairs.
func bigramCoverage(s string, table map[string]bool) float64 {
	runes := make([]rune, 0, len(s))
	for _, r := range s {
		if unicode.IsLetter(r) {
			runes = append(runes, unicode.ToLower(r))
		}
	}
	if len(runes) < 4 {
		return 0
	}
	hits := 0
	for i := 0; i+1 < len(runes); i++ {
		if table[string(runes[i:i+2])] {
			hits++
		}
	}
	return float64(hits) / float64(len(runes)-1)
}

// shiftLetters shifts Cyrillic and Latin letters of s by k positions.
func shiftLetters(s string, k int) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'а' && r <= 'я':
			b.WriteRune('а' + (r-'а'+rune(k))%32)
		case r >= 'А' && r <= 'Я':
			b.WriteRune('А' + (r-'А'+rune(k))%32)
		case r >= 'a' && r <= 'z':
			b.WriteRune('a' + (r-'a'+rune(k))%26)
		case r >= 'A' && r <= 'Z':
			b.WriteRune('A' + (r-'A'+rune(k))%26)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

var (
	// Cyrillic and Latin runs may contain spaces (multi-word notes); Latin
	// runs cover dash/underscore-bearing tokens such as "vn-surm-def123".
	cyrRunRe = regexp.MustCompile(`[А-Яа-яЁё ]{12,}`)
	latRunRe = regexp.MustCompile(`[A-Za-z0-9+/_= -]{12,}`)
)

const (
	cipherRunMaxChars = 256
	cipherMaxRuns     = 8
	cipherCovPlain    = 0.42 // at or above (with a common word): plain text
)

// detectCiphers finds encoded-natural-language fragments: Caesar/ROT13 runs
// (decodable, preview is the decoding), base64 of natural text, and probable
// simple substitutions such as litorea (Russian vowel skeleton, no words and
// no shift helps). Bounded: at most cipherMaxRuns runs, each truncated for
// scoring.
func detectCiphers(canon string) []cipherHit {
	var hits []cipherHit
	all := append(cyrRunRe.FindAllStringIndex(canon, -1), latRunRe.FindAllStringIndex(canon, -1)...)
	sort.Slice(all, func(i, j int) bool { return all[i][0] < all[j][0] })
	for i, loc := range all {
		if i >= cipherMaxRuns {
			break
		}
		run := canon[loc[0]:loc[1]]
		if len(run) > cipherRunMaxChars {
			run = run[:cipherRunMaxChars]
		}
		isLatin := run[0] < 0x80
		var table, dict map[string]bool
		var shifts int
		if isLatin {
			table, dict, shifts = enBigrams, enWords, 26
		} else {
			table, dict, shifts = ruBigrams, ruWords, 32
		}
		if naturalText(run, table, dict) {
			continue // plain text
		}
		bestK, bestW, bestCov := 0, -1, 0.0
		for k := 1; k < shifts; k++ {
			dec := shiftLetters(run, k)
			w, c := wordHits(dec, dict), bigramCoverage(dec, table)
			if !naturalText(dec, table, dict) {
				continue
			}
			if w > bestW || (w == bestW && c > bestCov) {
				bestK, bestW, bestCov = k, w, c
			}
		}
		if bestK != 0 {
			kind := "caesar"
			if isLatin && bestK == 13 {
				kind = "rot13"
			}
			hits = append(hits, cipherHit{Kind: kind, Preview: preview(shiftLetters(run, bestK))})
		}
	}
	// Simple substitutions that keep the vowel skeleton (litorea) are NOT
	// annotated: bigram coverage cannot separate them from plain words
	// (measured overlap), and a false note on every plain note without
	// dictionary hits is net-harmful. Caesar/ROT13/base64 stay covered.
	// base64 of natural language (an instruction smuggled as a blob).
	nBase := 0
	for _, loc := range base64CandRe.FindAllStringIndex(canon, -1) {
		if nBase >= 3 {
			break
		}
		for _, d := range decodeBase64(canon[loc[0]:loc[1]]) {
			if len(d) >= 20 && (naturalText(d, ruBigrams, ruWords) || naturalText(d, enBigrams, enWords)) {
				hits = append(hits, cipherHit{Kind: "base64_text", Preview: preview(d)})
				nBase++
				break
			}
		}
	}
	return hits
}

// preview trims s to one line of at most 80 characters.
func preview(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 80 {
		s = s[:80] + "…"
	}
	return s
}

var base64Encodings = []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding}

// cipherCandidates exposes Caesar/ROT13 decodings to the decoded view so a
// secret hidden by a cipher is found (and blocked) like a base64 secret.
// Runs that already match a rule (plaintext credentials such as an AKIA key,
// which is letter soup but not prose) are left to the direct view.
func cipherCandidates(s string) []candidate {
	var out []candidate
	for _, loc := range cyrRunRe.FindAllStringIndex(s, -1) {
		run := s[loc[0]:loc[1]]
		if len(match(run)) == 0 && !naturalText(run, ruBigrams, ruWords) {
			out = append(out, allShiftCandidates(s, loc, 32)...)
		}
	}
	for _, loc := range latRunRe.FindAllStringIndex(s, -1) {
		run := s[loc[0]:loc[1]]
		if len(match(run)) == 0 && !naturalText(run, enBigrams, enWords) {
			out = append(out, allShiftCandidates(s, loc, 26)...)
		}
	}
	return out
}

// allShiftCandidates exposes EVERY Caesar/ROT13 shift of a non-natural run to
// the decoded view: the secret-scan picks whichever decoding actually
// contains a credential (a key is not natural language, so no shift can be
// chosen by language signal — all are tried, bounded by the run/run caps).
func allShiftCandidates(s string, loc []int, shifts int) []candidate {
	run := s[loc[0]:loc[1]]
	if len(run) > cipherRunMaxChars {
		run = run[:cipherRunMaxChars]
	}
	out := make([]candidate, 0, shifts-1)
	for k := 1; k < shifts; k++ {
		out = append(out, candidate{start: loc[0], end: loc[0] + len(run), decoded: shiftLetters(run, k)})
	}
	return out
}

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
