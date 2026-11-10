// Package profilefs loads sub-agent profiles stored in the Day2 format:
//
//	<dir>/<name>.md             frontmatter (name, description, tools, model,
//	                            optional stack) + the role prompt as the body
//	<dir>/<name>.settings.json  permissions.allow/deny and env
//
// Parsing is strict and fail-closed: an unknown permission kind, an unknown
// top-level settings section, a non-allowlisted env key or a name that does
// not match the file name is a load error, never a silent widening.
package profilefs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"ai-adv-agent/internal/domain"
)

// MaxProfileBytes caps both the .md and the .settings.json file.
const MaxProfileBytes = 64 << 10

// ErrNotFound is returned by Get for a profile that does not exist.
var ErrNotFound = errors.New("profile not found")

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ignoredSections are settings.json top-level keys that belong to Claude Code
// and have no meaning for this agent. They are skipped explicitly; any other
// unknown key is an error.
var ignoredSections = map[string]bool{
	"sandbox":                      true,
	"disableAllHooks":              true,
	"disableBypassPermissionsMode": true,
	"enableAllProjectMcpServers":   true,
	"autoMemoryEnabled":            true,
	"$schema":                      true,
}

// envRules is the env allowlist with the values each key may take. A value
// such as GOFLAGS=-toolexec=… would bypass the exec argv checks, so values are
// pinned, not just keys.
var envRules = map[string]func(string) bool{
	"GOTOOLCHAIN": func(v string) bool { return v == "local" },
	"GOPROXY":     func(v string) bool { return v == "off" },
	"GOFLAGS":     func(v string) bool { return v == "-mod=readonly" },
	"CGO_ENABLED": func(v string) bool { return v == "0" || v == "1" },
	"CI":          func(v string) bool { return v == "true" || v == "1" },
}

// Repo implements port.AgentProfileRepository over a directory.
type Repo struct {
	Dir string
	// Log receives informational notes (ignored model hints). nil = os.Stderr.
	Log io.Writer
}

// New returns a repository reading profiles from dir.
func New(dir string) *Repo { return &Repo{Dir: dir} }

func (r *Repo) logf(format string, a ...interface{}) {
	w := r.Log
	if w == nil {
		w = os.Stderr
	}
	fmt.Fprintf(w, "[profiles] "+format+"\n", a...)
}

// List loads every <name>.md in the directory, sorted by name.
func (r *Repo) List() ([]domain.AgentProfile, error) {
	entries, err := os.ReadDir(r.Dir)
	if err != nil {
		return nil, fmt.Errorf("read profiles dir %q: %w", r.Dir, err)
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".md") {
			continue
		}
		names = append(names, strings.TrimSuffix(n, ".md"))
	}
	sort.Strings(names)
	out := make([]domain.AgentProfile, 0, len(names))
	for _, n := range names {
		p, err := r.Get(n)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// Get loads one profile by name.
func (r *Repo) Get(name string) (domain.AgentProfile, error) {
	if !nameRe.MatchString(name) {
		return domain.AgentProfile{}, fmt.Errorf("profile name %q: only [a-z0-9-] allowed", name)
	}
	mdPath := filepath.Join(r.Dir, name+".md")
	md, err := readCapped(mdPath)
	if errors.Is(err, fs.ErrNotExist) {
		return domain.AgentProfile{}, fmt.Errorf("%w: %q (нет файла %s)", ErrNotFound, name, mdPath)
	}
	if err != nil {
		return domain.AgentProfile{}, err
	}
	p, err := parseMarkdown(md)
	if err != nil {
		return domain.AgentProfile{}, fmt.Errorf("profile %q: %w", name, err)
	}
	if p.Name != name {
		return domain.AgentProfile{}, fmt.Errorf("profile %q: frontmatter name %q does not match file name", name, p.Name)
	}
	if p.ModelHint != "" {
		r.logf("profile %s: model %q из frontmatter игнорируется, используется llm.model", name, p.ModelHint)
	}

	setPath := filepath.Join(r.Dir, name+".settings.json")
	set, err := readCapped(setPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		// No settings: read-only. Map still intersects these with tools:.
		p.Allow = []domain.PermRule{{Kind: domain.PermRead}, {Kind: domain.PermGrep}, {Kind: domain.PermGlob}}
		return p, nil
	case err != nil:
		return domain.AgentProfile{}, err
	}
	if err := parseSettings(set, &p); err != nil {
		return domain.AgentProfile{}, fmt.Errorf("profile %q: %s: %w", name, filepath.Base(setPath), err)
	}
	return p, nil
}

func readCapped(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxProfileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > MaxProfileBytes {
		return nil, fmt.Errorf("%s: файл больше %d байт", path, MaxProfileBytes)
	}
	return data, nil
}

type frontmatter struct {
	Name        string    `yaml:"name"`
	Description string    `yaml:"description"`
	Tools       yaml.Node `yaml:"tools"`
	Model       string    `yaml:"model"`
	Stack       []string  `yaml:"stack"`
}

func parseMarkdown(data []byte) (domain.AgentProfile, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return domain.AgentProfile{}, fmt.Errorf("missing frontmatter (file must start with ---)")
	}
	rest := text[4:]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return domain.AgentProfile{}, fmt.Errorf("unterminated frontmatter")
	}
	head, body := rest[:end], rest[end+5:]

	var fm frontmatter
	dec := yaml.NewDecoder(strings.NewReader(head))
	dec.KnownFields(true)
	if err := dec.Decode(&fm); err != nil {
		return domain.AgentProfile{}, fmt.Errorf("frontmatter: %w", err)
	}
	if strings.TrimSpace(fm.Name) == "" {
		return domain.AgentProfile{}, fmt.Errorf("frontmatter: name is required")
	}
	tools, err := parseTools(fm.Tools)
	if err != nil {
		return domain.AgentProfile{}, err
	}
	for _, s := range fm.Stack {
		if s != "go" && s != "vue" {
			return domain.AgentProfile{}, fmt.Errorf("frontmatter: unsupported stack %q (want go, vue)", s)
		}
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return domain.AgentProfile{}, fmt.Errorf("empty prompt body")
	}
	return domain.AgentProfile{
		Name:        strings.TrimSpace(fm.Name),
		Description: strings.TrimSpace(fm.Description),
		ModelHint:   strings.TrimSpace(fm.Model),
		Prompt:      body,
		Tools:       tools,
		Stack:       fm.Stack,
	}, nil
}

