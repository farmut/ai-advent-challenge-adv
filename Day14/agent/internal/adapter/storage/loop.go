package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
	"ai-adv-agent/internal/usecase"
)

// maxLoopFileBytes caps the active run state file: the state file is a
// write-ahead log of summaries and findings, and a file past 1 MB means
// something other than summaries got in.
const maxLoopFileBytes = 1 << 20

// ---- Path helpers ----

// LoopPath derives the active loop run state file from the history file path.
func LoopPath(historyPath string) string { return derived(historyPath, ".loop", ".json") }

// loopArchivePath derives the archived run file for the given run id.
func loopArchivePath(historyPath, id string) string {
	return derived(historyPath, ".loop."+id, ".json")
}

// loopLockPath derives the inter-process loop lock file.
func loopLockPath(historyPath string) string { return derived(historyPath, ".loop", ".lock") }

// ---- LoopFile ----

// LoopFile is a file-backed LoopRepository. The active run lives at
// LoopPath(historyPath); finished runs are moved next to it as
// ".loop.<id>.json"; concurrency between loop processes is serialised
// through the ".loop.lock" file.
var _ port.LoopRepository = (*LoopFile)(nil)

type LoopFile struct {
	hist string // history file path all loop files derive from
}

// NewLoopFile creates a LoopFile whose files are derived from historyPath.
// Empty historyPath is a no-op store.
func NewLoopFile(historyPath string) *LoopFile {
	return &LoopFile{hist: historyPath}
}

// path of the active run state file ("" = no-op store).
func (r *LoopFile) path() string {
	if r.hist == "" {
		return ""
	}
	return LoopPath(r.hist)
}

// LoadActive loads and validates the active run state file. A missing file
// means no run is in progress; a present but corrupt or invalid file is an
// error — resuming from silently reset state would corrupt the goal.
func (r *LoopFile) LoadActive() (domain.LoopRun, bool, error) {
	if r.path() == "" {
		return domain.LoopRun{}, false, nil
	}
	data, err := os.ReadFile(r.path())
	if os.IsNotExist(err) {
		return domain.LoopRun{}, false, nil
	}
	if err != nil {
		return domain.LoopRun{}, false, fmt.Errorf("loop: не удалось прочитать %q: %w", r.path(), err)
	}
	var run domain.LoopRun
	if err := json.Unmarshal(data, &run); err != nil {
		return domain.LoopRun{}, false, fmt.Errorf("loop: невалидный файл состояния %q: %w", r.path(), err)
	}
	if err := run.Validate(); err != nil {
		return domain.LoopRun{}, false, fmt.Errorf("loop: файл состояния %q не прошёл валидацию: %w", r.path(), err)
	}
	return run, true, nil
}

// Save validates and persists the run atomically (0600). Secrets are scrubbed
// from every attempt string before marshalling; the redacted copy is what
// gets written. A marshalled state larger than 1 MB is rejected.
func (r *LoopFile) Save(run domain.LoopRun) error {
	if r.path() == "" {
		return nil
	}
	if err := run.Validate(); err != nil {
		return fmt.Errorf("loop: невалидное состояние: %w", err)
	}
	usecase.Apply(&run)
	data, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		return fmt.Errorf("loop: marshal: %w", err)
	}
	if len(data) > maxLoopFileBytes {
		return fmt.Errorf("loop: состояние больше %d байт (%d) — в файл попало больше сводок", maxLoopFileBytes, len(data))
	}
	return writeFileAtomic(r.path(), data)
}

// Archive moves the active run state to the archive slot of the given run id.
// Archiving with no active run is a no-op. The id is validated against the
// domain id pattern so it cannot escape into arbitrary file names.
func (r *LoopFile) Archive(id string) error {
	if r.path() == "" {
		return nil
	}
	if !domain.LoopIDPattern.MatchString(id) {
		return fmt.Errorf("loop: невалидный id для архива %q", id)
	}
	if _, err := os.Stat(r.path()); os.IsNotExist(err) {
		return nil
	}
	dst := loopArchivePath(r.hist, id)
	if err := os.Rename(r.path(), dst); err != nil {
		return fmt.Errorf("loop: архивация в %q: %w", dst, err)
	}
	return nil
}

// Lock acquires the inter-process loop lock and returns its unlock function.
// The lock file holds the owner's PID and creation time. If the lock is held
// by a process that is no longer alive (crashed without unlocking), a warning
// is printed to stderr and the stale lock is taken over; PID reuse in the
// window between crash and relock is accepted as a rare false positive.
func (r *LoopFile) Lock() (func(), error) {
	if r.path() == "" {
		return func() {}, nil
	}
	lockPath := loopLockPath(r.hist)
	f, err := openLockFile(lockPath)
	if err != nil {
		if !os.IsExist(err) {
			return nil, fmt.Errorf("loop: lock %q: %w", lockPath, err)
		}
		if !stealStaleLock(lockPath) {
			return nil, fmt.Errorf("loop: уже запущен другой процесс (lock %q)", lockPath)
		}
		f, err = openLockFile(lockPath)
		if err != nil {
			return nil, fmt.Errorf("loop: lock %q после перехвата: %w", lockPath, err)
		}
	}
	if _, err := fmt.Fprintf(f, "%d %d\n", os.Getpid(), time.Now().Unix()); err != nil {
		_ = f.Close()
		_ = os.Remove(lockPath)
		return nil, fmt.Errorf("loop: запись lock %q: %w", lockPath, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(lockPath)
		return nil, fmt.Errorf("loop: закрытие lock %q: %w", lockPath, err)
	}
	return func() { _ = os.Remove(lockPath) }, nil
}

func openLockFile(lockPath string) (*os.File, error) {
	return os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, filePerm)
}

// stealStaleLock reports whether the lock file at lockPath belongs to a dead
// process and removes it in that case.
func stealStaleLock(lockPath string) bool {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return false
	}
	var pid int
	if _, err := fmt.Sscanf(string(data), "%d", &pid); err != nil || pid <= 0 {
		return false
	}
	if err := syscall.Kill(pid, 0); err == nil || err == syscall.EPERM {
		return false // жив или чужой (не наша сессия) — не трогаем
	}
	fmt.Fprintf(os.Stderr, "loop: предупреждение: протухший lock (pid %d не жив), перехватываем %q\n", pid, filepath.Base(lockPath))
	_ = os.Remove(lockPath)
	return true
}
