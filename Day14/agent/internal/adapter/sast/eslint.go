package sast

import (
	"encoding/json"
	"fmt"
	"strings"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// eslintFile is the subset of one `eslint -f json` entry the scanner reads.
// The report also carries "source" — the complete text of every file with
// messages. It is deliberately not a field here: file contents must never
// travel into findings, state or reports.
type eslintFile struct {
	FilePath string          `json:"filePath"`
	Messages []eslintMessage `json:"messages"`
}

type eslintMessage struct {
	RuleID  *string `json:"ruleId"`
	Message string  `json:"message"`
	Line    int     `json:"line"`
	Fatal   bool    `json:"fatal"`
}

// eslintRule is how one rule maps onto a finding.
type eslintRule struct {
	sev domain.Severity
	cwe string
	fix string
}

// eslintRules: the rules that execute or inject code are High; the rest of
// eslint-plugin-security is defence in depth (Medium). See README table.
var eslintRules = map[string]eslintRule{
	"security/detect-eval-with-expression":           {domain.SeverityHigh, "CWE-95", "не вызывай eval/new Function с данными, используй явный разбор"},
	"security/detect-child-process":                  {domain.SeverityHigh, "CWE-78", "не запускай процессы из фронтенд-кода; argv без шелла и без ввода пользователя"},
	"security/detect-non-literal-require":            {domain.SeverityHigh, "CWE-94", "используй статический import вместо require с вычисляемым путём"},
	"vue/no-v-html":                                  {domain.SeverityHigh, "CWE-79", "замени v-html на текстовую интерполяцию {{ }} или санитизируй HTML"},
	"security/detect-bidi-characters":                {domain.SeverityMedium, "CWE-1007", "удали управляющие bidi-символы из исходника"},
	"security/detect-buffer-noassert":                {domain.SeverityMedium, "CWE-125", "не отключай проверку границ Buffer"},
	"security/detect-disable-mustache-escape":        {domain.SeverityMedium, "CWE-79", "не отключай экранирование шаблонов"},
	"security/detect-new-buffer":                     {domain.SeverityMedium, "CWE-119", "используй Buffer.from/Buffer.alloc"},
	"security/detect-no-csrf-before-method-override": {domain.SeverityMedium, "CWE-352", "подключай CSRF-защиту до methodOverride"},
	"security/detect-non-literal-fs-filename":        {domain.SeverityMedium, "CWE-22", "не передавай в fs путь из ввода без нормализации и проверки корня"},
	"security/detect-non-literal-regexp":             {domain.SeverityMedium, "CWE-1333", "не строй RegExp из ввода пользователя"},
	"security/detect-object-injection":               {domain.SeverityMedium, "CWE-915", "проверяй ключ по allowlist или используй Map"},
	"security/detect-possible-timing-attacks":        {domain.SeverityMedium, "CWE-208", "сравнивай секреты за постоянное время"},
	"security/detect-pseudoRandomBytes":              {domain.SeverityMedium, "CWE-338", "используй crypto.getRandomValues/randomBytes"},
	"security/detect-unsafe-regex":                   {domain.SeverityMedium, "CWE-1333", "перепиши регулярное выражение без вложенных квантификаторов"},
}

// parseESLint maps an eslint JSON report. Exit 0 (clean) and 1 (lint
// errors) are normal; anything else is a crash or a config error. A fatal
// message (file not parsed) means the file was not analysed: fail-closed.
func parseESLint(res port.CommandResult, workdir string) ([]domain.Finding, error) {
	if res.ExitCode != 0 && res.ExitCode != 1 {
		return nil, fmt.Errorf("eslint завершился с кодом %d", res.ExitCode)
	}
	var files []eslintFile
	if err := json.Unmarshal([]byte(strings.TrimSpace(res.Stdout)), &files); err != nil {
		return nil, fmt.Errorf("невалидный JSON eslint: %v", err)
	}
	var out []domain.Finding
	for _, f := range files {
		rel := relPath(f.FilePath, workdir)
		for _, m := range f.Messages {
			if m.Fatal {
				return nil, fmt.Errorf("eslint не разобрал %s:%d", rel, m.Line)
			}
			fd := domain.Finding{
				ID:     fmt.Sprintf("eslint-%d", len(out)+1),
				File:   rel,
				Line:   m.Line,
				Issue:  cut(m.Message, 300),
				Source: domain.SourceESLint,
			}
			switch {
			case m.RuleID == nil:
				// With --no-inline-config an eslint-disable comment is
				// reported without a rule: suppressing the linter is
				// itself a finding.
				fd.Severity, fd.CWE, fd.Rule = domain.SeverityMedium, "CWE-710", "inline-config"
				fd.Fix = "убери комментарий eslint-disable и исправь первопричину"
			default:
				fd.Rule = *m.RuleID
				if r, ok := eslintRules[fd.Rule]; ok {
					fd.Severity, fd.CWE, fd.Fix = r.sev, r.cwe, r.fix
				} else if strings.HasPrefix(fd.Rule, "security/") {
					fd.Severity, fd.CWE = domain.SeverityMedium, "CWE-710"
				} else {
					fd.Severity, fd.CWE = domain.SeverityLow, "CWE-710"
				}
			}
			if fd.Line < 1 {
				fd.Line = 1
			}
			out = append(out, fd)
		}
	}
	return out, nil
}
