package domain

import (
	"fmt"
	"strings"
)

// PermKind is the permission kind of a Claude-style permission rule
// ("Read(./**)", "Bash(go test:*)", "Grep").
type PermKind string

// Known permission kinds. Only the first six can be granted; the rest exist in
// the Day2 settings files and may only appear in deny lists.
const (
	PermRead      PermKind = "Read"
	PermGrep      PermKind = "Grep"
	PermGlob      PermKind = "Glob"
	PermEdit      PermKind = "Edit"
	PermWrite     PermKind = "Write"
	PermBash      PermKind = "Bash"
	PermWebFetch  PermKind = "WebFetch"
	PermWebSearch PermKind = "WebSearch"
	PermTask      PermKind = "Task"
	PermAgent     PermKind = "Agent"
)

// Known reports whether k is one of the permission kinds above.
func (k PermKind) Known() bool {
	switch k {
	case PermRead, PermGrep, PermGlob, PermEdit, PermWrite, PermBash,
		PermWebFetch, PermWebSearch, PermTask, PermAgent:
		return true
	}
	return false
}

// PermRule is one permission rule. An empty Pattern means "any".
type PermRule struct {
	Kind    PermKind
	Pattern string
}

// String renders the rule back in its source form.
func (r PermRule) String() string {
	if r.Pattern == "" {
		return string(r.Kind)
	}
	return string(r.Kind) + "(" + r.Pattern + ")"
}

// AgentProfile is a sub-agent role definition loaded from a profile file
// (<name>.md with frontmatter + <name>.settings.json). It is named
// AgentProfile so it is not confused with UserProfile.
type AgentProfile struct {
	Name        string
	Description string
	// ModelHint is the frontmatter "model" value. It is informational only:
	// the model always comes from llm.model.
	ModelHint string
	// Prompt is the .md body without the frontmatter.
	Prompt string
	// Tools is the frontmatter "tools" list. A permission whose kind is not
	// listed here is not granted, even if settings allow it.
	Tools []PermKind
	Allow []PermRule
	Deny  []PermRule
	// Env holds settings.json env entries that passed the allowlist.
	Env map[string]string
	// Stack is optional ("go", "vue"): which code a reviewer profile covers.
	Stack []string
}

// HasTool reports whether the frontmatter lists the kind.
func (p AgentProfile) HasTool(k PermKind) bool {
	for _, t := range p.Tools {
		if t == k {
			return true
		}
	}
	return false
}

// ParsePermRule parses "Kind" or "Kind(pattern)". The kind must be one of the
// known kinds; anything else is an error so a typo cannot silently drop or
// widen a permission.
func ParsePermRule(s string) (PermRule, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return PermRule{}, fmt.Errorf("empty permission rule")
	}
	kind, pattern := s, ""
	if i := strings.IndexByte(s, '('); i >= 0 {
		if !strings.HasSuffix(s, ")") {
			return PermRule{}, fmt.Errorf("permission rule %q: missing closing parenthesis", s)
		}
		kind, pattern = s[:i], s[i+1:len(s)-1]
		if strings.TrimSpace(pattern) == "" {
			return PermRule{}, fmt.Errorf("permission rule %q: empty pattern", s)
		}
	}
	k := PermKind(strings.TrimSpace(kind))
	if !k.Known() {
		return PermRule{}, fmt.Errorf("permission rule %q: unknown kind %q", s, kind)
	}
	return PermRule{Kind: k, Pattern: pattern}, nil
}
