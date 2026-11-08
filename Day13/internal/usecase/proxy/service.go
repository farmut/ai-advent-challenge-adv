package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"agile-consultant/internal/ports"
	"agile-consultant/internal/usecase/secrets"
)

// Deps are the service dependencies.
type Deps struct {
	Scanner *secrets.Scanner
	Policy  secrets.Policy
	Router  ports.UpstreamRouter
	Audit   ports.AuditLogger
	Limiter ports.RateLimiter
	Clock   ports.Clock
	FP      ports.Fingerprinter
	Out     OutputConfig
	// MaxAuditRunes truncates each masked text in the audit (0 = no limit).
	MaxAuditRunes int
	// ScanRoles lists roles whose content the input guard inspects and
	// masks. Empty means all roles.
	ScanRoles []string
	// SystemContent is how system/developer prompts are written to the
	// audit: SystemAuditHash (default) or SystemAuditFull.
	SystemContent string
	Logger        *slog.Logger
}

// System prompt audit modes (Deps.SystemContent).
const (
	SystemAuditHash = "hash"
	SystemAuditFull = "full"
	// PlaceholderSystemLeak replaces a model answer that leaked the system
	// prompt in the audit (hash mode), so the prompt is not stored there.
	PlaceholderSystemLeak = "[SYSTEM_LEAK]"
)

// Reject carries a request that delivery could not parse, so it is still
// rate limited and audited.
type Reject struct {
	Kind    OutcomeKind
	Code    string
	Param   string
	Message string
}

// Call is one chat completion request.
type Call struct {
	RequestID string
	ClientIP  string
	// KeyID identifies the authenticated proxy API key (not the key).
	KeyID    string
	Model    string
	Messages []ports.ChatMessage
	Params   ports.ChatParams
	Stream   bool
	// Unsupported names an unsupported parameter ("n", "tools",
	// "messages.content"); "" when everything is supported.
	Unsupported string
	Reject      *Reject
}

// Outcome is the pipeline result rendered by delivery.
type Outcome struct {
	Kind    OutcomeKind
	Code    string
	Param   string
	Message string
	// Warning is set on input blocks ("secrets_detected").
	Warning string

	Decision       ports.RateDecision
	InputAction    string
	InputFindings  []ports.AuditFinding
	OutputAction   string
	OutputFindings []ports.AuditFinding

	Content      string
	FinishReason string
	Usage        ports.Usage
	CostMicroUSD int64
	Model        string
	Created      time.Time
}

// Service runs the guard pipeline.
type Service struct {
	scanner   *secrets.Scanner
	policy    secrets.Policy
	router    ports.UpstreamRouter
	audit     ports.AuditLogger
	limiter   ports.RateLimiter
	clock     ports.Clock
	fp        ports.Fingerprinter
	out       OutputConfig
	maxRunes  int
	scanRoles map[string]bool
	sysHash   bool
	log       *slog.Logger
}

// NewService validates deps and returns a Service.
func NewService(d Deps) (*Service, error) {
	switch d.SystemContent {
	case "", SystemAuditHash, SystemAuditFull:
	default:
		return nil, fmt.Errorf("proxy: unknown system content mode %q", d.SystemContent)
	}
	if d.Scanner == nil || d.Router == nil || d.Audit == nil || d.Limiter == nil || d.Clock == nil || d.FP == nil {
		return nil, errors.New("proxy: scanner, router, audit, limiter, clock and fingerprinter are required")
	}
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	var roles map[string]bool
	if len(d.ScanRoles) > 0 {
		roles = map[string]bool{}
		for _, r := range d.ScanRoles {
			roles[r] = true
		}
	}
	return &Service{
		scanner: d.Scanner, policy: d.Policy, router: d.Router, audit: d.Audit,
		limiter: d.Limiter, clock: d.Clock, fp: d.FP, out: d.Out,
		maxRunes: d.MaxAuditRunes, scanRoles: roles, sysHash: d.SystemContent != SystemAuditFull, log: d.Logger,
	}, nil
}

// Aliases returns the configured model aliases.
func (s *Service) Aliases() []string { return s.router.Aliases() }

var validRoles = map[string]bool{"system": true, "user": true, "assistant": true, "tool": true, "developer": true}

// scanned is one input message after the guard.
type scanned struct {
	role     string
	sent     string // content sent upstream
	masked   string // content written to the audit (always masked)
	findings []secrets.Finding
	guarded  bool
}

