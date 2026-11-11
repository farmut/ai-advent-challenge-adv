package secrets

import (
	"sort"
	"strings"

	"agile-consultant/internal/ports"
	"agile-consultant/internal/usecase/guard"
)

// Finding is one detected secret. Start and End are byte offsets in the
// canonical text; for derived findings they cover the encoded or
// concatenated fragment. Value is the secret itself and must never be
// logged.
type Finding struct {
	Type    Type
	View    View
	Start   int
	End     int
	Value   string
	Derived bool
}

// Placeholder returns the mask that replaces the finding span.
func (f Finding) Placeholder() string {
	switch {
	case f.Derived && f.View == ViewJoined:
		return PlaceholderJoined
	case f.Derived:
		return PlaceholderEncoded
	}
	return Placeholder(f.Type)
}

// Result is the outcome of a scan. Canonical is the text that will be sent
// upstream (after masking): StripInvisible + NFKC of the input.
type Result struct {
	Canonical string
	Findings  []Finding
}

// HasDerived reports whether any finding came from a derived view.
func (r Result) HasDerived() bool {
	for _, f := range r.Findings {
		if f.Derived {
			return true
		}
	}
	return false
}

// Scanner runs all detectors over several views of a text.
type Scanner struct {
	n     ports.TextNormalizer
	allow map[string]struct{}
	depth int
}

// New returns a Scanner. Values listed in allowlist are never reported.
// depth limits nested decoding (base64 of hex of ...); values < 0 mean 0.
func New(n ports.TextNormalizer, allowlist []string, depth int) *Scanner {
	allow := make(map[string]struct{}, len(allowlist))
	for _, a := range allowlist {
		if a != "" {
			allow[a] = struct{}{}
		}
	}
	if depth < 0 {
		depth = 0
	}
	return &Scanner{n: n, allow: allow, depth: depth}
}

// Canonicalize returns NFKC(StripInvisible(s)).
func (s *Scanner) Canonicalize(raw string) string {
	return s.n.NFKC(guard.StripInvisible(raw))
}

// Scan inspects raw, normalized, joined and decoded views. The decoded pass
// runs over the canonical text and over the joined view, so a secret split by
// concatenation AND encoded (base64_plus carrier) is still found. Results are
// deduplicated by (type, span).
func (s *Scanner) Scan(raw string) Result {
	canon := s.Canonicalize(raw)
	direct := s.direct(raw, canon)
	joined, jm, jok := s.joined(canon, direct)
	all := append([]Finding(nil), direct...)
	all = append(all, joined...)
	all = append(all, s.decodedFrom(decodeCandidates(canon), func(a, b int) (int, int) { return a, b }, direct, s.depth)...)
	if jok {
		all = append(all, s.decodedFrom(decodeCandidates(jm.text), func(a, b int) (int, int) { return jm.span(a, b) }, direct, s.depth)...)
	}
	for _, p := range encodedPartsViews(canon) {
		for _, f := range match(p.text) {
			if isPII(f.Type) {
				continue
			}
			if _, ok := s.allow[f.Value]; ok {
				continue
			}
			if sameValue(direct, f.Value) {
				continue
			}
			all = append(all, Finding{Type: f.Type, View: ViewDecoded, Start: p.start, End: p.end, Value: f.Value, Derived: true})
		}
	}
	all = dedupe(all)
	sortFindings(all)
	return Result{Canonical: canon, Findings: all}
}

// DetectCiphers reports encoded-natural-language fragments (Caesar, ROT13,
// probable simple substitution such as litorea). The caller annotates the
// upstream request so the model does not execute instructions smuggled in a
// cipher; secrets in ciphers are handled separately by Scan (decoded view).
func (s *Scanner) DetectCiphers(raw string) []CipherHit {
	return detectCiphers(s.Canonicalize(raw))
}

// CipherHit is the public shape of an encoded-fragment detection.
type CipherHit = cipherHit

// ScanDirect inspects only the raw and normalized views (used for model
// output, where derived findings are out of scope).
func (s *Scanner) ScanDirect(raw string) Result {
	canon := s.Canonicalize(raw)
	f := s.direct(raw, canon)
	sortFindings(f)
	return Result{Canonical: canon, Findings: f}
}

