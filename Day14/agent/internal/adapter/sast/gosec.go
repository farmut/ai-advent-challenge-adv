package sast

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// gosecReport is the subset of `gosec -fmt=json` the scanner reads.
type gosecReport struct {
	GolangErrors map[string]json.RawMessage `json:"Golang errors"`
	Issues       *[]gosecIssue              `json:"Issues"`
}

type gosecIssue struct {
	Severity   string `json:"severity"`
	Confidence string `json:"confidence"`
	CWE        struct {
		ID string `json:"id"`
	} `json:"cwe"`
	RuleID  string `json:"rule_id"`
	Details string `json:"details"`
	File    string `json:"file"`
	Code    string `json:"code"`
	Line    string `json:"line"`
}

// gosecEscalate lists rules raised to High unless gosec's own confidence is
// LOW: injection and hard-coded credentials are exploitable as soon as they
// are real.
var gosecEscalate = map[string]bool{
	"G101": true, // hard-coded credentials
	"G201": true, // SQL string formatting
	"G202": true, // SQL string concatenation
	"G204": true, // subprocess launched with variable
}

// gosecFix is the code-side fix hint per rule (gosec reports none).
var gosecFix = map[string]string{
	"G101": "убери секрет из кода, читай значение из переменной окружения",
	"G107": "не строй URL запроса из пользовательского ввода, проверяй его по allowlist",
	"G112": "задай ReadHeaderTimeout у http.Server",
	"G114": "используй http.Server с таймаутами вместо http.ListenAndServe",
	"G201": "используй параметризованный запрос с плейсхолдерами ($1 или ?) вместо fmt.Sprintf",
	"G202": "используй параметризованный запрос с плейсхолдерами ($1 или ?) вместо конкатенации",
	"G204": "передавай фиксированную программу и argv без шелла, не подставляй ввод в команду",
	"G304": "очисти путь filepath.Clean и проверь, что он внутри разрешённого корня",
	"G401": "используй sha256 (или bcrypt/argon2 для паролей) вместо md5/sha1",
	"G402": "убери InsecureSkipVerify и проверяй сертификат",
	"G404": "используй crypto/rand вместо math/rand",
	"G501": "не импортируй crypto/md5, используй sha256",
	"G505": "не импортируй crypto/sha1, используй sha256",
}

// parseGosec maps a gosec JSON report. The run uses -no-fail, so any exit
// code is accepted as long as the report is valid; "Golang errors" mean some
// code was not analysed, which fails the scan (fail-closed).
func parseGosec(res port.CommandResult, workdir string) ([]domain.Finding, error) {
	var rep gosecReport
	if err := json.Unmarshal([]byte(strings.TrimSpace(res.Stdout)), &rep); err != nil {
		return nil, fmt.Errorf("невалидный JSON gosec: %v", err)
	}
	if rep.Issues == nil {
		return nil, fmt.Errorf("в отчёте gosec нет поля Issues")
	}
	if len(rep.GolangErrors) > 0 {
		files := make([]string, 0, len(rep.GolangErrors))
		for f := range rep.GolangErrors {
			files = append(files, relPath(f, workdir))
		}
		return nil, fmt.Errorf("gosec не смог проанализировать файлы %v", files)
	}
	out := make([]domain.Finding, 0, len(*rep.Issues))
	for i, is := range *rep.Issues {
		sev, err := gosecSeverity(is)
		if err != nil {
			return nil, fmt.Errorf("Issues[%d]: %v", i, err)
		}
		line, err := firstInt(is.Line)
		if err != nil {
			return nil, fmt.Errorf("Issues[%d]: line %q: %v", i, is.Line, err)
		}
		f := domain.Finding{
			ID:       fmt.Sprintf("gosec-%d", i+1),
			Severity: sev,
			CWE:      cweOf(is.CWE.ID),
			File:     relPath(is.File, workdir),
			Line:     line,
			Rule:     is.RuleID,
			Issue:    cut(is.Details, 300),
			Fix:      gosecFix[is.RuleID],
			Source:   domain.SourceGosec,
		}
		// G101 evidence would be the secret itself.
		if is.RuleID != "G101" {
			f.Evidence = evidenceLine(is.Code, line)
		}
		out = append(out, f)
	}
	return out, nil
}

func gosecSeverity(is gosecIssue) (domain.Severity, error) {
	var sev domain.Severity
	switch strings.ToUpper(is.Severity) {
	case "HIGH":
		sev = domain.SeverityHigh
	case "MEDIUM":
		sev = domain.SeverityMedium
	case "LOW":
		sev = domain.SeverityLow
	default:
		return "", fmt.Errorf("неизвестная severity %q", is.Severity)
	}
	if gosecEscalate[is.RuleID] && !strings.EqualFold(is.Confidence, "LOW") && sev.Rank() < domain.SeverityHigh.Rank() {
		sev = domain.SeverityHigh
	}
	return sev, nil
}

// cweOf renders gosec's numeric CWE id; a rule without one gets CWE-710
// (improper adherence to coding standards).
func cweOf(id string) string {
	id = strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(id)), "CWE-")
	if n, err := strconv.Atoi(id); err == nil && n > 0 {
		return "CWE-" + strconv.Itoa(n)
	}
	return "CWE-710"
}

// firstInt parses "12" or the start of a range "12-14".
func firstInt(s string) (int, error) {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '-'); i > 0 {
		s = s[:i]
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if n < 1 {
		return 0, fmt.Errorf("номер строки < 1")
	}
	return n, nil
}

// evidenceLine picks the reported line out of gosec's "N: code" snippet.
func evidenceLine(code string, line int) string {
	prefix := strconv.Itoa(line) + ":"
	for _, l := range strings.Split(code, "\n") {
		if strings.HasPrefix(l, prefix) {
			return cut(strings.TrimSpace(strings.TrimPrefix(l, prefix)), 200)
		}
	}
	return ""
}
