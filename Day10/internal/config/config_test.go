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