// direct runs all rules on canon. A value that is not present verbatim in
// raw was hidden by invisible characters or look-alikes: view=normalized.
func (s *Scanner) direct(raw, canon string) []Finding {
	fs := match(canon)
	out := fs[:0]
	for _, f := range fs {
		if _, ok := s.allow[f.Value]; ok {
			continue
		}
		f.View = ViewNormalized
		if strings.Contains(raw, f.Value) {
			f.View = ViewRaw
		}
		out = append(out, f)
	}
	return out
}

// match applies the rules to text and drops phone matches inside cards.
func match(text string) []Finding {
	var out []Finding
	var cards [][2]int
	for _, r := range rules {
		for _, loc := range r.Re.FindAllStringSubmatchIndex(text, -1) {
			a, b := loc[2*r.Group], loc[2*r.Group+1]
			if a < 0 {
				continue
			}
			v := text[a:b]
			if r.Validate != nil && !r.Validate(v) {
				continue
			}
			if r.Type == Card {
				cards = append(cards, [2]int{a, b})
			}
			if r.Type == Phone && overlapsAny(a, b, cards) {
				continue
			}
			out = append(out, Finding{Type: r.Type, Start: a, End: b, Value: v})
		}
	}
	return dedupe(out)
}

func overlapsAny(a, b int, spans [][2]int) bool {
	for _, s := range spans {
		if a < s[1] && s[0] < b {
			return true
		}
	}
	return false
}

// dedupe drops identical (type, span) duplicates from alternative regexes.
func dedupe(fs []Finding) []Finding {
	type key struct {
		t    Type
		a, b int
	}
	seen := map[key]bool{}
	out := fs[:0]
	for _, f := range fs {
		k := key{f.Type, f.Start, f.End}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, f)
	}
	return out
}

// joined reports credentials that exist only after concatenation syntax is
// removed. PII is excluded: "user+tag@example.com" must not become a block.
// The mapped view is returned so Scan can run the decoded pass over it.
func (s *Scanner) joined(canon string, direct []Finding) ([]Finding, mapped, bool) {
	m, ok := joinedView(canon)
	if !ok {
		return nil, mapped{}, false
	}
	var out []Finding
	for _, f := range match(m.text) {
		if isPII(f.Type) {
			continue
		}
		if _, ok := s.allow[f.Value]; ok {
			continue
		}
		a, b := m.span(f.Start, f.End)
		if b-a == f.End-f.Start || sameValue(direct, f.Value) {
			continue // nothing was glued inside: it is a direct finding
		}
		out = append(out, Finding{Type: f.Type, View: ViewJoined, Start: a, End: b, Value: f.Value, Derived: true})
	}
	return out, m, true
}

func sameValue(fs []Finding, v string) bool {
	for _, f := range fs {
		if f.Value == v {
			return true
		}
	}
	return false
}

// decodedFrom decodes the given candidates, scans each decoding (recursively
// up to depth) and maps every hit back to canonical offsets through toCanon.
// Every hit is derived: an encoded secret cannot be masked precisely, so the
// policy blocks such requests.
func (s *Scanner) decodedFrom(cands []candidate, toCanon func(a, b int) (int, int), direct []Finding, depth int) []Finding {
	if depth <= 0 {
		return nil
	}
	var out []Finding
	for _, c := range cands {
		a, b := toCanon(c.start, c.end)
		if inside(a, b, direct) {
			continue // already masked as a direct finding
		}
		inner := s.Canonicalize(c.decoded)
		hits := s.direct(c.decoded, inner)
		hits = append(hits, s.decoded(inner, hits, depth-1)...)
		for _, h := range hits {
			out = append(out, Finding{Type: h.Type, View: ViewDecoded, Start: a, End: b, Value: h.Value, Derived: true})
		}
	}
	return out
}

// decoded scans encoded candidate fragments of canon (kept for callers that
// need the canonical-text pass only).
func (s *Scanner) decoded(canon string, direct []Finding, depth int) []Finding {
	return s.decodedFrom(decodeCandidates(canon), func(a, b int) (int, int) { return a, b }, direct, depth)
}

func inside(a, b int, fs []Finding) bool {
	for _, f := range fs {
		if !f.Derived && f.Start <= a && b <= f.End {
			return true
		}
	}
	return false
}

func sortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Start != fs[j].Start {
			return fs[i].Start < fs[j].Start
		}
		return fs[i].End > fs[j].End
	})
}
