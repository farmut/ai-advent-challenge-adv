package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.yaml")
	data := `
models:
  tuned:
    name: agile-v1.0-4bit
    path: /tmp/tuned
    default: true
  baseline:
    name: baseline
    path: /tmp/baseline
server:
  bind: 127.0.0.1:9999
`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Server.Bind != "127.0.0.1:9999" {
		t.Errorf("bind: got %q", cfg.Server.Bind)
	}
	if cfg.Inference.MaxTokens != 512 {
		t.Errorf("max tokens default: got %d", cfg.Inference.MaxTokens)
	}
	if cfg.DefaultModel().Name != "agile-v1.0-4bit" {
		t.Errorf("default model: got %q", cfg.DefaultModel().Name)
	}
}

func TestSwitchDefault(t *testing.T) {
	cfg := &Config{
		Models: map[string]Model{
			"tuned":    {Name: "tuned"},
			"baseline": {Name: "baseline"},
		},
	}
	cfg.Models["tuned"] = Model{Name: "tuned", Default: true}
	if err := cfg.SwitchDefault("baseline"); err != nil {
		t.Fatal(err)
	}
	if !cfg.Models["baseline"].Default || cfg.Models["tuned"].Default {
		t.Errorf("switch failed")
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.yaml")
	cfg := &Config{
		Models: map[string]Model{
			"tuned": {Name: "tuned", Path: "/tmp/t", Default: true},
		},
		Server: Server{Bind: "127.0.0.1:1111"},
	}
	cfg.SetPath(path)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DefaultModel().Name != "tuned" {
		t.Errorf("loaded default: %q", loaded.DefaultModel().Name)
	}
}

func TestPromptFilePriority(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		env  string
		want string
	}{
		{"legacy when unset", "", "", ""},
		{"yaml", "configs/prompts/a.txt", "", "configs/prompts/a.txt"},
		{"env overrides yaml", "configs/prompts/a.txt", "/abs/b.txt", "/abs/b.txt"},
		{"env only", "", "/abs/b.txt", "/abs/b.txt"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(PromptFileEnv, tc.env)
			cfg := &Config{Prompt: Prompt{File: tc.yaml}}
			if got := cfg.PromptFile(); got != tc.want {
				t.Errorf("PromptFile() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSaveLoadKeepsPrompt(t *testing.T) {
	t.Setenv(PromptFileEnv, "")
	path := filepath.Join(t.TempDir(), "app.yaml")
	cfg := &Config{
		Models: map[string]Model{"b": {Name: "b", Path: "/tmp/b", Default: true}},
		Prompt: Prompt{File: "configs/prompts/consultant.v1.txt"},
	}
	cfg.SetPath(path)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Prompt.File != cfg.Prompt.File {
		t.Errorf("prompt.file = %q, want %q", loaded.Prompt.File, cfg.Prompt.File)
	}
	if loaded.Clone().Prompt != cfg.Prompt {
		t.Errorf("Clone lost prompt")
	}
}
