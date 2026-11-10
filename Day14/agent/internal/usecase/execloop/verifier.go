package execloop

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
	"ai-adv-agent/internal/usecase/contracts"
)

// verifyTask is the trusted instruction the acceptance judge receives. The
// acceptance criteria and the changed files arrive in nonce DATA blocks; the
// judge is read-only on the task directory.
const verifyTask = `Ты — верификатор приёмки подзадачи. В DATA-блоках: критерии приёмки и изменённые файлы подзадачи (с номерами строк). Проверь каждый критерий по файлам каталога. Ответь одним JSON-объектом по схеме step_result.v1, без текста вокруг JSON:

{"schema":"step_result.v1","subtask_id":"<id подзадачи>","status":"done","criteria":[{"text":"<критерий дословно>","met":true,"evidence":"<файл:строка или короткое обоснование>"}]}

Правила:
- criteria — строго по числу критериев приёмки, в том же порядке, text — дословный текст критерия;
- status="done" только если все criteria имеют met=true; иначе status="failed";
- evidence ≤ 200 символов, конкретно (файл:строка);
- задать вопрос пользователю невозможно: решай по данным из DATA.
`

// errVerifyContract marks a verifier answer that never satisfied the
// step_result.v1 contract; unlike unmet criteria this fails the subtask.
var errVerifyContract = errors.New("верификатор не ответил по контракту step_result.v1")

// Verifier is the read-only LLM judge that checks a finished subtask against
// its acceptance criteria. The verdict is computed by code: done requires
// every criterion met and exactly one criterion per acceptance item.
type Verifier struct {
	roles       port.RoleRunner
	meter       *MeteredLLM
	ws          port.Workspace
	jsonRetries int
	onAskUser   string
	fileMax     int
	totalMax    int
	logf        func(string, ...any)
}

// NewVerifier assembles the verifier from the loop config.
func NewVerifier(roles port.RoleRunner, meter *MeteredLLM, ws port.Workspace, cfg Config, logf func(string, ...any)) *Verifier {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Verifier{
		roles:       roles,
		meter:       meter,
		ws:          ws,
		jsonRetries: cfg.JSONRetries,
		onAskUser:   cfg.OnAskUser,
		fileMax:     cfg.VerifyFileMaxBytes,
		totalMax:    cfg.VerifyTotalMaxBytes,
		logf:        logf,
	}
}

// Verify checks the subtask's acceptance against dir (the task directory of
// its last attempt). It returns the unmet criterion texts when the answer is
// contract-valid but the criteria are not all met; those texts are the
// feedback the next attempt receives.
func (v *Verifier) Verify(ctx context.Context, st *domain.Subtask, dir string) ([]string, error) {
	blocks := v.blocks(st, dir)
	task := verifyTask
	for r := 0; r <= v.jsonRetries; r++ {
		if err := v.meter.gate(ctx); err != nil {
			return nil, err
		}
		res, err := v.roles.Run(ctx, port.RoleRequest{
			Profile:    ProfileVerifier,
			Task:       task,
			DataBlocks: blocks,
			Roots:      domain.WorkRoots{Read: dir, Write: ""}, // read-only judge
		})
		if err != nil {
			return nil, fmt.Errorf("верификатор: %w", err)
		}
		if res.GuardBlocked {
			return nil, fmt.Errorf("верификатор: ответ заблокирован guard'ом: %s", res.GuardReason)
		}
		if loc := askUserRe.FindString(res.Content); loc != "" {
			if v.onAskUser == AskFail {
				return nil, fmt.Errorf("%w: вопрос пользователю в полностью автономном цикле", errAskUser)
			}
			v.logf("[loop] ask_user подавлен (policy=decide)")
			task = verifyTask + "\n\nВопросы пользователю недоступны (предыдущий ответ отклонён: вопрос пользователю). Ответь заново целиком, решая по данным из DATA."
			continue
		}
		sr, err := contracts.DecodeStrict[contracts.StepResult, *contracts.StepResult](res.Content)
		if err == nil {
			err = sr.Validate()
		}
		if err == nil && sr.SubtaskID != st.ID {
			err = fmt.Errorf("subtask_id: ожидается %q, получено %q", st.ID, sr.SubtaskID)
		}
		if err == nil {
			if sr.Status == contracts.StepStatusDone &&
				len(sr.Criteria) == len(st.Acceptance) &&
				len(sr.Unmet()) == 0 {
				return nil, nil
			}
			return sr.Unmet(), nil
		}
		task = verifyTask + "\n\nПредыдущий ответ отклонён валидатором: " + err.Error() +
			". Исправь ответ и повтори его целиком."
	}
	return nil, fmt.Errorf("%w после %d попыток", errVerifyContract, v.jsonRetries+1)
}

// blocks builds the verifier's DATA blocks: the acceptance criteria first,
// then the change list and the changed files with line numbers, bounded per
// file and in total.
func (v *Verifier) blocks(st *domain.Subtask, dir string) []domain.DataBlock {
	var b strings.Builder
	fmt.Fprintf(&b, "Подзадача: %s (%s)\nКритерии приёмки:\n", st.ID, st.Title)
	for i, a := range st.Acceptance {
		fmt.Fprintf(&b, "%d. %s\n", i+1, a)
	}
	blocks := []domain.DataBlock{{Label: "acceptance", Content: b.String()}}

	changed := lastChanged(st)
	if len(changed) == 0 {
		return blocks
	}
	blocks = append(blocks, domain.DataBlock{Label: "changes", Content: "Изменённые файлы: " + strings.Join(changed, ", ")})
	total := 0
	for _, rel := range changed {
		left := v.totalMax - total
		if left <= 0 {
			blocks = append(blocks, domain.DataBlock{Label: "truncated", Content: "[…оставшиеся файлы не показаны: превышен суммарный лимит…]"})
			break
		}
		maxPerFile := v.fileMax
		if left < maxPerFile {
			maxPerFile = left
		}
		text, _, err := v.ws.ReadNumbered(dir, rel, maxPerFile)
		if err != nil {
			blocks = append(blocks, domain.DataBlock{Label: "file-" + rel, Content: "[файл не прочитан: " + err.Error() + "]"})
			continue
		}
		blocks = append(blocks, domain.DataBlock{Label: "file-" + rel, Content: text})
		total += len(text)
	}
	return blocks
}

// lastChanged returns the changed file list of the subtask's last attempt.
func lastChanged(st *domain.Subtask) []string {
	if len(st.Attempts) == 0 {
		return nil
	}
	return st.Attempts[len(st.Attempts)-1].Changed
}
