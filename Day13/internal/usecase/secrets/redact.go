package secrets

import (
	"sort"
	"strings"
)

// Span is a byte range of a text to replace with Placeholder. Priority
// breaks ties between overlapping spans of equal length (smaller wins).
type Span struct {
	Start, End  int
	Placeholder string
	Priority    int
}

// Spans converts findings to spans.
func Spans(fs []Finding) []Span {
	out := make([]Span, 0, len(fs))
	for _, f := range fs {
		out = append(out, Span{Start: f.Start, End: f.End, Placeholder: f.Placeholder(), Priority: priority(f.Type)})
	}
	return out
}

// Redact replaces every finding span in canon with its placeholder.
func Redact(canon string, fs []Finding) string { return RedactSpans(canon, Spans(fs)) }

// RedactSpans replaces the spans in text. Overlapping spans are merged into
// their union, so no byte of any span survives; the placeholder of the
// longest span (then the smaller priority) is used. Invalid spans are
// ignored.
func RedactSpans(text string, in []Span) string {
	type span struct {
		a, b, n, prio int
		ph            string
	}
	spans := make([]span, 0, len(in))
	for _, s := range in {
		if s.Start < 0 || s.End > len(text) || s.End <= s.Start {
			continue
		}
		spans = append(spans, span{s.Start, s.End, s.End - s.Start, s.Priority, s.Placeholder})
	}
	if len(spans) == 0 {
		return text
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].a < spans[j].a })
	merged := []span{spans[0]}
	for _, s := range spans[1:] {
		last := &merged[len(merged)-1]
		if s.a >= last.b {
			merged = append(merged, s)
			continue
		}
		if s.n > last.n || (s.n == last.n && s.prio < last.prio) {
			last.n, last.prio, last.ph = s.n, s.prio, s.ph
		}
		if s.b > last.b {
			last.b = s.b
		}
	}
	var b strings.Builder
	b.Grow(len(text))
	pos := 0
	for _, s := range merged {
		b.WriteString(text[pos:s.a])
		b.WriteString(s.ph)
		pos = s.b
	}
	b.WriteString(text[pos:])
	return b.String()
}
