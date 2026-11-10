package execloop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
	"ai-adv-agent/internal/usecase/devpipeline"
)

// Events receives run-level updates while a loop runs. Stage and Findings
// are the devpipeline events (the Loop itself is the pipeline's Events
// implementation and forwards them); Budget reports the meter's usage every
// configured number of LLM calls.
type Events interface {
	devpipeline.Events
	Budget(usage domain.BudgetUsage)
}

// nopEvents sinks events when the caller passes none.
type nopEvents struct{}

func (nopEvents) Stage(string, domain.SubtaskStatus, string) {}
func (nopEvents) Findings(string, []domain.Finding)          {}
func (nopEvents) Budget(domain.BudgetUsage)                  {}

// Loop drives one execution run: plan → run every subtask through the
// pipeline → verify acceptance → promote, replan the pending subtasks when
// one fails, and finish with a markdown report. The run state is persisted
// write-ahead through port.LoopRepository on every stage change, so a crash
// or a /loop stop resumes from the last saved state.
type Loop struct {
	planner  *Planner
	pipe     *devpipeline.Pipeline
	verifier *Verifier
	repo     port.LoopRepository
	ws       port.Workspace
	ev       Events
	cfg      Config
	clock    port.Clock
	meter    *MeteredLLM
	logf     func(string, ...any)

	// run is the run currently being driven: the pipeline reports its stage
	// events through the Loop (which is its Events implementation), and the
	// write-ahead save closure reads curTaskID/attemptBase to attribute
	// attempts to the right subtask with run-wide numbering.
	run         *domain.LoopRun
	curTaskID   string
	attemptBase int
}

