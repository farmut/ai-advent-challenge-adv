// Package guard implements the input/output defences of the consultant
// against hidden instructions, invisible payloads and injected context:
// input sanitisation, a block policy, content boundary markers and output
// validation. It depends only on the standard library and internal/ports.
package guard

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"agile-consultant/internal/ports"
)

// Kind is a category of suspicious input content.
type Kind string

// Finding kinds.
const (
	KindInvisible      Kind = "invisible"       // zero-width and other format characters
	KindBidi           Kind = "bidi"            // bidi embedding/override/isolate controls
	KindTagChars       Kind = "tag_chars"       // Unicode Tags block (ASCII smuggling)
	KindVSSupplement   Kind = "vs_supplement"   // variation selectors supplement (byte smuggling)
	KindControl        Kind = "control"         // C0/C1 control characters
	KindSoftHyphen     Kind = "soft_hyphen"     // U+00AD
	KindNBSP           Kind = "nbsp"            // non-breaking spaces folded to a space
	KindHTMLComment    Kind = "html_comment"    // <!-- ... -->
	KindHiddenElement  Kind = "hidden_element"  // hidden / display:none / ... elements
	KindScriptStyle    Kind = "script_style"    // script/style/template/noscript
	KindMDComment      Kind = "md_comment"      // [//]: # (...) link-reference comments
	KindSpecialToken   Kind = "special_token"   // <|...|> chat special tokens
	KindPseudoTag      Kind = "pseudo_tag"      // <system>, </assistant>, <rules>, ...
	KindBoundaryClose  Kind = "boundary_close"  // </user_question ... attempts to close the data block
	KindInvisibleRatio Kind = "invisible_ratio" // too many invisible characters (policy)
	KindUnstable       Kind = "unstable"        // no fixed point within MaxPasses
)

// MaxPasses bounds the sanitiser fixed-point loop.
const MaxPasses = 5

// Finding is a count of one kind of suspicious content.
type Finding struct {
	Kind  Kind
	Count int
}

// Result is the outcome of sanitising one text.
type Result struct {
	// Clean is the text to send to the model.
	Clean string
	// Check is lower(NFKC(Clean)), used for detection only.
	Check string
	// Findings are sorted by kind.
	Findings []Finding
	// Removed is the inner text of cut comments and hidden elements.
	Removed []string
	// Hidden is text cut from inside bidi overrides (logical and visual order).
	Hidden []string
	// DecodedTags is the ASCII decoded from Unicode Tags characters.
	DecodedTags string
	// InvisibleCount is the number of removed invisible, bidi, tag,
	// variation-selector and control characters.
	InvisibleCount int
	// InvisibleRatio is InvisibleCount over the input length in runes.
	InvisibleRatio float64
	// Passes is the number of sanitiser passes performed.
	Passes int
}

// Count returns the number of findings of kind k.
func (r Result) Count(k Kind) int {
	for _, f := range r.Findings {
		if f.Kind == k {
			return f.Count
		}
	}
	return 0
}

// Has reports whether the result has at least one finding of kind k.
func (r Result) Has(k Kind) bool { return r.Count(k) > 0 }

// Kinds returns the finding kinds as strings.
func (r Result) Kinds() []string {
	out := make([]string, len(r.Findings))
	for i, f := range r.Findings {
		out[i] = string(f.Kind)
	}
	return out
}

// HiddenContent returns Removed, Hidden and DecodedTags in one slice.
func (r Result) HiddenContent() []string {
	out := make([]string, 0, len(r.Removed)+len(r.Hidden)+1)
	out = append(out, r.Removed...)
	out = append(out, r.Hidden...)
	if r.DecodedTags != "" {
		out = append(out, r.DecodedTags)
	}
	return out
}

// Sanitizer strips hidden and invisible content from untrusted text.
type Sanitizer struct {
	n ports.TextNormalizer
}

// NewSanitizer returns a Sanitizer using n for NFC/NFKC.
func NewSanitizer(n ports.TextNormalizer) *Sanitizer { return &Sanitizer{n: n} }

