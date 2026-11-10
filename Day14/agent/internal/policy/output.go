package policy

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// FilterOutput removes the parts of a tool result that name a path the deny
// globs forbid. Without it a role that may not read ".env" could still learn
// its contents from `grep KEY` or `git_diff`, because those tools take no path
// argument for Check to refuse.
//
// It returns the filtered text and how many entries (file blocks, list lines,
// diff blocks) were hidden. Tools without OutputPaths, and every tool when the
// guard is disabled (no Specs), pass through unchanged.
//
// Parsing is fail-closed: output whose shape the parser does not recognise is
// replaced by a DENIED_BY_POLICY error rather than forwarded, because an
// unparsed block may be exactly the one that should have been hidden. The
// formats were taken from live output of fff-mcp 0.10.0, server-filesystem
// 2026.7.10 and ../git-mcp-server; see output_test.go for the golden samples.
func (p FS) FilterOutput(tool, out string) (string, int, error) {
	spec, ok := p.Specs[tool]
	if !ok || !spec.OutputPaths {
		return out, 0, nil
	}
	var (
		filtered string
		hidden   int
		err      error
	)
	switch tool {
	case "grep", "multi_grep", "find_files":
		filtered, hidden, err = p.filterFFF(out)
	case "search_files":
		filtered, hidden = p.filterPathLines(out)
	case "git_list_files":
		filtered, hidden, err = p.filterGitList(out)
	case "git_diff":
		filtered, hidden, err = p.filterGitDiff(out)
	default:
		err = errUnparsedOutput(tool)
	}
	if err != nil {
		return "", 0, p.deny(tool, "", "", err)
	}
	if hidden > 0 {
		p.audit(tool, "", "", "FILTER", CodeDeniedByPolicy+" hidden="+strconv.Itoa(hidden))
	}
	return filtered, hidden, nil
}

// errUnparsedOutput is the fail-closed verdict for output of an unknown shape.
func errUnparsedOutput(tool string) *Error {
	return newErr(CodeDeniedByPolicy,
		"вывод инструмента %s не удалось разобрать, поэтому он скрыт целиком (политика не может проверить, нет ли в нём закрытых файлов). Сузь запрос или прочитай нужный файл напрямую.", tool)
}

