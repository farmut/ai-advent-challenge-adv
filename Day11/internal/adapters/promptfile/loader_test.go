package promptfile

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"agile-consultant/internal/usecase"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := write("consultant.v9.txt", "sys\n{{QUESTION}}\n")
	bad := write("bad.txt", "no placeholder")

	tmpl, err := Load(good)
	if err != nil {
		t.Fatalf("load good: %v", err)
	}
	if tmpl.Name != "consultant.v9" {
		t.Errorf("name = %q", tmpl.Name)
	}
	if tmpl.Text != "sys\n{{QUESTION}}\n" {
		t.Errorf("text = %q", tmpl.Text)
	}
	if _, err := Load(bad); !errors.Is(err, usecase.ErrInvalidTemplate) {
		t.Errorf("bad template err = %v", err)
	}
	if _, err := Load(filepath.Join(dir, "missing.txt")); err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadRepoV1(t *testing.T) {
	tmpl, err := Load(filepath.Join("..", "..", "..", "configs", "prompts", "consultant.v1.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Text != usecase.LegacyQwen25Template {
		t.Error("repo v1 prompt differs from legacy template")
	}
}

func TestResolveEmptyIsLegacy(t *testing.T) {
	tmpl, err := Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Name != usecase.LegacyTemplateName {
		t.Errorf("name = %q", tmpl.Name)
	}
}

func TestLoadRepoV2(t *testing.T) {
	tmpl, err := Load(filepath.Join("..", "..", "..", "configs", "prompts", "consultant.v2.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Name != "consultant.v2" {
		t.Errorf("name = %q", tmpl.Name)
	}
	if !regexp.MustCompile(`CANARY-[0-9a-f]{8}\b`).MatchString(tmpl.Text) {
		t.Error("v2 must contain a CANARY-<hex8> marker")
	}
	if !strings.Contains(tmpl.Text, "<user_question>\n{{QUESTION}}\n</user_question>") {
		t.Error("v2 must wrap the question in <user_question>")
	}
	// Literal ChatML tokens inside the rules text would be tokenised as real
	// special tokens and break the turn structure.
	start, end := strings.Index(tmpl.Text, "<rules>"), strings.Index(tmpl.Text, "</rules>")
	if start < 0 || end < start {
		t.Fatal("v2 must have a <rules> block")
	}
	if strings.Contains(tmpl.Text[start:end], "<|") {
		t.Error("rules block must not contain literal special tokens")
	}
	// Sandwich: a reminder turn must follow the question.
	_, after, _ := strings.Cut(tmpl.Text, "{{QUESTION}}")
	if !strings.Contains(after, "<|im_start|>system") || !strings.HasSuffix(after, "<|im_start|>assistant\n") {
		t.Error("v2 must end with a system reminder followed by the assistant turn")
	}
}
