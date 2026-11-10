package policy

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// Write globs narrow the write root. They are written relative to WriteRoot
// (the profile's working directory). A glob starting with "!" is negative:
// a match is refused even if a positive glob also matches (deny beats allow).
//
// Positive globs are matched strictly, segment by segment ("**" = any number
// of segments), so "README.md" means the root README only. Negative globs use
// the looser deny dialect (also matching the base name anywhere), because for
// a denial over-matching is the safe direction.

// ValidateWriteGlob rejects patterns that could never be relative to the write
// root: absolute paths, "~" and ".." segments.
func ValidateWriteGlob(g string) error {
	s := strings.TrimPrefix(g, "!")
	switch {
	case strings.TrimSpace(s) == "":
		return fmt.Errorf("write glob %q is empty", g)
	case strings.HasPrefix(s, "/") || filepath.IsAbs(s) || strings.HasPrefix(s, "~"):
		return fmt.Errorf("write glob %q must be relative to the write root", g)
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == ".." {
			return fmt.Errorf("write glob %q must not contain ..", g)
		}
	}
	if _, err := path.Match(strings.ReplaceAll(s, "**", "*"), "x"); err != nil {
		return fmt.Errorf("write glob %q: %v", g, err)
	}
	return nil
}

// NormalizeGlob strips a leading "./" so Claude-style "./internal/**" and
// "internal/**" mean the same thing.
func NormalizeGlob(g string) string {
	neg := strings.HasPrefix(g, "!")
	s := strings.TrimPrefix(g, "!")
	for strings.HasPrefix(s, "./") {
		s = s[2:]
	}
	if s == "." || s == "" {
		s = "**"
	}
	if neg {
		return "!" + s
	}
	return s
}

// splitWriteGlobs separates positive and negative globs.
func splitWriteGlobs(globs []string) (pos, neg []string) {
	for _, g := range globs {
		if strings.HasPrefix(g, "!") {
			neg = append(neg, strings.ToLower(strings.TrimPrefix(g, "!")))
		} else {
			pos = append(pos, strings.ToLower(g))
		}
	}
	return pos, neg
}

// matchPathGlob matches rel against a "**"-aware glob, segment by segment.
func matchPathGlob(pattern, rel string) bool {
	return matchSegs(strings.Split(pattern, "/"), strings.Split(rel, "/"))
}

func matchSegs(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(segs); i++ {
			if matchSegs(pat[1:], segs[i:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	ok, err := path.Match(pat[0], segs[0])
	return err == nil && ok && matchSegs(pat[1:], segs[1:])
}

// dirCouldHoldMatch reports whether a directory at rel could contain a path the
// glob matches — that is what create_directory needs: "internal" is a valid
// directory for "internal/**", and any directory is for "**/*.go".
func dirCouldHoldMatch(pattern, rel string) bool {
	return prefixSegs(strings.Split(pattern, "/"), strings.Split(rel, "/"))
}

func prefixSegs(pat, segs []string) bool {
	if len(segs) == 0 {
		return len(pat) > 0
	}
	if len(pat) == 0 {
		return false
	}
	if pat[0] == "**" {
		return true
	}
	ok, err := path.Match(pat[0], segs[0])
	return err == nil && ok && prefixSegs(pat[1:], segs[1:])
}

// checkWriteGlobs applies WriteGlobs to an approved absolute write path.
func (p FS) checkWriteGlobs(tool, arg, abs string, isDir bool) error {
	if len(p.WriteGlobs) == 0 {
		return nil // whole write root (backwards compatible)
	}
	wr, err := filepath.EvalSymlinks(p.WriteRoot)
	if err != nil {
		wr = p.WriteRoot
	}
	r, err := relUnder(wr, abs)
	if err != nil {
		return p.deny(tool, arg, abs, newErr(CodeReadOnlyPath, "путь вне каталога записи."))
	}
	rel := strings.ToLower(filepath.ToSlash(r))
	pos, neg := splitWriteGlobs(p.WriteGlobs)
	for _, g := range neg {
		if matchGlob(g, rel) {
			return p.deny(tool, arg, rel, newErr(CodeReadOnlyPath,
				"путь %q закрыт для записи профилем роли (правило %q). Не меняй его — работай с другими файлами.", rel, "!"+g))
		}
	}
	for _, g := range pos {
		if matchPathGlob(g, rel) || (isDir && dirCouldHoldMatch(g, rel)) {
			return nil
		}
	}
	return p.deny(tool, arg, rel, newErr(CodeReadOnlyPath,
		"запись в %q не разрешена профилем роли. Писать можно только в пути: %s.", rel, strings.Join(pos, ", ")))
}
