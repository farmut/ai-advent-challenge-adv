package proxy

import (
	"context"
	"log/slog"

	"agile-consultant/internal/ports"
)

// Denial is a request refused before the chat pipeline, by the
// authentication layer: KindUnauthorized (401) or KindRateLimited (too
// many authentication failures from the IP). Nothing of the request (body,
// token, HMAC of the token) is audited.
type Denial struct {
	RequestID string
	ClientIP  string
	Kind      OutcomeKind
	// Reason is one of the ports.AuthReason* values.
	Reason string
}

// Reject audits a denial and returns its outcome. Audit failures are
// logged; the request is refused anyway.
func (s *Service) Reject(ctx context.Context, d Denial) Outcome {
	now := s.clock.Now()
	o := Outcome{Kind: d.Kind, Created: now, Usage: ports.Usage{Source: ports.UsageSourceNone}}
	switch d.Kind {
	case KindRateLimited:
		o.Code, o.Message = CodeRateLimited, "Too many failed authentication attempts. Retry later."
	default:
		o.Kind, o.Code, o.Message = KindUnauthorized, CodeInvalidAPIKey, MessageUnauthorized
	}
	rec := ports.AuditRecord{
		V: ports.AuditVersion, TS: now, RequestID: d.RequestID,
		ClientIPHash: s.fp.Fingerprint("client_ip", d.ClientIP),
		Status:       o.Kind.AuditStatus(), HTTPStatus: o.Kind.HTTPStatus(), ErrorCode: o.Code, Reason: d.Reason,
		Input:        ports.AuditInput{Mode: string(s.defaultMode())},
		PromptMasked: []ports.ChatMessage{},
		Usage:        ports.AuditUsage{Source: ports.UsageSourceNone},
	}
	if err := s.audit.Write(ctx, rec); err != nil {
		s.log.Error("audit write failed", slog.String("request_id", d.RequestID), slog.String("error", err.Error()))
	}
	return o
}
