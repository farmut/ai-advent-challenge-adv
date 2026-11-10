package execloop

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
	"ai-adv-agent/internal/usecase/contracts"
)

// Profiles the loop's own LLM-driven steps run under. There is no dedicated
// planner/verifier profile file: both are read-only reasoning roles and run
// under the researcher profile with their workspace narrowed by Roots.
const (
	// ProfilePlanner plans the run. It is a dedicated read-only profile so
	// that scripted upstreams can tell planning requests from acceptance
	// verification (both are analytical, but they answer different
	// contracts).
	ProfilePlanner = "planner"
	// ProfileVerifier checks acceptance criteria after the pipeline passed.
	ProfileVerifier = "researcher"
)

// errAskUser marks a role answer that tries to ask the user a question in a
// loop where asking is impossible. With on_ask_user=fail the subtask is
// blocked; with decide the question is suppressed and the model decides.
var errAskUser = errors.New("роль пытается задать вопрос пользователю")

// askUserRe is the conservative detector for that: a question mark plus an
// explicit request for clarification/confirmation/choice. Mere mentions of
// questions in code or prose do not match.
var askUserRe = regexp.MustCompile(`(?is)(уточн|подтверд|выбер|спрос|задать вопрос|нужно знать).{0,80}\?`)

// planAnswer is the strict-decoding envelope of plan.v1. The domain-level
// validation (profile names, root coverage, the dependency graph) needs the
// run's context, so it runs on the decoded domain value; the envelope only
// pins the schema and lets DecodeStrict enforce the exact field set.
type planAnswer struct {
	Schema   string            `json:"schema"`
	Subtasks []domain.PlanItem `json:"subtasks"`
}

func (p *planAnswer) Validate() error {
	if p.Schema != "plan.v1" {
		return fmt.Errorf("schema: ожидается %q, получено %q", "plan.v1", p.Schema)
	}
	if p.Subtasks == nil {
		return fmt.Errorf("subtasks обязателен (пустой список — []")
	}
	return nil
}

// planTask is the trusted instruction the planner receives. The goal, the
// roots and any failure summaries arrive in nonce DATA blocks only, so text
// inside them can never become an instruction — in particular it cannot
// change the goal or the budget.
const planTask = `Составь план выполнения цели из DATA-блоков. Ответь одним JSON-объектом по схеме plan.v1, без текста вокруг JSON и без markdown-огородки:

{"schema":"plan.v1","subtasks":[{"id":"users-endpoint","title":"Краткое название","spec":"Что сделать: требования, затрагиваемые файлы, ограничения","profile":"coder","stack":["go"],"depends_on":[],"acceptance":["конкретный проверяемый критерий"],"root":0}]}

Правила:
- id: ^[a-z0-9-]{1,32}$, уникальные внутри плана;
- title и spec — непустые, spec самодостаточен (подзадачу выполняет роль, которая видит только spec и feedback);
- profile — строго одно из имён каталога профилей ниже;
- stack: "go" и/или "vue";
- depends_on — id из этого же плана; циклы запрещены, каждый пункт цели должен быть покрыт хотя бы одной подзадачей (root — 0-based индекс пункта из DATA-блока "roots");
- acceptance: 1..8 конкретных проверяемых критериев;
- цель, пункты цели и бюджет менять нельзя: всё нужное для решений есть в DATA;
- задать вопрос пользователю невозможно: неуверенные решения принимай сам и фиксируй в плане.

Каталог профилей (имя — описание):
`

// replanExtra is appended to the task on a replan: the done subtasks are
// immutable and the failures (in a DATA block) must shape the new pending
// set.
const replanExtra = `
Это реплан: часть подзадач уже выполнена и неизменна. Ты можешь заменить или перестроить только подзадачи, которые ещё не начаты (pending). Провалы из DATA-блока "failures" учти при планировании: устрани их причину в новых подзадачах. Подзадачи со статусом done, failed, blocked или skipped присутствуют в плане в том же виде и не изменяются.
`

// Planner asks the LLM for a plan.v1 and validates it strictly. The goal
// reaches the model only as DATA; the run's Goal is never written from the
// model's answer.
type Planner struct {
	roles       port.RoleRunner
	meter       *MeteredLLM
	known       map[string]bool
	profiles    []Profile
	jsonRetries int
	onAskUser   string
	logf        func(string, ...any)
}

// NewPlanner assembles the planner from the loop config.
func NewPlanner(roles port.RoleRunner, meter *MeteredLLM, cfg Config, logf func(string, ...any)) *Planner {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Planner{
		roles:       roles,
		meter:       meter,
		known:       cfg.known(),
		profiles:    cfg.Profiles,
		jsonRetries: cfg.JSONRetries,
		onAskUser:   cfg.OnAskUser,
		logf:        logf,
	}
}