// Chat runs the pipeline. The audit record is written on every path.
func (s *Service) Chat(ctx context.Context, c Call) Outcome {
	start := s.clock.Now()
	out := Outcome{Model: c.Model, Created: start}
	rec := ports.AuditRecord{
		V: ports.AuditVersion, TS: start, RequestID: c.RequestID,
		ClientIPHash: s.fp.Fingerprint("client_ip", c.ClientIP), KeyID: c.KeyID, Model: c.Model,
		Input:        ports.AuditInput{Mode: string(s.defaultMode())},
		PromptMasked: []ports.ChatMessage{},
		Usage:        ports.AuditUsage{Source: ports.UsageSourceNone},
	}
	finish := func(o Outcome, upstreamMS int64) Outcome {
		return s.finish(ctx, rec, o, start, upstreamMS)
	}

	// 1. rate limit
	out.Decision = s.limiter.Allow(c.ClientIP)
	if !out.Decision.Allowed {
		out.Kind, out.Code = KindRateLimited, CodeRateLimited
		out.Message = fmt.Sprintf("Rate limit exceeded: %d requests per window. Retry later.", out.Decision.Limit)
		out.Usage.Source = ports.UsageSourceNone
		return finish(out, 0)
	}
	if c.Reject != nil {
		out.Kind, out.Code, out.Param, out.Message = c.Reject.Kind, c.Reject.Code, c.Reject.Param, c.Reject.Message
		return finish(out, 0)
	}

	// The input is scanned before validation so that every audit record,
	// including rejected ones, stores only masked text.
	msgs := s.scan(c.Messages)
	rec.PromptMasked = s.auditPrompt(msgs)
	var all []secrets.Finding
	for _, m := range msgs {
		if m.guarded {
			all = append(all, m.findings...)
		}
	}
	out.InputFindings = s.summarizeInput(all)
	rec.Input.Findings = out.InputFindings
	promptEstimate := ports.Usage{PromptTokens: estimateSent(msgs), Source: ports.UsageSourceNone}

	// 2. validate
	if o, bad := validate(c, out); bad {
		o.Usage = promptEstimate
		return finish(o, 0)
	}

	// 3. route
	route, ok := s.router.Resolve(c.Model)
	if !ok {
		out.Kind, out.Code, out.Param = KindModelNotFound, CodeModelNotFound, "model"
		out.Message = fmt.Sprintf("The model %q does not exist or is not allowed by the proxy.", c.Model)
		out.Usage = promptEstimate
		return finish(out, 0)
	}
	rec.Upstream, rec.UpstreamModel, rec.PriceRef = route.UpstreamName, route.UpstreamModel, route.Price.Ref
	if route.AuditModel != "" {
		rec.UpstreamModel = route.AuditModel
	}
	if !route.Ready || route.Upstream == nil {
		out.Kind, out.Code = KindUpstreamNotConfigured, CodeUpstreamNotReady
		out.Message = "The upstream for this model is not configured (missing API key)."
		out.Usage = promptEstimate
		return finish(out, 0)
	}

	// 4-5. input policy; a block never reaches the upstream.
	action := s.policy.Decide(all)
	out.InputAction = string(action)
	rec.Input.Action = out.InputAction
	if action == secrets.ActionBlocked {
		out.Kind, out.Code, out.Param = KindInputBlocked, CodeInputBlocked, "messages"
		out.Warning = "secrets_detected"
		out.Message = fmt.Sprintf("Request blocked by input guard: secrets detected (%s). Nothing was sent to the model.", typeCounts(out.InputFindings, ", ", " x"))
		out.Usage = promptEstimate
		return finish(out, 0)
	}

	// 6. upstream
	req := ports.UpstreamRequest{Model: route.UpstreamModel, Params: c.Params}
	var systems []string
	for _, m := range msgs {
		req.Messages = append(req.Messages, ports.ChatMessage{Role: m.role, Content: m.sent})
		if isSystemRole(m.role) {
			systems = append(systems, m.sent)
		}
	}
	us := s.clock.Now()
	res, err := route.Upstream.Chat(ctx, req)
	upMS := s.clock.Now().Sub(us).Milliseconds()
	if err != nil {
		out.Kind, out.Code, out.Message = upstreamKind(err)
		out.Usage = promptEstimate
		return finish(out, upMS)
	}

	// 7. output guard
	og := s.checkOutput(res.Content, systems)
	out.OutputAction, out.OutputFindings = og.action, og.findings
	out.Content, out.FinishReason = og.delivered, res.FinishReason
	if out.FinishReason == "" {
		out.FinishReason = "stop"
	}
	if og.action == OutputBlocked {
		out.FinishReason = "content_filter"
	}
	rec.Output = ports.AuditOutput{Action: og.action, Findings: og.findings}
	rec.ResponseMasked = s.truncate(og.masked)
	if s.sysHash && og.systemLeak {
		rec.ResponseMasked = PlaceholderSystemLeak
		rec.Output.Findings = withSystemLeakPlaceholder(og.findings)
	}

	// 8. cost
	if res.HasUsage {
		out.Usage = res.Usage
		out.Usage.Source = ports.UsageSourceUpstream
	} else {
		out.Usage = ports.Usage{PromptTokens: estimateMessages(req.Messages), CompletionTokens: estimateTokens(res.Content), Source: ports.UsageSourceEstimated}
	}
	out.CostMicroUSD = cost(out.Usage, route.Price)
	out.Kind = KindOK
	return finish(out, upMS)
}

