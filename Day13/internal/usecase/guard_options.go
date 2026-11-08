package usecase

import (
	"errors"

	"agile-consultant/internal/ports"
)

// Default input limits in runes.
const (
	DefaultMaxQuestionRunes = 4000
	DefaultMaxContextRunes  = 8000
)

// RefusalAnswer is delivered instead of the model answer when the guard
// blocks a request. It follows the 4-field format.
const RefusalAnswer = "Вердикт: Вне компетенции\n" +
	"Принцип: Фокус на ценности — консультант отвечает только на вопросы об Agile-практиках.\n" +
	"Обоснование: Запрос не прошёл проверку безопасности и не был обработан.\n" +
	"Рекомендация: Переформулируйте вопрос обычным текстом, например: «Как провести ретроспективу спринта?»"

// Errors returned by Consultant.Ask.
var (
	ErrInputTooLong  = errors.New("input too long")
	ErrEmptyQuestion = errors.New("empty question")
)

// GuardOptions configures the input/output guard. The limits apply in both
// modes; sanitisation, nonce boundaries and output validation only when
// Enabled.
type GuardOptions struct {
	Enabled          bool
	MaxQuestionRunes int
	MaxContextRunes  int
}

// DefaultGuardOptions returns the guard enabled with default limits.
func DefaultGuardOptions() GuardOptions {
	return GuardOptions{
		Enabled:          true,
		MaxQuestionRunes: DefaultMaxQuestionRunes,
		MaxContextRunes:  DefaultMaxContextRunes,
	}
}

func (o GuardOptions) withDefaults() GuardOptions {
	if o.MaxQuestionRunes <= 0 {
		o.MaxQuestionRunes = DefaultMaxQuestionRunes
	}
	if o.MaxContextRunes <= 0 {
		o.MaxContextRunes = DefaultMaxContextRunes
	}
	return o
}

// GuardDeps are the adapters the guard needs. Both are required.
type GuardDeps struct {
	Normalizer ports.TextNormalizer
	Nonce      ports.NonceSource
}
