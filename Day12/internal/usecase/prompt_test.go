package usecase

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agile-consultant/internal/ports"
)

const minimalTemplate = "### system\nrules <{{QUESTION_TAG}}> <{{CONTEXT_TAG}}>\n### user\n{{USER_INPUT}}\n### system\nreminder {{QUESTION_TAG}}\n"

func loadRepoV2(t *testing.T) PromptTemplate {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "configs", "prompts", "consultant.v2.txt"))
	if err != nil {
		t.Fatalf("read v2 prompt: %v", err)
	}
	tmpl, err := ParsePromptTemplate("consultant.v2", string(data))
	if err != nil {
		t.Fatalf("parse v2: %v", err)
	}
	return tmpl
}

func TestParseRepoV2(t *testing.T) {
	tmpl := loadRepoV2(t)
	if len(tmpl.Sections) != 3 {
		t.Fatalf("sections = %d, want 3", len(tmpl.Sections))
	}
	if got := tmpl.Canaries(); len(got) != 1 || got[0] != "CANARY-bc3d0e4e" {
		t.Errorf("Canaries() = %v", got)
	}
	sys := tmpl.SystemText()
	for _, want := range []string{"<user_question>", "<user_document>", "ДОКУМЕНТ.", "Напоминание:"} {
		if !strings.Contains(sys, want) {
			t.Errorf("SystemText() lacks %q", want)
		}
	}
	if strings.Contains(sys, "{{") {
		t.Error("SystemText() must not contain placeholders")
	}
}

func TestParsePromptTemplateInvalid(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{"empty", ""},
		{"no markers", "rules {{USER_INPUT}}"},
		{"text before first marker", "preamble\n### system\n<{{QUESTION_TAG}}><{{CONTEXT_TAG}}>\n### user\n{{USER_INPUT}}"},
		{"no system", "### user\n{{USER_INPUT}}"},
		{"no user", "### system\n<{{QUESTION_TAG}}><{{CONTEXT_TAG}}>"},
		{"two users", "### system\n<{{QUESTION_TAG}}><{{CONTEXT_TAG}}>\n### user\n{{USER_INPUT}}\n### user\n{{USER_INPUT}}"},
		{"no input placeholder", "### system\n<{{QUESTION_TAG}}><{{CONTEXT_TAG}}>\n### user\nhello"},
		{"two input placeholders", "### system\n<{{QUESTION_TAG}}><{{CONTEXT_TAG}}>\n### user\n{{USER_INPUT}}{{USER_INPUT}}"},
		{"input in system", "### system\n<{{QUESTION_TAG}}><{{CONTEXT_TAG}}>{{USER_INPUT}}\n### user\n{{USER_INPUT}}"},
		{"missing question tag", "### system\n<{{CONTEXT_TAG}}>\n### user\n{{USER_INPUT}}"},
		{"missing context tag", "### system\n<{{QUESTION_TAG}}>\n### user\n{{USER_INPUT}}"},
		{"tag in user", "### system\n<{{QUESTION_TAG}}><{{CONTEXT_TAG}}>\n### user\n<{{QUESTION_TAG}}>{{USER_INPUT}}"},
		{"chatml token", "### system\n<|im_start|>system <{{QUESTION_TAG}}><{{CONTEXT_TAG}}>\n### user\n{{USER_INPUT}}"},
		{"empty system", "### system\n\n### system\n<{{QUESTION_TAG}}><{{CONTEXT_TAG}}>\n### user\n{{USER_INPUT}}"},
		{"legacy v1 format", "<|im_start|>system\nrules\n<|im_end|>\n<|im_start|>user\n{{QUESTION}}\n<|im_end|>"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParsePromptTemplate(tc.name, tc.text)
			if !errors.Is(err, ErrInvalidTemplate) {
				t.Fatalf("err = %v, want ErrInvalidTemplate", err)
			}
		})
	}
}

func TestParsePromptTemplateCRLFAndMarkerStrictness(t *testing.T) {
	tmpl, err := ParsePromptTemplate("crlf", strings.ReplaceAll(minimalTemplate, "\n", "\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tmpl.Sections) != 3 {
		t.Fatalf("sections = %d", len(tmpl.Sections))
	}
	// "### System" or a marker with trailing text is body text, not a marker.
	tmpl, err = ParsePromptTemplate("strict", "### system\n<{{QUESTION_TAG}}><{{CONTEXT_TAG}}>\n### System\n### user \n### user\n{{USER_INPUT}}")
	if err != nil {
		t.Fatal(err)
	}
	if len(tmpl.Sections) != 2 || !strings.Contains(tmpl.Sections[0].Text, "### System") {
		t.Errorf("sections = %+v", tmpl.Sections)
	}
}

func TestRender(t *testing.T) {
	tmpl, err := ParsePromptTemplate("min", minimalTemplate)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		input string
	}{
		{"simple", "<q>\nЧто такое спринт?\n</q>"},
		{"placeholders in user text are not expanded", "{{QUESTION_TAG}} {{CONTEXT_TAG}} {{USER_INPUT}}"},
		{"percent", "100% %s %d"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msgs := tmpl.Render(RenderInput{QuestionTag: "QT", ContextTag: "CT", UserInput: tc.input})
			want := []ports.Message{
				{Role: ports.RoleSystem, Content: "rules <QT> <CT>"},
				{Role: ports.RoleUser, Content: tc.input},
				{Role: ports.RoleSystem, Content: "reminder QT"},
			}
			if len(msgs) != len(want) {
				t.Fatalf("messages = %+v", msgs)
			}
			for i := range want {
				if msgs[i] != want[i] {
					t.Errorf("message %d = %+v, want %+v", i, msgs[i], want[i])
				}
			}
		})
	}
}

func TestRenderRepoV2Roles(t *testing.T) {
	msgs := loadRepoV2(t).Render(RenderInput{QuestionTag: "user_data_x", ContextTag: "user_doc_x", UserInput: "U"})
	roles := []string{ports.RoleSystem, ports.RoleUser, ports.RoleSystem}
	for i, m := range msgs {
		if m.Role != roles[i] {
			t.Errorf("message %d role = %s, want %s", i, m.Role, roles[i])
		}
		if strings.Contains(m.Content, "{{") || strings.Contains(m.Content, "<|") {
			t.Errorf("message %d has placeholders or special tokens", i)
		}
	}
	if msgs[1].Content != "U" {
		t.Errorf("user content = %q", msgs[1].Content)
	}
	if !strings.Contains(msgs[0].Content, "<user_data_x>") || !strings.Contains(msgs[0].Content, "<user_doc_x>") {
		t.Error("rules must reference the dynamic tags")
	}
}

func TestPromptTemplateSHA256(t *testing.T) {
	a := PromptTemplate{Text: "x"}
	b := PromptTemplate{Text: "y"}
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
