package domain

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestLoopStatus_CanTransition(t *testing.T) {
	cases := []struct {
		name string
		from LoopStatus
		to   LoopStatus
		want bool
	}{
		{"planning→running", LoopPlanning, LoopRunning, true},
		{"planning→done запрещён", LoopPlanning, LoopDone, false},
		{"running→paused", LoopRunning, LoopPaused, true},
		{"running→done", LoopRunning, LoopDone, true},
		{"running→planning запрещён", LoopRunning, LoopPlanning, false},
		{"paused→running", LoopPaused, LoopRunning, true},
		{"paused→done запрещён", LoopPaused, LoopDone, false},
		{"paused→aborted", LoopPaused, LoopAborted, true},
		{"done→anything запрещён", LoopDone, LoopRunning, false},
		{"failed→anything запрещён", LoopFailed, LoopAborted, false},
		{"unknown→anything запрещён", LoopStatus("weird"), LoopRunning, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.from.CanTransition(tc.to); got != tc.want {
				t.Errorf("CanTransition(%q → %q) = %v, want %v", tc.from, tc.to, got, tc.want)
			}
		})
	}
}

func TestSubtask_Transition(t *testing.T) {
	now := time.Now()
	st := Subtask{ID: "a", Status: SubtaskPending}
	if err := st.Transition(SubtaskGenerating, now); err != nil {
		t.Fatalf("pending→generating: %v", err)
	}
	if err := st.Transition(SubtaskTesting, now); err != nil {
		t.Fatalf("generating→testing: %v", err)
	}
	if err := st.Transition(SubtaskDone, now); err == nil {
		t.Fatal("testing→done должен быть запрещён")
	}
	if st.Status != SubtaskTesting {
		t.Errorf("невалидный переход изменил статус: %q", st.Status)
	}
}

func TestSubtask_TransitionBlockedOnlyViaResume(t *testing.T) {
	now := time.Now()
	st := Subtask{ID: "a", Status: SubtaskBlocked}

	// Обычный Transition не выводит из blocked — только явный resume.
	if err := st.Transition(SubtaskPending, now); err == nil {
		t.Fatal("blocked→pending через Transition должен быть запрещён")
	}
	if st.Status != SubtaskBlocked {
		t.Errorf("статус изменился без resume: %q", st.Status)
	}

	if err := st.TransitionResume(now); err != nil {
		t.Fatalf("TransitionResume: %v", err)
	}
	if st.Status != SubtaskPending {
		t.Errorf("после resume статус %q, want %q", st.Status, SubtaskPending)
	}

	// Resume работает только из blocked.
	if err := st.TransitionResume(now); err == nil {
		t.Fatal("resume из pending должен быть запрещён")
	}
}

// validLoopRun returns the smallest run that passes Validate.
func validLoopRun() LoopRun {
	return LoopRun{
		SchemaVersion: LoopSchemaVersion,
		ID:            "run-1",
		Goal:          Goal{Text: "сделать CRM"},
		Status:        LoopRunning,
		Subtasks: []Subtask{{
			ID:         "a",
			Title:      "подзадача A",
			Spec:       "сделать A",
			Profile:    "coder",
			Acceptance: []string{"A работает"},
			Status:     SubtaskPending,
		}},
		CurrentIdx: 0,
		Budget:     Budget{MaxSubtasks: 10, MaxAttemptsPerSubtask: 4},
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

func TestLoopRun_Validate(t *testing.T) {
	big := strings.Repeat("x", loopMaxFieldLen+1)

	cases := []struct {
		name    string
		mutate  func(*LoopRun)
		wantErr string
	}{
		{"валидный минимум", nil, ""},
		{"версия схемы 2", func(r *LoopRun) { r.SchemaVersion = 2 }, "версия"},
		{"плохой id", func(r *LoopRun) { r.ID = "Bad ID!" }, "id"},
		{"неизвестный статус", func(r *LoopRun) { r.Status = LoopStatus("weird") }, "статус"},
		{"51 подзадача", func(r *LoopRun) {
			r.Subtasks = make([]Subtask, loopMaxSubtasks+1)
			for i := range r.Subtasks {
				r.Subtasks[i] = Subtask{ID: fmt.Sprintf("st-%d", i), Title: "t", Spec: "s", Acceptance: []string{"a"}}
			}
		}, "подзадач"},
		{"6 попыток", func(r *LoopRun) {
			for i := range loopMaxAttempts + 1 {
				r.Subtasks[0].Attempts = append(r.Subtasks[0].Attempts, Attempt{N: i + 1})
			}
		}, "попыток"},
		{"21 находка", func(r *LoopRun) {
			fs := make([]Finding, loopMaxFindings+1)
			r.Subtasks[0].Attempts = []Attempt{{N: 1, Findings: fs}}
		}, "находок"},
		{"поле длиннее 2KB", func(r *LoopRun) { r.Subtasks[0].Title = big }, "байт"},
		{"report длиннее 2KB", func(r *LoopRun) { r.Report = big }, "байт"},
		{"goal длиннее 2KB", func(r *LoopRun) { r.Goal.Text = big }, "байт"},
		{"дубль id подзадач", func(r *LoopRun) {
			r.Subtasks = append(r.Subtasks, r.Subtasks[0])
		}, "дубль"},
		{"пустой title", func(r *LoopRun) { r.Subtasks[0].Title = "" }, "title"},
		{"пустой spec", func(r *LoopRun) { r.Subtasks[0].Spec = "" }, "spec"},
		{"пустой acceptance", func(r *LoopRun) { r.Subtasks[0].Acceptance = nil }, "acceptance"},
		{"неизвестный статус подзадачи", func(r *LoopRun) { r.Subtasks[0].Status = SubtaskStatus("weird") }, "статус"},
		{"current_idx отрицательный", func(r *LoopRun) { r.CurrentIdx = -1 }, "current_idx"},
		{"current_idx за пределами", func(r *LoopRun) { r.CurrentIdx = len(r.Subtasks) + 1 }, "current_idx"},
		{"max_subtasks 0", func(r *LoopRun) { r.Budget.MaxSubtasks = 0 }, "max_subtasks"},
		{"max_subtasks 51", func(r *LoopRun) { r.Budget.MaxSubtasks = loopMaxSubtasks + 1 }, "max_subtasks"},
		{"max_attempts 6", func(r *LoopRun) { r.Budget.MaxAttemptsPerSubtask = loopMaxAttempts + 1 }, "max_attempts"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := validLoopRun()
			if tc.mutate != nil {
				tc.mutate(&run)
			}
			err := run.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() = %v, want ошибку содержащую %q", err, tc.wantErr)
			}
		})
	}
}
