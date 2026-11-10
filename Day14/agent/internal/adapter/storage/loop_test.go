package storage_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-adv-agent/internal/adapter/storage"
	"ai-adv-agent/internal/domain"
)

func TestLoopPath_Standard(t *testing.T) {
	got := storage.LoopPath("chat_history.json")
	want := "chat_history.loop.json"
	if got != want {
		t.Errorf("LoopPath(chat_history.json) = %q, want %q", got, want)
	}
	if p := storage.LoopPath(""); p != "" {
		t.Errorf("LoopPath(\"\") should be empty, got %q", p)
	}
}

// newLoopRepo returns a LoopFile rooted in a fresh temp dir.
func newLoopRepo(t *testing.T) (*storage.LoopFile, string) {
	t.Helper()
	dir := t.TempDir()
	hist := filepath.Join(dir, "chat_history.json")
	return storage.NewLoopFile(hist), hist
}

// validRun returns the smallest run that passes domain validation.
func validRun() domain.LoopRun {
	return domain.LoopRun{
		SchemaVersion: domain.LoopSchemaVersion,
		ID:            "run-1",
		Goal:          domain.Goal{Text: "сделать CRM"},
		Status:        domain.LoopRunning,
		Subtasks: []domain.Subtask{{
			ID:         "a",
			Title:      "подзадача A",
			Spec:       "сделать A",
			Profile:    "coder",
			Acceptance: []string{"A работает"},
			Status:     domain.SubtaskPending,
		}},
		CurrentIdx: 0,
		Budget:     domain.Budget{MaxSubtasks: 10, MaxAttemptsPerSubtask: 4},
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

func TestLoopFile_SaveLoadRoundtrip(t *testing.T) {
	repo, hist := newLoopRepo(t)
	run := validRun()

	if _, ok, err := repo.LoadActive(); err != nil || ok {
		t.Fatalf("до Save: ok=%v err=%v, want false/nil", ok, err)
	}
	if err := repo.Save(run); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(storage.LoopPath(hist))
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("права файла = %o, want 0600", info.Mode().Perm())
	}

	got, ok, err := repo.LoadActive()
	if err != nil || !ok {
		t.Fatalf("LoadActive: ok=%v err=%v, want true/nil", ok, err)
	}
	if got.ID != run.ID || got.Goal.Text != run.Goal.Text || len(got.Subtasks) != 1 {
		t.Errorf("LoadActive вернул %+v, want %+v", got, run)
	}
}

func TestLoopFile_LoadActiveInvalid(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{"битый JSON", "{not json"},
		{"JSON не проходит валидацию", `{"schema_version":1,"id":"run-1","status":"weird","subtasks":[],"current_idx":0,"budget":{"max_subtasks":10,"max_attempts_per_subtask":4}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, hist := newLoopRepo(t)
			if err := os.WriteFile(storage.LoopPath(hist), []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			_, ok, err := repo.LoadActive()
			if err == nil {
				t.Fatalf("LoadActive: ok=%v err=nil, want ошибку", ok)
			}
		})
	}
}

func TestLoopFile_SaveRejectsOversize(t *testing.T) {
	repo, _ := newLoopRepo(t)
	run := validRun()
	// Валидация ограничивает длину каждой строки, но не число пунктов:
	// 12 000 коротких acceptance дают файл больше 1 МБ.
	acc := make([]string, 12000)
	for i := range acc {
		acc[i] = fmt.Sprintf("критерий-%04d: подзадача работает корректно и покрыта тестами", i)
	}
	run.Subtasks[0].Acceptance = acc

	err := repo.Save(run)
	if err == nil || !strings.Contains(err.Error(), "1 МБ") && !strings.Contains(err.Error(), "байт") {
		t.Fatalf("Save() = %v, want ошибку о размере файла", err)
	}
}

func TestLoopFile_SecondLockFails(t *testing.T) {
	repo, _ := newLoopRepo(t)
	unlock, err := repo.Lock()
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	defer unlock()

	if _, err := repo.Lock(); err == nil {
		t.Fatal("второй Lock должен давать ошибку")
	}
}

// deadPID запускает и дожидается завершения процесса, возвращая его PID —
// гарантированно мёртвый на macOS и Linux (после Wait зомби освобождён).
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("запуск true: %v", err)
	}
	pid := cmd.Process.Pid
	if pid <= 0 {
		t.Fatalf("некорректный pid %d", pid)
	}
	return pid
}

func TestLoopFile_StaleLockTaken(t *testing.T) {
	repo, hist := newLoopRepo(t)

	// Протухший lock: владелец уже завершился.
	lockPath := strings.TrimSuffix(storage.LoopPath(hist), ".json") + ".lock"
	content := fmt.Sprintf("%d %d\n", deadPID(t), time.Now().Add(-time.Hour).Unix())
	if err := os.WriteFile(lockPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	unlock, err := repo.Lock()
	if err != nil {
		t.Fatalf("Lock с протухшим lock-файлом: %v", err)
	}
	defer unlock()

	// Захваченный lock снова блокирует второго.
	if _, err := repo.Lock(); err == nil {
		t.Fatal("второй Lock после перехвата должен давать ошибку")
	}
}

func TestLoopFile_LiveForeignLockNotStolen(t *testing.T) {
	repo, hist := newLoopRepo(t)
	// Живой PID (наш собственный) — lock не перехватывается.
	lockPath := strings.TrimSuffix(storage.LoopPath(hist), ".json") + ".lock"
	content := fmt.Sprintf("%d %d\n", os.Getpid(), time.Now().Unix())
	if err := os.WriteFile(lockPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Lock(); err == nil {
		t.Fatal("Lock живого процесса перехвачен — недопустимо")
	}
}

func TestLoopFile_SaveRedactsSecrets(t *testing.T) {
	t.Setenv("LLM_API_KEY", "literal-secret-key-0123456789")
	repo, hist := newLoopRepo(t)

	run := validRun()
	run.Subtasks[0].Attempts = []domain.Attempt{{
		N:        1,
		Stage:    domain.SubtaskReviewing,
		Feedback: []string{"ключ sk-abcdefghijklmnopQRSTUV не валиден", "токен: Bearer abcdefghij"},
		Error:    "aws AKIAIOSFODNN7EXAMPLE отклонён и literal-secret-key-0123456789 совпал",
		Findings: []domain.Finding{{
			Issue: "утёк ghp_abcdefghij0123456789abcdef в логе",
			Fix:   "используй -----BEGIN PRIVATE KEY-----\nMIIB\n-----END PRIVATE KEY----- аккуратно",
		}},
		StartedAt: time.Now(),
	}}

	if err := repo.Save(run); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(storage.LoopPath(hist))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{
		"sk-abcdefghijklmnopQRSTUV",
		"AKIAIOSFODNN7EXAMPLE",
		"Bearer abcdefghij",
		"ghp_abcdefghij0123456789abcdef",
		"BEGIN PRIVATE KEY",
		"literal-secret-key-0123456789",
	} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("файл состояния содержит секрет %q", secret)
		}
	}

	// После загрузки секретов тоже нет: redact идёт до записи.
	got, ok, err := repo.LoadActive()
	if err != nil || !ok {
		t.Fatalf("LoadActive: ok=%v err=%v", ok, err)
	}
	if strings.Contains(got.Subtasks[0].Attempts[0].Error, "AKIA") {
		t.Error("загруженный run содержит незамаскированный секрет")
	}
}

func TestLoopFile_Archive(t *testing.T) {
	repo, hist := newLoopRepo(t)
	if err := repo.Save(validRun()); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := repo.Archive("run-1"); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if _, err := os.Stat(storage.LoopPath(hist)); !os.IsNotExist(err) {
		t.Error("активный файл должен исчезнуть после архивации")
	}
	archive := filepath.Join(filepath.Dir(hist), "chat_history.loop.run-1.json")
	if _, err := os.Stat(archive); err != nil {
		t.Errorf("архивный файл не создан: %v", err)
	}
	if _, ok, err := repo.LoadActive(); err != nil || ok {
		t.Errorf("после архивации LoadActive: ok=%v err=%v, want false/nil", ok, err)
	}

	// Архивация без активного run — no-op, невалидный id — ошибка.
	if err := repo.Archive("run-1"); err != nil {
		t.Errorf("повторная Archive: %v", err)
	}
	if err := repo.Archive("../escape"); err == nil {
		t.Error("Archive с id вне паттерна должна давать ошибку")
	}
}