type acc struct {
	counts      map[Kind]int
	removed     []string
	hidden      []string
	decoded     []string
	invisible   int
	inputLength int
}

func (a *acc) add(k Kind, n int) {
	if n > 0 {
		a.counts[k] += n
	}
}

// Sanitize runs the sanitiser passes until the text stops changing (at most
// MaxPasses). Every pass: strip invisible characters, NFC, cut HTML and
// markdown hidden content outside code, drop special tokens and pseudo-tag
// markup. If no fixed point is reached a KindUnstable finding is recorded.
func (s *Sanitizer) Sanitize(in string) Result {
	a := &acc{counts: map[Kind]int{}, inputLength: utf8.RuneCountInString(in)}
	cur := normalizeNewlines(in)
	res := Result{}
	stable := false
	for pass := 1; pass <= MaxPasses; pass++ {
		res.Passes = pass
		next := s.pass(cur, a)
		if next == cur {
			stable = true
			break
		}
		cur = next
	}
	if !stable {
		a.add(KindUnstable, 1)
	}
	res.Clean = strings.TrimSpace(cur)
	res.Check = strings.ToLower(s.n.NFKC(res.Clean))
	// Detection on the compatibility-folded copy catches fullwidth and
	// similar look-alikes that the literal pass cannot remove.
	a.add(KindSpecialToken, len(specialTokenRe.FindAllString(res.Check, -1)))
	a.add(KindBoundaryClose, len(boundaryCloseRe.FindAllString(res.Check, -1)))
	a.add(KindPseudoTag, len(pseudoTagRe.FindAllString(res.Check, -1)))

	res.Removed = a.removed
	res.Hidden = a.hidden
	res.DecodedTags = strings.Join(a.decoded, " ")
	res.InvisibleCount = a.invisible
	if a.inputLength > 0 {
		res.InvisibleRatio = float64(a.invisible) / float64(a.inputLength)
	}
	for k, c := range a.counts {
		res.Findings = append(res.Findings, Finding{Kind: k, Count: c})
	}
	sort.Slice(res.Findings, func(i, j int) bool { return res.Findings[i].Kind < res.Findings[j].Kind })
	return res
}

func (s *Sanitizer) pass(in string, a *acc) string {
	out := stripInvisible(in, a)
	out = s.n.NFC(out)
	out = stripMarkup(out, a)
	out = stripTokens(out, a)
	return out
}

func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// StripInvisible removes invisible, bidi, tag, variation-selector and control
// characters (keeping emoji ZWJ sequences, VS16 and flag tag sequences),
// folds non-breaking spaces to a space and drops soft hyphens. Text inside
// bidi overrides is removed as well. It is exported for e2e detectors.
func StripInvisible(s string) string {
	return stripInvisible(normalizeNewlines(s), &acc{counts: map[Kind]int{}})
}

// InvisibleCount returns how many characters StripInvisible would remove as
// invisible, bidi, tag, variation-selector or control characters.
func InvisibleCount(s string) int {
	a := &acc{counts: map[Kind]int{}}
	stripInvisible(normalizeNewlines(s), a)
	return a.invisible
}

// classify returns the kind of an invisible rune, or "" for visible runes.
func classify(r rune) Kind {
	switch {
	case r == '\n' || r == '\t':
		return ""
	case r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0x9F):
		return KindControl
	case r == 0x00AD:
		return KindSoftHyphen
	case r == 0x00A0 || r == 0x202F || r == 0x2007:
		return KindNBSP
	case r == 0x200E || r == 0x200F || r == 0x061C ||
		(r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069):
		return KindBidi
	case (r >= 0x200B && r <= 0x200D) || (r >= 0x2060 && r <= 0x2064) ||
		r == 0xFEFF || r == 0x180E || r == 0x034F ||
		r == 0x115F || r == 0x1160 || r == 0x3164 || r == 0xFFA0 ||
		(r >= 0xFFF9 && r <= 0xFFFB) || (r >= 0xFE00 && r <= 0xFE0F):
		return KindInvisible
	case r >= 0xE0000 && r <= 0xE007F:
		return KindTagChars
	case r >= 0xE0100 && r <= 0xE01EF:
		return KindVSSupplement
	}
	return ""
}

