// Package httpdelivery is the Gin HTTP layer of the guard proxy. It is the
// only package that imports Gin.
package httpdelivery

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"agile-consultant/internal/ports"
	"agile-consultant/internal/usecase/proxy"
)

// ChatService is the use case consumed by the handlers.
type ChatService interface {
	Chat(ctx context.Context, c proxy.Call) proxy.Outcome
	// Reject audits a request refused by the auth layer.
	Reject(ctx context.Context, d proxy.Denial) proxy.Outcome
	Aliases() []string
}

// Deps are the router dependencies.
type Deps struct {
	Service ChatService
	// Ready reports readiness for /readyz.
	Ready        func() bool
	Logger       *slog.Logger
	MaxBodyBytes int64
	// Auth checks proxy API keys (required).
	Auth ports.Authenticator
	// AuthFailures limits authentication failures per client IP
	// (required).
	AuthFailures FailureLimiter
}

// NewRouter returns the HTTP handler. Client IPs come only from the TCP
// peer address: X-Forwarded-For and similar headers are ignored. Every
// route except /healthz and /readyz requires a proxy API key; the chain is
// requestID → recovery → accessLog → authFailLimit → auth → bodyLimit.
func NewRouter(d Deps) (http.Handler, error) {
	if d.Service == nil || d.Auth == nil || d.AuthFailures == nil {
		return nil, errors.New("httpdelivery: service, auth and auth failure limiter are required")
	}
	gin.SetMode(gin.ReleaseMode)
	if d.Logger == nil {
		d.Logger = slog.New(slog.DiscardHandler)
	}
	r := gin.New()
	if err := r.SetTrustedProxies(nil); err != nil {
		return nil, err
	}
	r.ForwardedByClientIP = false
	r.RemoteIPHeaders = nil
	r.HandleMethodNotAllowed = true
	r.Use(requestID(), recovery(d.Logger), accessLog(d.Logger),
		authFailLimit(d.AuthFailures, d.Service), auth(d.Auth, d.Service), bodyLimit(d.MaxBodyBytes))

	h := &handlers{svc: d.Service, ready: d.Ready}
	r.POST("/v1/chat/completions", h.chat)
	r.GET("/v1/models", h.models)
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/readyz", h.readyz)
	r.NoRoute(func(c *gin.Context) {
		writeError(c, http.StatusNotFound, "invalid_request_error", "not_found", "", "Unknown endpoint.")
	})
	r.NoMethod(func(c *gin.Context) {
		writeError(c, http.StatusMethodNotAllowed, "invalid_request_error", "method_not_allowed", "", "Method not allowed.")
	})
	return r, nil
}

type handlers struct {
	svc   ChatService
	ready func() bool
}

func (h *handlers) models(c *gin.Context) {
	type model struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		OwnedBy string `json:"owned_by"`
	}
	data := []model{}
	for _, a := range h.svc.Aliases() {
		data = append(data, model{ID: a, Object: "model", OwnedBy: "llm-proxy"})
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
}

func (h *handlers) readyz(c *gin.Context) {
	if h.ready != nil && !h.ready() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ready"})
}
