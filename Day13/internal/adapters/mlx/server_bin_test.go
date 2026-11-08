package mlx

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestServerBin(t *testing.T) {
	root := t.TempDir()
	mod := filepath.Join(root, "Day13")
	sub := filepath.Join(mod, "e2e", "proxy")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mod, "go.mod"), []byte("module x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A second module with the "make setup" venv.
	withVenv := filepath.Join(root, "Day14")
	withVenvSub := filepath.Join(withVenv, "e2e")
	venvBin := filepath.Join(withVenv, ".venv", "bin", "mlx_lm.server")
	if err := os.MkdirAll(filepath.Dir(venvBin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(withVenvSub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(withVenv, "go.mod"), []byte("module y\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(venvBin, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	notFound := func(string) (string, error) { return "", errors.New("not found") }
	found := func(string) (string, error) { return "/usr/local/bin/mlx_lm.server", nil }
	wd := func(d string) func() (string, error) { return func() (string, error) { return d, nil } }
	tests := []struct {
		name  string
		env   string
		look  func(string) (string, error)
		getwd func() (string, error)
		want  string
	}{
		{"env wins over local venv", "/opt/mlx", found, wd(withVenvSub), "/opt/mlx"},
		{"local venv wins over path", "", found, wd(withVenvSub), venvBin},
		{"local venv without path", "", notFound, wd(withVenvSub), venvBin},
		{"path when no local venv", "", found, wd(sub), "/usr/local/bin/mlx_lm.server"},
		{"Day6 fallback", "", notFound, wd(sub), filepath.Join(root, "Day6", ".venv", "bin", "mlx_lm.server")},
		{"no module", "", notFound, wd(root), "mlx_lm.server"},
		{"no module path", "", found, wd(root), "/usr/local/bin/mlx_lm.server"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := serverBin(func(string) string { return tt.env }, tt.look, tt.getwd)
			if got != tt.want {
				t.Fatalf("serverBin = %q, want %q", got, tt.want)
			}
		})
	}
}
