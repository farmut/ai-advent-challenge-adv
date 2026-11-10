package main

import (
	"errors"
	"os"
	"testing"

	"ai-adv-agent/internal/domain"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func TestValidateLoopFlags(t *testing.T) {
	cases := []struct {
		name                                string
		goal                                string
		resume, query, interactive, wantErr bool
	}{
		{"no loop flags", "", false, false, false, false},
		{"goal alone", "сделай API", false, false, false, false},
		{"resume alone", "", true, false, false, false},
		{"goal with query", "x", false, true, false, true},
		{"resume with query", "", true, true, false, true},
		{"goal with interactive", "x", false, false, true, true},
		{"resume with interactive", "", true, false, true, true},
		{"goal with resume", "x", true, false, false, true},
	}
	for _, c := range cases {
		err := validateLoopFlags(c.goal, c.resume, c.query, c.interactive)
		if c.wantErr && err == nil {
			t.Errorf("%s: want error, got nil", c.name)
		}
		if !c.wantErr && err != nil {
			t.Errorf("%s: want nil, got %v", c.name, err)
		}
	}
}

func TestLoopExitCode(t *testing.T) {
	cases := []struct {
		name string
		run  domain.LoopRun
		err  error
		want int
	}{
		{"done", domain.LoopRun{Status: domain.LoopDone}, nil, exitDone},
		{"failed", domain.LoopRun{Status: domain.LoopFailed}, nil, exitFailed},
		{"paused by sigint", domain.LoopRun{Status: domain.LoopPaused}, nil, exitAborted},
		{"aborted budget", domain.LoopRun{Status: domain.LoopAborted}, nil, exitAborted},
		{"never started", domain.LoopRun{}, errors.New("lock conflict"), exitConfigError},
	}
	for _, c := range cases {
		if got := loopExitCode(c.run, c.err); got != c.want {
			t.Errorf("%s: loopExitCode = %d, want %d", c.name, got, c.want)
		}
	}
}
