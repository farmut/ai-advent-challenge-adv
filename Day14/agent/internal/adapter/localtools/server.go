// Package localtools is the built-in "local" tool server. It serves the file
// tools under the same names and argument schemas as
// @modelcontextprotocol/server-filesystem, plus grep and run_command, without
// spawning npx or fff-mcp — so loop runs and e2e tests are deterministic and
// need no network.
//
// It is NOT the security boundary: every call is first approved by
// policy.FS.Check (which rewrites paths to absolute approved ones). The
// server still refuses any path outside Root and never follows symlinks while
// walking, as defence in depth.
package localtools

import (
	"bufio"
	"context"
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
	"time"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// ServerName is the MCP server name the local tools are registered under.
const ServerName = "local"

// Limits. They bound tool results so a single call cannot blow the request
// body cap of the guard proxy.
const (
	MaxReadBytes   = 64 << 10
	MaxWriteBytes  = 1 << 20
	MaxGrepMatches = 200
	MaxGrepFile    = 1 << 20
	MaxTreeEntries = 2000
	MaxLineChars   = 400
)

// Server serves the local tools for one root.
type Server struct {
	// Root is the absolute directory every path must stay inside.
	Root string
	// WorkDir is where run_command executes (absolute, inside Root or the
	// sandbox workspace). Empty = Root.
	WorkDir string
	// Runner executes run_command. nil: run_command fails (fail-closed).
	Runner port.CommandRunner
	// Command carries the per-role command settings (timeout, output cap,
	// env, stack). Argv and Workdir are filled per call.
	Command port.CommandSpec
	// Hide reports paths (relative to Root, slash form) that walks must skip:
	// the deny globs. nil hides nothing.
	Hide func(rel string) bool
}

// New returns a server rooted at root.
func New(root string) *Server { return &Server{Root: root} }

// ListTools implements port.MCPToolLister.
func (s *Server) ListTools(context.Context, domain.MCPServerConfig) ([]domain.MCPTool, error) {
	return Tools(), nil
}

// CallTool implements port.MCPToolCaller.
func (s *Server) CallTool(ctx context.Context, _ domain.MCPServerConfig, name string, args map[string]interface{}) (string, error) {
	return s.Call(ctx, name, args)
}

// Call dispatches one tool call.
func (s *Server) Call(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	switch name {
	case "read_text_file":
		return s.readText(args)
	case "read_multiple_files":
		return s.readMultiple(args)
	case "list_directory":
		return s.listDirectory(args)
	case "directory_tree":
		return s.directoryTree(args)
	case "search_files":
		return s.searchFiles(args)
	case "get_file_info":
		return s.fileInfo(args)
	case "write_file":
		return s.writeFile(args)
	case "edit_file":
		return s.editFile(args)
	case "create_directory":
		return s.createDirectory(args)
	case "grep":
		return s.grep(ctx, args)
	case "run_command":
		return s.runCommand(ctx, args)
	}
	return "", fmt.Errorf("unknown tool %q", name)
}

// ---------------------------------------------------------------------------
// paths
// ---------------------------------------------------------------------------

func (s *Server) root() (string, error) {
	if s.Root == "" {
		return "", errors.New("local tools: root is not configured")
	}
	r, err := filepath.EvalSymlinks(s.Root)
	if err != nil {
		return "", fmt.Errorf("local tools: root unavailable: %w", err)
	}
	return r, nil
}

// resolve turns an argument into an absolute path inside Root. mustExist
// resolves the path itself; otherwise its parent.
func (s *Server) resolve(raw string, mustExist bool) (string, string, error) {
	root, err := s.root()
	if err != nil {
		return "", "", err
	}
	if strings.TrimSpace(raw) == "" {
		return "", "", errors.New("path is required")
	}
	p := raw
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	p = filepath.Clean(p)
	var real string
	if mustExist {
		real, err = filepath.EvalSymlinks(p)
		if err != nil {
			return "", "", fmt.Errorf("path %q does not exist", raw)
		}
	} else {
		parent, err := filepath.EvalSymlinks(filepath.Dir(p))
		if err != nil {
			return "", "", fmt.Errorf("parent directory of %q does not exist", raw)
		}
		real = filepath.Join(parent, filepath.Base(p))
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("path %q is outside the allowed root", raw)
	}
	return real, filepath.ToSlash(rel), nil
}

func (s *Server) hidden(rel string) bool {
	return s.Hide != nil && rel != "." && s.Hide(rel)
}

func strArg(args map[string]interface{}, key string) (string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return "", fmt.Errorf("argument %q is required", key)
	}
	str, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string", key)
	}
	return str, nil
}

func optStr(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return v
}

