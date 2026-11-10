package execloop

import (
	"fmt"
	"path/filepath"
	"strings"

	"ai-adv-agent/internal/domain"
)

// report renders the run's markdown report: the goal, the budget, every
// subtask with its attempts, and the security findings split into fixed
// (Critical/High of earlier attempts) and remaining (last attempts of
// subtasks that did not finish), plus the Medium/Low informational list.
func (l *Loop) report(run *domain.LoopRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Отчёт цикла выполнения\n\n")
	fmt.Fprintf(&b, "- Статус: **%s**\n", run.Status)
	fmt.Fprintf(&b, "- Цель: %s\n", run.Goal.Text)
	fmt.Fprintf(&b, "- Бюджет: вызовов LLM %d/%d, токенов %d/%d, $%.4f/$%.2f, репланов %d/%d\n",
		run.Usage.LLMCalls, run.Budget.MaxLLMCalls,
		run.Usage.Tokens, run.Budget.MaxTokens,
		run.Usage.CostUSD, run.Budget.MaxCostUSD,
		run.Usage.Replans, run.Budget.MaxReplans)
	if l.cfg.WorkspaceRoot != "" {
		fmt.Fprintf(&b, "- base: %s\n", filepath.Join(l.cfg.WorkspaceRoot, run.ID, "base"))
	}
	b.WriteString("\n## Подзадачи\n\n")
	for i := range run.Subtasks {
		st := &run.Subtasks[i]
		fmt.Fprintf(&b, "- %s **%s** (%s, профиль %s) — попыток: %d\n",
			statusIcon(st.Status), st.Title, st.ID, st.Profile, len(st.Attempts))
		for _, a := range st.Attempts {
			line := fmt.Sprintf("  - попытка %d: %s", a.N, attemptSummary(a))
			if a.Interrupted {
				line += " (прервана)"
			}
			b.WriteString(line + "\n")
		}
		if st.Status == domain.SubtaskFailed || st.Status == domain.SubtaskBlocked {
			if why := lastAttemptReason(st); why != "" {
				fmt.Fprintf(&b, "  - причина: %s\n", why)
			}
		}
	}

	fixed, remaining, info := collectFindings(run)
	b.WriteString("\n## Находки безопасности\n")
	writeFindingList(&b, "\n### Исправлены (Critical/High)\n", fixed)
	writeFindingList(&b, "\n### Остаются (Critical/High)\n", remaining)
	writeFindingList(&b, "\n### Medium/Low (информационно)\n", info)
	return b.String()
}

// statusIcon maps a subtask status to a report marker.
func statusIcon(s domain.SubtaskStatus) string {
	switch s {
	case domain.SubtaskDone:
		return "✅"
	case domain.SubtaskFailed:
		return "❌"
	case domain.SubtaskBlocked:
		return "⛔"
	case domain.SubtaskSkipped:
		return "⏭"
	default:
		return "▶️"
	}
}

// attemptSummary renders one attempt's outcome line.
func attemptSummary(a domain.Attempt) string {
	parts := []string{string(a.Stage)}
	if a.Result != "" {
		parts = append(parts, string(a.Result))
	}
	if len(a.Findings) > 0 {
		var sev []string
		for _, f := range a.Findings {
			sev = append(sev, fmt.Sprintf("%s %s", f.Severity, f.CWE))
		}
		parts = append(parts, "находки: "+strings.Join(sev, ", "))
	}
	if a.Error != "" {
		parts = append(parts, "ошибка: "+a.Error)
	}
	return strings.Join(parts, ", ")
}

// lastAttemptReason extracts the failure reason of the last attempt.
func lastAttemptReason(st *domain.Subtask) string {
	if n := len(st.Attempts); n > 0 {
		if why := st.Attempts[n-1].Error; why != "" {
			return why
		}
	}
	return ""
}

// collectFindings splits the run's findings: findings of earlier attempts are
// fixed by definition (the round that followed addressed them — or the
// subtask would not have moved on); findings of the last attempt of a
// subtask that did not finish remain. Non-blocking severities are
// informational either way.
func collectFindings(run *domain.LoopRun) (fixed, remaining, info []domain.Finding) {
	for i := range run.Subtasks {
		st := &run.Subtasks[i]
		for j, a := range st.Attempts {
			last := j == len(st.Attempts)-1
			for _, f := range a.Findings {
				switch {
				case !last:
					if f.Severity.Blocking() {
						fixed = append(fixed, f)
					} else {
						info = append(info, f)
					}
				case st.Status == domain.SubtaskDone:
					info = append(info, f)
				case f.Severity.Blocking():
					remaining = append(remaining, f)
				default:
					info = append(info, f)
				}
			}
		}
	}
	return fixed, remaining, info
}

// writeFindingList renders one findings section, or nothing when empty.
func writeFindingList(b *strings.Builder, header string, fs []domain.Finding) {
	if len(fs) == 0 {
		return
	}
	b.WriteString(header)
	for _, f := range fs {
		fmt.Fprintf(b, "- %s %s — %s:%d — %s\n", f.Severity, f.CWE, f.File, f.Line, f.Issue)
	}
}