// finish fills and writes the audit record. On the success path an audit
// failure fails the request (fail closed): an unaudited answer is not
// delivered.
func (s *Service) finish(ctx context.Context, rec ports.AuditRecord, o Outcome, start time.Time, upMS int64) Outcome {
	rec.Status = o.Kind.AuditStatus()
	if o.Kind == KindOK && o.OutputAction == OutputBlocked {
		rec.Status = ports.AuditStatusOutputBlocked
	}
	rec.HTTPStatus = o.Kind.HTTPStatus()
	rec.ErrorCode = o.Code
	if rec.Output.Action == "" {
		rec.Output.Action = o.OutputAction
	}
	rec.Usage = ports.AuditUsage{
		PromptTokens: o.Usage.PromptTokens, CompletionTokens: o.Usage.CompletionTokens,
		TotalTokens: o.Usage.PromptTokens + o.Usage.CompletionTokens, Source: o.Usage.Source,
	}
	if rec.Usage.Source == "" {
		rec.Usage.Source = ports.UsageSourceNone
	}
	rec.CostMicroUSD = o.CostMicroUSD
	rec.LatencyMS = ports.AuditLatency{Total: s.clock.Now().Sub(start).Milliseconds(), Upstream: upMS}
	if err := s.audit.Write(ctx, rec); err != nil {
		s.log.Error("audit write failed", slog.String("request_id", rec.RequestID), slog.String("error", err.Error()))
		if o.Kind == KindOK {
			return Outcome{Kind: KindInternal, Code: CodeInternal, Message: "Audit log unavailable; the answer was withheld.",
				Decision: o.Decision, Model: o.Model, Created: o.Created}
		}
	}
	return o
}

func (s *Service) defaultMode() secrets.Mode {
	if s.policy.Default == "" {
		return secrets.ModeMask
	}
	return s.policy.Default
}

func validate(c Call, out Outcome) (Outcome, bool) {
	bad := func(code, param, msg string) (Outcome, bool) {
		out.Kind, out.Code, out.Param, out.Message = KindInvalid, code, param, msg
		return out, true
	}
	switch {
	case c.Stream:
		return bad(CodeUnsupported, "stream", "stream=true is not supported by the guard proxy; use stream=false.")
	case c.Unsupported != "":
		return bad(CodeUnsupported, c.Unsupported, fmt.Sprintf("Parameter %q is not supported by the guard proxy.", c.Unsupported))
	case strings.TrimSpace(c.Model) == "":
		return bad(CodeInvalidRequest, "model", "model is required.")
	case len(c.Messages) == 0:
		return bad(CodeInvalidRequest, "messages", "messages must be a non-empty array.")
	}
	for i, m := range c.Messages {
		if !validRoles[m.Role] {
			return bad(CodeInvalidRequest, fmt.Sprintf("messages[%d].role", i), fmt.Sprintf("Unknown role %q.", m.Role))
		}
	}
	return out, false
}