func optInt(args map[string]interface{}, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

// ---------------------------------------------------------------------------
// reads
// ---------------------------------------------------------------------------

func readCapped(path string, max int) (string, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if err != nil {
		return "", false, err
	}
	if len(data) > max {
		return string(data[:max]), true, nil
	}
	return string(data), false, nil
}

func (s *Server) readText(args map[string]interface{}) (string, error) {
	raw, err := strArg(args, "path")
	if err != nil {
		return "", err
	}
	abs, _, err := s.resolve(raw, true)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(abs); err != nil || fi.IsDir() {
		return "", fmt.Errorf("%q is not a regular file", raw)
	}
	text, truncated, err := readCapped(abs, MaxReadBytes)
	if err != nil {
		return "", err
	}
	lines := strings.SplitAfter(text, "\n")
	if h := optInt(args, "head"); h > 0 && h < len(lines) {
		lines = lines[:h]
	}
	if t := optInt(args, "tail"); t > 0 && t < len(lines) {
		lines = lines[len(lines)-t:]
	}
	out := strings.Join(lines, "")
	if truncated {
		out += fmt.Sprintf("\n[truncated: file exceeds %d bytes]", MaxReadBytes)
	}
	return out, nil
}

func (s *Server) readMultiple(args map[string]interface{}) (string, error) {
	list, ok := args["paths"].([]interface{})
	if !ok || len(list) == 0 {
		return "", errors.New(`argument "paths" must be a non-empty array of strings`)
	}
	var b strings.Builder
	for i, v := range list {
		p, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("paths[%d] must be a string", i)
		}
		text, err := s.readText(map[string]interface{}{"path": p})
		if i > 0 {
			b.WriteString("\n---\n")
		}
		if err != nil {
			fmt.Fprintf(&b, "%s: Error - %v", p, err)
			continue
		}
		fmt.Fprintf(&b, "%s:\n%s", p, text)
	}
	return b.String(), nil
}

func (s *Server) listDirectory(args map[string]interface{}) (string, error) {
	raw, err := strArg(args, "path")
	if err != nil {
		return "", err
	}
	abs, rel, err := s.resolve(raw, true)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, e := range entries {
		if s.hidden(joinRel(rel, e.Name())) {
			continue
		}
		kind := "[FILE]"
		if e.IsDir() {
			kind = "[DIR]"
		}
		fmt.Fprintf(&b, "%s %s\n", kind, e.Name())
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

func joinRel(dir, name string) string {
	if dir == "." || dir == "" {
		return name
	}
	return dir + "/" + name
}

type treeEntry struct {
	Name     string       `json:"name"`
	Type     string       `json:"type"`
	Children []*treeEntry `json:"children,omitempty"`
}

func (s *Server) directoryTree(args map[string]interface{}) (string, error) {
	raw, err := strArg(args, "path")
	if err != nil {
		return "", err
	}
	abs, rel, err := s.resolve(raw, true)
	if err != nil {
		return "", err
	}
	count := 0
	var walk func(dir, rel string) ([]*treeEntry, error)
	walk = func(dir, rel string) ([]*treeEntry, error) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		out := []*treeEntry{}
		for _, e := range entries {
			r := joinRel(rel, e.Name())
			if s.hidden(r) || e.Type()&fs.ModeSymlink != 0 {
				continue
			}
			if count++; count > MaxTreeEntries {
				return out, nil
			}
			te := &treeEntry{Name: e.Name(), Type: "file"}
			if e.IsDir() {
				te.Type = "directory"
				kids, err := walk(filepath.Join(dir, e.Name()), r)
				if err != nil {
					return nil, err
				}
				te.Children = kids
			}
			out = append(out, te)
		}
		return out, nil
	}
	tree, err := walk(abs, rel)
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return "", err
	}
	out := string(data)
	if count > MaxTreeEntries {
		out += fmt.Sprintf("\n[truncated: more than %d entries]", MaxTreeEntries)
	}
	return out, nil
}

// walkFiles visits regular files under start (absolute) without following
// symlinks and skipping hidden paths. rel is relative to Root.
func (s *Server) walkFiles(ctx context.Context, start string, fn func(abs, rel string, d fs.DirEntry) error) error {
	root, err := s.root()
	if err != nil {
		return err
	}
	return filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry: skip
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		r, _ := filepath.Rel(root, p)
		r = filepath.ToSlash(r)
		if p != start && s.hidden(r) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		return fn(p, r, d)
	})
}

