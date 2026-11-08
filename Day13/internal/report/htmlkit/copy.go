// Package htmlkit holds helpers shared by the HTML report generators
// (Day12 prompt-injection e2e and the Day13 guard-proxy e2e): headless
// Chrome screenshots, revealing invisible characters and file copying.
package htmlkit

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// CopyFile copies src to dst (created or truncated, mode 0644).
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// CopyDir copies every file of src into dst file by file, overwriting files
// with the same relative path. Other files already in dst are kept: dst is
// never removed, so several generators can share one directory
// (reports/summary).
func CopyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if err := CopyFile(path, target); err != nil {
			return fmt.Errorf("copy %s: %w", rel, err)
		}
		return nil
	})
}

// CopyFiles copies the named files to dst, creating dst if needed. names
// maps a source path to the target file name inside dst.
func CopyFiles(dst string, names map[string]string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for src, name := range names {
		if err := CopyFile(src, filepath.Join(dst, name)); err != nil {
			return fmt.Errorf("copy %s: %w", filepath.Base(src), err)
		}
	}
	return nil
}
