package main

import (
	"strings"
	"testing"
)

func TestClassifyInput(t *testing.T) {
	cases := []struct {
		in   string
		want cmdKind
	}{
		{"проанализируй репозиторий", cmdInput},
		{"", cmdInput},
		{"/help", cmdHelp},
		{"/помощь", cmdHelp},
		{"/HELP", cmdHelp}, // case-insensitive
		{"/?", cmdHelp},
		{"/agents", cmdAgents},
		{"/агенты", cmdAgents},
		{"/memory", cmdMemory},
		{"/память", cmdMemory},
		{"/mcp", cmdMCP},
		{"/mcp-list", cmdMCP},
		{"/tools", cmdTools},
		{"/инструменты", cmdTools},
		{"/perms", cmdPerms},
		{"/права", cmdPerms},
		{"/copy", cmdCopy},
		{"/копировать", cmdCopy},
		{"/loop", cmdLoop},
		{"/цикл", cmdLoop},
		{"/LOOP stop", cmdLoop}, // case-insensitive, args don't change the kind
		{"/findings", cmdFindings},
		{"/находки", cmdFindings},
		{"/profile", cmdProfile},
		{"/профиль", cmdProfile},
		{"/profile coder", cmdProfile},
		{"/select", cmdSelect},
		{"/выделение", cmdSelect},
		{"/clear", cmdClear},
		{"/exit", cmdQuit},
		{"/выход", cmdQuit},
		{"/quit extra args", cmdQuit}, // only the first word matters
		{"/bogus", cmdUnknown},
		{"/", cmdUnknown},
		{"  /help  ", cmdHelp}, // trimmed
	}
	for _, c := range cases {
		if got := classifyInput(c.in, orchestratorCommands); got != c.want {
			t.Errorf("classifyInput(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// A slash line must never be classified as model input, so the REPL/TUI never
// forward it to the LLM.
func TestSlashNeverInput(t *testing.T) {
	for _, in := range []string{"/help", "/bogus", "/", "/exit", "/память"} {
		if classifyInput(in, orchestratorCommands) == cmdInput {
			t.Errorf("%q must not be treated as model input", in)
		}
	}
}

func TestRenderCommandHelp(t *testing.T) {
	h := renderCommandHelp(orchestratorCommands)
	for _, want := range []string{"/help", "/помощь", "/agents", "/memory", "/mcp", "/tools", "/copy", "/select", "/exit"} {
		if !strings.Contains(h, want) {
			t.Errorf("help output missing %q", want)
		}
	}
}

func TestParseLoopArgs(t *testing.T) {
	cases := []struct {
		line string
		sub  string
		arg  string
	}{
		{"/loop", "", ""},
		{"/цикл", "", ""},
		{"/loop status", "status", ""},
		{"/цикл статус", "status", ""},
		{"/loop stop", "stop", ""},
		{"/loop стоп", "stop", ""},
		{"/loop resume", "resume", ""},
		{"/loop продолжить", "resume", ""},
		{"/loop сделай API", "goal", "сделай API"},
		{"/цикл @goal.md", "goal", "@goal.md"},
		{"/loop status лишнее", "status", "лишнее"},
		{"/loop STATUS", "status", ""}, // subcommand match is case-insensitive
		{"/loop Статус", "status", ""},
		{"/loop stop сейчас", "stop", "сейчас"},
	}
	for _, c := range cases {
		sub, arg := parseLoopArgs(c.line)
		if sub != c.sub || arg != c.arg {
			t.Errorf("parseLoopArgs(%q) = (%q, %q), want (%q, %q)", c.line, sub, arg, c.sub, c.arg)
		}
	}
}
