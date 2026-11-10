package devpipeline

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
	"ai-adv-agent/internal/usecase/contracts"
)

// Input is one subtask handed to the pipeline.
type Input struct {
	RunID  string
	TaskID string
	// Spec is the trusted task text (the subtask's specification).
	Spec string
	// Acceptance lists the acceptance criteria (used by the verifier, PR7;
	// the pipeline carries it for the record).
	Acceptance []string
	// Profile overrides Config.Generator ("" = the configured generator).
	Profile string
	// StackHint names the stacks when the changed files classify to none.
	StackHint []string
}

// Outcome is the subtask-level result of RunSubtask. Attempts holds every
// attempt in order, each one saved write-ahead while it ran.
type Outcome struct {
	// Status is done, failed or blocked.
	Status domain.SubtaskStatus
	// Reason is a Russian, human-readable explanation for failed/blocked.
	Reason   string
	Attempts []domain.Attempt
}

// Events receives stage and finding updates while a subtask runs. The
// execution loop (PR7) implements it to drive the UI; tests implement it to
// observe the pipeline.
type Events interface {
	Stage(taskID string, stage domain.SubtaskStatus, msg string)
	Findings(taskID string, fs []domain.Finding)
}

// Pipeline runs one subtask through generate → test → security review,
// feeding code-built feedback back to the generator until the code passes,
// the security iterations run out or the same blocking findings repeat.
//
// Every transition is persisted write-ahead through save before it happens.
type Pipeline struct {
	roles  port.RoleRunner
	runner port.CommandRunner
	sast   port.SecurityScanner
	ws     port.Workspace
	save   func(domain.Attempt) error
	ev     Events
	cfg    Config
}