func isEmoji(r rune) bool {
	switch {
	case r >= 0x1F000 && r <= 0x1FAFF, r >= 0x2600 && r <= 0x27BF,
		r >= 0x2300 && r <= 0x23FF, r >= 0x2B00 && r <= 0x2BFF,
		r == 0x00A9, r == 0x00AE, r == 0x203C, r == 0x2049, r == 0x2122,
		r == 0x2139, r >= 0x2194 && r <= 0x21AA, r == 0x3030, r == 0x303D,
		r == 0x3297, r == 0x3299:
		return true
	}
	return false
}

func isKeycapBase(r rune) bool { return (r >= '0' && r <= '9') || r == '#' || r == '*' }

const (
	blackFlag  = 0x1F3F4
	tagCancel  = 0xE007F
	zwj        = 0x200D
	vs16       = 0xFE0F
	keycap     = 0x20E3
	maxFlagTag = 7
)

// flagSequenceEnd returns the index after a valid emoji tag sequence
// (U+1F3F4, 1..7 tag letters/digits, U+E007F) starting at i, or -1.
func flagSequenceEnd(rs []rune, i int) int {
	if rs[i] != blackFlag {
		return -1
	}
	j := i + 1
	for j < len(rs) && j-i-1 < maxFlagTag && ((rs[j] >= 0xE0061 && rs[j] <= 0xE007A) || (rs[j] >= 0xE0030 && rs[j] <= 0xE0039)) {
		j++
	}
	if j == i+1 || j >= len(rs) || rs[j] != tagCancel {
		return -1
	}
	return j + 1
}

func isBidiOpen(r rune) bool {
	return r == 0x202A || r == 0x202B || r == 0x202D || r == 0x202E || (r >= 0x2066 && r <= 0x2068)
}

func isBidiOverride(r rune) bool { return r == 0x202D || r == 0x202E }

func isBidiClose(r rune) bool { return r == 0x202C || r == 0x2069 }

func stripInvisible(s string, a *acc) string {
	rs := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	var lastKept rune
	var tags strings.Builder
	flushTags := func() {
		if tags.Len() > 0 {
			a.decoded = append(a.decoded, tags.String())
			tags.Reset()
		}
	}
	// Bidi override handling: text inside LRO/RLO is cut and reported.
	depth, overrideDepth := 0, 0
	var hidden []rune
	flushHidden := func() {
		if len(hidden) > 0 {
			text := strings.TrimSpace(string(hidden))
			if text != "" {
				a.hidden = append(a.hidden, text, reverse(text))
			}
			hidden = hidden[:0]
		}
	}
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if end := flagSequenceEnd(rs, i); end > 0 {
			flushTags()
			if overrideDepth > 0 {
				hidden = append(hidden, rs[i:end]...)
			} else {
				b.WriteString(string(rs[i:end]))
			}
			lastKept = tagCancel
			i = end - 1
			continue
		}
		k := classify(r)
		if k != KindTagChars {
			flushTags()
		}
		switch k {
		case "":
			if overrideDepth > 0 {
				hidden = append(hidden, r)
			} else {
				b.WriteRune(r)
			}
			lastKept = r
			continue
		case KindNBSP:
			a.add(KindNBSP, 1)
			if overrideDepth > 0 {
				hidden = append(hidden, ' ')
			} else {
				b.WriteByte(' ')
			}
			lastKept = ' '
			continue
		case KindSoftHyphen:
			a.add(KindSoftHyphen, 1)
			continue
		case KindInvisible:
			if r == zwj && (isEmoji(lastKept) || lastKept == vs16 || isSkinTone(lastKept)) && i+1 < len(rs) && isEmoji(rs[i+1]) {
				writeKept(&b, &hidden, overrideDepth, r)
				lastKept = r
				continue
			}
			if r == vs16 && (isEmoji(lastKept) || (isKeycapBase(lastKept) && i+1 < len(rs) && rs[i+1] == keycap)) {
				writeKept(&b, &hidden, overrideDepth, r)
				lastKept = r
				continue
			}
		case KindTagChars:
			if r >= 0xE0020 && r <= 0xE007E {
				tags.WriteRune(r - 0xE0000)
			}
		case KindBidi:
			switch {
			case isBidiOpen(r):
				depth++
				if isBidiOverride(r) || overrideDepth > 0 {
					overrideDepth++
				}
			case isBidiClose(r):
				if depth > 0 {
					depth--
				}
				if overrideDepth > 0 {
					overrideDepth--
					if overrideDepth == 0 {
						flushHidden()
					}
				}
			}
		}
		a.add(k, 1)
		a.invisible++
	}
	flushTags()
	flushHidden() // unterminated override: cut to the end (fail-closed)
	return b.String()
}

