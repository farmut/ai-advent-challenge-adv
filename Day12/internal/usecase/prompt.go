package usecase

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"agile-consultant/internal/ports"
	"agile-consultant/internal/usecase/guard"
)

// Prompt template placeholders.
const (
	// UserInputPlaceholder is replaced by the boundary-wrapped user input.
	// It must appear exactly once, in the single user section.
	UserInputPlaceholder = "{{USER_INPUT}}"
	// QuestionTagPlaceholder is replaced by the name of the tag that wraps
	// the question (e.g. user_question or user_data_<nonce>).
	QuestionTagPlaceholder = "{{QUESTION_TAG}}"
	// ContextTagPlaceholder is replaced by the name of the tag that wraps
	// the attached document.
	ContextTagPlaceholder = "{{CONTEXT_TAG}}"
)

// ErrInvalidTemplate is returned when a prompt template is malformed.
var ErrInvalidTemplate = errors.New("invalid prompt template")

var (
	sectionMarkerRe = regexp.MustCompile(`^### (system|user)$`)
	canaryRe        = regexp.MustCompile(`CANARY-[A-Za-z0-9-]+`)
)

// Section is one chat message of a prompt template.
type Section struct {
	Role string
	Text string
}

// PromptTemplate is a named prompt split into role sections. The file format
// is a sequence of sections, each introduced by a line "### system" or
// "### user"; text before the first marker is not allowed.
type PromptTemplate struct {
	Name     string
	Text     string
	Sections []Section
}

// RenderInput holds the values substituted into a template.
type RenderInput struct {
	QuestionTag string
	ContextTag  string
	UserInput   string
}

// ParsePromptTemplate parses and validates a prompt template.
func ParsePromptTemplate(name, text string) (PromptTemplate, error) {
	t := PromptTemplate{Name: name, Text: text}
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	var cur *Section
	var body []string
	flush := func() {
		if cur != nil {
			cur.Text = strings.Trim(strings.Join(body, "\n"), "\n")
			t.Sections = append(t.Sections, *cur)
		}
		body = body[:0]
	}
	for i, line := range strings.Split(normalized, "\n") {
		if m := sectionMarkerRe.FindStringSubmatch(line); m != nil {
			flush()
			cur = &Section{Role: m[1]}
			continue
		}
		if cur == nil {
			if strings.TrimSpace(line) != "" {
				return PromptTemplate{}, fmt.Errorf("%w %q: line %d: text before the first section marker", ErrInvalidTemplate, name, i+1)
			}
			continue
		}
		body = append(body, line)
	}
	flush()
	if err := t.Validate(); err != nil {
		return PromptTemplate{}, err
	}
	return t, nil
}

// Validate checks the section invariants: at least one system section,
// exactly one user section with exactly one {{USER_INPUT}}, tag placeholders
// at least once and only in system sections, no ChatML special tokens.
func (t PromptTemplate) Validate() error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%w %q: %s", ErrInvalidTemplate, t.Name, fmt.Sprintf(format, args...))
	}
	if len(t.Sections) == 0 {
		return fail("no sections (expected \"### system\" / \"### user\" markers)")
	}
	var systems, users, qTags, cTags int
	for i, s := range t.Sections {
		if strings.Contains(s.Text, "<|") {
			return fail("section %d contains a ChatML special token", i+1)
		}
		inputs := strings.Count(s.Text, UserInputPlaceholder)
		q := strings.Count(s.Text, QuestionTagPlaceholder)
		c := strings.Count(s.Text, ContextTagPlaceholder)
		switch s.Role {
		case ports.RoleSystem:
			systems++
			if inputs > 0 {
				return fail("%s in system section %d", UserInputPlaceholder, i+1)
			}
			if strings.TrimSpace(s.Text) == "" {
				return fail("empty system section %d", i+1)
			}
			qTags += q
			cTags += c
		case ports.RoleUser:
			users++
			if inputs != 1 {
				return fail("user section must contain exactly one %s, found %d", UserInputPlaceholder, inputs)
			}
			if q > 0 || c > 0 {
				return fail("tag placeholders are allowed only in system sections")
			}
		default:
			return fail("unknown role %q", s.Role)
		}
	}
	switch {
	case systems == 0:
		return fail("at least one system section is required")
	case users != 1:
		return fail("exactly one user section is required, found %d", users)
	case qTags == 0:
		return fail("%s is required in a system section", QuestionTagPlaceholder)
	case cTags == 0:
		return fail("%s is required in a system section", ContextTagPlaceholder)
	}
	return nil
}

// Render builds the chat messages. Tag placeholders are substituted only in
// system sections; the user input is inserted last and verbatim, so
// placeholders inside user text are never expanded.
func (t PromptTemplate) Render(in RenderInput) []ports.Message {
	tags := strings.NewReplacer(QuestionTagPlaceholder, in.QuestionTag, ContextTagPlaceholder, in.ContextTag)
	out := make([]ports.Message, 0, len(t.Sections))
	for _, s := range t.Sections {
		text := s.Text
		if s.Role == ports.RoleUser {
			text = strings.Replace(text, UserInputPlaceholder, in.UserInput, 1)
		} else {
			text = tags.Replace(text)
		}
		out = append(out, ports.Message{Role: s.Role, Content: text})
	}
	return out
}

// SystemText returns all system sections joined by a blank line, with tag
// placeholders replaced by the static (guard off) tag names.
func (t PromptTemplate) SystemText() string {
	tags := strings.NewReplacer(QuestionTagPlaceholder, guard.StaticQuestionTag, ContextTagPlaceholder, guard.StaticContextTag)
	parts := make([]string, 0, len(t.Sections))
	for _, s := range t.Sections {
		if s.Role == ports.RoleSystem {
			parts = append(parts, tags.Replace(s.Text))
		}
	}
	return strings.Join(parts, "\n\n")
}

// Canaries returns the unique CANARY-* markers of the system sections.
func (t PromptTemplate) Canaries() []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range canaryRe.FindAllString(t.SystemText(), -1) {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

// SHA256 returns the hex-encoded SHA-256 of the template file text.
func (t PromptTemplate) SHA256() string {
	sum := sha256.Sum256([]byte(t.Text))
	return hex.EncodeToString(sum[:])
}