// New assembles a Loop and the devpipeline it drives. The pipeline's save
// closure is the Loop itself: every attempt the pipeline persists lands in
// the run state file write-ahead. clk nil means the wall clock; logf nil
// means silent (the ask-user suppression lines go here).
func New(roles port.RoleRunner, runner port.CommandRunner, sast port.SecurityScanner, meter *MeteredLLM, repo port.LoopRepository, ws port.Workspace, ev Events, cfg Config, pipeCfg devpipeline.Config, clk port.Clock, logf func(string, ...any)) (*Loop, error) {
	if roles == nil {
		return nil, fmt.Errorf("execloop: RoleRunner не задан")
	}
	if runner == nil {
		return nil, fmt.Errorf("execloop: CommandRunner не задан")
	}
	if sast == nil {
		return nil, fmt.Errorf("execloop: SecurityScanner не задан")
	}
	if repo == nil {
		return nil, fmt.Errorf("execloop: LoopRepository не задан")
	}
	if ws == nil {
		return nil, fmt.Errorf("execloop: Workspace не задан")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if clk == nil {
		clk = systemClock{}
	}
	if ev == nil {
		ev = nopEvents{}
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	l := &Loop{
		repo:  repo,
		ws:    ws,
		ev:    ev,
		cfg:   cfg,
		clock: clk,
		meter: meter,
		logf:  logf,
	}
	l.planner = NewPlanner(roles, meter, cfg, logf)
	l.verifier = NewVerifier(roles, meter, ws, cfg, logf)
	pipe, err := devpipeline.New(roles, runner, sast, ws, l.saveAttempt, l, pipeCfg)
	if err != nil {
		return nil, err
	}
	l.pipe = pipe
	return l, nil
}

// Status returns the active run, if any.
func (l *Loop) Status() (domain.LoopRun, bool, error) {
	return l.repo.LoadActive()
}

// Start begins a new run for the goal. The goal is immutable from here on:
// the planner sees it only inside DATA blocks, and a subtask answer that
// tries to rephrase it never reaches run.Goal.
func (l *Loop) Start(ctx context.Context, goal domain.Goal) (domain.LoopRun, error) {
	if strings.TrimSpace(goal.Text) == "" {
		return domain.LoopRun{}, fmt.Errorf("execloop: цель пуста")
	}
	unlock, err := l.repo.Lock()
	if err != nil {
		return domain.LoopRun{}, err
	}
	defer unlock()
	if _, active, err := l.repo.LoadActive(); err != nil {
		return domain.LoopRun{}, err
	} else if active {
		return domain.LoopRun{}, fmt.Errorf("execloop: уже есть активный запуск цикла — используйте resume")
	}
	if len(goal.Roots) == 0 {
		// A plain-text goal is one root bullet: the plan contract requires
		// every root covered by at least one subtask.
		goal.Roots = []string{goal.Text}
	}
	id, err := newRunID()
	if err != nil {
		return domain.LoopRun{}, err
	}
	now := l.clock.Now()
	run := domain.LoopRun{
		SchemaVersion: domain.LoopSchemaVersion,
		ID:            id,
		Goal:          goal,
		Status:        domain.LoopPlanning,
		Budget:        l.budget(now),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := l.persist(&run); err != nil {
		return run, err
	}
	l.logf("[loop] планирование: составляю план из цели…")
	plan, err := l.planner.Plan(ctx, &run)
	if err != nil {
		return l.finishErr(&run, ctx, err)
	}
	if err := applyPlan(&run, plan); err != nil {
		run.Report = err.Error()
		return l.terminate(&run, domain.LoopFailed)
	}
	ids := make([]string, 0, len(run.Subtasks))
	for i := range run.Subtasks {
		ids = append(ids, run.Subtasks[i].ID)
	}
	l.logf("[loop] план: %d подзадач: %s", len(run.Subtasks), strings.Join(ids, ", "))
	if err := l.ws.Seed(run.ID, planStacks(plan)); err != nil {
		run.Report = err.Error()
		return l.terminate(&run, domain.LoopFailed)
	}
	run.Status = domain.LoopRunning
	if err := l.persist(&run); err != nil {
		return run, err
	}
	return l.drive(ctx, &run)
}

// Resume continues the paused active run: the interrupted attempt is already
// marked, in-flight subtasks restart their round, blocked subtasks return to
// pending (the only way out of blocked), and the deadline is recalculated
// from now, capped at twice the original budget.
func (l *Loop) Resume(ctx context.Context) (domain.LoopRun, error) {
	unlock, err := l.repo.Lock()
	if err != nil {
		return domain.LoopRun{}, err
	}
	defer unlock()
	run, active, err := l.repo.LoadActive()
	if err != nil {
		return run, err
	}
	if !active {
		return domain.LoopRun{}, fmt.Errorf("execloop: нет активного запуска для продолжения")
	}
	if run.Status != domain.LoopPaused {
		return run, fmt.Errorf("execloop: продолжение доступно только для приостановленного запуска (статус %q)", run.Status)
	}
	now := l.clock.Now()
	capDeadline := run.Budget.Deadline.Add(l.cfg.Deadline)
	run.Budget.Deadline = now.Add(l.cfg.Deadline)
	if run.Budget.Deadline.After(capDeadline) {
		run.Budget.Deadline = capDeadline
	}
	for i := range run.Subtasks {
		st := &run.Subtasks[i]
		switch st.Status {
		case domain.SubtaskBlocked:
			_ = st.TransitionResume(now)
		case domain.SubtaskGenerating, domain.SubtaskTesting, domain.SubtaskReviewing,
			domain.SubtaskFixing, domain.SubtaskVerifying:
			// The round was interrupted: its attempt stays in the log
			// (Interrupted=true), the subtask restarts from pending.
			st.Status = domain.SubtaskPending
		}
	}
	run.Status = domain.LoopRunning
	run.UpdatedAt = now
	if err := l.persist(&run); err != nil {
		return run, err
	}
	return l.drive(ctx, &run)
}

// budget builds the run's absolute budget envelope from the config.
func (l *Loop) budget(now time.Time) domain.Budget {
	return domain.Budget{
		MaxSubtasks:           l.cfg.MaxSubtasks,
		MaxAttemptsPerSubtask: l.cfg.MaxAttemptsPerSubtask,
		MaxLLMCalls:           l.cfg.MaxLLMCalls,
		MaxTokens:             l.cfg.MaxTokens,
		MaxCostUSD:            l.cfg.MaxCostUSD,
		Deadline:              now.Add(l.cfg.Deadline),
		MaxReplans:            l.cfg.MaxReplans,
	}
}

// drive runs the subtasks in topological order until all are done, nothing
// can progress anymore, the context is canceled (paused) or a budget is
// spent (aborted).
func (l *Loop) drive(ctx context.Context, run *domain.LoopRun) (domain.LoopRun, error) {
	l.run = run
	defer func() { l.run = nil; l.curTaskID = "" }()
	for {
		if err := ctx.Err(); err != nil {
			return l.pause(run)
		}
		if reason := l.deadlineBreached(run); reason != "" {
			return l.abort(run, reason)
		}
		order, err := topoOrder(run)
		if err != nil {
			run.Report = err.Error()
			return l.terminate(run, domain.LoopFailed)
		}
		progressed := false
		replanned := false
		for _, id := range order {
			if err := ctx.Err(); err != nil {
				return l.pause(run)
			}
			idx := indexOf(run, id)
			if idx < 0 {
				continue
			}
			st := &run.Subtasks[idx]
			switch st.Status {
			case domain.SubtaskDone, domain.SubtaskFailed, domain.SubtaskBlocked, domain.SubtaskSkipped:
				continue
			}
			run.CurrentIdx = idx
			if bad, why := depsBlocked(run, st); bad {
				if err := st.Transition(domain.SubtaskSkipped, l.clock.Now()); err != nil {
					l.logf("[loop] переход %s → skipped: %v", st.ID, err)
					st.Status = domain.SubtaskSkipped
				}
				l.ev.Stage(st.ID, domain.SubtaskSkipped, why)
				if err := l.persist(run); err != nil {
					return *run, err
				}
				progressed = true
				continue
			}
			if err := l.runSubtask(ctx, run, idx); err != nil {
				if ctx.Err() != nil {
					return l.pause(run)
				}
				if errors.Is(err, port.ErrBudgetExceeded) {
					return l.abort(run, err.Error())
				}
				return *run, err // infrastructure error (save, workspace)
			}
			if ctx.Err() != nil {
				return l.pause(run)
			}
			st = &run.Subtasks[idx]
			switch st.Status {
			case domain.SubtaskDone, domain.SubtaskBlocked:
				progressed = true
			case domain.SubtaskFailed:
				progressed = true
				if run.Usage.Replans < run.Budget.MaxReplans {
					if err := l.replan(ctx, run); err != nil {
						if ctx.Err() != nil {
							return l.pause(run)
						}
						if errors.Is(err, port.ErrBudgetExceeded) {
							return l.abort(run, err.Error())
						}
						// A replan that breaks its contract does not rescue
						// the subtask; the failed state stays and the run
						// continues with the rest.
						l.logf("[loop] реплан не выполнен: %v", err)
					} else {
						// The subtask set changed: the order and indexes of
						// this pass are stale, recompute them.
						replanned = true
					}
				}
			}
			if replanned {
				break
			}
		}
		if replanned {
			continue
		}
		if !progressed {
			break
		}
	}
	allDone := true
	for i := range run.Subtasks {
		if run.Subtasks[i].Status != domain.SubtaskDone {
			allDone = false
			break
		}
	}
	status := domain.LoopFailed
	if allDone {
		status = domain.LoopDone
	}
	return l.terminate(run, status)
}

// runSubtask runs one subtask through the pipeline and the acceptance
// verifier, spending at most the remaining attempt budget. A verifier
// rejection spends the round: the subtask returns to fixing with the unmet
// criteria as feedback until the attempts run out.
func (l *Loop) runSubtask(ctx context.Context, run *domain.LoopRun, idx int) error {
	st := &run.Subtasks[idx]
	l.curTaskID = st.ID
	defer func() { l.curTaskID = "" }()
	spec := st.Spec
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		left := run.Budget.MaxAttemptsPerSubtask - len(st.Attempts)
		if left <= 0 {
			// Resume edge: the attempts are already spent (hand-edited or
			// interrupted state); the FSM has no pending→failed move.
			st.Status = domain.SubtaskFailed
			return l.persist(run)
		}
		l.attemptBase = len(st.Attempts)
		out, err := l.pipe.RunSubtask(ctx, devpipeline.Input{
			RunID:      run.ID,
			TaskID:     st.ID,
			Spec:       spec,
			Acceptance: st.Acceptance,
			Profile:    st.Profile,
			StackHint:  st.Stack,
		}, left)
		if err != nil {
			return err
		}
		if reason, ok := l.meter.Breached(); ok {
			// The pipeline formats role errors into its outcome reason, so
			// the budget sentinel survives only via the meter's latch.
			return fmt.Errorf("%w: %s", port.ErrBudgetExceeded, reason)
		}
		for _, att := range out.Attempts {
			att.N += l.attemptBase
			upsertAttempt(st, att)
		}
		switch out.Status {
		case domain.SubtaskBlocked:
			// The pipeline already transitioned the status through the
			// Stage events and persisted every attempt.
			return l.persist(run)
		case domain.SubtaskFailed:
			if ctx.Err() != nil {
				// Canceled mid-round: the pipeline had already reported
				// the failure, but a stop is a pause, not a failure — the
				// attempt is interrupted and the round restarts on resume.
				markLastAttempt(st, func(a *domain.Attempt) { a.Interrupted = true })
				st.Status = domain.SubtaskPending
			}
			return l.persist(run)
		}
		// The pipeline's verdict passed: acceptance verification decides
		// done. The task directory holds the pipeline's result; TaskDir
		// resolves it without the destructive reset Prepare performs.
		dir, err := l.ws.TaskDir(run.ID, st.ID)
		if err != nil {
			st.Status = domain.SubtaskFailed
			return l.persist(run)
		}
		if err := st.Transition(domain.SubtaskVerifying, l.clock.Now()); err != nil {
			l.logf("[loop] переход %s → verifying: %v", st.ID, err)
			st.Status = domain.SubtaskVerifying
		}
		l.ev.Stage(st.ID, domain.SubtaskVerifying, "проверка критериев приёмки")
		if err := l.persist(run); err != nil {
			return err
		}
		unmet, err := l.verifier.Verify(ctx, st, dir)
		if err != nil {
			if errors.Is(err, errAskUser) {
				st.Status = domain.SubtaskBlocked
				l.ev.Stage(st.ID, domain.SubtaskBlocked, err.Error())
				return l.persist(run)
			}
			if ctx.Err() != nil || errors.Is(err, port.ErrBudgetExceeded) {
				return err
			}
			markLastAttempt(st, func(a *domain.Attempt) { a.Error = err.Error() })
			st.Status = domain.SubtaskFailed
			l.ev.Stage(st.ID, domain.SubtaskFailed, "верификация: "+err.Error())
			return l.persist(run)
		}
		if len(unmet) == 0 {
			if err := st.Transition(domain.SubtaskDone, l.clock.Now()); err != nil {
				st.Status = domain.SubtaskDone
			}
			l.ev.Stage(st.ID, domain.SubtaskDone, "критерии приёмки выполнены")
			if err := l.ws.Promote(run.ID, st.ID); err != nil {
				st.Status = domain.SubtaskFailed
				l.ev.Stage(st.ID, domain.SubtaskFailed, "promote: "+err.Error())
				return l.persist(run)
			}
			return l.persist(run)
		}
		// Unmet criteria: the round is spent. The last attempt records the
		// verifying stage and the reason.
		markLastAttempt(st, func(a *domain.Attempt) {
			a.Stage = domain.SubtaskVerifying
			a.Error = "критерии приёмки не выполнены"
		})
		if len(st.Attempts) >= run.Budget.MaxAttemptsPerSubtask {
			if err := st.Transition(domain.SubtaskFailed, l.clock.Now()); err != nil {
				st.Status = domain.SubtaskFailed
			}
			l.ev.Stage(st.ID, domain.SubtaskFailed, "критерии приёмки не выполнены: "+strings.Join(unmet, "; "))
			return l.persist(run)
		}
		if err := st.Transition(domain.SubtaskFixing, l.clock.Now()); err != nil {
			st.Status = domain.SubtaskFixing
		}
		l.ev.Stage(st.ID, domain.SubtaskFixing, "возврат на доработку по критериям приёмки")
		var fb strings.Builder
		for _, u := range unmet {
			fmt.Fprintf(&fb, "- не выполнено: %s\n", u)
		}
		spec = st.Spec + "\n\nОбратная связь верификатора приёмки (обязательна к исправлению):\n" + fb.String()
		if err := l.persist(run); err != nil {
			return err
		}
	}
}

// replan asks the planner to rebuild the pending subtasks after a failure.
// Done/failed/blocked/skipped subtasks are immutable; only pending ones are
// replaced, and the rebuilt run re-validates.
func (l *Loop) replan(ctx context.Context, run *domain.LoopRun) error {
	var fails []FailureSummary
	for i := range run.Subtasks {
		st := &run.Subtasks[i]
		if st.Status != domain.SubtaskFailed {
			continue
		}
		fsum := FailureSummary{ID: st.ID, Title: st.Title, Reason: "попытки исчерпаны"}
		if n := len(st.Attempts); n > 0 {
			last := st.Attempts[n-1]
			switch {
			case last.Error != "":
				fsum.Reason = last.Error
			case last.Result != "":
				fsum.Reason = string(last.Result)
			}
			fsum.Findings = domain.Blocking(last.Findings)
		}
		fails = append(fails, fsum)
	}
	if len(fails) == 0 {
		return nil
	}
	plan, err := l.planner.Replan(ctx, run, fails)
	if err != nil {
		return err
	}
	if err := applyReplan(run, plan, run.Budget.MaxSubtasks); err != nil {
		return fmt.Errorf("реплан: %w", err)
	}
	// A replan may introduce a stack the initial plan did not have; Seed is
	// a no-op when base already exists.
	if err := l.ws.Seed(run.ID, subtaskStacks(run.Subtasks)); err != nil {
		return fmt.Errorf("реплан: засев workspace: %w", err)
	}
	run.Usage.Replans++
	run.UpdatedAt = l.clock.Now()
	l.logf("[loop] реплан %d/%d: обновлены pending-подзадачи", run.Usage.Replans, run.Budget.MaxReplans)
	return l.persist(run)
}

// deadlineBreached reports whether the run's wall clock ran out.
func (l *Loop) deadlineBreached(run *domain.LoopRun) string {
	if run.Budget.Deadline.IsZero() {
		return ""
	}
	if !l.clock.Now().Before(run.Budget.Deadline) {
		return fmt.Sprintf("истёк дедлайн (%s)", run.Budget.Deadline.Format(time.RFC3339))
	}
	if reason, ok := l.meter.Breached(); ok {
		return reason
	}
	return ""
}

// pause stops the run for a later Resume: the in-flight attempt is marked
// interrupted and the run stays the active one.
func (l *Loop) pause(run *domain.LoopRun) (domain.LoopRun, error) {
	if run.CurrentIdx >= 0 && run.CurrentIdx < len(run.Subtasks) {
		st := &run.Subtasks[run.CurrentIdx]
		if inFlight(st.Status) && len(st.Attempts) > 0 {
			st.Attempts[len(st.Attempts)-1].Interrupted = true
		}
	}
	run.Status = domain.LoopPaused
	run.UpdatedAt = l.clock.Now()
	if err := l.persist(run); err != nil {
		return *run, err
	}
	return *run, nil
}

// abort ends the run on a spent budget.
func (l *Loop) abort(run *domain.LoopRun, reason string) (domain.LoopRun, error) {
	run.Report = l.report(run) + fmt.Sprintf("\n**Прервано:** %s\n", reason)
	run.Status = domain.LoopAborted
	run.UpdatedAt = l.clock.Now()
	if err := l.repo.Save(*run); err != nil {
		return *run, err
	}
	if err := l.repo.Archive(run.ID); err != nil {
		return *run, err
	}
	return *run, nil
}

// finishErr maps a planning-stage error to its terminal (or paused) state.
func (l *Loop) finishErr(run *domain.LoopRun, ctx context.Context, err error) (domain.LoopRun, error) {
	if ctx != nil && ctx.Err() != nil {
		return l.pause(run)
	}
	if errors.Is(err, port.ErrBudgetExceeded) {
		return l.abort(run, err.Error())
	}
	if errors.Is(err, errAskUser) && l.cfg.OnAskUser == AskFail {
		run.Report = err.Error()
		return l.terminate(run, domain.LoopFailed)
	}
	run.Report = err.Error()
	return l.terminate(run, domain.LoopFailed)
}

// terminate builds the report, saves the run in its final state and moves it
// to the archive slot.
func (l *Loop) terminate(run *domain.LoopRun, status domain.LoopStatus) (domain.LoopRun, error) {
	run.Report = l.report(run)
	run.Status = status
	run.UpdatedAt = l.clock.Now()
	if err := l.repo.Save(*run); err != nil {
		return *run, err
	}
	if err := l.repo.Archive(run.ID); err != nil {
		return *run, err
	}
	return *run, nil
}

// persist saves the run write-ahead.
func (l *Loop) persist(run *domain.LoopRun) error {
	run.UpdatedAt = l.clock.Now()
	return l.repo.Save(*run)
}

// saveAttempt is the pipeline's write-ahead save: it attributes the attempt
// to the current subtask with run-wide numbering and persists the run.
func (l *Loop) saveAttempt(att domain.Attempt) error {
	if l.run == nil || l.curTaskID == "" {
		return fmt.Errorf("execloop: попытка сохранена вне подзадачи")
	}
	st := findSubtask(l.run, l.curTaskID)
	if st == nil {
		return fmt.Errorf("execloop: неизвестная подзадача %q", l.curTaskID)
	}
	att.N += l.attemptBase
	upsertAttempt(st, att)
	l.run.UpdatedAt = l.clock.Now()
	return l.repo.Save(*l.run)
}

// Stage implements devpipeline.Events: it mirrors the pipeline's stage
// events into the subtask state machine (write-ahead) and forwards them.
// The pipeline's SubtaskDone is not a state-machine move here — acceptance
// verification stands between reviewing and done.
func (l *Loop) Stage(taskID string, to domain.SubtaskStatus, msg string) {
	if l.run != nil && taskID == l.curTaskID {
		if st := findSubtask(l.run, taskID); st != nil {
			if to != st.Status && to != domain.SubtaskDone {
				if err := st.Transition(to, l.clock.Now()); err != nil {
					l.logf("[loop] переход подзадачи %s %s → %s: %v", taskID, st.Status, to, err)
				}
				if err := l.persist(l.run); err != nil {
					l.logf("[loop] сохранение состояния: %v", err)
				}
			}
		}
	}
	l.ev.Stage(taskID, to, msg)
}

// Findings implements devpipeline.Events.
func (l *Loop) Findings(taskID string, fs []domain.Finding) {
	l.ev.Findings(taskID, fs)
}

// ---- run-graph helpers ----

// topoOrder returns the subtask ids in dependency order of the current run.
func topoOrder(run *domain.LoopRun) ([]string, error) {
	items := make([]domain.PlanItem, len(run.Subtasks))
	for i, st := range run.Subtasks {
		items[i] = domain.PlanItem{ID: st.ID, DependsOn: st.DependsOn}
	}
	return domain.TopoOrder(items)
}

// indexOf returns the position of the subtask id in the run.
func indexOf(run *domain.LoopRun, id string) int {
	for i := range run.Subtasks {
		if run.Subtasks[i].ID == id {
			return i
		}
	}
	return -1
}

// findSubtask returns the subtask pointer or nil.
func findSubtask(run *domain.LoopRun, id string) *domain.Subtask {
	for i := range run.Subtasks {
		if run.Subtasks[i].ID == id {
			return &run.Subtasks[i]
		}
	}
	return nil
}

// depsBlocked reports whether a dependency of the subtask failed, was
// blocked or skipped — then the subtask is skipped too.
func depsBlocked(run *domain.LoopRun, st *domain.Subtask) (bool, string) {
	for _, dep := range st.DependsOn {
		d := findSubtask(run, dep)
		if d == nil {
			continue
		}
		switch d.Status {
		case domain.SubtaskFailed:
			return true, fmt.Sprintf("зависимость %s провалена", dep)
		case domain.SubtaskBlocked:
			return true, fmt.Sprintf("зависимость %s заблокирована", dep)
		case domain.SubtaskSkipped:
			return true, fmt.Sprintf("зависимость %s пропущена", dep)
		}
	}
	return false, ""
}

// inFlight reports whether the status belongs to a round in progress.
func inFlight(s domain.SubtaskStatus) bool {
	switch s {
	case domain.SubtaskGenerating, domain.SubtaskTesting, domain.SubtaskReviewing,
		domain.SubtaskFixing, domain.SubtaskVerifying:
		return true
	}
	return false
}

// upsertAttempt replaces the attempt with the same N or appends it.
func upsertAttempt(st *domain.Subtask, att domain.Attempt) {
	for i := range st.Attempts {
		if st.Attempts[i].N == att.N {
			st.Attempts[i] = att
			return
		}
	}
	st.Attempts = append(st.Attempts, att)
}

// markLastAttempt mutates the subtask's last attempt in place.
func markLastAttempt(st *domain.Subtask, fn func(*domain.Attempt)) {
	if n := len(st.Attempts); n > 0 {
		fn(&st.Attempts[n-1])
	}
}

// subtaskFromItem turns a plan item into a fresh pending subtask.
func subtaskFromItem(it domain.PlanItem) domain.Subtask {
	return domain.Subtask{
		ID:         it.ID,
		Title:      it.Title,
		Spec:       it.Spec,
		Profile:    it.Profile,
		Stack:      it.Stack,
		DependsOn:  it.DependsOn,
		Acceptance: it.Acceptance,
		Root:       it.Root,
		Status:     domain.SubtaskPending,
	}
}

// applyPlan installs the initial plan: subtasks in dependency order, all
// pending, and the run re-validates.
func applyPlan(run *domain.LoopRun, plan domain.PlanV1) error {
	order, err := domain.TopoOrder(plan.Subtasks)
	if err != nil {
		return err
	}
	byID := make(map[string]domain.PlanItem, len(plan.Subtasks))
	for _, it := range plan.Subtasks {
		byID[it.ID] = it
	}
	run.Subtasks = make([]domain.Subtask, 0, len(order))
	for _, id := range order {
		run.Subtasks = append(run.Subtasks, subtaskFromItem(byID[id]))
	}
	run.CurrentIdx = 0
	return run.Validate()
}

// planStacks is the union of the plan items' stacks, in first-seen order.
func planStacks(plan domain.PlanV1) []string {
	seen := map[string]bool{}
	var out []string
	for _, it := range plan.Subtasks {
		for _, s := range it.Stack {
			if s != "" && !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}

// subtaskStacks is the union of the subtasks' stacks, in first-seen order.
func subtaskStacks(subtasks []domain.Subtask) []string {
	seen := map[string]bool{}
	var out []string
	for i := range subtasks {
		for _, s := range subtasks[i].Stack {
			if s != "" && !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}

// applyReplan replaces only the pending subtasks with the replan, keeps every
// other subtask immutable and re-validates the run. Pending subtasks absent
// from the replan are dropped; ids new to the plan are appended (within the
// subtask cap).
func applyReplan(run *domain.LoopRun, plan domain.PlanV1, maxSubtasks int) error {
	order, err := domain.TopoOrder(plan.Subtasks)
	if err != nil {
		return err
	}
	byID := make(map[string]domain.PlanItem, len(plan.Subtasks))
	for _, it := range plan.Subtasks {
		byID[it.ID] = it
	}
	inPlan := make(map[string]bool, len(order))
	kept := make([]domain.Subtask, 0, len(order))
	for _, id := range order {
		if ex := findSubtask(run, id); ex != nil && ex.Status != domain.SubtaskPending {
			inPlan[id] = true
			kept = append(kept, *ex) // done/failed/blocked/skipped are immutable
			continue
		}
		if len(kept) >= maxSubtasks {
			return fmt.Errorf("реплан: число подзадач превысит лимит %d", maxSubtasks)
		}
		inPlan[id] = true
		kept = append(kept, subtaskFromItem(byID[id]))
	}
	// Subtasks the planner dropped from the plan: pending ones disappear,
	// the rest keeps its state (a failed subtask stays visible in the run).
	for i := range run.Subtasks {
		st := run.Subtasks[i]
		if !inPlan[st.ID] && st.Status != domain.SubtaskPending {
			kept = append(kept, st)
		}
	}
	run.Subtasks = kept
	run.CurrentIdx = 0
	return run.Validate()
}

// newRunID draws a random run id (8 lowercase hex chars, matching the
// domain id pattern).
func newRunID() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("execloop: генерация id запуска: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
