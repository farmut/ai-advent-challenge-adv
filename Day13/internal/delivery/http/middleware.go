package httpdelivery

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	headerRequestID = "X-Request-ID"
	ctxRequestID    = "request_id"
)

var requestIDRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// requestID accepts a well-formed client X-Request-ID or generates one.
func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(headerRequestID)
		if !requestIDRe.MatchString(id) {
			id = newID()
		}
		c.Set(ctxRequestID, id)
		c.Header(headerRequestID, id)
		c.Next()
	}
}

func newID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("t%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func reqID(c *gin.Context) string { return c.GetString(ctxRequestID) }

// recovery turns panics into 500 internal_error and logs only the request
// id and the panic type (never the value, which may contain user data).
func recovery(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				if r == http.ErrAbortHandler {
					panic(r)
				}
				log.Error("panic recovered", slog.String("request_id", reqID(c)), slog.String("type", fmt.Sprintf("%T", r)))
				if !c.Writer.Written() {
					writeError(c, http.StatusInternalServerError, "api_error", "internal_error", "", "Internal error.")
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}

// accessLog logs method, path without query, status and latency. Bodies and
// headers are never logged.
func accessLog(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Info("http",
			slog.String("request_id", reqID(c)),
			slog.String("method", c.Request.Method),
			slog.String("path", c.Request.URL.Path),
			slog.Int("status", c.Writer.Status()),
			slog.Int64("latency_ms", time.Since(start).Milliseconds()),
		)
	}
}

// bodyLimit caps the request body.
func bodyLimit(n int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if n > 0 && c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, n)
		}
		c.Next()
	}
}

func isTooLarge(err error) bool {
	var mbe *http.MaxBytesError
	return errors.As(err, &mbe)
}