func (s *Server) searchFiles(args map[string]interface{}) (string, error) {
	raw, err := strArg(args, "path")
	if err != nil {
		return "", err
	}
	pattern, err := strArg(args, "pattern")
	if err != nil {
		return "", err
	}
	abs, _, err := s.resolve(raw, true)
	if err != nil {
		return "", err
	}
	lower := strings.ToLower(pattern)
	var hits []string
	err = s.walkFiles(context.Background(), abs, func(_, rel string, d fs.DirEntry) error {
		if rel == "." {
			return nil
		}
		name := strings.ToLower(d.Name())
		ok, _ := filepath.Match(lower, name)
		if ok || strings.Contains(name, lower) {
			hits = append(hits, rel)
		}
		if len(hits) >= MaxGrepMatches {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(hits) == 0 {
		return "No matches found", nil
	}
	return strings.Join(hits, "\n"), nil
}

func (s *Server) fileInfo(args map[string]interface{}) (string, error) {
	raw, err := strArg(args, "path")
	if err != nil {
		return "", err
	}
	abs, _, err := s.resolve(raw, true)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("size: %d\nmodified: %s\nisDirectory: %t\nisFile: %t\npermissions: %o",
		fi.Size(), fi.ModTime().UTC().Format(time.RFC3339), fi.IsDir(), fi.Mode().IsRegular(), fi.Mode().Perm()), nil
}

// ---------------------------------------------------------------------------
// writes
// ---------------------------------------------------------------------------

// Files are written 0644 and directories 0755: the sandbox runs as an
// unprivileged uid that must be able to read the workspace (see PR5).
const (
	fileMode = 0o644
	dirMode  = 0o755
)

func (s *Server) writeFile(args map[string]interface{}) (string, error) {
	raw, err := strArg(args, "path")
	if err != nil {
		return "", err
	}
	content, err := strArg(args, "content")
	if err != nil {
		return "", err
	}
	if len(content) > MaxWriteBytes {
		return "", fmt.Errorf("content exceeds %d bytes", MaxWriteBytes)
	}
	abs, _, err := s.resolve(raw, false)
	if err != nil {
		return "", err
	}
	if fi, err := os.Lstat(abs); err == nil && !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%q is not a regular file", raw)
	}
	if err := writeAtomic(abs, []byte(content)); err != nil {
		return "", err
	}
	return fmt.Sprintf("Successfully wrote to %s", raw), nil
}

// writeAtomic writes through a temp file and rename, so a crash never leaves
// a half-written source file behind.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(fileMode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

type edit struct {
	OldText string `json:"oldText"`
	NewText string `json:"newText"`
}

func (s *Server) editFile(args map[string]interface{}) (string, error) {
	raw, err := strArg(args, "path")
	if err != nil {
		return "", err
	}
	list, ok := args["edits"].([]interface{})
	if !ok || len(list) == 0 {
		return "", errors.New(`argument "edits" must be a non-empty array of {oldText,newText}`)
	}
	edits := make([]edit, 0, len(list))
	for i, v := range list {
		m, ok := v.(map[string]interface{})
		if !ok {
			return "", fmt.Errorf("edits[%d] must be an object {oldText,newText}", i)
		}
		o, ok1 := m["oldText"].(string)
		n, ok2 := m["newText"].(string)
		if !ok1 || !ok2 || o == "" {
			return "", fmt.Errorf("edits[%d]: oldText (non-empty) and newText must be strings", i)
		}
		edits = append(edits, edit{OldText: o, NewText: n})
	}
	dryRun, _ := args["dryRun"].(bool)

	abs, _, err := s.resolve(raw, true)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(abs)
	if err != nil || !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%q is not a regular file", raw)
	}
	text, truncated, err := readCapped(abs, MaxWriteBytes)
	if err != nil {
		return "", err
	}
	if truncated {
		return "", fmt.Errorf("%q is larger than %d bytes", raw, MaxWriteBytes)
	}
	var diff strings.Builder
	fmt.Fprintf(&diff, "--- %s\n+++ %s\n", raw, raw)
	for i, e := range edits {
		switch n := strings.Count(text, e.OldText); {
		case n == 0:
			return "", fmt.Errorf("edits[%d]: oldText not found in %s — read the file first; oldText must match exactly, including indentation", i, raw)
		case n > 1:
			return "", fmt.Errorf("edits[%d]: oldText occurs %d times in %s — include more context so it is unique", i, n, raw)
		}
		text = strings.Replace(text, e.OldText, e.NewText, 1)
		fmt.Fprintf(&diff, "@@ edit %d @@\n", i+1)
		for _, l := range strings.Split(e.OldText, "\n") {
			diff.WriteString("-" + l + "\n")
		}
		for _, l := range strings.Split(e.NewText, "\n") {
			diff.WriteString("+" + l + "\n")
		}
	}
	if dryRun {
		return "dry run, nothing written:\n" + diff.String(), nil
	}
	if err := writeAtomic(abs, []byte(text)); err != nil {
		return "", err
	}
	return diff.String(), nil
}

func (s *Server) createDirectory(args map[string]interface{}) (string, error) {
	raw, err := strArg(args, "path")
	if err != nil {
		return "", err
	}
	root, err := s.root()
	if err != nil {
		return "", err
	}
	p := raw
	if !filepath.IsAbs(p) {
		p = filepath.Join(root, p)
	}
	p = filepath.Clean(p)
	// Walk up to the deepest existing ancestor and prove it is inside Root:
	// MkdirAll would otherwise follow a symlinked ancestor out of the root.
	anc := p
	for {
		if _, err := os.Lstat(anc); err == nil {
			break
		}
		parent := filepath.Dir(anc)
		if parent == anc {
			break
		}
		anc = parent
	}
	real, err := filepath.EvalSymlinks(anc)
	if err != nil {
		return "", err
	}
	if r, err := filepath.Rel(root, real); err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside the allowed root", raw)
	}
	tail, _ := filepath.Rel(anc, p)
	if err := os.MkdirAll(filepath.Join(real, tail), dirMode); err != nil {
		return "", err
	}
	return fmt.Sprintf("Successfully created directory %s", raw), nil
}

