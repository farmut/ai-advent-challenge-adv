package contracts

import (
	"strconv"
	"strings"
	"unicode"

	"ai-adv-agent/internal/domain"
)

// SchemaFeedback is the schema of the feedback the generator receives.
const SchemaFeedback = "feedback.v1"

// FeedbackKind classifies a feedback item.
type FeedbackKind string

// Feedback kinds.
const (
	FeedbackSecurity  FeedbackKind = "security"
	FeedbackTest      FeedbackKind = "test"
	FeedbackGuard     FeedbackKind = "guard"
	FeedbackNoChanges FeedbackKind = "no_changes"
	// FeedbackCriteria is for unmet acceptance criteria (verifier, PR7).
	FeedbackCriteria FeedbackKind = "criteria"
)

// Limits on the reviewer-supplied parts of a feedback line. Feedback reaches
// the generator as instructions (not as DATA), so every untrusted fragment
// in it is short, single-line and free of DATA markers.
const (
	maxIssueRunes  = 200
	maxFixRunes    = 300
	maxFileRunes   = 256
	maxReasonRunes = 300
)

// FeedbackItem is one instruction for the generator.
type FeedbackItem struct {
	Kind FeedbackKind `json:"kind"`
	Text string       `json:"text"`
}

// Feedback is built by code only. A reviewer's raw text never reaches the
// generator: security items are rendered from finding fields by template.
type Feedback struct {
	Schema  string         `json:"schema"`
	Attempt int            `json:"attempt"`
	Items   []FeedbackItem `json:"items"`
}

// NewFeedback stamps the schema.
func NewFeedback(attempt int, items []FeedbackItem) Feedback {
	return Feedback{Schema: SchemaFeedback, Attempt: attempt, Items: items}
}

// JSON renders the feedback as one line of JSON.
func (f Feedback) JSON() string { return compactJSON(f) }

// Texts returns the item texts (for the persisted attempt).
func (f Feedback) Texts() []string {
	out := make([]string, 0, len(f.Items))
	for _, it := range f.Items {
		out = append(out, it.Text)
	}
	return out
}

// SecurityItem renders a blocking finding with the fixed template
//
//	исправь: <CWE> <issue> в <file>:<line> — <fix>
//
// The fix part is omitted when the finding has none.
func SecurityItem(f domain.Finding) FeedbackItem {
	var b strings.Builder
	b.WriteString("исправь: ")
	b.WriteString(sanitize(f.CWE, 16))
	b.WriteString(" ")
	b.WriteString(sanitize(f.Issue, maxIssueRunes))
	b.WriteString(" в ")
	b.WriteString(sanitize(f.File, maxFileRunes))
	b.WriteString(":")
	b.WriteString(strconv.Itoa(f.Line))
	if fix := sanitize(f.Fix, maxFixRunes); fix != "" {
		b.WriteString(" — ")
		b.WriteString(fix)
	}
	return FeedbackItem{Kind: FeedbackSecurity, Text: b.String()}
}

// TestItem says which command failed; its output goes in a DATA block.
func TestItem(argv []string, exitCode int, timedOut bool) FeedbackItem {
	what := "завершилась с кодом " + strconv.Itoa(exitCode)
	if timedOut {
		what = "превысила таймаут"
	}
	return FeedbackItem{Kind: FeedbackTest, Text: "исправь: команда `" + sanitize(strings.Join(argv, " "), maxReasonRunes) +
		"` " + what + ". Хвост вывода — в DATA-блоке test-output; исправь код или тесты так, чтобы проверка прошла."}
}

// GuardItem explains that the guard proxy blocked the previous answer.
func GuardItem(reason string) FeedbackItem {
	r := sanitize(reason, maxReasonRunes)
	if r == "" {
		r = "причина не указана"
	}
	return FeedbackItem{Kind: FeedbackGuard, Text: "ответ заблокирован guard'ом: " + r +
		". Не выводи опасные команды, секреты, ключи и подозрительные URL — значения бери из переменных окружения."}
}

// NoChangesItem says the previous attempt changed no file.
func NoChangesItem() FeedbackItem {
	return FeedbackItem{Kind: FeedbackNoChanges, Text: "изменений нет: в каталоге задачи не изменён ни один файл. Внеси изменения инструментами write_file/edit_file."}
}

// sanitize makes an untrusted fragment safe to embed in an instruction line:
// one line, no control characters, no DATA markers, at most n runes.
func sanitize(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	s = strings.NewReplacer("<<<", "‹‹‹", ">>>", "›››").Replace(s)
	return cutRunes(s, n)
}
