// Package proxy is the guard-proxy pipeline: rate limit, validation,
// routing, input guard, upstream call, output guard, cost and audit. It
// depends only on the standard library, ports and other use case packages.
package proxy

import (
	"net/http"

	"agile-consultant/internal/ports"
)

// OutcomeKind classifies the result of a chat call.
type OutcomeKind int

// Outcome kinds.
const (
	KindOK OutcomeKind = iota
	KindInputBlocked
	KindRateLimited
	KindInvalid
	KindTooLarge
	KindModelNotFound
	KindUpstreamTimeout
	KindUpstreamUnavailable
	KindUpstreamBad
	KindUpstreamNotConfigured
	KindInternal
	// KindUnauthorized is a request without a valid proxy API key. It is
	// produced by Service.Reject, never by Chat.
	KindUnauthorized
)

// Error codes (OpenAI error.code).
const (
	CodeInputBlocked    = "guard_input_blocked"
	CodeRateLimited     = "rate_limit_exceeded"
	CodeUnsupported     = "unsupported_parameter"
	CodeInvalidJSON     = "invalid_json"
	CodeInvalidRequest  = "invalid_request"
	CodeTooLarge        = "request_too_large"
	CodeModelNotFound   = "model_not_found"
	CodeUpstreamTimeout = "upstream_timeout"
	CodeUpstreamDown    = "upstream_unavailable"
	CodeUpstreamError   = "upstream_error"
	// CodeUpstreamModelNotFound is an upstream 404 (model not pulled/loaded).
	CodeUpstreamModelNotFound = "upstream_model_not_found"
	CodeUpstreamNotReady      = "upstream_not_configured"
	CodeInternal              = "internal_error"
	CodeInvalidAPIKey         = "invalid_api_key"
)

// MessageUnauthorized is the single 401 message: it does not reveal why
// the key was refused (missing, wrong scheme, unknown key, token in query).
const MessageUnauthorized = "Invalid or missing API key."

// HTTPStatus is the response status for the kind. It is defined here so the
// audit record and the HTTP response cannot disagree.
func (k OutcomeKind) HTTPStatus() int {
	switch k {
	case KindOK:
		return http.StatusOK
	case KindUnauthorized:
		return http.StatusUnauthorized
	case KindInputBlocked, KindInvalid:
		return http.StatusBadRequest
	case KindRateLimited:
		return http.StatusTooManyRequests
	case KindTooLarge:
		return http.StatusRequestEntityTooLarge
	case KindModelNotFound:
		return http.StatusNotFound
	case KindUpstreamTimeout:
		return http.StatusGatewayTimeout
	case KindUpstreamUnavailable, KindUpstreamNotConfigured:
		return http.StatusServiceUnavailable
	case KindUpstreamBad:
		return http.StatusBadGateway
	}
	return http.StatusInternalServerError
}

// ErrorType is the OpenAI error.type for the kind.
func (k OutcomeKind) ErrorType() string {
	switch k {
	case KindInputBlocked, KindInvalid, KindTooLarge, KindModelNotFound, KindUnauthorized:
		return "invalid_request_error"
	case KindRateLimited:
		return "rate_limit_error"
	}
	return "api_error"
}

// AuditStatus is the audit status for the kind (output_blocked is set by
// the pipeline on top of KindOK).
func (k OutcomeKind) AuditStatus() string {
	switch k {
	case KindOK:
		return ports.AuditStatusOK
	case KindInputBlocked:
		return ports.AuditStatusInputBlocked
	case KindRateLimited:
		return ports.AuditStatusRateLimited
	case KindInvalid, KindTooLarge:
		return ports.AuditStatusInvalid
	case KindModelNotFound:
		return ports.AuditStatusModelNotFound
	case KindUnauthorized:
		return ports.AuditStatusUnauthorized
	}
	return ports.AuditStatusUpstreamError
}
