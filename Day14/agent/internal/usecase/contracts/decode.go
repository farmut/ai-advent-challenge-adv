// Package contracts holds the JSON contracts exchanged with LLM roles and the
// strict decoder every one of them goes through. A role's answer is data from
// an untrusted source: it is accepted only when it is exactly one JSON object
// of the expected schema, with no unknown fields and no trailing data, and
// passes the schema's own validation.
package contracts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// MaxAnswerBytes bounds the answer text DecodeStrict looks at. It is part of
// the contract (a review of ≤20 findings with ≤2 KB fields fits easily), not
// a tunable.
const MaxAnswerBytes = 256 << 10

// validatable is a pointer to T with a Validate method. Validate may
// normalise the value (trim lists, cut long fields) as well as reject it.
type validatable[T any] interface {
	*T
	Validate() error
}

// DecodeStrict extracts the JSON object from an LLM answer and decodes it
// into T:
//   - the object is taken from the first ```json fenced block or, without a
//     fence, from the first balanced {…};
//   - unknown fields and trailing data after the object are errors;
//   - Validate runs last.
//
// The error text is meant to be shown to the model on a retry: it names what
// was wrong without echoing the answer back.
func DecodeStrict[T any, PT validatable[T]](content string) (T, error) {
	var zero T
	if len(content) > MaxAnswerBytes {
		return zero, fmt.Errorf("ответ длиннее %d байт", MaxAnswerBytes)
	}
	raw, err := extractJSON(content)
	if err != nil {
		return zero, err
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var v T
	if err := dec.Decode(&v); err != nil {
		return zero, fmt.Errorf("невалидный JSON: %v", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return zero, fmt.Errorf("после JSON-объекта есть лишние данные")
	}
	if err := PT(&v).Validate(); err != nil {
		return zero, err
	}
	return v, nil
}

// extractJSON returns the JSON text of the answer: the body of the first
// ```json fence, or the first balanced {…} object.
func extractJSON(s string) (string, error) {
	if i := strings.Index(s, "```json"); i >= 0 {
		body := s[i+len("```json"):]
		end := strings.Index(body, "```")
		if end < 0 {
			return "", fmt.Errorf("блок ```json не закрыт")
		}
		body = strings.TrimSpace(body[:end])
		if body == "" {
			return "", fmt.Errorf("блок ```json пуст")
		}
		return body, nil
	}
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return "", fmt.Errorf("в ответе нет JSON-объекта (ожидается один блок ```json)")
	}
	end, ok := balancedEnd(s[start:])
	if !ok {
		return "", fmt.Errorf("JSON-объект не закрыт")
	}
	return s[start : start+end], nil
}

// balancedEnd returns the length of the {…} object at the start of s,
// skipping braces inside strings.
func balancedEnd(s string) (int, bool) {
	depth := 0
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1, true
			}
		}
	}
	return 0, false
}

// compactJSON renders v as one-line JSON without HTML escaping, so Russian
// text and "<" stay readable for the model.
func compactJSON(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		// Only plain structs of strings and ints are encoded here.
		return "{}"
	}
	return strings.TrimSpace(b.String())
}