func isSkinTone(r rune) bool { return r >= 0x1F3FB && r <= 0x1F3FF }

func writeKept(b *strings.Builder, hidden *[]rune, overrideDepth int, r rune) {
	if overrideDepth > 0 {
		*hidden = append(*hidden, r)
		return
	}
	b.WriteRune(r)
}

func reverse(s string) string {
	rs := []rune(s)
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
	return string(rs)
}

var (
	specialTokenRe  = regexp.MustCompile(`<\|[^|>]*\|>`)
	boundaryCloseRe = regexp.MustCompile(`<\s*/\s*user_(question|document|data_|doc_)`)
	pseudoTagRe     = regexp.MustCompile(`(?i)<\s*/?\s*(system|assistant|user|rules|instructions?)\b[^<>]*>`)
	tagNameRe       = regexp.MustCompile(`^<([A-Za-z][A-Za-z0-9-]*)`)
	hiddenAttrRe    = regexp.MustCompile(`(?i)\shidden(\s|=|/?>)`)
	ariaHiddenRe    = regexp.MustCompile(`(?i)aria-hidden\s*=\s*["']?\s*true`)
	hiddenStyleRe   = regexp.MustCompile(`(?i)(display\s*:\s*none|visibility\s*:\s*hidden|font-size\s*:\s*0(\.0+)?\s*(px|em|rem|pt|%)?\s*(;|"|'|!|$)|opacity\s*:\s*0(\.0+)?\s*(;|"|'|!|$))`)
	mdCommentRe     = regexp.MustCompile(`^[ \t]{0,3}\[[^\]\n]*\]:[ \t]*(#|<>)[ \t]*(\((.*)\)|"(.*)"|'(.*)')?[ \t]*$`)
	innerTagRe      = regexp.MustCompile(`<(?:"[^"]*"|'[^']*'|[^<>"'])*>`)
)

var rawTextElements = map[string]bool{"script": true, "style": true, "template": true, "noscript": true}

var voidElements = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true, "img": true,
	"input": true, "link": true, "meta": true, "source": true, "track": true, "wbr": true,
}

