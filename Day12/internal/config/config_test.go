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
		{"empty when unset", "", "", ""},
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
		Prompt: Prompt{File: "configs/prompts/consultant.v2.txt"},
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

func TestGuardEnabled(t *testing.T) {
	on, off := true, false
	tests := []struct {
		name    string
		yaml    *bool
		env     string
		want    bool
		wantErr bool
	}{
		{"default on", nil, "", true, false},
		{"yaml off", &off, "", false, false},
		{"yaml on", &on, "", true, false},
		{"env off overrides yaml on", &on, "off", false, false},
		{"env on overrides yaml off", &off, "on", true, false},
		{"invalid env", &on, "false", false, true},
		{"invalid env upper", nil, "ON", false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(GuardEnv, tc.env)
			cfg := &Config{Guard: Guard{Enabled: tc.yaml}}
			got, err := cfg.GuardEnabled()
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && got != tc.want {
				t.Errorf("GuardEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGuardYAMLRoundTrip(t *testing.T) {
	t.Setenv(GuardEnv, "")
	dir := t.TempDir()
	path := filepath.Join(dir, "app.yaml")
	if err := os.WriteFile(path, []byte("guard:\n  enabled: false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := cfg.GuardEnabled(); got {
		t.Fatal("guard.enabled: false not honoured")
	}
	clone := cfg.Clone()
	*cfg.Guard.Enabled = true
	if got, _ := clone.GuardEnabled(); got {
		t.Error("Clone must deep-copy guard.enabled")
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := reloaded.GuardEnabled(); !got {
		t.Error("saved guard.enabled: true lost")
	}

	if err := os.WriteFile(path, []byte("server:\n  port: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := missing.GuardEnabled(); !got {
		t.Error("missing guard section must default to on")
	}
}
