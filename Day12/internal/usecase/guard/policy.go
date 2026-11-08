package guard

import (
	"fmt"
	"strings"
)

// Policy decides which sanitiser findings block a request. Everything not
// listed as blocking is silently cleaned.
type Policy struct {
	// MaxInvisible is the largest tolerated number of removed invisible
	// characters per text.
	MaxInvisible int
	// MaxRatio is the largest tolerated share of invisible characters.
	MaxRatio float64
	// MinInvisibleForRatio is the minimum invisible count before MaxRatio
	// applies, so a single stray zero-width space in a short text is
	// cleaned rather than blocked.
	MinInvisibleForRatio int
}

// DefaultPolicy returns the production thresholds.
func DefaultPolicy() Policy {
	return Policy{MaxInvisible: 16, MaxRatio: 0.1, MinInvisibleForRatio: 3}
}

// blockingKinds always block when present.
var blockingKinds = []Kind{KindSpecialToken, KindTagChars, KindVSSupplement, KindBoundaryClose, KindUnstable}

// Decision is the policy verdict for one request.
type Decision struct {
	Block bool
	// Reasons are "<part>:<kind>" strings, e.g. "question:special_token".
	Reasons []string
}

// Decide evaluates the sanitised question and document.
func (p Policy) Decide(question, document Result) Decision {
	var reasons []string
	reasons = append(reasons, p.reasons("question", question)...)
	reasons = append(reasons, p.reasons("document", document)...)
	if strings.TrimSpace(question.Clean) == "" {
		reasons = append(reasons, "question:empty_after_sanitize")
	}
	return Decision{Block: len(reasons) > 0, Reasons: reasons}
}

func (p Policy) reasons(part string, r Result) []string {
	var out []string
	for _, k := range blockingKinds {
		if r.Has(k) {
			out = append(out, part+":"+string(k))
		}
	}
	if r.InvisibleCount > p.MaxInvisible {
		out = append(out, fmt.Sprintf("%s:%s(count=%d>%d)", part, KindInvisibleRatio, r.InvisibleCount, p.MaxInvisible))
	} else if r.InvisibleCount >= p.MinInvisibleForRatio && r.InvisibleRatio > p.MaxRatio {
		out = append(out, fmt.Sprintf("%s:%s(ratio=%.2f>%.2f)", part, KindInvisibleRatio, r.InvisibleRatio, p.MaxRatio))
	}
	return out
}