// stripMarkup removes HTML comments, hidden elements, script-like elements
// and markdown comments outside fenced and inline code. Unterminated
// constructs are cut to the end of the text (fail-closed).
func stripMarkup(s string, a *acc) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		lineStart := i == 0 || s[i-1] == '\n'
		if lineStart {
			if end := fencedBlockEnd(s, i); end > 0 {
				b.WriteString(s[i:end])
				i = end
				continue
			}
			if end, text, ok := mdComment(s, i); ok {
				a.add(KindMDComment, 1)
				a.addRemoved(text)
				i = end
				continue
			}
		}
		switch {
		case s[i] == '`':
			n := runLen(s, i, '`')
			if j := closingRun(s, i+n, n); j >= 0 {
				b.WriteString(s[i : j+n])
				i = j + n
			} else {
				b.WriteString(s[i : i+n])
				i += n
			}
			continue
		case strings.HasPrefix(s[i:], "<!--"):
			end := commentEnd(s, i)
			a.add(KindHTMLComment, 1)
			a.addRemoved(innerText(s[i:end]))
			i = end
			continue
		case s[i] == '<':
			if end, kind, ok := hiddenElement(s, i); ok {
				a.add(kind, 1)
				a.addRemoved(innerText(s[i:end]))
				i = end
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

func (a *acc) addRemoved(text string) {
	if text = strings.TrimSpace(text); text != "" {
		a.removed = append(a.removed, text)
	}
}

func innerText(s string) string {
	s = strings.NewReplacer("<!--->", " ", "<!-->", " ", "<!--", " ", "-->", " ", "--!>", " ").Replace(s)
	s = innerTagRe.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

func runLen(s string, i int, c byte) int {
	n := 0
	for i+n < len(s) && s[i+n] == c {
		n++
	}
	return n
}

// closingRun finds a backtick run of exactly n characters at or after i.
func closingRun(s string, i, n int) int {
	for i < len(s) {
		j := strings.IndexByte(s[i:], '`')
		if j < 0 {
			return -1
		}
		j += i
		m := runLen(s, j, '`')
		if m == n {
			return j
		}
		i = j + m
	}
	return -1
}

// fencedBlockEnd returns the index after a closed fenced code block that
// starts at line start i, or -1. Unclosed fences are not treated as code so
// they cannot shield hidden content (fail-closed).
func fencedBlockEnd(s string, i int) int {
	j := i
	for j < len(s) && j-i < 3 && s[j] == ' ' {
		j++
	}
	if j >= len(s) || (s[j] != '`' && s[j] != '~') {
		return -1
	}
	c := s[j]
	n := runLen(s, j, c)
	if n < 3 {
		return -1
	}
	nl := strings.IndexByte(s[j:], '\n')
	if nl < 0 {
		return -1
	}
	if c == '`' && strings.ContainsRune(s[j+n:j+nl], '`') {
		return -1
	}
	pos := j + nl + 1
	for pos <= len(s) {
		lineEnd := strings.IndexByte(s[pos:], '\n')
		var line string
		if lineEnd < 0 {
			line = s[pos:]
		} else {
			line = s[pos : pos+lineEnd]
		}
		t := strings.TrimLeft(line, " ")
		if len(line)-len(t) <= 3 && runLen(t, 0, c) >= n && strings.TrimSpace(strings.TrimLeft(t, string(c))) == "" {
			if lineEnd < 0 {
				return len(s)
			}
			return pos + lineEnd + 1
		}
		if lineEnd < 0 {
			break
		}
		pos += lineEnd + 1
	}
	return -1
}

// mdComment matches a link-reference definition with target "#" or "<>"
// (the markdown comment idiom) on the line starting at i.
func mdComment(s string, i int) (end int, text string, ok bool) {
	lineEnd := strings.IndexByte(s[i:], '\n')
	line := s[i:]
	end = len(s)
	if lineEnd >= 0 {
		line = s[i : i+lineEnd]
		end = i + lineEnd + 1
	}
	m := mdCommentRe.FindStringSubmatch(line)
	if m == nil {
		return 0, "", false
	}
	return end, m[3] + m[4] + m[5], true
}

// commentEnd returns the index after the HTML comment starting at i.
// "<!-->" and "<!--->" close immediately, "-->" and "--!>" close a comment,
// nested "<!--" increase the depth and an unterminated comment runs to the
// end of the text.
func commentEnd(s string, i int) int {
	j := i + 4
	if strings.HasPrefix(s[j:], ">") {
		return j + 1
	}
	if strings.HasPrefix(s[j:], "->") {
		return j + 2
	}
	depth := 1
	for j < len(s) {
		switch {
		case strings.HasPrefix(s[j:], "<!--"):
			depth++
			j += 4
		case strings.HasPrefix(s[j:], "-->"):
			depth--
			j += 3
		case strings.HasPrefix(s[j:], "--!>"):
			depth--
			j += 4
		default:
			j++
			continue
		}
		if depth == 0 {
			return j
		}
	}
	return len(s)
}

// tagEnd returns the index after the '>' that closes the tag starting at i,
// honouring quoted attribute values, or -1.
func tagEnd(s string, i int) int {
	var quote byte
	for j := i + 1; j < len(s); j++ {
		c := s[j]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '>':
			return j + 1
		case c == '<':
			return -1
		}
	}
	return -1
}

// hiddenElement recognises a hidden or script-like element starting at i and
// returns the index after its end. Without a matching close tag the element
// runs to the end of the text.
func hiddenElement(s string, i int) (end int, kind Kind, ok bool) {
	m := tagNameRe.FindStringSubmatch(s[i:])
	if m == nil {
		return 0, "", false
	}
	name := strings.ToLower(m[1])
	te := tagEnd(s, i)
	if te < 0 {
		return 0, "", false
	}
	tag := s[i:te]
	switch {
	case rawTextElements[name]:
		kind = KindScriptStyle
	case hiddenAttrRe.MatchString(tag), ariaHiddenRe.MatchString(tag), hiddenStyleRe.MatchString(tag):
		kind = KindHiddenElement
	default:
		return 0, "", false
	}
	if voidElements[name] || strings.HasSuffix(tag, "/>") {
		return te, kind, true
	}
	return elementClose(s, te, name), kind, true
}

// elementClose finds the end of the element name whose content starts at
// i, counting nested elements of the same name.
func elementClose(s string, i int, name string) int {
	lower := asciiLower(s) // byte-for-byte, keeps indices aligned with s
	open, closeTag := "<"+name, "</"+name
	depth := 1
	j := i
	for j < len(s) {
		k := strings.IndexByte(lower[j:], '<')
		if k < 0 {
			break
		}
		j += k
		switch {
		case strings.HasPrefix(lower[j:], closeTag) && boundaryAfter(lower, j+len(closeTag)):
			depth--
			e := strings.IndexByte(lower[j:], '>')
			if e < 0 {
				return len(s)
			}
			j += e + 1
			if depth == 0 {
				return j
			}
		case strings.HasPrefix(lower[j:], open) && boundaryAfter(lower, j+len(open)):
			depth++
			j += len(open)
		default:
			j++
		}
	}
	return len(s)
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

func boundaryAfter(s string, i int) bool {
	if i >= len(s) {
		return true
	}
	c := s[i]
	return c == '>' || c == '/' || c == ' ' || c == '\t' || c == '\n'
}

// stripTokens removes literal chat special tokens everywhere and pseudo-tag
// markup (keeping the enclosed text) outside code.
func stripTokens(s string, a *acc) string {
	a.add(KindSpecialToken, len(specialTokenRe.FindAllString(s, -1)))
	s = specialTokenRe.ReplaceAllString(s, " ")
	return mapOutsideCode(s, func(text string) string {
		a.add(KindPseudoTag, len(pseudoTagRe.FindAllString(text, -1)))
		return pseudoTagRe.ReplaceAllString(text, " ")
	})
}

// mapOutsideCode applies f to the parts of s outside closed fenced blocks
// and inline code spans.
func mapOutsideCode(s string, f func(string) string) string {
	var b strings.Builder
	b.Grow(len(s))
	last, i := 0, 0
	for i < len(s) {
		lineStart := i == 0 || s[i-1] == '\n'
		if lineStart {
			if end := fencedBlockEnd(s, i); end > 0 {
				b.WriteString(f(s[last:i]))
				b.WriteString(s[i:end])
				i, last = end, end
				continue
			}
		}
		if s[i] == '`' {
			n := runLen(s, i, '`')
			if j := closingRun(s, i+n, n); j >= 0 {
				b.WriteString(f(s[last:i]))
				b.WriteString(s[i : j+n])
				i = j + n
				last = i
				continue
			}
			i += n
			continue
		}
		i++
	}
	b.WriteString(f(s[last:]))
	return b.String()
}
