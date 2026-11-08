package httpdelivery

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"agile-consultant/internal/ports"
	"agile-consultant/internal/usecase/proxy"
)

const (
	ctxKeyID = "key_id"
	// MaxTokenLength bounds the bearer token; longer tokens are refused
	// before any hashing.
	MaxTokenLength = 512
	// authFailPrefix namespaces the failure counter keys.
	authFailPrefix = "authfail:"
	// WWWAuthenticate is the challenge sent with every 401.
	WWWAuthenticate = `Bearer realm="llm-proxy"`
)

// tokenQueryParams are query parameters that look like credentials. A
// request carrying any of them is refused: tokens in URLs end up in proxy
// and browser logs.
var tokenQueryParams = []string{"api_key", "access_token", "key", "token"}

// publicPaths are served without authentication. Their bodies hold only a
// status, so they reveal nothing about the configuration.
var publicPaths = map[string]bool{"/healthz": true, "/readyz": true}

// FailureLimiter counts authentication failures per key. Peek must not
// record; Allow records one failure.
type FailureLimiter interface {
	Peek(key string) ports.RateDecision
	Allow(key string) ports.RateDecision
}

// authFailLimit refuses clients (TCP peer IP) with too many recent
// authentication failures with 429 without checking their key, and counts
// every 401 produced further down the chain.
func authFailLimit(fl FailureLimiter, svc ChatService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if publicPaths[c.Request.URL.Path] {
			c.Next()
			return
		}
		key := authFailPrefix + c.RemoteIP()
		if d := fl.Peek(key); !d.Allowed {
			o := svc.Reject(c.Request.Context(), proxy.Denial{RequestID: reqID(c), ClientIP: c.RemoteIP(),
				Kind: proxy.KindRateLimited, Reason: ports.AuthReasonFailLimit})
			ra := ceilSeconds(d.RetryAfter)
			if ra < 1 {
				ra = 1
			}
			c.Header("Retry-After", strconv.FormatInt(ra, 10))
			writeError(c, o.Kind.HTTPStatus(), o.Kind.ErrorType(), o.Code, "", o.Message)
			c.Abort()
			return
		}
		c.Next()
		if c.Writer.Status() == http.StatusUnauthorized {
			fl.Allow(key)
		}
	}
}

// auth requires a valid proxy API key on every route except publicPaths,
// including unknown routes and methods. On success the Authorization header
// is removed from the request and the key id is put into the context.
func auth(a ports.Authenticator, svc ChatService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if publicPaths[c.Request.URL.Path] {
			c.Next()
			return
		}
		keyID, reason := authenticate(c.Request, a)
		if reason != "" {
			svc.Reject(c.Request.Context(), proxy.Denial{RequestID: reqID(c), ClientIP: c.RemoteIP(),
				Kind: proxy.KindUnauthorized, Reason: reason})
			c.Header("WWW-Authenticate", WWWAuthenticate)
			writeError(c, http.StatusUnauthorized, proxy.KindUnauthorized.ErrorType(), proxy.CodeInvalidAPIKey, "", proxy.MessageUnauthorized)
			c.Abort()
			return
		}
		c.Request.Header.Del("Authorization")
		c.Set(ctxKeyID, keyID)
		c.Next()
	}
}

// authenticate returns the key id or a refusal reason (ports.AuthReason*).
func authenticate(r *http.Request, a ports.Authenticator) (string, string) {
	if hasTokenQuery(r.URL) {
		return "", ports.AuthReasonQuery
	}
	token, reason := ParseBearer(r.Header)
	if reason != "" {
		return "", reason
	}
	id, ok := a.Authenticate(token)
	if !ok {
		return "", ports.AuthReasonInvalid
	}
	return id, ""
}

func hasTokenQuery(u *url.URL) bool {
	if u.RawQuery == "" {
		return false
	}
	q, _ := url.ParseQuery(u.RawQuery)
	for k := range q {
		for _, p := range tokenQueryParams {
			if strings.EqualFold(k, p) {
				return true
			}
		}
	}
	return false
}

// ParseBearer extracts the token from exactly one Authorization header of
// the form "Bearer <token>": case-insensitive scheme, exactly one space and
// a token without whitespace of at most MaxTokenLength bytes. It returns a
// refusal reason instead of a token on any deviation.
func ParseBearer(h http.Header) (string, string) {
	vals := h.Values("Authorization")
	switch {
	case len(vals) == 0, len(vals) == 1 && vals[0] == "":
		return "", ports.AuthReasonMissing
	case len(vals) > 1:
		return "", ports.AuthReasonScheme
	}
	scheme, token, ok := strings.Cut(vals[0], " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") || token == "" || strings.ContainsAny(token, " \t\r\n\v\f") {
		return "", ports.AuthReasonScheme
	}
	if len(token) > MaxTokenLength {
		return "", ports.AuthReasonTooLong
	}
	return token, ""
}
