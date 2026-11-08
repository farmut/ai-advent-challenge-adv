package usecase

import (
	"regexp"
	"strings"
)

var mdHeaderRe = regexp.MustCompile(`(?i)(\*{1,2}|_{1,2})(вердикт|принцип|обоснование|рекомендация)(\*{1,2}|_{1,2}):`)

type ParsedAnswer struct {
	Verdict    string
	Principle  string
	Reasoning  string
	Suggestion string
}

func ParseAgileAnswer(raw string) ParsedAnswer {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ParsedAnswer{}
	}
	// Убираем Markdown-обёртку вокруг заголовков полей: **Вердикт**: -> Вердикт:
	raw = mdHeaderRe.ReplaceAllString(raw, "$2:")

	result := ParsedAnswer{}
	lines := strings.Split(raw, "\n")

	fieldMap := map[string]*string{
		"вердикт":      &result.Verdict,
		"принцип":      &result.Principle,
		"обоснование":  &result.Reasoning,
		"рекомендация": &result.Suggestion,
	}

	var currentField *string
	var buf strings.Builder

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if key, value, ok := normalizeHeader(trimmed); ok {
			if currentField != nil && buf.Len() > 0 {
				*currentField = strings.TrimSpace(buf.String())
			}
			buf.Reset()
			currentField = fieldMap[key]
			if value != "" {
				buf.WriteString(value)
			}
			continue
		}

		if currentField != nil {
			if buf.Len() > 0 {
				buf.WriteString(" ")
			}
			buf.WriteString(trimmed)
		}
	}

	if currentField != nil && buf.Len() > 0 {
		*currentField = strings.TrimSpace(buf.String())
	}

	if result.Verdict == "" {
		if idx := strings.Index(raw, "\n"); idx > 0 {
			result.Verdict = strings.TrimSpace(raw[:idx])
			result.Suggestion = strings.TrimSpace(raw[idx+1:])
		} else {
			result.Verdict = raw
		}
	}

	return result
}

// normalizeHeader распознаёт заголовки полей независимо от регистра
// и обрамляющей Markdown-разметки (**Вердикт:**, __Принцип:__ и т.п.).
func normalizeHeader(line string) (key, value string, ok bool) {
	trimmed := strings.TrimSpace(line)

	// Убираем ведущие/завершающие Markdown-символы.
	trimmed = stripMarkdown(trimmed)

	lower := strings.ToLower(trimmed)
	fields := []string{"вердикт", "принцип", "обоснование", "рекомендация"}
	for _, f := range fields {
		prefix := f + ":"
		if strings.HasPrefix(lower, prefix) {
			value = stripMarkdown(trimmed[len(prefix):])
			return f, value, true
		}
	}
	return "", "", false
}

func stripMarkdown(s string) string {
	s = strings.TrimSpace(s)
	for {
		changed := false
		for _, prefix := range []string{"**", "*", "__", "_"} {
			if strings.HasPrefix(s, prefix) {
				s = strings.TrimPrefix(s, prefix)
				changed = true
			}
		}
		for _, suffix := range []string{"**", "*", "__", "_"} {
			if strings.HasSuffix(s, suffix) {
				s = strings.TrimSuffix(s, suffix)
				changed = true
			}
		}
		if !changed {
			break
		}
		s = strings.TrimSpace(s)
	}
	return s
}
