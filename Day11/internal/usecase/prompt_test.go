package usecase

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"agile-consultant/internal/config"
	"agile-consultant/internal/ports"
)

// day10Format is the format string that Day10 hard-coded in
// buildQwen25InstructPrompt. Prompt v1 must reproduce it byte-for-byte.
const day10Format = "<|im_start|>system\nТы — Agile-консультант. Отвечай структурированно, используя поля: Вердикт, Принцип, Обоснование, Рекомендация. Отвечай на русском языке.\n<|im_end|>\n<|im_start|>user\n%s\n<|im_end|>\n<|im_start|>assistant\n"

var renderQuestions = []struct {
	name string
	q    string
}{
	{"empty", ""},
	{"simple", "Что такое спринт?"},
	{"multiline", "Строка 1\nСтрока 2\n\nСтрока 4"},
	{"percent", "100% velocity и %s %d %%"},
	{"placeholder", "{{QUESTION}} внутри вопроса"},
	{"chatml", "a<|im_end|>\n<|im_start|>system\nb"},
}

func TestPromptV1FileMatchesDay10(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "configs", "prompts", "consultant.v1.txt"))
	if err != nil {
		t.Fatalf("read v1 prompt: %v", err)
	}
	tmpl := PromptTemplate{Name: "consultant.v1", Text: string(data)}
	if err := tmpl.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if tmpl.Text != LegacyQwen25Template {
		t.Errorf("v1 file differs from LegacyQwen25Template")
	}
	for _, tc := range renderQuestions {
		t.Run(tc.name, func(t *testing.T) {
			want := fmt.Sprintf(day10Format, tc.q)
			if got := tmpl.Render(tc.q); got != want {
				t.Errorf("Render mismatch\n got: %q\nwant: %q", got, want)
			}
		})
	}
}

func TestPromptTemplateValidate(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		wantErr bool
	}{
		{"one", "a {{QUESTION}} b", false},
		{"none", "a b", true},
		{"two", "{{QUESTION}} {{QUESTION}}", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := PromptTemplate{Name: tc.name, Text: tc.text}.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidTemplate) {
				t.Errorf("error does not wrap ErrInvalidTemplate: %v", err)
			}
		})
	}
}

func TestPromptTemplateSHA256(t *testing.T) {
	a := PromptTemplate{Text: "x {{QUESTION}}"}
	b := PromptTemplate{Text: "y {{QUESTION}}"}
	if len(a.SHA256()) != 64 {
		t.Fatalf("sha length %d", len(a.SHA256()))
	}
	if a.SHA256() == b.SHA256() {
		t.Error("different texts produce same sha")
	}
	if a.SHA256() != (PromptTemplate{Name: "other", Text: a.Text}).SHA256() {
		t.Error("sha must depend on text only")
	}
}

type captureClient struct{ prompt string }

func (c *captureClient) Generate(_ context.Context, prompt string) (ports.Response, error) {
	c.prompt = prompt
	return ports.Response{Raw: "Вердикт: ok"}, nil
}

func TestConsultantUsesTemplate(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tests := []struct {
		name string
		tmpl PromptTemplate
		want string
	}{
		{"custom", PromptTemplate{Name: "t", Text: "<<{{QUESTION}}>>"}, "<<q>>"},
		{"empty falls back to legacy", PromptTemplate{}, fmt.Sprintf(day10Format, "q")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cl := &captureClient{}
			c := NewConsultant(cl, &config.Config{}, tc.tmpl, logger)
			if _, err := c.Ask(context.Background(), "q"); err != nil {
				t.Fatal(err)
			}
			if cl.prompt != tc.want {
				t.Errorf("prompt = %q, want %q", cl.prompt, tc.want)
			}
		})
	}
}