func upstreamKind(err error) (OutcomeKind, string, string) {
	switch {
	case errors.Is(err, ports.ErrUpstreamTimeout), errors.Is(err, context.DeadlineExceeded):
		return KindUpstreamTimeout, CodeUpstreamTimeout, "The upstream model did not answer in time."
	case errors.Is(err, ports.ErrUpstreamModelNotFound):
		return KindUpstreamBad, CodeUpstreamModelNotFound, "Upstream model not found (pull/load the model)."
	case errors.Is(err, ports.ErrUpstreamBadResponse):
		return KindUpstreamBad, CodeUpstreamError, "The upstream model returned an invalid response."
	case errors.Is(err, ports.ErrUpstreamNotConfigured):
		return KindUpstreamNotConfigured, CodeUpstreamNotReady, "The upstream for this model is not configured."
	}
	return KindUpstreamUnavailable, CodeUpstreamDown, "The upstream model is unavailable."
}

// scan canonicalises and masks every message. Messages with roles outside
// ScanRoles are forwarded as is but still masked in the audit.
func (s *Service) scan(ms []ports.ChatMessage) []scanned {
	out := make([]scanned, 0, len(ms))
	for _, m := range ms {
		r := s.scanner.Scan(m.Content)
		masked := secrets.Redact(r.Canonical, r.Findings)
		sc := scanned{role: m.Role, masked: masked, findings: r.Findings, sent: m.Content}
		if s.scanRoles == nil || s.scanRoles[m.Role] {
			sc.guarded = true
			sc.sent = masked
		}
		out = append(out, sc)
	}
	return out
}

func estimateSent(ms []scanned) int {
	t := 0
	for _, m := range ms {
		t += estimateTokens(m.sent)
	}
	return t
}

func (s *Service) auditPrompt(ms []scanned) []ports.ChatMessage {
	out := make([]ports.ChatMessage, 0, len(ms))
	for _, m := range ms {
		content := s.truncate(m.masked)
		if s.sysHash && isSystemRole(m.role) {
			content = s.systemDigest(m.masked)
		}
		out = append(out, ports.ChatMessage{Role: m.role, Content: content})
	}
	return out
}

func isSystemRole(role string) bool { return role == "system" || role == "developer" }

// systemDigest is the audit form of a system prompt in hash mode: a keyed
// fingerprint and the length, never the text.
func (s *Service) systemDigest(masked string) string {
	return fmt.Sprintf("[SYSTEM sha=%s runes=%d]", s.fp.Fingerprint("system_prompt", masked), utf8.RuneCountInString(masked))
}

func withSystemLeakPlaceholder(fs []ports.AuditFinding) []ports.AuditFinding {
	out := append([]ports.AuditFinding(nil), fs...)
	for i := range out {
		if out[i].Type == OutSystemLeak {
			out[i].Placeholder = PlaceholderSystemLeak
		}
	}
	return out
}

const truncMark = "…[truncated]"

func (s *Service) truncate(t string) string {
	if s.maxRunes <= 0 || utf8.RuneCountInString(t) <= s.maxRunes {
		return t
	}
	rs := []rune(t)
	return string(rs[:s.maxRunes]) + truncMark
}

func (s *Service) summarizeInput(fs []secrets.Finding) []ports.AuditFinding {
	type key struct {
		t       secrets.Type
		view    secrets.View
		derived bool
	}
	agg := map[key]*ports.AuditFinding{}
	seen := map[key]map[string]bool{}
	var order []key
	for _, f := range fs {
		k := key{f.Type, f.View, f.Derived}
		a, ok := agg[k]
		if !ok {
			a = &ports.AuditFinding{Type: string(f.Type), View: string(f.View), Action: string(s.policy.ModeFor(f)),
				Derived: f.Derived, Placeholder: f.Placeholder()}
			agg[k] = a
			seen[k] = map[string]bool{}
			order = append(order, k)
		}
		a.Count++
		fp := s.fp.Fingerprint(string(f.Type), f.Value)
		if !seen[k][fp] {
			seen[k][fp] = true
			a.Fingerprints = append(a.Fingerprints, fp)
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

// typeCounts renders "type<sep2>count" pairs aggregated by type, sorted.
func typeCounts(fs []ports.AuditFinding, sep, kv string) string {
	counts := map[string]int{}
	for _, f := range fs {
		counts[f.Type] += f.Count
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s%s%d", k, kv, counts[k]))
	}
	return strings.Join(parts, sep)
}

// FindingsHeader renders findings as "type=count,..." sorted by type.
func FindingsHeader(fs []ports.AuditFinding) string { return typeCounts(fs, ",", "=") }
