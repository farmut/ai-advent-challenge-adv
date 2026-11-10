package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-adv-agent/internal/domain"
)

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return fi.Mode().Perm()
}

func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestSave_WritesOwnerOnlyFiles(t *testing.T) {
	dir := t.TempDir()
	tests := []struct {
		name string
		path string
		save func(path string) error
	}{
		{"history", "h.json", func(p string) error {
			return NewHistoryFile(p).Save([]domain.Message{{Role: domain.RoleUser, Content: "hi"}})
		}},
		{"summary", "s.txt", func(p string) error { return NewSummaryFile(p).Save("sum") }},
		{"profile", "p.md", func(p string) error { return NewProfileFile(p).Save(domain.UserProfile{Name: "A"}) }},
		{"mcp", "m.yaml", func(p string) error { return NewMCPConfigFile(p).Save(domain.MCPConfig{}) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, tt.path)
			if err := tt.save(path); err != nil {
				t.Fatalf("save: %v", err)
			}
			if got := fileMode(t, path); got != 0o600 {
				t.Errorf("mode = %o, want 600", got)
			}
		})
	}
	assertNoTempFiles(t, dir)
}

func TestSave_TightensExistingWorldReadableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "h.json")
	if err := os.WriteFile(path, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil { // defeat umask
		t.Fatal(err)
	}
	if err := NewHistoryFile(path).Save(nil); err != nil {
		t.Fatalf("save: %v", err)
	}
	if got := fileMode(t, path); got != 0o600 {
		t.Errorf("mode after overwrite = %o, want 600", got)
	}
}

func TestWriteJSON_MarshalErrorLeavesOriginalUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	const original = `{"keep":true}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(path, map[string]any{"bad": make(chan int)}); err == nil {
		t.Fatal("expected a marshal error")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("original file modified: %q", got)
	}
	assertNoTempFiles(t, dir)
}

func TestWriteFileAtomic_RenameFailureCleansUp(t *testing.T) {
	dir := t.TempDir()
	// A directory at the target path makes the final rename fail.
	target := filepath.Join(dir, "occupied")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "x"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(target, []byte("data")); err == nil {
		t.Fatal("expected rename over a non-empty directory to fail")
	}
	assertNoTempFiles(t, dir)
}
