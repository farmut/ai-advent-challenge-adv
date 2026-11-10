package policy

import (
	"fmt"
	"strings"
)

// ExecPolicy decides which argv a role may run through run_command. There is
// no shell anywhere on the path: argv goes to the sandbox runner as is. The
// zero value allows nothing.
type ExecPolicy struct {
	// Allow holds argv prefixes ("go test:*" -> ["go","test"]): the command
	// must start with these tokens, any further arguments are fine.
	Allow [][]string
	// AllowExact holds argv that must match exactly ("go build ./...").
	AllowExact [][]string
	// DenyPatterns are wildcard patterns ('*' = any run of characters,
	// spaces included) matched against strings.Join(argv, " ").
	DenyPatterns []string
	// DenyAll is set by a bare "Bash" deny rule: no command at all.
	DenyAll bool
}

// Usable reports whether at least one command could ever be allowed. A role
// whose policy is not usable is not offered run_command at all.
func (e ExecPolicy) Usable() bool {
	return !e.DenyAll && (len(e.Allow) > 0 || len(e.AllowExact) > 0)
}

// Describe renders the allowed commands for prompts and /perms.
func (e ExecPolicy) Describe() []string {
	var out []string
	for _, a := range e.Allow {
		out = append(out, strings.Join(a, " ")+" …")
	}
	for _, a := range e.AllowExact {
		out = append(out, strings.Join(a, " "))
	}
	return out
}

// ParseExec builds an ExecPolicy from Claude-style Bash patterns (the text
// inside "Bash(...)"): "cmd args:*" is a prefix, anything else is an exact
// argv. Wildcards are not accepted in allow rules (fail-closed: an allow rule
// must name what it allows). In deny rules "" or "*" means DenyAll.
func ParseExec(allow, deny []string) (ExecPolicy, error) {
	var e ExecPolicy
	for _, a := range allow {
		a = strings.TrimSpace(a)
		prefix := strings.HasSuffix(a, ":*")
		body := strings.TrimSuffix(a, ":*")
		if body == "" || strings.ContainsAny(body, "*?[") {
			return ExecPolicy{}, fmt.Errorf("exec allow %q: want \"cmd args:*\" or an exact command without wildcards", a)
		}
		argv := strings.Fields(body)
		if prefix {
			e.Allow = append(e.Allow, argv)
		} else {
			e.AllowExact = append(e.AllowExact, argv)
		}
	}
	for _, d := range deny {
		d = strings.TrimSpace(d)
		if d == "" || d == "*" {
			e.DenyAll = true
			continue
		}
		if strings.HasSuffix(d, ":*") {
			// Deny is matched loosely on purpose: "rm:*" also catches "rmdir".
			// Over-denying is the safe direction.
			d = strings.TrimSuffix(d, ":*") + "*"
		}
		e.DenyPatterns = append(e.DenyPatterns, d)
	}
	return e, nil
}

// shellMeta are refused in every argument. There is no shell to interpret
// them; refusing them anyway is defence in depth against a future runner that
// accidentally gains one.
const shellMeta = ";|&$`<>\n\r\x00"

// globalDenyCommands are refused for every profile, whatever it allows: they
// fetch code, run arbitrary binaries or change module state.
var globalDenyCommands = [][]string{
	{"go", "run"}, {"go", "generate"}, {"go", "get"}, {"go", "install"},
	{"go", "mod"}, {"go", "work"}, {"go", "tool"},
	{"npm", "install"}, {"npm", "i"}, {"npm", "exec"}, {"npm", "run"},
	{"npx"},
}

