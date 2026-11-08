package proxy

import (
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"agile-consultant/internal/ports"
	"agile-consultant/internal/usecase/guard"
	"agile-consultant/internal/usecase/secrets"
)

// Output finding types (besides secrets.Type values).
const (
	OutSystemLeak       = "system_leak"
	OutDangerousCommand = "dangerous_command"
	OutSuspiciousURL    = "suspicious_url"
	PlaceholderURL      = "[REDACTED_URL]"
)

// Output actions.
const (
	OutputPass     = "pass"
	OutputRedacted = "redacted"
	OutputBlocked  = "blocked"
)

// OutputConfig configures the output guard.
type OutputConfig struct {
	// NGram is the word n-gram size for system prompt leaks (default 8).
	NGram int
	// URLAllowlist lists domains whose URLs are never suspicious
	// (subdomains included).
	URLAllowlist []string
	// RefusalText replaces blocked answers.
	RefusalText string
}

// DefaultRefusal is used when OutputConfig.RefusalText is empty.
const DefaultRefusal = "Ответ модели скрыт политикой безопасности прокси."

var dangerousCommands = []*regexp.Regexp{
	regexp.MustCompile(`\b(?:curl|wget)\b[^\n|]*\|\s*(?:sudo\s+)?(?:sh|bash|zsh|python[0-9.]*)\b`),
	regexp.MustCompile(`\bbase64\s+(?:-d|--decode|-D)\b[^\n|]*\|\s*(?:sudo\s+)?(?:sh|bash|zsh)\b`),
	regexp.MustCompile(`\brm\s+-[a-z]*(?:rf|fr)[a-z]*\s+(?:/(?:\s|$|\*)|~/?(?:\s|$)|\*|\$home\b)`),
	regexp.MustCompile(`\bmkfs\.`),
	regexp.MustCompile(`\bdd\s+if=\S+\s+of=/dev/`),
	regexp.MustCompile(`:\(\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`),
	regexp.MustCompile(`\bchmod\s+-r\s+777\s+/(?:\s|$)`),
	regexp.MustCompile(`/dev/tcp/`),
	regexp.MustCompile(`\b(?:nc|ncat|netcat)\b[^\n]*\s-e\b`),
	regexp.MustCompile(`\bpowershell(?:\.exe)?\b[^\n]*\s-(?:enc|encodedcommand)\b`),
	regexp.MustCompile(`\biex\b[^\n]*downloadstring`),
}

var (
	urlRe       = regexp.MustCompile("(?i)\\b(?:https?|ftp)://[^\\s<>\"'`\\])]+")
	schemeURLRe = regexp.MustCompile("(?i)\\b(?:javascript|vbscript):[^\\s<>\"'`)]*|\\bdata:[a-z]+/[^\\s<>\"'`)]*")
	queryKeyRe  = regexp.MustCompile(`(?i)token|key|secret|password|passwd|session|auth`)
	digitsRe    = regexp.MustCompile(`^[0-9]+$`)
)

var shorteners = map[string]bool{
	"bit.ly": true, "tinyurl.com": true, "t.co": true, "goo.gl": true, "is.gd": true,
	"ow.ly": true, "cutt.ly": true, "rb.gy": true, "shorturl.at": true, "tiny.cc": true,
	"buff.ly": true, "t.ly": true, "v.gd": true, "clck.ru": true,
}

var badTLDs = map[string]bool{"zip": true, "mov": true, "xyz": true, "top": true, "tk": true}

type outputResult struct {
	action     string
	systemLeak bool
	findings   []ports.AuditFinding
	delivered  string
	masked     string
}

type outHit struct {
	typ, view, action, value, placeholder string
}

