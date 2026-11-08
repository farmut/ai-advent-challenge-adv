package usecase

import (
	"strings"
	"testing"
)

func TestParseAgileAnswer(t *testing.T) {
	raw := `Вердикт: Проблема в нечёткой цели.
Принцип: SMART-цели.
Обоснование: Без ясной цели команда теряет фокус.
Рекомендация: Сформулируйте цель по SMART.`

	parsed := ParseAgileAnswer(raw)
	if !strings.Contains(parsed.Verdict, "Проблема") {
		t.Errorf("verdict: %q", parsed.Verdict)
	}
	if !strings.Contains(parsed.Principle, "SMART") {
		t.Errorf("principle: %q", parsed.Principle)
	}
	if !strings.Contains(parsed.Reasoning, "фокус") {
		t.Errorf("reasoning: %q", parsed.Reasoning)
	}
	if !strings.Contains(parsed.Suggestion, "Сформулируйте") {
		t.Errorf("suggestion: %q", parsed.Suggestion)
	}
}

func TestParseAgileAnswerFallback(t *testing.T) {
	raw := "Просто текст ответа без разбивки.\nВторая строка."
	parsed := ParseAgileAnswer(raw)
	if parsed.Verdict != "Просто текст ответа без разбивки." {
		t.Errorf("fallback verdict: %q", parsed.Verdict)
	}
}

func TestParseAgileAnswerMarkdownHeaders(t *testing.T) {
	raw := `**Вердикт:** Применение Agile хорошо.
**Принцип:** Гибкость.
**Обоснование:** Agile помогает адаптироваться.
**Рекомендация:** Используйте Scrum.
1. Разделите на спринты.
2. Проводите ретроспективы.`

	parsed := ParseAgileAnswer(raw)
	if strings.Contains(parsed.Verdict, "Вердикт") {
		t.Errorf("verdict should not contain duplicated header: %q", parsed.Verdict)
	}
	if !strings.Contains(parsed.Verdict, "Применение Agile хорошо") {
		t.Errorf("verdict: %q", parsed.Verdict)
	}
	if !strings.Contains(parsed.Principle, "Гибкость") {
		t.Errorf("principle: %q", parsed.Principle)
	}
	if !strings.Contains(parsed.Reasoning, "адаптироваться") {
		t.Errorf("reasoning: %q", parsed.Reasoning)
	}
	if !strings.Contains(parsed.Suggestion, "Scrum") {
		t.Errorf("suggestion: %q", parsed.Suggestion)
	}
	if !strings.Contains(parsed.Suggestion, "1. Разделите на спринты") {
		t.Errorf("suggestion should preserve numbered list: %q", parsed.Suggestion)
	}
}

func TestParseAgileAnswerDuplicateHeaderLines(t *testing.T) {
	// Модель иногда выдаёт пустой заголовок, а потом markdown-заголовок с текстом.
	raw := `Вердикт:
**Вердикт:** Применение Agile хорошо.
Принцип: Гибкость.`

	parsed := ParseAgileAnswer(raw)
	if parsed.Verdict != "Применение Agile хорошо." {
		t.Errorf("expected clean verdict, got: %q", parsed.Verdict)
	}
	if !strings.Contains(parsed.Principle, "Гибкость") {
		t.Errorf("principle: %q", parsed.Principle)
	}
}

func TestParseAgileAnswerMarkdownColonOutside(t *testing.T) {
	// Модель иногда обрамляет только слово: **Вердикт**: текст
	raw := `**Вердикт**: Внедрение парного программирования полезно.
**Принцип**: Обучение.
**Обоснование**: Парное программирование распространяет знания.
**Рекомендация**: 1. Начните с добровольцев. 2. Подберите задачи.`

	parsed := ParseAgileAnswer(raw)
	if strings.Contains(parsed.Verdict, "Вердикт") {
		t.Errorf("verdict should not contain header: %q", parsed.Verdict)
	}
	if !strings.Contains(parsed.Verdict, "Внедрение парного программирования полезно") {
		t.Errorf("verdict: %q", parsed.Verdict)
	}
	if !strings.Contains(parsed.Suggestion, "1. Начните с добровольцев") {
		t.Errorf("suggestion should preserve list: %q", parsed.Suggestion)
	}
}
