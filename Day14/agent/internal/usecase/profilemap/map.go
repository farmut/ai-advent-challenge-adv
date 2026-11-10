// Package profilemap turns a Claude-style agent profile (Day2 format) into the
// agent's own permission model: tool names, write globs, deny globs and an
// exec policy. Every rule is fail-closed: what cannot be expressed exactly is
// either refused at load time (in allow) or over-denied (in deny).
package profilemap

import (
	"fmt"
	"sort"
	"strings"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/policy"
	"ai-adv-agent/internal/usecase"
)

// Roots are the absolute directories a role works in. In chat mode Read is the
// project root and Write the shared workspace; in loop mode both are the task
// directory.
type Roots struct {
	Read, Write string
}

// Grant is what a profile is allowed to do, in the agent's terms.
type Grant struct {
	// AllowTools lists tool names (sorted). Servers decide which exist.
	AllowTools []string
	// WriteGlobs are relative to the write root; "!glob" is negative.
	WriteGlobs []string
	// DenyGlobs are extra deny globs relative to the read root.
	DenyGlobs []string
	// ExecAllow / ExecDeny are the Bash patterns for run_command.
	ExecAllow, ExecDeny []string
	// Exec is the parsed exec policy (validated).
	Exec policy.ExecPolicy
	// Env is the allowlisted environment for run_command.
	Env map[string]string
	// Notes lists rules that were ignored on purpose (for an info log).
	Notes []string
}

// CanWrite reports whether the grant allows writing anywhere.
func (g Grant) CanWrite() bool {
	for _, w := range g.WriteGlobs {
		if !strings.HasPrefix(w, "!") {
			return true
		}
	}
	return false
}

var (
	readTools  = []string{"read_text_file", "read_multiple_files", "get_file_info", "list_directory", "directory_tree"}
	grepTools  = []string{"grep", "multi_grep"}
	globTools  = []string{"find_files", "search_files", "list_directory", "directory_tree"}
	editTools  = []string{"edit_file"}
	writeTools = []string{"write_file", "create_directory"}
	gitTools   = []string{"git_diff", "git_current_branch", "git_list_files"}
)

// Map converts a profile into a Grant.
func Map(p domain.AgentProfile) (Grant, error) {
	var g Grant
	tools := map[string]bool{}
	add := func(names ...string) {
		for _, n := range names {
			tools[n] = true
		}
	}
	var negWrite []string
	posWrite := map[string]bool{}
	execDenyAll := false

	for _, r := range p.Allow {
		switch r.Kind {
		case domain.PermWebFetch, domain.PermWebSearch, domain.PermTask, domain.PermAgent:
			return Grant{}, fmt.Errorf("profile %s: право %s не поддерживается агентом (нет такого инструмента) — уберите его из allow", p.Name, r)
		}
		if !r.Kind.Known() {
			return Grant{}, fmt.Errorf("profile %s: неизвестное право %s", p.Name, r)
		}
		if !p.HasTool(r.Kind) {
			g.Notes = append(g.Notes, fmt.Sprintf("%s: нет %s в tools: frontmatter — право не выдано", r, r.Kind))
			continue
		}
		switch r.Kind {
		case domain.PermRead:
			if r.Pattern != "" && policy.NormalizeGlob(r.Pattern) != "**" {
				return Grant{}, fmt.Errorf("profile %s: %s — сужение чтения не поддерживается, используйте Read(./**) и deny", p.Name, r)
			}
			add(readTools...)
		case domain.PermGrep:
			if r.Pattern != "" {
				return Grant{}, fmt.Errorf("profile %s: %s — шаблон для Grep не поддерживается", p.Name, r)
			}
			add(grepTools...)
		case domain.PermGlob:
			if r.Pattern != "" {
				return Grant{}, fmt.Errorf("profile %s: %s — шаблон для Glob не поддерживается", p.Name, r)
			}
			add(globTools...)
		case domain.PermEdit, domain.PermWrite:
			glob, err := writeGlob(r.Pattern)
			if err != nil {
				return Grant{}, fmt.Errorf("profile %s: %s: %w", p.Name, r, err)
			}
			posWrite[glob] = true
			if r.Kind == domain.PermEdit {
				add(editTools...)
			} else {
				add(writeTools...)
			}
		case domain.PermBash:
			if r.Pattern == "" {
				return Grant{}, fmt.Errorf("profile %s: голый Bash в allow (любая команда) не поддерживается — перечислите команды Bash(cmd:*)", p.Name)
			}
			if isGitRead(r.Pattern) {
				add(gitTools...)
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(r.Pattern), "git ") {
				g.Notes = append(g.Notes, fmt.Sprintf("%s: git-команды кроме status/diff не поддерживаются — пропущено", r))
				continue
			}
			g.ExecAllow = append(g.ExecAllow, strings.TrimSpace(r.Pattern))
		}
	}

	for _, r := range p.Deny {
		switch r.Kind {
		case domain.PermRead:
			if ignoreHome(r) {
				g.Notes = append(g.Notes, fmt.Sprintf("deny %s: путь вне корня — корень и так его отсекает", r))
				continue
			}
			glob := policy.NormalizeGlob(r.Pattern)
			if err := policy.ValidateWriteGlob(glob); err != nil {
				return Grant{}, fmt.Errorf("profile %s: deny %s: %w", p.Name, r, err)
			}
			g.DenyGlobs = append(g.DenyGlobs, glob)
		case domain.PermEdit, domain.PermWrite:
			if ignoreHome(r) {
				g.Notes = append(g.Notes, fmt.Sprintf("deny %s: путь вне корня — корень и так его отсекает", r))
				continue
			}
			glob, err := writeGlob(r.Pattern)
			if err != nil {
				return Grant{}, fmt.Errorf("profile %s: deny %s: %w", p.Name, r, err)
			}
			negWrite = append(negWrite, "!"+glob)
		case domain.PermBash:
			if r.Pattern == "" {
				execDenyAll = true
				continue
			}
			g.ExecDeny = append(g.ExecDeny, strings.TrimSpace(r.Pattern))
		case domain.PermGrep:
			if r.Pattern == "" {
				for _, n := range grepTools {
					delete(tools, n)
				}
			}
		case domain.PermGlob:
			if r.Pattern == "" {
				for _, n := range globTools {
					delete(tools, n)
				}
			}
		default:
			// WebFetch/WebSearch/Task/Agent: no such tools exist here.
		}
	}

	if execDenyAll {
		g.ExecAllow = nil
		g.ExecDeny = []string{"*"}
		for _, n := range gitTools {
			delete(tools, n)
		}
	}
	exec, err := policy.ParseExec(g.ExecAllow, g.ExecDeny)
	if err != nil {
		return Grant{}, fmt.Errorf("profile %s: %w", p.Name, err)
	}
	g.Exec = exec
	if exec.Usable() {
		add("run_command")
	}

	if len(posWrite) > 0 {
		pos := make([]string, 0, len(posWrite))
		for w := range posWrite {
			pos = append(pos, w)
		}
		sort.Strings(pos)
		g.WriteGlobs = append(pos, negWrite...)
	} else {
		for _, n := range append(append([]string{}, editTools...), writeTools...) {
			delete(tools, n)
		}
		if len(g.ExecAllow) > 0 {
			return Grant{}, fmt.Errorf("profile %s: команды (Bash) разрешены без прав на запись — команде негде выполняться", p.Name)
		}
	}

	for n := range tools {
		g.AllowTools = append(g.AllowTools, n)
	}
	sort.Strings(g.AllowTools)
	if len(p.Env) > 0 {
		g.Env = make(map[string]string, len(p.Env))
		for k, v := range p.Env {
			g.Env[k] = v
		}
	}
	return g, nil
}