// New assembles a Pipeline and validates its config.
func New(roles port.RoleRunner, runner port.CommandRunner, sast port.SecurityScanner, ws port.Workspace, save func(domain.Attempt) error, ev Events, cfg Config) (*Pipeline, error) {
	if roles == nil {
		return nil, fmt.Errorf("devpipeline: RoleRunner не задан")
	}
	if runner == nil {
		return nil, fmt.Errorf("devpipeline: CommandRunner не задан")
	}
	if sast == nil {
		return nil, fmt.Errorf("devpipeline: SecurityScanner не задан")
	}
	if ws == nil {
		return nil, fmt.Errorf("devpipeline: Workspace не задан")
	}
	if save == nil {
		return nil, fmt.Errorf("devpipeline: функция сохранения попыток не задана")
	}
	if ev == nil {
		ev = nopEvents{}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Pipeline{roles: roles, runner: runner, sast: sast, ws: ws, save: save, ev: ev, cfg: cfg}, nil
}

// nopEvents sinks events when the caller passes no Events.
type nopEvents struct{}

func (nopEvents) Stage(string, domain.SubtaskStatus, string) {}
func (nopEvents) Findings(string, []domain.Finding)          {}

// stage switches the attempt's stage, reports it and persists the attempt
// write-ahead, so a crash never loses where the subtask got to.
func (p *Pipeline) stage(in Input, att *domain.Attempt, st domain.SubtaskStatus, msg string) error {
	att.Stage = st
	p.ev.Stage(in.TaskID, st, msg)
	return p.save(*att)
}

// finish stamps the attempt, persists it and appends it to the outcome list.
func (p *Pipeline) finish(att *domain.Attempt, attempts []domain.Attempt) []domain.Attempt {
	att.FinishedAt = time.Now()
	// The attempt was already persisted at every stage, so the verdict must
	// not be hidden by a failed final save: it is best-effort here.
	_ = p.save(*att)
	return append(attempts, *att)
}

// fail ends the subtask with a failed status after persisting the attempt.
func (p *Pipeline) fail(in Input, att *domain.Attempt, attempts []domain.Attempt, reason string) Outcome {
	att.Error = reason
	p.ev.Stage(in.TaskID, domain.SubtaskFailed, reason)
	return Outcome{Status: domain.SubtaskFailed, Reason: reason, Attempts: p.finish(att, attempts)}
}

// RunSubtask runs the subtask until it passes, fails or is blocked, spending
// at most maxAttempts generate → test → review rounds.
func (p *Pipeline) RunSubtask(ctx context.Context, in Input, maxAttempts int) (Outcome, error) {
	if strings.TrimSpace(in.TaskID) == "" {
		return Outcome{}, fmt.Errorf("devpipeline: TaskID обязателен")
	}
	if maxAttempts < 1 {
		return Outcome{}, fmt.Errorf("devpipeline: число попыток должно быть >= 1, получено %d", maxAttempts)
	}
	dir, err := p.ws.Prepare(in.RunID, in.TaskID)
	if err != nil {
		return Outcome{Status: domain.SubtaskFailed, Reason: err.Error()}, nil
	}

	var attempts []domain.Attempt
	var fb *contracts.Feedback // feedback the next generator run receives
	var fbData []domain.DataBlock
	prevKey := ""
	secIter := 0

	for n := 1; n <= maxAttempts; n++ {
		att := domain.Attempt{N: n, StartedAt: time.Now()}

		before, err := p.ws.Snapshot(dir)
		if err != nil {
			att.Result = domain.ResultError
			return p.fail(in, &att, attempts, err.Error()), nil
		}

		// ---- generating ----
		if err := p.stage(in, &att, domain.SubtaskGenerating, "генерация кода"); err != nil {
			return p.fail(in, &att, attempts, err.Error()), nil
		}
		genProfile := in.Profile
		if genProfile == "" {
			genProfile = p.cfg.Generator
		}
		task := in.Spec
		if fb != nil {
			task += "\n\nОбратная связь по предыдущей попытке (feedback.v1, обязательна к исправлению):\n" + fb.JSON()
		}
		res, err := p.roles.Run(ctx, port.RoleRequest{
			Profile:       genProfile,
			Task:          task,
			DataBlocks:    fbData,
			Roots:         domain.WorkRoots{Read: dir, Write: dir},
			MaxToolRounds: p.cfg.MaxToolRounds,
		})
		if err != nil {
			att.Result = domain.ResultError
			if errors.Is(err, port.ErrGuardBlocked) {
				// The input guard refused the whole request: no code can be
				// produced, the subtask is blocked, not failed.
				att.Result = domain.ResultGuardBlocked
				reason := fmt.Sprintf("запрос генератора отклонён guard'ом прокси: %v", err)
				p.ev.Stage(in.TaskID, domain.SubtaskBlocked, reason)
				return Outcome{Status: domain.SubtaskBlocked, Reason: reason, Attempts: p.finish(&att, attempts)}, nil
			}
			return p.fail(in, &att, attempts, fmt.Sprintf("генератор: %v", err)), nil
		}
		if res.GuardBlocked {
			att.Result = domain.ResultGuardBlocked
			next := contracts.NewFeedback(n, []contracts.FeedbackItem{contracts.GuardItem(res.GuardReason)})
			fb, fbData = &next, nil
			att.Feedback = fb.Texts()
			if n == maxAttempts {
				return p.fail(in, &att, attempts, "попытки исчерпаны: генератор так и не дал ответ (guard)"), nil
			}
			attempts = p.finish(&att, attempts)
			continue
		}

		after, err := p.ws.Snapshot(dir)
		if err != nil {
			att.Result = domain.ResultError
			return p.fail(in, &att, attempts, err.Error()), nil
		}
		changed, deleted := diff(before, after)
		if len(changed) == 0 && len(deleted) == 0 {
			att.Result = domain.ResultNoChanges
			next := contracts.NewFeedback(n, []contracts.FeedbackItem{contracts.NoChangesItem()})
			fb, fbData = &next, nil
			att.Feedback = fb.Texts()
			if n == maxAttempts {
				return p.fail(in, &att, attempts, "попытки исчерпаны: генератор не изменил ни одного файла"), nil
			}
			attempts = p.finish(&att, attempts)
			continue
		}
		att.Changed = changed
		stacks, err := detectStacks(changed, in.StackHint)
		if err != nil {
			att.Result = domain.ResultError
			return p.fail(in, &att, attempts, err.Error()), nil
		}
		att.Stacks = stacks

		// ---- testing ----
		if err := p.stage(in, &att, domain.SubtaskTesting, "запуск тестов"); err != nil {
			return p.fail(in, &att, attempts, err.Error()), nil
		}
		passed, cmds, failArgv, failRes, failOut, runErr := p.runTests(ctx, dir, stacks)
		att.TestResult = domain.TestResult{Ran: true, Passed: passed && runErr == nil, Commands: cmds}
		if runErr != nil {
			att.Result = domain.ResultError
			return p.fail(in, &att, attempts, fmt.Sprintf("тесты не запустились: %v", runErr)), nil
		}
		if !passed {
			att.Result = domain.ResultTestsFailed
			out := tailBytes(failOut, p.cfg.TestOutputMaxBytes)
			next := contracts.NewFeedback(n, []contracts.FeedbackItem{contracts.TestItem(failArgv, failRes.ExitCode, failRes.TimedOut)})
			fb = &next
			fbData = []domain.DataBlock{{Label: "test-output", Content: out}}
			att.Feedback = fb.Texts()
			if n == maxAttempts {
				return p.fail(in, &att, attempts, "попытки исчерпаны: тесты не проходят"), nil
			}
			if err := p.stage(in, &att, domain.SubtaskFixing, "возврат на доработку по результатам тестов"); err != nil {
				return p.fail(in, &att, attempts, err.Error()), nil
			}
			attempts = p.finish(&att, attempts)
			continue
		}

		// ---- reviewing ----
		secIter++
		att.SecurityIteration = secIter
		if err := p.stage(in, &att, domain.SubtaskReviewing, "security-ревью"); err != nil {
			return p.fail(in, &att, attempts, err.Error()), nil
		}
		sastFindings, err := p.runSAST(ctx, dir, stacks)
		if err != nil {
			if errors.Is(err, port.ErrScanOutput) {
				att.Result = domain.ResultReviewFailed
				return p.fail(in, &att, attempts, "ревью не пройдено: вывод SAST непригоден (fail-closed)"), nil
			}
			att.Result = domain.ResultError
			return p.fail(in, &att, attempts, err.Error()), nil
		}
		blocks := p.reviewBlocks(dir, changed, deleted)
		llmFindings, warnings, err := p.runReviewers(ctx, dir, stacks, blocks)
		att.Warnings = warnings
		if err != nil {
			if errors.Is(err, errReviewFailed) {
				att.Result = domain.ResultReviewFailed
			} else {
				att.Result = domain.ResultError
			}
			return p.fail(in, &att, attempts, err.Error()), nil
		}

		counter := func(rel string) (int, bool) {
			_, lines, err := p.ws.ReadNumbered(dir, rel, 1)
			return lines, err == nil
		}
		llmFindings = verify(llmFindings, after, counter)
		for i := range sastFindings {
			// SAST ran on the task directory itself: its reports are trusted
			// to name real files; the path is still normalised.
			sastFindings[i].Verified = true
			if rel, ok := normRel(sastFindings[i].File); ok {
				sastFindings[i].File = rel
			}
		}
		all := merge(append(sastFindings, llmFindings...))
		att.Findings = all
		att.Verdict = domain.ComputeVerdict(all)
		att.FindingsKey = domain.FindingsKey(all)
		p.ev.Findings(in.TaskID, all)

		if att.Verdict == domain.VerdictPass {
			att.Result = domain.ResultPassed
			if err := p.stage(in, &att, domain.SubtaskDone, "тесты и security-ревью пройдены"); err != nil {
				return p.fail(in, &att, attempts, err.Error()), nil
			}
			return Outcome{Status: domain.SubtaskDone, Attempts: p.finish(&att, attempts)}, nil
		}

		att.Result = domain.ResultSecurityFail
		key := domain.FindingsKey(all)
		if key != "" && key == prevKey {
			att.Result = domain.ResultNoProgress
			return p.fail(in, &att, attempts, "нет прогресса: блокирующие находки совпадают с предыдущей итерацией"), nil
		}
		if secIter >= p.cfg.MaxSecurityIterations {
			return p.fail(in, &att, attempts, fmt.Sprintf("исчерпан лимит итераций безопасности (%d)", p.cfg.MaxSecurityIterations)), nil
		}
		prevKey = key
		items := make([]contracts.FeedbackItem, 0, len(all))
		for _, f := range domain.Blocking(all) {
			items = append(items, contracts.SecurityItem(f))
		}
		next := contracts.NewFeedback(n, items)
		fb, fbData = &next, nil
		att.Feedback = fb.Texts()
		if err := p.stage(in, &att, domain.SubtaskFixing, "возврат на доработку по находкам безопасности"); err != nil {
			return p.fail(in, &att, attempts, err.Error()), nil
		}
		attempts = p.finish(&att, attempts)
	}

	return Outcome{Status: domain.SubtaskFailed, Reason: "попытки исчерпаны", Attempts: attempts}, nil
}

// runTests runs every configured test command of every stack in dir. A
// non-zero exit or a timeout is a test failure; a non-nil error means the
// command could not run at all (the sandbox is broken — infrastructure, not
// a failure of the code).
func (p *Pipeline) runTests(ctx context.Context, dir string, stacks []string) (passed bool, cmds []domain.CommandOutcome, failArgv []string, failRes port.CommandResult, failOut string, runErr error) {
	for _, st := range stacks {
		for _, argv := range p.cfg.TestCommands[st] {
			res, err := p.runner.Run(ctx, port.CommandSpec{
				Argv:      append([]string(nil), argv...),
				Workdir:   dir,
				Timeout:   p.cfg.CommandTimeout,
				MaxOutput: p.cfg.TestOutputMaxBytes,
				Stack:     st,
			})
			if err != nil {
				return false, cmds, argv, port.CommandResult{ExitCode: -1}, "", err
			}
			cmds = append(cmds, domain.CommandOutcome{Argv: argv, Stack: st, ExitCode: res.ExitCode, TimedOut: res.TimedOut})
			if res.ExitCode != 0 || res.TimedOut {
				out := res.Stdout
				if res.Stderr != "" {
					out += "\n" + res.Stderr
				}
				return false, cmds, argv, res, out, nil
			}
		}
	}
	return true, cmds, nil, port.CommandResult{}, "", nil
}

// runSAST runs the static analysers of every stack. An error wrapping
// port.ErrScanOutput means the scan output was unusable (fail-closed); any
// other error is infrastructure.
func (p *Pipeline) runSAST(ctx context.Context, dir string, stacks []string) ([]domain.Finding, error) {
	var findings []domain.Finding
	for _, st := range stacks {
		fs, err := p.sast.Scan(ctx, dir, st)
		if err != nil {
			return nil, err
		}
		findings = append(findings, fs...)
	}
	return findings, nil
}

// errReviewFailed marks a review that failed closed (unusable reviewer or
// SAST output, or a guard-blocked reviewer). It is distinct from an
// infrastructure error, which the loop may retry or abort on.
var errReviewFailed = errors.New("ревью не пройдено")

// reviewTask is the trusted instruction every security reviewer receives.
const reviewTask = "Проанализируй изменённые файлы из DATA-блоков на уязвимости безопасности " +
	"(инъекции, XSS, выполнение кода, секреты в коде, небезопасная работа с путями и процессами). " +
	"Самостоятельно читать файлы не нужно — ниже полный текст изменённых файлов с номерами строк и список изменений. " +
	"Ответь одним JSON-объектом по схеме security_findings.v1 (поле verdict: pass, когда блокирующих находок нет), " +
	"без текста вокруг JSON."

// runReviewers runs one security reviewer per stack and strictly decodes its
// answer, re-requesting with the validator's complaint up to cfg.JSONRetries
// times. An error wrapping errReviewFailed means the review failed closed;
// any other error is infrastructure.
func (p *Pipeline) runReviewers(ctx context.Context, dir string, stacks []string, blocks []domain.DataBlock) ([]domain.Finding, []string, error) {
	var findings []domain.Finding
	var warnings []string
	for _, st := range stacks {
		reviewer := p.cfg.Reviewers[st]
		task := reviewTask
		var sf contracts.SecurityFindings
		answered := false
		for r := 0; r <= p.cfg.JSONRetries; r++ {
			res, err := p.roles.Run(ctx, port.RoleRequest{
				Profile:       reviewer,
				Task:          task,
				DataBlocks:    blocks,
				Roots:         domain.WorkRoots{Read: dir, Write: ""},
				MaxToolRounds: p.cfg.MaxToolRounds,
			})
			if err != nil {
				return nil, warnings, fmt.Errorf("ревьюер %s: %w", reviewer, err)
			}
			if res.GuardBlocked {
				return nil, warnings, fmt.Errorf("%w: ответ ревьюера %s заблокирован guard'ом: %s", errReviewFailed, reviewer, res.GuardReason)
			}
			v, err := contracts.DecodeStrict[contracts.SecurityFindings, *contracts.SecurityFindings](res.Content)
			if err == nil {
				sf, answered = v, true
				break
			}
			task = reviewTask + "\n\nПредыдущий ответ отклонён валидатором: " + err.Error() +
				". Исправь ответ и повтори его целиком."
		}
		if !answered {
			return nil, warnings, fmt.Errorf("%w: ответ ревьюера %s не соответствует контракту security_findings.v1 после %d попыток", errReviewFailed, reviewer, p.cfg.JSONRetries+1)
		}
		if sf.Dropped > 0 {
			warnings = append(warnings, fmt.Sprintf("ревьюер %s: отброшено %d находок сверх лимита %d", reviewer, sf.Dropped, contracts.MaxFindings))
		}
		findings = append(findings, sf.ToDomain()...)
	}
	return findings, warnings, nil
}

// reviewBlocks builds the reviewer's DATA blocks: the change list first, then
// every changed file with line numbers, bounded per file and in total.
func (p *Pipeline) reviewBlocks(dir string, changed, deleted []string) []domain.DataBlock {
	var b strings.Builder
	fmt.Fprintf(&b, "Изменённые файлы (%d): %s\n", len(changed), strings.Join(changed, ", "))
	if len(deleted) > 0 {
		fmt.Fprintf(&b, "Удалённые файлы: %s\n", strings.Join(deleted, ", "))
	}
	blocks := []domain.DataBlock{{Label: "changes", Content: b.String()}}
	total := 0
	for _, rel := range changed {
		left := p.cfg.ReviewTotalMaxBytes - total
		if left <= 0 {
			blocks = append(blocks, domain.DataBlock{Label: "truncated", Content: "[…оставшиеся файлы не показаны: превышен суммарный лимит размера ревью…]"})
			break
		}
		maxPerFile := p.cfg.ReviewFileMaxBytes
		if left < maxPerFile {
			maxPerFile = left
		}
		text, _, err := p.ws.ReadNumbered(dir, rel, maxPerFile)
		if err != nil {
			blocks = append(blocks, domain.DataBlock{Label: "file-" + rel, Content: "[файл не прочитан: " + err.Error() + "]"})
			continue
		}
		blocks = append(blocks, domain.DataBlock{Label: "file-" + rel, Content: text})
		total += len(text)
	}
	return blocks
}

// tailBytes returns the last n bytes of s, cut on a rune boundary.
func tailBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	for i := range len(s) {
		if s[i]&0xC0 != 0x80 {
			return s[i:]
		}
	}
	return s
}
