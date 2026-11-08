package promptfile

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"agile-consultant/internal/ports"
	"agile-consultant/internal/usecase"
)

const validBody = "### system\nrules <{{QUESTION_TAG}}> <{{CONTEXT_TAG}}>\n### user\n{{USER_INPUT}}\n"

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := write("consultant.v9.txt", validBody)
	bad := write("bad.txt", "### system\nno placeholders\n")

	tmpl, err := Load(good)
	if err != nil {
		t.Fatalf("load good: %v", err)
	}
	if tmpl.Name != "consultant.v9" {
		t.Errorf("name = %q", tmpl.Name)
	}
	if tmpl.Text != validBody {
		t.Errorf("text = %q", tmpl.Text)
	}
	if len(tmpl.Sections) != 2 {
		t.Errorf("sections = %d", len(tmpl.Sections))
	}
	if _, err := Load(bad); !errors.Is(err, usecase.ErrInvalidTemplate) {
		t.Errorf("bad template err = %v", err)
	}
	if _, err := Load(filepath.Join(dir, "missing.txt")); err == nil {
		t.Error("expected error for missing file")
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
	if strings.Contains(tmpl.Text, "<|") {
		t.Error("v2 must not contain literal ChatML special tokens")
	}
	if !regexp.MustCompile(`CANARY-[0-9a-f]{8}\b`).MatchString(tmpl.SystemText()) {
		t.Error("v2 system text must contain a CANARY-<hex8> marker")
	}
	roles := make([]string, len(tmpl.Sections))
	for i, s := range tmpl.Sections {
		roles[i] = s.Role
	}
	want := []string{ports.RoleSystem, ports.RoleUser, ports.RoleSystem}
	if strings.Join(roles, ",") != strings.Join(want, ",") {
		t.Fatalf("roles = %v, want %v (rules, user, reminder sandwich)", roles, want)
	}
	rules := tmpl.Sections[0].Text
	if !strings.HasPrefix(rules, "<rules>") || !strings.HasSuffix(rules, "</rules>") {
		t.Error("first system section must be a <rules> block")
	}
	for _, s := range []string{"ДОКУМЕНТ.", "<" + usecase.ContextTagPlaceholder + ">", "<" + usecase.QuestionTagPlaceholder + ">"} {
		if !strings.Contains(rules, s) {
			t.Errorf("rules must mention %q", s)
		}
	}
	if !strings.HasPrefix(tmpl.Sections[2].Text, "Напоминание:") {
		t.Error("last system section must be the reminder")
	}
	if tmpl.Sections[1].Text != usecase.UserInputPlaceholder {
		t.Errorf("user section = %q", tmpl.Sections[1].Text)
	}
}
