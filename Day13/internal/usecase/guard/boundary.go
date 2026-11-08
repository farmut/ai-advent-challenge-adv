package guard

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"agile-consultant/internal/ports"
)

// Static tag names used when the guard is off.
const (
	StaticQuestionTag = "user_question"
	StaticContextTag  = "user_document"
)

// Errors returned by boundaries.
var (
	// ErrBoundaryCollision means the data already contains the boundary tag
	// or nonce; the request must be rejected.
	ErrBoundaryCollision = errors.New("boundary tag collides with input data")
	// ErrNonce means a nonce could not be obtained or is malformed.
	ErrNonce = errors.New("boundary nonce unavailable")
)

var nonceRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Wrapped is the user input enclosed in boundary markers.
type Wrapped struct {
	QuestionTag string
	ContextTag  string
	// UserInput is the content of the user message: the optional document
	// block followed by the question block.
	UserInput string
	// Nonce is "" for the static boundary.
	Nonce string
}

// Boundary wraps untrusted question and document text in markers.
type Boundary interface {
	Wrap(question, document string) (Wrapped, error)
}

// StaticBoundary uses the fixed user_question/user_document tags and never
// fails. It is the guard-off baseline.
type StaticBoundary struct{}

// Wrap implements Boundary.
func (StaticBoundary) Wrap(question, document string) (Wrapped, error) {
	return Wrapped{
		QuestionTag: StaticQuestionTag,
		ContextTag:  StaticContextTag,
		UserInput:   wrap(StaticQuestionTag, StaticContextTag, question, document),
	}, nil
}

// NonceBoundary uses per-request tags user_data_<nonce>/user_doc_<nonce>
// that the user cannot predict, so the data block cannot be closed from
// inside.
type NonceBoundary struct {
	src ports.NonceSource
}

// NewNonceBoundary returns a NonceBoundary drawing nonces from src.
func NewNonceBoundary(src ports.NonceSource) NonceBoundary { return NonceBoundary{src: src} }

// Wrap implements Boundary. It fails closed: a nonce error or a collision of
// the tag with the data is returned as an error.
func (b NonceBoundary) Wrap(question, document string) (Wrapped, error) {
	if b.src == nil {
		return Wrapped{}, fmt.Errorf("%w: no nonce source", ErrNonce)
	}
	nonce, err := b.src.Nonce()
	if err != nil {
		return Wrapped{}, fmt.Errorf("%w: %w", ErrNonce, err)
	}
	if !nonceRe.MatchString(nonce) {
		return Wrapped{}, fmt.Errorf("%w: malformed nonce", ErrNonce)
	}
	qTag, cTag := "user_data_"+nonce, "user_doc_"+nonce
	for _, text := range []string{question, document} {
		if strings.Contains(strings.ToLower(text), nonce) {
			return Wrapped{}, ErrBoundaryCollision
		}
	}
	return Wrapped{
		QuestionTag: qTag,
		ContextTag:  cTag,
		UserInput:   wrap(qTag, cTag, question, document),
		Nonce:       nonce,
	}, nil
}

func wrap(qTag, cTag, question, document string) string {
	var b strings.Builder
	if strings.TrimSpace(document) != "" {
		fmt.Fprintf(&b, "<%s>\n%s\n</%s>\n\n", cTag, document, cTag)
	}
	fmt.Fprintf(&b, "<%s>\n%s\n</%s>", qTag, question, qTag)
	return b.String()
}
