// Package workspace implements port.Workspace on the local filesystem:
//
//	<root>/<runID>/base       accepted state of the run
//	<root>/<runID>/<taskID>   working copy of one subtask
//
// A subtask starts from a copy of base and replaces base only when it is
// done, so a failed subtask never damages the accepted state. Files are
// written 0644 and directories 0755: the sandbox runs as uid 65532 and must
// be able to read the bind-mounted tree.
package workspace

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Config bounds what the workspace handles. The limits match the sandbox's
// copy limits (sandbox.copy_max_files / copy_max_bytes): a tree the sandbox
// would refuse is refused here first.
type Config struct {
	// Root is the absolute workspace root (sandbox.workspace_root).
	Root string
	// Templates maps a stack to a template directory seeded into base.
	Templates map[string]string
	MaxFiles  int
	MaxBytes  int64
}

// FS is the filesystem workspace.
type FS struct {
	cfg  Config
	root string // symlink-resolved
}

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// baseDir is the reserved name of the accepted state.
const baseDir = "base"

// New validates the config and creates the root.
func New(cfg Config) (*FS, error) {
	if cfg.Root == "" || !filepath.IsAbs(cfg.Root) {
		return nil, fmt.Errorf("workspace: root должен быть абсолютным путём, получено %q", cfg.Root)
	}
	if cfg.MaxFiles <= 0 || cfg.MaxBytes <= 0 {
		return nil, fmt.Errorf("workspace: лимиты max_files/max_bytes должны быть > 0")
	}
	if err := os.MkdirAll(cfg.Root, 0o755); err != nil {
		return nil, fmt.Errorf("workspace: %w", err)
	}
	real, err := filepath.EvalSymlinks(cfg.Root)
	if err != nil {
		return nil, fmt.Errorf("workspace: %w", err)
	}
	return &FS{cfg: cfg, root: real}, nil
}

func checkID(kind, id string) error {
	if !idRe.MatchString(id) || id == baseDir || strings.Contains(id, "..") {
		return fmt.Errorf("workspace: недопустимый %s %q", kind, id)
	}
	return nil
}

func (w *FS) runDir(runID string) string { return filepath.Join(w.root, runID) }

// Seed fills base from the stacks' templates. It is a no-op when base
// already exists (a resumed run keeps its accepted state).
func (w *FS) Seed(runID string, stacks []string) error {
	if err := checkID("runID", runID); err != nil {
		return err
	}
	base := filepath.Join(w.runDir(runID), baseDir)
	if _, err := os.Stat(base); err == nil {
		return nil
	}
	if err := os.MkdirAll(w.runDir(runID), 0o755); err != nil {
		return fmt.Errorf("workspace: %w", err)
	}
	// Build base aside and rename it in, so a failed seed leaves no base.
	tmp, err := os.MkdirTemp(w.runDir(runID), ".seed-")
	if err != nil {
		return fmt.Errorf("workspace: %w", err)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("workspace: %w", err)
	}
	for _, st := range stacks {
		src, ok := w.cfg.Templates[st]
		if !ok || src == "" {
			_ = os.RemoveAll(tmp)
			return fmt.Errorf("workspace: нет шаблона для стека %q", st)
		}
		if err := w.copyTree(src, tmp); err != nil {
			_ = os.RemoveAll(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, base); err != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("workspace: %w", err)
	}
	return nil
}

// TaskDir resolves the task directory without touching it.
func (w *FS) TaskDir(runID, taskID string) (string, error) {
	if err := checkID("runID", runID); err != nil {
		return "", err
	}
	if err := checkID("taskID", taskID); err != nil {
		return "", err
	}
	dir := filepath.Join(w.runDir(runID), taskID)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("workspace: каталог подзадачи %q не найден", taskID)
	}
	return dir, nil
}