// parseTools accepts "Read, Grep" (Claude Code style) or a YAML list.
func parseTools(n yaml.Node) ([]domain.PermKind, error) {
	var raw []string
	switch n.Kind {
	case 0:
		return nil, nil
	case yaml.ScalarNode:
		for _, s := range strings.Split(n.Value, ",") {
			if s = strings.TrimSpace(s); s != "" {
				raw = append(raw, s)
			}
		}
	case yaml.SequenceNode:
		if err := n.Decode(&raw); err != nil {
			return nil, fmt.Errorf("frontmatter tools: %w", err)
		}
	default:
		return nil, fmt.Errorf("frontmatter tools: want a comma-separated string or a list")
	}
	out := make([]domain.PermKind, 0, len(raw))
	for _, s := range raw {
		k := domain.PermKind(strings.TrimSpace(s))
		if !k.Known() {
			return nil, fmt.Errorf("frontmatter tools: unknown tool %q", s)
		}
		out = append(out, k)
	}
	return out, nil
}

type permissions struct {
	DefaultMode string   `json:"defaultMode"`
	Allow       []string `json:"allow"`
	Deny        []string `json:"deny"`
}

func parseSettings(data []byte, p *domain.AgentProfile) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for k := range top {
		if k != "permissions" && k != "env" && !ignoredSections[k] {
			return fmt.Errorf("unknown section %q", k)
		}
	}
	if raw, ok := top["permissions"]; ok {
		var perms permissions
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&perms); err != nil {
			return fmt.Errorf("permissions: %w", err)
		}
		for _, s := range perms.Allow {
			r, err := domain.ParsePermRule(s)
			if err != nil {
				return fmt.Errorf("permissions.allow: %w", err)
			}
			p.Allow = append(p.Allow, r)
		}
		for _, s := range perms.Deny {
			r, err := domain.ParsePermRule(s)
			if err != nil {
				return fmt.Errorf("permissions.deny: %w", err)
			}
			p.Deny = append(p.Deny, r)
		}
	}
	if raw, ok := top["env"]; ok {
		var env map[string]string
		dec := json.NewDecoder(bytes.NewReader(raw))
		if err := dec.Decode(&env); err != nil {
			return fmt.Errorf("env: %w", err)
		}
		for k, v := range env {
			ok, known := envRules[k]
			if !known {
				return fmt.Errorf("env: key %q is not allowlisted", k)
			}
			if !ok(v) {
				return fmt.Errorf("env: value of %s is not allowed", k)
			}
		}
		if len(env) > 0 {
			p.Env = env
		}
	}
	return nil
}
