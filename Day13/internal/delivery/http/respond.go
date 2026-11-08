package httpdelivery

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"agile-consultant/internal/ports"
	"agile-consultant/internal/usecase/proxy"
)

// Response headers.
const (
	HeaderGuardInput          = "X-Guard-Input"
	HeaderGuardInputFindings  = "X-Guard-Input-Findings"
	HeaderGuardOutput         = "X-Guard-Output"
	HeaderGuardOutputFindings = "X-Guard-Output-Findings"
	HeaderGuardWarning        = "X-Guard-Warning"
	HeaderGuardCost           = "X-Guard-Cost-USD"
	HeaderGuardUsageSource    = "X-Guard-Usage-Source"
	HeaderRateLimit           = "X-RateLimit-Limit"
	HeaderRateRemaining       = "X-RateLimit-Remaining"
	HeaderRateReset           = "X-RateLimit-Reset"
)

type errorFinding struct {
	Type  string `json:"type"`
	Count int    `json:"count"`
	View  string `json:"view,omitempty"`
}

type errorBody struct {
	Type     string         `json:"type"`
	Code     string         `json:"code"`
	Param    *string        `json:"param"`
	Message  string         `json:"message"`
	Warning  string         `json:"warning,omitempty"`
	Findings []errorFinding `json:"findings,omitempty"`
}

func writeError(c *gin.Context, status int, typ, code, param, msg string) {
	c.JSON(status, gin.H{"error": newErrorBody(typ, code, param, msg)})
}

func newErrorBody(typ, code, param, msg string) errorBody {
	b := errorBody{Type: typ, Code: code, Message: msg}
	if param != "" {
		b.Param = &param
	}
	return b
}

func ceilSeconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64(math.Ceil(d.Seconds()))
}

// writeOutcome renders the pipeline outcome (contract: plan section 3).
func writeOutcome(c *gin.Context, o proxy.Outcome) {
	d := o.Decision
	if d.Limit > 0 {
		c.Header(HeaderRateLimit, strconv.Itoa(d.Limit))
		c.Header(HeaderRateRemaining, strconv.Itoa(d.Remaining))
		reset := int64(0)
		if !d.ResetAt.IsZero() {
			reset = ceilSeconds(d.ResetAt.Sub(o.Created).Truncate(time.Millisecond))
		}
		c.Header(HeaderRateReset, strconv.FormatInt(reset, 10))
	}
	status := o.Kind.HTTPStatus()
	switch o.Kind {
	case proxy.KindOK:
		writeOK(c, o)
		return
	case proxy.KindRateLimited:
		ra := ceilSeconds(d.RetryAfter)
		if ra < 1 {
			ra = 1
		}
		c.Header("Retry-After", strconv.FormatInt(ra, 10))
	case proxy.KindInputBlocked:
		c.Header(HeaderGuardInput, "blocked")
		c.Header(HeaderGuardInputFindings, proxy.FindingsHeader(o.InputFindings))
		c.Header(HeaderGuardWarning, o.Warning)
		body := newErrorBody(o.Kind.ErrorType(), o.Code, o.Param, o.Message)
		body.Warning = o.Warning
		for _, f := range o.InputFindings {
			body.Findings = append(body.Findings, errorFinding{Type: f.Type, Count: f.Count, View: f.View})
		}
		c.JSON(status, gin.H{"error": body})
		return
	}
	writeError(c, status, o.Kind.ErrorType(), o.Code, o.Param, o.Message)
}

type chatChoice struct {
	Index        int               `json:"index"`
	Message      ports.ChatMessage `json:"message"`
	FinishReason string            `json:"finish_reason"`
}

type chatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type chatCompletion struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []chatChoice `json:"choices"`
	Usage   chatUsage    `json:"usage"`
}

func writeOK(c *gin.Context, o proxy.Outcome) {
	in := o.InputAction
	if in == "" {
		in = "pass"
	}
	c.Header(HeaderGuardInput, in)
	if len(o.InputFindings) > 0 {
		c.Header(HeaderGuardInputFindings, proxy.FindingsHeader(o.InputFindings))
	}
	c.Header(HeaderGuardOutput, o.OutputAction)
	if len(o.OutputFindings) > 0 {
		c.Header(HeaderGuardOutputFindings, proxy.FindingsHeader(o.OutputFindings))
	}
	c.Header(HeaderGuardCost, proxy.FormatUSD(o.CostMicroUSD))
	c.Header(HeaderGuardUsageSource, o.Usage.Source)
	c.JSON(http.StatusOK, chatCompletion{
		ID: "chatcmpl-" + reqID(c), Object: "chat.completion", Created: o.Created.Unix(), Model: o.Model,
		Choices: []chatChoice{{Index: 0, Message: ports.ChatMessage{Role: "assistant", Content: o.Content}, FinishReason: o.FinishReason}},
		Usage:   chatUsage{PromptTokens: o.Usage.PromptTokens, CompletionTokens: o.Usage.CompletionTokens, TotalTokens: o.Usage.PromptTokens + o.Usage.CompletionTokens},
	})
}