// writeGlob normalises and validates an Edit/Write pattern.
func writeGlob(pattern string) (string, error) {
	glob := policy.NormalizeGlob(pattern)
	if err := policy.ValidateWriteGlob(glob); err != nil {
		return "", err
	}
	return glob, nil
}

func ignoreHome(r domain.PermRule) bool {
	return strings.HasPrefix(r.Pattern, "~") || strings.HasPrefix(r.Pattern, "/")
}

// isGitRead reports whether a Bash pattern is one of the read-only git commands
// the git MCP server covers.
func isGitRead(pattern string) bool {
	p := strings.TrimSuffix(strings.TrimSpace(pattern), ":*")
	return p == "git status" || p == "git diff"
}

// MarkerPrefix starts the first line of every profile system prompt. The fake
// upstream in e2e tests uses it to recognise the role.
const MarkerPrefix = "# profile: "

// SystemPrompt composes a role's system prompt: the profile marker first,
// then the rules shared by every profile (untrusted DATA blocks, policy
// refusals), then the profile body.
func SystemPrompt(p domain.AgentProfile) string {
	body := strings.TrimSpace(p.Prompt)
	marker := MarkerPrefix + p.Name
	if first, rest, _ := strings.Cut(body, "\n"); strings.TrimSpace(first) == marker {
		body = strings.TrimSpace(rest)
	}
	var b strings.Builder
	b.WriteString(marker + "\n\n")
	b.WriteString(CommonRules)
	b.WriteString("\n")
	b.WriteString(body)
	b.WriteString("\n")
	return b.String()
}

// CommonRules is the prefix shared by every profile prompt.
var CommonRules = "# Общие правила агента\n\n" +
	"- " + usecase.UntrustedDataRule + "\n" +
	"- Ответ инструмента с кодом PERMISSION_DENIED, READ_ONLY_PATH, DENIED_BY_POLICY или BAD_ARGUMENT — это граница политики. Не обходи её другим инструментом или другой формой вызова: исправь аргументы или сообщи, чего не хватило.\n" +
	"- Сети нет. Секреты, ключи и значения переменных окружения не выводи никогда.\n"