// Prepare replaces the task directory with a copy of base.
func (w *FS) Prepare(runID, taskID string) (string, error) {
	if err := checkID("runID", runID); err != nil {
		return "", err
	}
	if err := checkID("taskID", taskID); err != nil {
		return "", err
	}
	base := filepath.Join(w.runDir(runID), baseDir)
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", fmt.Errorf("workspace: %w", err)
	}
	dst := filepath.Join(w.runDir(runID), taskID)
	if err := os.RemoveAll(dst); err != nil {
		return "", fmt.Errorf("workspace: %w", err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return "", fmt.Errorf("workspace: %w", err)
	}
	if err := w.copyTree(base, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// Promote swaps the task directory in as the new base. The old base is kept
// until the swap succeeded and restored if it did not.
func (w *FS) Promote(runID, taskID string) error {
	if err := checkID("runID", runID); err != nil {
		return err
	}
	if err := checkID("taskID", taskID); err != nil {
		return err
	}
	rd := w.runDir(runID)
	task := filepath.Join(rd, taskID)
	if st, err := os.Lstat(task); err != nil || !st.IsDir() {
		return fmt.Errorf("workspace: каталог подзадачи %q не найден", taskID)
	}
	base := filepath.Join(rd, baseDir)
	old := filepath.Join(rd, ".base-old-"+taskID)
	_ = os.RemoveAll(old)
	hadBase := false
	if _, err := os.Lstat(base); err == nil {
		if err := os.Rename(base, old); err != nil {
			return fmt.Errorf("workspace: %w", err)
		}
		hadBase = true
	}
	if err := os.Rename(task, base); err != nil {
		if hadBase {
			_ = os.Rename(old, base)
		}
		return fmt.Errorf("workspace: %w", err)
	}
	if hadBase {
		_ = os.RemoveAll(old)
	}
	return nil
}

// inside resolves dir and requires it to lie under the workspace root.
func (w *FS) inside(dir string) (string, error) {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("workspace: %w", err)
	}
	rel, err := filepath.Rel(w.root, real)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return "", fmt.Errorf("workspace: каталог %q вне workspace root", dir)
	}
	return real, nil
}

// Snapshot hashes every regular file under dir. Symlinks are skipped (the
// sandbox copy skips them too); exceeding the limits is an error.
func (w *FS) Snapshot(dir string) (map[string]string, error) {
	real, err := w.inside(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	var total int64
	err = filepath.WalkDir(real, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 || (!d.IsDir() && !d.Type().IsRegular()) {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if len(out) >= w.cfg.MaxFiles {
			return fmt.Errorf("больше %d файлов", w.cfg.MaxFiles)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		if total > w.cfg.MaxBytes {
			return fmt.Errorf("больше %d байт", w.cfg.MaxBytes)
		}
		sum, err := hashFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(real, p)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = sum
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("workspace: снапшот: %w", err)
	}
	return out, nil
}

func hashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ErrNotText marks a file that cannot be shown as text.
var ErrNotText = errors.New("workspace: файл не текстовый")

// ReadNumbered renders rel with line numbers, showing at most maxBytes of
// source text (maxBytes <= 0 shows nothing but still counts lines).
func (w *FS) ReadNumbered(dir, rel string, maxBytes int) (string, int, error) {
	real, err := w.inside(dir)
	if err != nil {
		return "", 0, err
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if !filepath.IsLocal(clean) {
		return "", 0, fmt.Errorf("workspace: путь %q вне каталога задачи", rel)
	}
	full := filepath.Join(real, clean)
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", 0, fmt.Errorf("workspace: %w", err)
	}
	if resolved != full {
		return "", 0, fmt.Errorf("workspace: %q — символическая ссылка", rel)
	}
	st, err := os.Lstat(full)
	if err != nil {
		return "", 0, fmt.Errorf("workspace: %w", err)
	}
	if !st.Mode().IsRegular() {
		return "", 0, fmt.Errorf("workspace: %q — не обычный файл", rel)
	}
	if st.Size() > w.cfg.MaxBytes {
		return "", 0, fmt.Errorf("workspace: %q больше %d байт", rel, w.cfg.MaxBytes)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", 0, fmt.Errorf("workspace: %w", err)
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return "", 0, fmt.Errorf("%w: %s", ErrNotText, rel)
	}
	lines := strings.SplitAfter(string(data), "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	var b strings.Builder
	shown := 0
	for i, l := range lines {
		if shown+len(l) > maxBytes {
			b.WriteString("[…усечено: показано " + strconv.Itoa(i) + " из " + strconv.Itoa(len(lines)) + " строк…]\n")
			break
		}
		shown += len(l)
		fmt.Fprintf(&b, "%5d| %s", i+1, strings.TrimSuffix(l, "\n"))
		b.WriteByte('\n')
	}
	return b.String(), len(lines), nil
}

// copyTree copies regular files and directories from src into dst (which
// exists), skipping symlinks, within the configured limits.
func (w *FS) copyTree(src, dst string) error {
	files := 0
	var total int64
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
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
			return os.MkdirAll(target, 0o755)
		case !d.Type().IsRegular():
			return nil
		}
		files++
		if files > w.cfg.MaxFiles {
			return fmt.Errorf("больше %d файлов", w.cfg.MaxFiles)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		if total > w.cfg.MaxBytes {
			return fmt.Errorf("больше %d байт", w.cfg.MaxBytes)
		}
		return copyFile(p, target)
	})
	if err != nil {
		return fmt.Errorf("workspace: копирование %s: %w", src, err)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, 0o644)
}
