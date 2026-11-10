package sast

import (
	"os"
	"testing"

	"ai-adv-agent/internal/domain"
	"ai-adv-agent/internal/port"
)

// Golden tests for the gosec/eslint report mappers: the mapping rules are
// fixed by the plan (PR6), so the expected findings are listed explicitly.

func loadGolden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("testdata/%s: %v", name, err)
	}
	return string(b)
}

func findingByRule(t *testing.T, fs []domain.Finding, rule string) domain.Finding {
	t.Helper()
	for _, f := range fs {
		if f.Rule == rule {
			return f
		}
	}
	t.Fatalf("находка %q не найдена в %+v", rule, fs)
	return domain.Finding{}
}

func TestParseGosecGolden(t *testing.T) {
	res := port.CommandResult{ExitCode: 0, Stdout: loadGolden(t, "gosec.json")}
	fs, err := parseGosec(res, DefaultContainerWorkdir)
	if err != nil {
		t.Fatalf("parseGosec: %v", err)
	}
	if len(fs) != 4 {
		t.Fatalf("находок: %d, ожидалось 4", len(fs))
	}

	// Plain HIGH maps 1:1.
	high := findingByRule(t, fs, "G501")
	if high.Severity != domain.SeverityHigh || high.CWE != "CWE-326" {
		t.Errorf("G501: %+v", high)
	}
	if high.File != "crypto.go" || high.Line != 4 {
		t.Errorf("G501: путь не нормализован: %s:%d", high.File, high.Line)
	}
	if high.Source != domain.SourceGosec {
		t.Errorf("G501: источник %q", high.Source)
	}

	// G201 with non-LOW confidence escalates MEDIUM → HIGH.
	g201 := findingByRule(t, fs, "G201")
	if g201.Severity != domain.SeverityHigh || g201.CWE != "CWE-89" {
		t.Errorf("G201 (HIGH confidence) должна подниматься до high: %+v", g201)
	}
	if g201.File != "db/handler.go" || g201.Line != 12 {
		t.Errorf("G201: путь не нормализован: %s:%d", g201.File, g201.Line)
	}

	// The same rule with LOW confidence stays MEDIUM.
	g202 := findingByRule(t, fs, "G202")
	if g202.Severity != domain.SeverityMedium {
		t.Errorf("G202 (LOW confidence) не должна эскалироваться: %+v", g202)
	}

	// LOW maps 1:1.
	g304 := findingByRule(t, fs, "G304")
	if g304.Severity != domain.SeverityLow || g304.CWE != "CWE-22" {
		t.Errorf("G304: %+v", g304)
	}
}

func TestParseESLintGolden(t *testing.T) {
	// Exit 1 is the normal "lint errors found" outcome.
	res := port.CommandResult{ExitCode: 1, Stdout: loadGolden(t, "eslint.json")}
	fs, err := parseESLint(res, DefaultContainerWorkdir)
	if err != nil {
		t.Fatalf("parseESLint: %v", err)
	}
	if len(fs) != 6 {
		t.Fatalf("находок: %d, ожидалось 6", len(fs))
	}

	want := []struct {
		rule     string
		severity domain.Severity
		cwe      string
		file     string
	}{
		{"vue/no-v-html", domain.SeverityHigh, "CWE-79", "src/app.vue"},
		{"security/detect-eval-with-expression", domain.SeverityHigh, "CWE-95", "server.js"},
		{"security/detect-child-process", domain.SeverityHigh, "CWE-78", "server.js"},
		{"security/detect-non-literal-require", domain.SeverityHigh, "CWE-94", "server.js"},
		{"security/detect-object-injection", domain.SeverityMedium, "CWE-915", "src/app.vue"},
		// A message without a rule id is a suppressed linter (eslint-disable):
		// itself a finding.
		{"inline-config", domain.SeverityMedium, "CWE-710", "README.md.js"},
	}
	for _, w := range want {
		f := findingByRule(t, fs, w.rule)
		if f.Severity != w.severity || f.CWE != w.cwe || f.File != w.file {
			t.Errorf("%s: получено %+v, ожидалось %s %s %s", w.rule, f, w.severity, w.cwe, w.file)
		}
		if f.Source != domain.SourceESLint {
			t.Errorf("%s: источник %q", w.rule, f.Source)
		}
	}
}

func TestParseGosecFailsClosed(t *testing.T) {
	// "Golang errors" mean some code was never analysed: fail-closed.
	res := port.CommandResult{ExitCode: 0, Stdout: `{"Golang errors": {"/tmp/work/broken.go": {}} , "Issues": []}`}
	if _, err := parseGosec(res, DefaultContainerWorkdir); err == nil {
		t.Fatalf("ожидалась ошибка при Golang errors")
	}
	if _, err := parseGosec(port.CommandResult{ExitCode: 0, Stdout: "не JSON"}, DefaultContainerWorkdir); err == nil {
		t.Fatalf("ожидалась ошибка при невалидном JSON")
	}
}

func TestParseESLintFailsClosed(t *testing.T) {
	if _, err := parseESLint(port.CommandResult{ExitCode: 2, Stdout: "[]"}, DefaultContainerWorkdir); err == nil {
		t.Fatalf("ожидалась ошибка при exit 2")
	}
	fatal := `[{"filePath":"/tmp/work/a.js","messages":[{"ruleId":null,"message":"Parsing error","line":1,"fatal":true}],"errorCount":1}]`
	if _, err := parseESLint(port.CommandResult{ExitCode: 1, Stdout: fatal}, DefaultContainerWorkdir); err == nil {
		t.Fatalf("ожидалась ошибка при fatal-сообщении")
	}
}
