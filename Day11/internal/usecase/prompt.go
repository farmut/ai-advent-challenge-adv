package usecase

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// QuestionPlaceholder is the marker in a prompt template that is replaced by
// the user's question.
const QuestionPlaceholder = "{{QUESTION}}"

// LegacyQwen25Template is the built-in Qwen2.5 ChatML template used before the
// prompt was moved to a file. It is the fallback when no prompt file is
// configured and is byte-identical to configs/prompts/consultant.v1.txt.
const LegacyQwen25Template = "<|im_start|>system\nТы — Agile-консультант. Отвечай структурированно, используя поля: Вердикт, Принцип, Обоснование, Рекомендация. Отвечай на русском языке.\n<|im_end|>\n<|im_start|>user\n" + QuestionPlaceholder + "\n<|im_end|>\n<|im_start|>assistant\n"

// LegacyTemplateName is the name reported for the built-in template.
const LegacyTemplateName = "legacy-builtin"

// ErrInvalidTemplate is returned when a template does not contain exactly one
// question placeholder.
var ErrInvalidTemplate = errors.New("invalid prompt template")

// PromptTemplate is a named prompt with a single {{QUESTION}} placeholder.
type PromptTemplate struct {
	Name string
	Text string
}

// LegacyTemplate returns the built-in fallback template.
func LegacyTemplate() PromptTemplate {
	return PromptTemplate{Name: LegacyTemplateName, Text: LegacyQwen25Template}
}

// Render substitutes the question into the template. The question is inserted
// verbatim: no escaping or sanitisation is performed.
func (t PromptTemplate) Render(question string) string {
	return strings.Replace(t.Text, QuestionPlaceholder, question, 1)
}

// SHA256 returns the hex-encoded SHA-256 of the template text.
func (t PromptTemplate) SHA256() string {
	sum := sha256.Sum256([]byte(t.Text))
	return hex.EncodeToString(sum[:])
}

// Validate checks that the template contains exactly one placeholder.
func (t PromptTemplate) Validate() error {
	n := strings.Count(t.Text, QuestionPlaceholder)
	if n != 1 {
		return fmt.Errorf("%w %q: expected exactly one %s, found %d", ErrInvalidTemplate, t.Name, QuestionPlaceholder, n)
	}
	return nil
}
