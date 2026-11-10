// Package sast implements port.SecurityScanner on top of the sandbox runner:
// gosec for Go, eslint (eslint-plugin-security + vue/no-v-html) for Vue/TS.
// Both run offline inside the sandbox image; this package only builds the
// command, parses the JSON report and maps it onto domain findings.
//
// Everything about the output is treated as untrusted and fail-closed:
// truncated, timed-out or unparsable output is port.ErrScanOutput, never "no
// findings".
package sast

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// DefaultContainerWorkdir is where sbx-run copies the task tree; the
// analysers print absolute paths under it.
const DefaultContainerWorkdir = "/tmp/work"

// Config configures the scanner.
type Config struct {
	// Commands maps a stack ("go", "vue") to the analyser argv. argv[0]
	// selects the parser: gosec or eslint.
	Commands map[string][]string
	// ContainerWorkdir is stripped from reported paths.
	ContainerWorkdir string
	// Timeout and MaxOutput are per-command caps (0 = the sandbox's own).
	Timeout   time.Duration
	MaxOutput int
}

type parser func(res port.CommandResult) ([]domain.Finding, error)

// Scanner runs one analyser per stack.
type Scanner struct {
	runner  port.CommandRunner
	cfg     Config
	parsers map[string]parser
}

// New checks that every command uses a known analyser.
func New(runner port.CommandRunner, cfg Config) (*Scanner, error) {
	if runner == nil {
		return nil, fmt.Errorf("sast: нет CommandRunner (песочница не настроена)")
	}
	if cfg.ContainerWorkdir == "" {
		cfg.ContainerWorkdir = DefaultContainerWorkdir
	}
	s := &Scanner{runner: runner, cfg: cfg, parsers: map[string]parser{}}
	for stack, argv := range cfg.Commands {
		if len(argv) == 0 {
			return nil, fmt.Errorf("sast: пустая команда для стека %q", stack)
		}
		switch argv[0] {
		case "gosec":
			s.parsers[stack] = func(r port.CommandResult) ([]domain.Finding, error) {
				return parseGosec(r, cfg.ContainerWorkdir)
			}
		case "eslint":
			s.parsers[stack] = func(r port.CommandResult) ([]domain.Finding, error) {
				return parseESLint(r, cfg.ContainerWorkdir)
			}
		default:
			return nil, fmt.Errorf("sast: стек %q: неизвестный анализатор %q (gosec или eslint)", stack, argv[0])
		}
	}
	return s, nil
}

// Scan runs the stack's analyser in workdir.
func (s *Scanner) Scan(ctx context.Context, workdir, stack string) ([]domain.Finding, error) {
	argv, ok := s.cfg.Commands[stack]
	if !ok {
		return nil, fmt.Errorf("sast: для стека %q не задана команда (pipeline.sast.%s)", stack, stack)
	}
	res, err := s.runner.Run(ctx, port.CommandSpec{
		Argv:      append([]string(nil), argv...),
		Workdir:   workdir,
		Timeout:   s.cfg.Timeout,
		MaxOutput: s.cfg.MaxOutput,
		Stack:     stack,
	})
	if err != nil {
		return nil, fmt.Errorf("sast %s: %w", argv[0], err)
	}
	if res.TimedOut {
		return nil, fmt.Errorf("%w: %s превысил таймаут", port.ErrScanOutput, argv[0])
	}
	if res.Truncated {
		return nil, fmt.Errorf("%w: вывод %s усечён (поднимите sandbox.max_output_bytes)", port.ErrScanOutput, argv[0])
	}
	fs, err := s.parsers[stack](res)
	if err != nil {
		return nil, fmt.Errorf("%w: %s (exit %d): %v; stderr: %s", port.ErrScanOutput, argv[0], res.ExitCode, err, tail(res.Stderr, 300))
	}
	return fs, nil
}

// relPath maps an analyser path onto the task directory. A path outside the
// container workdir is returned cleaned but unchanged: it will not be found
// in the task snapshot, so the pipeline leaves the finding unverified.
func relPath(p, workdir string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if path.IsAbs(p) {
		p = path.Clean(p)
		wd := path.Clean(workdir)
		if strings.HasPrefix(p, wd+"/") {
			return strings.TrimPrefix(p, wd+"/")
		}
		return p
	}
	return strings.TrimPrefix(path.Clean(p), "./")
}

func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}

// cut shortens s to n bytes on a rune boundary.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && n < len(s) && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n] + "…"
}
