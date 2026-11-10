package app

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"ai-adv-agent/internal/adapter/sandbox"
	"ai-adv-agent/internal/config"
	"ai-adv-agent/internal/port"
)

func TestWireSandbox(t *testing.T) {
	t.Run("no image: no runner", func(t *testing.T) {
		tb := &Toolbelt{}
		if err := tb.wireSandbox(context.Background(), config.DefaultSandbox(), &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		if tb.Runner != nil {
			t.Fatal("runner wired without sandbox.image")
		}
	})

	t.Run("docker missing: runner fails closed", func(t *testing.T) {
		root := t.TempDir()
		sc := config.DefaultSandbox()
		sc.Image = "ai-adv-sandbox:1"
		sc.WorkspaceRoot = root
		sc.DockerBin = filepath.Join(root, "no-docker")
		var log bytes.Buffer
		tb := &Toolbelt{}
		if err := tb.wireSandbox(context.Background(), sc, &log); err != nil {
			t.Fatal(err)
		}
		if tb.Runner == nil {
			t.Fatal("runner must be wired even when docker is down")
		}
		if !strings.Contains(log.String(), "[sandbox]") {
			t.Errorf("preflight failure not reported: %q", log.String())
		}
		_, err := tb.Runner.Run(context.Background(), port.CommandSpec{Argv: []string{"go", "test", "./..."}, Workdir: root})
		if !errors.Is(err, sandbox.ErrUnavailable) {
			t.Fatalf("err = %v, want ErrUnavailable", err)
		}
	})

	t.Run("invalid config is an error", func(t *testing.T) {
		sc := config.DefaultSandbox()
		sc.Image = "ai-adv-sandbox:1"
		sc.WorkspaceRoot = "relative"
		if err := (&Toolbelt{}).wireSandbox(context.Background(), sc, &bytes.Buffer{}); err == nil {
			t.Fatal("expected error")
		}
	})
}
