package storage

import (
	"fmt"
	"os"
	"path/filepath"
)

// filePerm is the mode every state file is written with: chat history, memory
// layers and MCP configs may hold personal data or tokens, so only the owner
// may read them.
const filePerm os.FileMode = 0o600

// writeFileAtomic replaces path with data so that a reader never sees a
// half-written file and a crash never leaves a truncated one.
//
// The data goes to a temp file in the same directory (rename is only atomic
// within one filesystem), which is chmod'ed to 0600 before anything is written,
// fsync'ed, closed and then renamed over path. The rename also replaces the
// mode of a pre-existing file, so a file created earlier with 0644 is tightened
// to 0600 on the next save. On any error the temp file is removed and path is
// left untouched.
func writeFileAtomic(path string, data []byte) (err error) {
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}
	tmp, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %q: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if err = tmp.Chmod(filePerm); err != nil {
		return fmt.Errorf("chmod temp file for %q: %w", path, err)
	}
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("write temp file for %q: %w", path, err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("sync temp file for %q: %w", path, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("close temp file for %q: %w", path, err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %q: %w", path, err)
	}
	return nil
}