// outputPathDenied reports whether a path printed by a tool is covered by a
// deny glob.
//
// Tools print paths relative to different bases: fff to the read root,
// server-filesystem as absolute paths, git relative to the repository root
// (which usually sits above the read root). The path is therefore checked in
// every suffix form — "Day14/agent/.env", "agent/.env", ".env" — so a pattern
// anchored at the read root still matches when the printed path carries extra
// leading segments. This can only hide more, never less.
func (p FS) outputPathDenied(raw string) bool {
	s := strings.TrimSpace(raw)
	if s == "" {
		return false
	}
	if unq, err := strconv.Unquote(s); err == nil && strings.HasPrefix(s, `"`) {
		s = unq // git quotes paths with special characters C-style
	}
	if filepath.IsAbs(s) {
		if root := p.baseRoot(); root != "" {
			if r, err := relUnder(root, filepath.Clean(s)); err == nil {
				s = r
			}
		}
	}
	s = strings.TrimLeft(filepath.ToSlash(filepath.Clean(s)), "/")
	segs := strings.Split(s, "/")
	for i := range segs {
		if _, hit := p.denied(strings.Join(segs[i:], "/")); hit {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// fff-mcp: grep / multi_grep / find_files
// ---------------------------------------------------------------------------

var (
	// " 12: text", "  3| text", " 4-text" — a match or context line.
	fffContentRe = regexp.MustCompile(`^\s+\d+[:|\-]`)
	// "2/9 matches shown", "1/3 matches", "cursor: 1".
	fffMetaRe = regexp.MustCompile(`^(\d+/\d+ matches( shown)?|cursor: \S+)$`)
	// Known empty results.
	fffEmptyRe = regexp.MustCompile(`^(0 matches\.|0 results \(\d+ indexed\))$`)
	// "a/b.go: 6" (output_mode=count) and "a/b.go git:clean" (find_files).
	fffCountRe = regexp.MustCompile(`^(.+): \d+$`)
	fffGitRe   = regexp.MustCompile(`^(.+) git:\S+$`)
)

const fffHintPrefix = "→ Read "

func (p FS) filterFFF(out string) (string, int, error) {
	if fffEmptyRe.MatchString(strings.TrimSpace(out)) {
		return out, 0, nil
	}
	lines := strings.Split(out, "\n")
	kept := make([]string, 0, len(lines))
	hidden := 0
	inFile, fileDenied := false, false
	for _, line := range lines {
		switch {
		case line == "":
			if !fileDenied {
				kept = append(kept, line)
			}
		case strings.HasPrefix(line, fffHintPrefix):
			// "→ Read a/b.go (best match)": the hint names a file too.
			path := strings.TrimPrefix(line, fffHintPrefix)
			if i := strings.Index(path, " ("); i >= 0 {
				path = path[:i]
			}
			if !p.outputPathDenied(path) {
				kept = append(kept, line)
			}
		case fffMetaRe.MatchString(line):
			kept = append(kept, line)
		case fffContentRe.MatchString(line) || line == "--":
			if !inFile {
				return "", 0, errUnparsedOutput("fff")
			}
			if !fileDenied {
				kept = append(kept, line)
			}
		case strings.TrimLeft(line, " \t") != line:
			// Indented but not a numbered line: a format we do not know.
			return "", 0, errUnparsedOutput("fff")
		default:
			path := line
			if m := fffGitRe.FindStringSubmatch(line); m != nil {
				path = m[1]
			} else if m := fffCountRe.FindStringSubmatch(line); m != nil {
				path = m[1]
			}
			inFile = true
			fileDenied = p.outputPathDenied(path) || p.outputPathDenied(line)
			if fileDenied {
				hidden++
				continue
			}
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n"), hidden, nil
}

// ---------------------------------------------------------------------------
// One path per line: search_files and git_list_files
// ---------------------------------------------------------------------------

var (
	// git status --porcelain: "XY path" or "XY orig -> path".
	gitPorcelainRe = regexp.MustCompile(`^[ MADRCUT?!]{2} (.+)$`)
	// git diff --name-status: "M\tpath", "R100\told\tnew".
	gitNameStatusRe = regexp.MustCompile(`^[A-Z]\d*\t(.+)$`)
)

// filterPathLines handles outputs that are a list of paths, possibly decorated
// with git status codes. Every line is checked in all its plausible readings
// (the raw line, the porcelain path, both rename sides, the name-status
// fields), so an unexpected decoration cannot hide a denied path: whatever the
// format, the raw line is still checked by its suffixes. Marker lines such as
// "No matches found" or "working tree clean" contain no denied name and pass.
func (p FS) filterPathLines(out string) (string, int) {
	lines := strings.Split(out, "\n")
	kept := make([]string, 0, len(lines))
	hidden := 0
	for _, line := range lines {
		if p.pathLineDenied(line) {
			hidden++
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n"), hidden
}

func (p FS) pathLineDenied(line string) bool {
	if strings.TrimSpace(line) == "" {
		return false
	}
	candidates := []string{line}
	if m := gitPorcelainRe.FindStringSubmatch(line); m != nil {
		candidates = append(candidates, m[1])
		candidates = append(candidates, strings.Split(m[1], " -> ")...)
	}
	if m := gitNameStatusRe.FindStringSubmatch(line); m != nil {
		candidates = append(candidates, strings.Split(m[1], "\t")...)
	}
	for _, c := range candidates {
		if p.outputPathDenied(c) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// git_diff
// ---------------------------------------------------------------------------

const gitDiffHeader = "diff --git "

// filterGitDiff drops every per-file block ("diff --git …" up to the next
// header) that touches a denied path. Any text before the first header other
// than the server's "no … changes" marker is unrecognised and fails closed.
func (p FS) filterGitDiff(out string) (string, int, error) {
	switch strings.TrimSpace(out) {
	case "no unstaged changes", "no staged changes":
		return out, 0, nil
	}
	prefix, err := p.gitPrefix()
	if err != nil {
		return "", 0, err
	}
	lines := strings.Split(out, "\n")
	var (
		kept   []string
		block  []string
		hidden int
	)
	flush := func() {
		if len(block) == 0 {
			return
		}
		if p.diffBlockDenied(block) || p.diffBlockOutside(block, prefix) {
			hidden++
		} else {
			kept = append(kept, block...)
		}
		block = nil
	}
	for _, line := range lines {
		if strings.HasPrefix(line, gitDiffHeader) {
			flush()
			block = []string{line}
			continue
		}
		if block == nil {
			if strings.TrimSpace(line) == "" || strings.HasPrefix(line, gitTruncatedMarker) {
				kept = append(kept, line)
				continue
			}
			return "", 0, errUnparsedOutput("git_diff")
		}
		block = append(block, line)
	}
	flush()
	return strings.Join(kept, "\n"), hidden, nil
}

// diffBlockDenied checks every path a diff block names: the header (both
// sides, quoted or not), the ---/+++ lines and rename/copy lines.
func (p FS) diffBlockDenied(block []string) bool {
	header := strings.TrimPrefix(block[0], gitDiffHeader)
	candidates := []string{header}
	if i := strings.Index(header, " b/"); i >= 0 {
		candidates = append(candidates, strings.TrimPrefix(header[:i], "a/"), header[i+3:])
	}
	if strings.HasPrefix(header, `"`) {
		// `"a/x y" "b/x y"`: split on the closing quote of the first path.
		if i := strings.Index(header[1:], `" "`); i >= 0 {
			a, b := header[:i+2], header[i+3:]
			for _, q := range []string{a, b} {
				if u, err := strconv.Unquote(q); err == nil {
					candidates = append(candidates, stripDiffSide(u))
				}
			}
		}
	}
	candidates = append(candidates, diffBlockPaths(block)...)
	for _, c := range candidates {
		if p.outputPathDenied(c) {
			return true
		}
	}
	return false
}

// stripDiffSide removes git's "a/" / "b/" side prefix.
func stripDiffSide(s string) string {
	if strings.HasPrefix(s, "a/") || strings.HasPrefix(s, "b/") {
		return s[2:]
	}
	return s
}

// diffBlockPaths extracts the side paths a diff block names in its ---/+++ and
// rename/copy lines, with the a/ b/ prefixes removed.
func diffBlockPaths(block []string) []string {
	var out []string
	for _, line := range block[1:] {
		for _, prefix := range []string{"--- ", "+++ ", "rename from ", "rename to ", "copy from ", "copy to "} {
			if strings.HasPrefix(line, prefix) {
				v := strings.TrimPrefix(line, prefix)
				if u, err := strconv.Unquote(v); err == nil && strings.HasPrefix(v, `"`) {
					v = u
				}
				if v != "/dev/null" {
					out = append(out, stripDiffSide(v))
				}
			}
		}
	}
	return out
}

// diffBlockOutside reports whether a diff block touches a path outside the
// read root. The header is parsed for both sides; when it cannot be split
// unambiguously the ---/+++ lines decide, and a block with no readable path at
// all is treated as outside (fail-closed).
func (p FS) diffBlockOutside(block []string, prefix string) bool {
	paths := diffBlockPaths(block)
	header := strings.TrimPrefix(block[0], gitDiffHeader)
	if strings.HasPrefix(header, `"`) {
		if i := strings.Index(header[1:], `" "`); i >= 0 {
			for _, q := range []string{header[:i+2], header[i+3:]} {
				if u, err := strconv.Unquote(q); err == nil {
					paths = append(paths, stripDiffSide(u))
				}
			}
		}
	} else if strings.HasPrefix(header, "a/") {
		if i := strings.Index(header, " b/"); i >= 0 {
			paths = append(paths, header[2:i], header[i+3:])
		}
	}
	if len(paths) == 0 {
		return true
	}
	for _, c := range paths {
		if !gitPathInside(prefix, c) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// git paths relative to the repository root
// ---------------------------------------------------------------------------

// gitTruncatedMarker starts the git server's truncation notice.
const gitTruncatedMarker = "… [truncated:"

// gitListMarkers are the git server's fixed "nothing to list" answers.
var gitListMarkers = map[string]bool{
	"working tree clean — no changed files": true,
	"nothing staged for commit":             true,
	"no untracked files":                    true,
}

// gitPrefix returns the read root relative to the git repository root, in
// slash form ("." when they coincide). Git tools print paths against the
// repository root, which usually sits above the read root, so without this a
// role confined to Day14/agent would still learn every path in the repo.
// It fails closed when the repository cannot be located or the read root is
// not inside it.
func (p FS) gitPrefix() (string, error) {
	if p.ReadRoot == "" {
		return "", newErr(CodePermissionDenied, "чтение файлов не выдано этой роли — вывод git-инструментов скрыт.")
	}
	root := p.GitRoot
	if root == "" {
		var ok bool
		if root, ok = DetectGitRoot(p.ReadRoot); !ok {
			return "", newErr(CodeDeniedByPolicy,
				"не удалось определить корень git-репозитория для %s, поэтому вывод git-инструментов скрыт (политика не может проверить пути).", filepath.Base(p.ReadRoot))
		}
	}
	read := p.ReadRoot
	if r, err := filepath.EvalSymlinks(read); err == nil {
		read = r
	}
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	rel, err := relUnder(root, read)
	if err != nil {
		return "", newErr(CodeDeniedByPolicy, "корень чтения лежит вне git-репозитория — вывод git-инструментов скрыт.")
	}
	return filepath.ToSlash(rel), nil
}

// DetectGitRoot walks up from dir to the first directory holding ".git" (a
// directory, or a file for worktrees and submodules).
func DetectGitRoot(dir string) (string, bool) {
	d := dir
	if r, err := filepath.EvalSymlinks(d); err == nil {
		d = r
	}
	for {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return d, true
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", false
		}
		d = parent
	}
}

// gitPathInside reports whether a repository-relative path lies inside the
// read root whose repository-relative form is prefix.
func gitPathInside(prefix, raw string) bool {
	s := strings.TrimSpace(raw)
	if unq, err := strconv.Unquote(s); err == nil && strings.HasPrefix(s, `"`) {
		s = unq
	}
	if s == "" || filepath.IsAbs(s) {
		return false
	}
	c := path.Clean(filepath.ToSlash(s))
	if c == ".." || strings.HasPrefix(c, "../") {
		return false
	}
	if prefix == "." {
		return true
	}
	return c == prefix || strings.HasPrefix(c, prefix+"/")
}

// filterGitList filters git_list_files output. The format is decided for the
// whole output (porcelain, name-status or plain ls-files), so a file whose
// name merely looks like a status line cannot pick its own reading. A line is
// kept only when every path it names lies inside the read root and no deny
// glob matches it.
func (p FS) filterGitList(out string) (string, int, error) {
	trimmed := strings.TrimSpace(out)
	if gitListMarkers[trimmed] {
		return out, 0, nil
	}
	prefix, err := p.gitPrefix()
	if err != nil {
		return "", 0, err
	}
	lines := strings.Split(out, "\n")
	isPorcelain, isNameStatus := true, true
	for _, l := range lines {
		if strings.TrimSpace(l) == "" || strings.HasPrefix(l, gitTruncatedMarker) {
			continue
		}
		isPorcelain = isPorcelain && gitPorcelainRe.MatchString(l)
		isNameStatus = isNameStatus && gitNameStatusRe.MatchString(l)
	}
	kept := make([]string, 0, len(lines))
	hidden := 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "" || strings.HasPrefix(l, gitTruncatedMarker) {
			kept = append(kept, l)
			continue
		}
		var paths []string
		switch {
		case isPorcelain:
			m := gitPorcelainRe.FindStringSubmatch(l)
			paths = strings.Split(m[1], " -> ")
		case isNameStatus:
			m := gitNameStatusRe.FindStringSubmatch(l)
			paths = strings.Split(m[1], "\t")
		default:
			paths = []string{l}
		}
		drop := p.pathLineDenied(l)
		for _, c := range paths {
			if !gitPathInside(prefix, c) || p.outputPathDenied(c) {
				drop = true
			}
		}
		if drop {
			hidden++
			continue
		}
		kept = append(kept, l)
	}
	return strings.Join(kept, "\n"), hidden, nil
}
