package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"unicode"
)

// errLimit marks a copy that exceeded the file or byte limit.
var errLimit = errors.New("превышен лимит копирования")

// skipDirs are never copied: dependencies come from the image, VCS data is
// not needed to build or test.
var skipDirs = map[string]bool{"node_modules": true, ".git": true}

// copyTree copies regular files and directories from src into dst (created
// 0755). Symlinks and special files are skipped, so nothing in the copy can
// point outside it. More than maxFiles files or maxBytes bytes in total is an
// error wrapping errLimit.
func copyTree(src, dst string, maxFiles int, maxBytes int64) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	files := 0
	var total int64
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			return nil
		case d.IsDir():
			if skipDirs[d.Name()] {
				return fs.SkipDir
			}
			return os.Mkdir(target, 0o755)
		case !d.Type().IsRegular():
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files++
		total += info.Size()
		if files > maxFiles {
			return fmt.Errorf("%w: больше %d файлов в каталоге задачи", errLimit, maxFiles)
		}
		if total > maxBytes {
			return fmt.Errorf("%w: больше %d байт в каталоге задачи", errLimit, maxBytes)
		}
		mode := os.FileMode(0o644)
		if info.Mode()&0o111 != 0 {
			mode = 0o755
		}
		return copyFile(p, target, mode, info.Size())
	})
}

func copyFile(src, dst string, mode os.FileMode, size int64) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	// LimitReader: a file growing while we copy cannot exceed what was counted.
	if _, err := io.Copy(out, io.LimitReader(in, size)); err != nil {
		_ = out.Close()
		return fmt.Errorf("copy %s: %w", src, err)
	}
	return out.Close()
}

// checkGoDeps verifies that every requirement in dir/go.mod is present in the
// read-only module cache. No go.mod means nothing to check.
func checkGoDeps(dir, modCache string) error {
	f, err := os.Open(filepath.Join(dir, "go.mod"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("go.mod: %w", err)
	}
	defer f.Close()
	reqs, err := parseRequires(f)
	if err != nil {
		return fmt.Errorf("go.mod: %w", err)
	}
	var missing []string
	for _, r := range reqs {
		escPath, err1 := escapeModule(r[0])
		escVer, err2 := escapeModule(r[1])
		if err1 != nil || err2 != nil {
			missing = append(missing, r[0]+"@"+r[1])
			continue
		}
		mod := filepath.Join(modCache, "cache", "download", filepath.FromSlash(escPath), "@v", escVer+".mod")
		if _, err := os.Stat(mod); err != nil {
			missing = append(missing, r[0]+"@"+r[1])
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("зависимости не из шаблона (нет в образе, сети нет): %s. "+
			"Используй только модули из шаблона; новая зависимость требует пересборки образа", strings.Join(missing, ", "))
	}
	return nil
}

// parseRequires extracts (path, version) pairs from require directives, both
// single-line and block form. It is deliberately small: anything it does not
// understand is left to the go command, which fails offline on its own.
func parseRequires(r io.Reader) ([][2]string, error) {
	var out [][2]string
	sc := bufio.NewScanner(r)
	inBlock := false
	for sc.Scan() {
		line := sc.Text()
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if inBlock {
			if fields[0] == ")" {
				inBlock = false
				continue
			}
			if len(fields) >= 2 {
				out = append(out, [2]string{unquote(fields[0]), unquote(fields[1])})
			}
			continue
		}
		if fields[0] != "require" {
			continue
		}
		if len(fields) == 2 && fields[1] == "(" {
			inBlock = true
			continue
		}
		if len(fields) >= 3 {
			out = append(out, [2]string{unquote(fields[1]), unquote(fields[2])})
		}
	}
	return out, sc.Err()
}

func unquote(s string) string { return strings.Trim(s, "\"`") }

// escapeModule applies the module cache case-encoding: every upper-case
// letter becomes '!' plus its lower-case form. Paths with '!' or '..' are
// refused so a crafted go.mod cannot probe outside the cache.
func escapeModule(s string) (string, error) {
	if s == "" || strings.Contains(s, "!") || strings.Contains(s, "..") || strings.HasPrefix(s, "/") {
		return "", fmt.Errorf("bad module path or version %q", s)
	}
	var b strings.Builder
	for _, r := range s {
		if unicode.IsUpper(r) {
			b.WriteByte('!')
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		b.WriteRune(r)
	}
	return b.String(), nil
}

type pkgDeps struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
}

// checkVueDeps verifies that dir/package.json declares exactly the template's
// dependencies. No package.json means plain sources: allowed.
func checkVueDeps(dir, templatePath string) error {
	got, err := readDeps(filepath.Join(dir, "package.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("package.json: %w", err)
	}
	want, err := readDeps(templatePath)
	if err != nil {
		return fmt.Errorf("template package.json: %w", err)
	}
	if !equalDeps(got.Dependencies, want.Dependencies) || !equalDeps(got.DevDependencies, want.DevDependencies) {
		return fmt.Errorf("зависимости не из шаблона: package.json отличается от шаблона (%s). "+
			"Используй только пакеты из шаблона; новая зависимость требует пересборки образа", diffDeps(got, want))
	}
	return nil
}

func readDeps(path string) (pkgDeps, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return pkgDeps{}, err
	}
	var d pkgDeps
	if err := json.Unmarshal(data, &d); err != nil {
		return pkgDeps{}, err
	}
	return d, nil
}

func equalDeps(a, b map[string]string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func diffDeps(got, want pkgDeps) string {
	var diff []string
	collect := func(g, w map[string]string) {
		for k, v := range g {
			if w[k] != v {
				diff = append(diff, k+"@"+v)
			}
		}
		for k := range w {
			if _, ok := g[k]; !ok {
				diff = append(diff, "-"+k)
			}
		}
	}
	collect(got.Dependencies, want.Dependencies)
	collect(got.DevDependencies, want.DevDependencies)
	sort.Strings(diff)
	if len(diff) > 10 {
		diff = append(diff[:10], "…")
	}
	return strings.Join(diff, ", ")
}

// linkNodeModules creates dst as a real, writable directory holding one
// symlink per entry of the read-only src tree (packages, @scopes, .bin).
// Node resolves the symlinks to their real paths under src, so packages
// still find each other, while tools may create their own temp entries
// (vite: node_modules/.vite-temp) next to them.
func linkNodeModules(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	if err := os.Mkdir(dst, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	for _, e := range entries {
		if err := os.Symlink(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return fmt.Errorf("link %s: %w", e.Name(), err)
		}
	}
	return nil
}

// seedGoCache makes the read-only prebuilt build cache usable as a writable
// GOCACHE without copying data: directories are created in dst, every cache
// file becomes a symlink into seed. The go command reads entries through the
// links and writes new entries (the task's own packages) next to them.
// trim.txt is not linked: go rewrites it. A missing seed or an empty GOCACHE
// (go then uses its default) is not an error; an existing dst is left as is.
func seedGoCache(seed, dst string) error {
	if dst == "" || !filepath.IsAbs(dst) {
		return nil
	}
	if fi, err := os.Stat(seed); err != nil || !fi.IsDir() {
		return nil
	}
	if _, err := os.Lstat(dst); err == nil {
		return nil
	}
	return filepath.WalkDir(seed, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("gocache seed: %w", err)
		}
		rel, err := filepath.Rel(seed, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o755)
		case !d.Type().IsRegular() || rel == "trim.txt":
			return nil
		}
		return os.Symlink(p, target)
	})
}