// checkOutput validates a model answer. systems are the system messages as
// sent upstream.
func (s *Service) checkOutput(answer string, systems []string) outputResult {
	r := s.scanner.ScanDirect(answer)
	canon := r.Canonical
	low := strings.ToLower(canon)
	var hits []outHit
	var spans []secrets.Span
	blocked := false

	for _, f := range r.Findings {
		hits = append(hits, outHit{string(f.Type), string(f.View), "redact", f.Value, f.Placeholder()})
	}
	spans = append(spans, secrets.Spans(r.Findings)...)

	for _, sys := range systems {
		sysLow := strings.ToLower(s.scanner.Canonicalize(sys))
		if guard.LeaksSystemN(low, sysLow, s.out.NGram) {
			hits = append(hits, outHit{OutSystemLeak, "", "block", sysLow, ""})
			blocked = true
			break
		}
	}
	for _, re := range dangerousCommands {
		for _, m := range re.FindAllString(low, -1) {
			hits = append(hits, outHit{OutDangerousCommand, "", "block", m, ""})
			blocked = true
		}
	}
	for _, loc := range s.suspiciousURLs(canon) {
		hits = append(hits, outHit{OutSuspiciousURL, "", "redact", canon[loc[0]:loc[1]], PlaceholderURL})
		spans = append(spans, secrets.Span{Start: loc[0], End: loc[1], Placeholder: PlaceholderURL, Priority: 100})
	}

	res := outputResult{findings: s.summarizeOutput(hits)}
	res.masked = secrets.RedactSpans(canon, spans)
	switch {
	case blocked:
		res.action = OutputBlocked
		for _, h := range hits {
			res.systemLeak = res.systemLeak || h.typ == OutSystemLeak
		}
		res.delivered = s.refusal()
	case len(spans) > 0:
		res.action = OutputRedacted
		res.delivered = res.masked
	default:
		res.action = OutputPass
		res.delivered = answer
	}
	return res
}

func (s *Service) refusal() string {
	if s.out.RefusalText != "" {
		return s.out.RefusalText
	}
	return DefaultRefusal
}

// suspiciousURLs returns spans of suspicious URLs in text.
func (s *Service) suspiciousURLs(text string) [][2]int {
	var out [][2]int
	for _, loc := range urlRe.FindAllStringIndex(text, -1) {
		a, b := loc[0], loc[1]
		for b > a && strings.ContainsRune(".,;:!?", rune(text[b-1])) {
			b--
		}
		if s.urlSuspicious(text[a:b]) {
			out = append(out, [2]int{a, b})
		}
	}
	if guard.DangerousScheme(strings.ToLower(text)) {
		for _, loc := range schemeURLRe.FindAllStringIndex(text, -1) {
			out = append(out, [2]int{loc[0], loc[1]})
		}
	}
	return out
}

func (s *Service) urlSuspicious(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return true // unparsable URL in model output: treat as suspicious
	}
	host := strings.ToLower(u.Hostname())
	for _, d := range s.out.URLAllowlist {
		d = strings.ToLower(d)
		if host == d || strings.HasSuffix(host, "."+d) {
			return false
		}
	}
	switch {
	case host == "":
		return true
	case net.ParseIP(host) != nil || digitsRe.MatchString(strings.ReplaceAll(host, ".", "")):
		return true
	case strings.Contains(host, "xn--"):
		return true
	case u.User != nil:
		return true
	case shorteners[strings.TrimPrefix(host, "www.")]:
		return true
	}
	if i := strings.LastIndexByte(host, '.'); i >= 0 && badTLDs[host[i+1:]] {
		return true
	}
	if p := u.Port(); p != "" && p != "80" && p != "443" {
		return true
	}
	for k := range u.Query() {
		if queryKeyRe.MatchString(k) {
			return true
		}
	}
	return false
}

func (s *Service) summarizeOutput(hits []outHit) []ports.AuditFinding {
	type key struct{ typ, view, action string }
	agg := map[key]*ports.AuditFinding{}
	fps := map[key]map[string]bool{}
	var order []key
	for _, h := range hits {
		k := key{h.typ, h.view, h.action}
		f, ok := agg[k]
		if !ok {
			f = &ports.AuditFinding{Type: h.typ, View: h.view, Action: h.action, Placeholder: h.placeholder}
			agg[k] = f
			fps[k] = map[string]bool{}
			order = append(order, k)
		}
		f.Count++
		fp := s.fp.Fingerprint(h.typ, h.value)
		if !fps[k][fp] {
			fps[k][fp] = true
			f.Fingerprints = append(f.Fingerprints, fp)
		}
	}
	out := make([]ports.AuditFinding, 0, len(order))
	for _, k := range order {
		sort.Strings(agg[k].Fingerprints)
		out = append(out, *agg[k])
	}
	sortAudit(out)
	return out
}

func sortAudit(fs []ports.AuditFinding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Type != fs[j].Type {
			return fs[i].Type < fs[j].Type
		}
		return fs[i].View < fs[j].View
	})
}