// CheckExec validates one argv against the global deny list and the role's
// ExecPolicy. Order: shape → metacharacters → global deny → role deny → allow.
func (p FS) CheckExec(argv []string) error {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return p.deny("run_command", "argv", "", newErr(CodeBadArgument,
			"argv обязателен: массив строк, первая — имя программы, например [\"go\",\"test\",\"./...\"]."))
	}
	joined := strings.Join(argv, " ")
	if strings.ContainsAny(argv[0], `/\`) {
		return p.deny("run_command", "argv", joined, newErr(CodeDeniedByPolicy,
			"программа %q указана путём. Разрешены только имена программ из списка разрешённых команд.", argv[0]))
	}
	for _, a := range argv {
		if strings.ContainsAny(a, shellMeta) {
			return p.deny("run_command", "argv", joined, newErr(CodeDeniedByPolicy,
				"аргумент %q содержит метасимволы шелла (%q). Шелла нет: передавай аргументы отдельными строками без перенаправлений и конвейеров.", a, ";|&$`<>"))
		}
	}
	if reason := globallyDenied(argv); reason != "" {
		return p.deny("run_command", "argv", joined, newErr(CodeDeniedByPolicy,
			"команда %q запрещена глобальной политикой (%s). Не пытайся выполнить её в другой форме.", joined, reason))
	}
	if p.Exec.DenyAll {
		return p.deny("run_command", "argv", joined, newErr(CodePermissionDenied,
			"запуск команд не выдан этой роли. Верни результат текстом."))
	}
	for _, pat := range p.Exec.DenyPatterns {
		if wildcardMatch(pat, joined) {
			return p.deny("run_command", "argv", joined, newErr(CodeDeniedByPolicy,
				"команда %q запрещена профилем роли (правило %q).", joined, pat))
		}
	}
	for _, a := range p.Exec.AllowExact {
		if equalArgv(a, argv) {
			p.audit("run_command", "argv", joined, "ALLOW", "")
			return nil
		}
	}
	for _, a := range p.Exec.Allow {
		if len(argv) >= len(a) && equalArgv(a, argv[:len(a)]) {
			p.audit("run_command", "argv", joined, "ALLOW", "")
			return nil
		}
	}
	allowed := p.Exec.Describe()
	if len(allowed) == 0 {
		return p.deny("run_command", "argv", joined, newErr(CodePermissionDenied,
			"запуск команд не выдан этой роли. Верни результат текстом."))
	}
	return p.deny("run_command", "argv", joined, newErr(CodePermissionDenied,
		"команда %q не разрешена этой роли. Разрешены: %s.", joined, strings.Join(allowed, "; ")))
}

// globallyDenied returns a reason when argv hits the profile-independent deny
// list, or "" otherwise.
func globallyDenied(argv []string) string {
	for _, d := range globalDenyCommands {
		if len(argv) >= len(d) && equalArgv(d, argv[:len(d)]) {
			return strings.Join(d, " ")
		}
	}
	for _, raw := range argv[1:] {
		a := raw
		if strings.HasPrefix(a, "--") {
			a = a[1:]
		}
		switch {
		case strings.HasPrefix(a, "-exec"), strings.HasPrefix(a, "-toolexec"), strings.HasPrefix(a, "-vettool"):
			return "флаг " + raw + " запускает произвольную программу"
		case a == "-o" || strings.HasPrefix(a, "-o="):
			return "флаг -o пишет файл вне контроля политики"
		}
	}
	if argv[0] == "go" && len(argv) > 1 && argv[1] == "env" {
		for _, a := range argv[2:] {
			if a == "-w" || a == "-u" {
				return "go env " + a + " меняет окружение"
			}
		}
	}
	if argv[0] == "gofmt" {
		for _, a := range argv[1:] {
			if a == "-w" {
				return "gofmt -w перезаписывает файлы в обход edit_file"
			}
		}
	}
	return ""
}

func equalArgv(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// wildcardMatch matches s against a pattern in which '*' stands for any run of
// characters (including spaces and slashes). Every other character is literal.
func wildcardMatch(pattern, s string) bool {
	for {
		i := strings.IndexByte(pattern, '*')
		if i < 0 {
			return pattern == s
		}
		if !strings.HasPrefix(s, pattern[:i]) {
			return false
		}
		s = s[i:]
		pattern = pattern[i+1:]
		if pattern == "" {
			return true
		}
		// Try every position for the rest of the pattern.
		for j := 0; j <= len(s); j++ {
			if wildcardMatch(pattern, s[j:]) {
				return true
			}
		}
		return false
	}
}

// SandboxBinaries is the complete set of programs the sandbox image provides
// for run_command. sandbox.allowed_binaries may only narrow it.
var SandboxBinaries = []string{"go", "gofmt", "gosec", "eslint", "vitest", "vue-tsc", "npm"}

// IsSandboxBinary reports whether name is one of SandboxBinaries.
func IsSandboxBinary(name string) bool {
	for _, b := range SandboxBinaries {
		if b == name {
			return true
		}
	}
	return false
}

// CheckGlobalExec applies the profile-independent part of CheckExec: argv
// shape, shell metacharacters and the global deny list. The sandbox runner
// calls it again right before execution, so a caller that skipped the
// role policy still cannot run a globally forbidden command.
func CheckGlobalExec(argv []string) error {
	if len(argv) == 0 || strings.TrimSpace(argv[0]) == "" {
		return newErr(CodeBadArgument, "argv пуст")
	}
	if strings.ContainsAny(argv[0], `/\`) {
		return newErr(CodeDeniedByPolicy, "программа %q указана путём", argv[0])
	}
	for _, a := range argv {
		if strings.ContainsAny(a, shellMeta) {
			return newErr(CodeDeniedByPolicy, "аргумент %q содержит метасимволы шелла", a)
		}
	}
	if reason := globallyDenied(argv); reason != "" {
		return newErr(CodeDeniedByPolicy, "команда %q запрещена глобальной политикой (%s)", strings.Join(argv, " "), reason)
	}
	return nil
}