// ---------------------------------------------------------------------------
// grep
// ---------------------------------------------------------------------------

// grep prints fff-mcp-compatible output ("path" header, then " N: text"
// lines), so policy.FS.FilterOutput parses it with the same fail-closed
// parser. There is no path argument: the search scope is the whole Root,
// minus hidden paths.
func (s *Server) grep(ctx context.Context, args map[string]interface{}) (string, error) {
	pat, err := strArg(args, "pattern")
	if err != nil {
		return "", err
	}
	if cs, _ := args["caseSensitive"].(bool); !cs {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return "", fmt.Errorf("invalid pattern: %v", err)
	}
	root, err := s.root()
	if err != nil {
		return "", err
	}
	type hit struct {
		line int
		text string
	}
	byFile := map[string][]hit{}
	var order []string
	total := 0
	err = s.walkFiles(ctx, root, func(abs, rel string, d fs.DirEntry) error {
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if fi, err := d.Info(); err != nil || !fi.Mode().IsRegular() || fi.Size() > MaxGrepFile {
			return nil
		}
		f, err := os.Open(abs)
		if err != nil {
			return nil
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), MaxGrepFile)
		n := 0
		for sc.Scan() {
			n++
			line := sc.Text()
			if strings.IndexByte(line, 0) >= 0 {
				return nil // binary file
			}
			if !re.MatchString(line) {
				continue
			}
			total++
			if total > MaxGrepMatches {
				continue
			}
			if _, seen := byFile[rel]; !seen {
				order = append(order, rel)
			}
			if len(line) > MaxLineChars {
				line = line[:MaxLineChars] + "…"
			}
			byFile[rel] = append(byFile[rel], hit{n, line})
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if total == 0 {
		return "0 matches.", nil
	}
	sort.Strings(order)
	var b strings.Builder
	if total > MaxGrepMatches {
		fmt.Fprintf(&b, "%d/%d matches shown\n", MaxGrepMatches, total)
	}
	for _, f := range order {
		b.WriteString(f + "\n")
		for _, h := range byFile[f] {
			fmt.Fprintf(&b, " %d: %s\n", h.line, h.text)
		}
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// ---------------------------------------------------------------------------
// run_command
// ---------------------------------------------------------------------------

// ErrNoRunner is returned by run_command when no sandbox runner is wired.
var ErrNoRunner = errors.New("run_command: песочница не подключена — запуск команд недоступен")

func (s *Server) runCommand(ctx context.Context, args map[string]interface{}) (string, error) {
	if s.Runner == nil {
		return "", ErrNoRunner
	}
	list, ok := args["argv"].([]interface{})
	if !ok || len(list) == 0 {
		return "", errors.New(`argument "argv" must be a non-empty array of strings`)
	}
	argv := make([]string, len(list))
	for i, v := range list {
		str, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("argv[%d] must be a string", i)
		}
		argv[i] = str
	}
	spec := s.Command
	spec.Argv = argv
	spec.Workdir = s.WorkDir
	if spec.Workdir == "" {
		root, err := s.root()
		if err != nil {
			return "", err
		}
		spec.Workdir = root
	}
	res, err := s.Runner.Run(ctx, spec)
	if err != nil {
		return "", fmt.Errorf("run_command: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "exit_code: %d\n", res.ExitCode)
	if res.TimedOut {
		b.WriteString("timed_out: true\n")
	}
	if res.Truncated {
		b.WriteString("output_truncated: true\n")
	}
	b.WriteString("--- stdout ---\n" + res.Stdout)
	if !strings.HasSuffix(res.Stdout, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString("--- stderr ---\n" + res.Stderr)
	return b.String(), nil
}
