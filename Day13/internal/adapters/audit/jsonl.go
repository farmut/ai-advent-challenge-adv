// Package audit writes the proxy audit log as JSON Lines and computes keyed
// fingerprints of secrets.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"agile-consultant/internal/ports"
)

// JSONL is an append-only audit log. The directory is 0700 and the file
// 0600; every record is a single Write of one line.
type JSONL struct {
	mu     sync.Mutex
	f      *os.File
	path   string
	closed bool
}

var _ ports.AuditLogger = (*JSONL)(nil)

// Open creates (or appends to) the audit file at path.
func Open(path string) (*JSONL, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("audit: create dir: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("audit: chmod dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("audit: open: %w", err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("audit: chmod file: %w", err)
	}
	return &JSONL{f: f, path: path}, nil
}

// Path returns the audit file path.
func (j *JSONL) Path() string { return j.path }

// Ready reports whether the log is open.
func (j *JSONL) Ready() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return !j.closed
}

// ErrClosed is returned by Write after Close.
var ErrClosed = errors.New("audit: log closed")

// Write appends r as one JSON line.
func (j *JSONL) Write(_ context.Context, r ports.AuditRecord) error {
	b, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("audit: marshal: %w", err)
	}
	b = append(b, '\n')
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return ErrClosed
	}
	if _, err := j.f.Write(b); err != nil {
		return fmt.Errorf("audit: write: %w", err)
	}
	return nil
}

// Close syncs and closes the file. It is idempotent.
func (j *JSONL) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return nil
	}
	j.closed = true
	return errors.Join(j.f.Sync(), j.f.Close())
}