// Plan builds the initial plan for the run. After jsonRetries re-requests
// with the validator's complaint the plan fails, not guesses.
func (p *Planner) Plan(ctx context.Context, run *domain.LoopRun) (domain.PlanV1, error) {
	task := planTask + profileCatalog(p.profiles)
	return p.ask(ctx, run, task, "")
}

// Replan rebuilds the plan after a failed subtask. failures carries the
// structured summaries of what went wrong; the same strict validation and
// retry budget applies.
func (p *Planner) Replan(ctx context.Context, run *domain.LoopRun, failures []FailureSummary) (domain.PlanV1, error) {
	task := planTask + profileCatalog(p.profiles) + replanExtra
	return p.ask(ctx, run, task, failuresBlock(failures))
}

// ask runs the planner role with the strict decode → validate → retry loop.
func (p *Planner) ask(ctx context.Context, run *domain.LoopRun, task, feedback string) (domain.PlanV1, error) {
	blocks := []domain.DataBlock{
		{Label: "goal", Content: run.Goal.Text},
		{Label: "roots", Content: rootsBlock(run.Goal.Roots)},
	}
	if feedback != "" {
		blocks = append(blocks, domain.DataBlock{Label: "failures", Content: feedback})
	}
	attempt := task
	for r := 0; r <= p.jsonRetries; r++ {
		if err := p.meter.gate(ctx); err != nil {
			return domain.PlanV1{}, err
		}
		res, err := p.roles.Run(ctx, port.RoleRequest{
			Profile:    ProfilePlanner,
			Task:       attempt,
			DataBlocks: blocks,
			Roots:      domain.WorkRoots{}, // no workspace: the plan comes from DATA alone
		})
		if err != nil {
			return domain.PlanV1{}, fmt.Errorf("планировщик: %w", err)
		}
		if res.GuardBlocked {
			return domain.PlanV1{}, fmt.Errorf("планировщик: ответ заблокирован guard'ом: %s", res.GuardReason)
		}
		if reason := p.askUser(res.Content); reason != "" {
			if p.onAskUser == AskFail {
				return domain.PlanV1{}, fmt.Errorf("%w: %s", errAskUser, reason)
			}
			attempt = task + "\n\nВопросы пользователю недоступны (предыдущий ответ отклонён: " + reason + "). Принимай решения сам по данным из DATA и ответь заново целиком."
			continue
		}
		ans, err := contracts.DecodeStrict[planAnswer, *planAnswer](res.Content)
		if err == nil {
			plan := domain.PlanV1{Schema: ans.Schema, Subtasks: ans.Subtasks}
			err = plan.Validate(p.known, len(run.Goal.Roots), run.Budget.MaxSubtasks)
			if err == nil {
				return plan, nil
			}
		}
		attempt = task + "\n\nПредыдущий ответ отклонён валидатором: " + err.Error() +
			". Исправь план и повтори ответ целиком."
	}
	return domain.PlanV1{}, fmt.Errorf("план: ответ не соответствует контракту plan.v1 после %d попыток", p.jsonRetries+1)
}

// askUser reports whether the answer tries to ask the user a question, and
// with decide policy logs the suppression.
func (p *Planner) askUser(content string) string {
	loc := askUserRe.FindString(content)
	if loc == "" {
		return ""
	}
	if p.onAskUser == AskDecide {
		p.logf("[loop] ask_user подавлен (policy=decide)")
	}
	return "вопрос пользователю в полностью автономном цикле недоступен"
}

// profileCatalog renders the planner's catalogue of profile names and
// descriptions.
func profileCatalog(ps []Profile) string {
	var b strings.Builder
	for _, p := range ps {
		desc := p.Description
		if desc == "" {
			desc = "—"
		}
		fmt.Fprintf(&b, "- %s — %s\n", p.Name, desc)
	}
	return b.String()
}

// rootsBlock renders the goal bullet points the plan's root indexes refer to.
func rootsBlock(roots []string) string {
	var b strings.Builder
	for i, r := range roots {
		fmt.Fprintf(&b, "%d. %s\n", i, r)
	}
	return b.String()
}

// FailureSummary is the structured, code-built summary of one failed subtask
// the replanner receives. No raw output ever reaches it: only the subtask's
// identity, the failure reason and its blocking findings.
type FailureSummary struct {
	ID       string
	Title    string
	Reason   string
	Findings []domain.Finding // blocking findings of the last attempt, fields only
}

// failuresBlock renders the failure summaries for the replan DATA block.
func failuresBlock(fs []FailureSummary) string {
	var b strings.Builder
	for _, f := range fs {
		fmt.Fprintf(&b, "- подзадача %s (%s): %s\n", f.ID, f.Title, f.Reason)
		for _, fnd := range f.Findings {
			fmt.Fprintf(&b, "  - %s %s в %s:%d — %s\n", fnd.Severity, fnd.CWE, fnd.File, fnd.Line, fnd.Issue)
		}
	}
	return b.String()
}
