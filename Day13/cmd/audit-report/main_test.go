package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agile-consultant/internal/auditreport"
)

func TestRunFixture(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "r.json")
	var out bytes.Buffer
	if err := run([]string{"--audit", "testdata/*.jsonl", "--json", jsonPath}, &out); err != nil {
		t.Fatal(err)
	}
	md := out.String()
	for _, want := range []string{
		"Records: **3**",
		"| ok | 2 |",
		"| input_blocked | 1 |",
		"Cost: **$0.000120**",
		"estimated 1",
		"| input | aws_access_key | block | 1 |",
		"| output | suspicious_url | redact | 1 |",
		"мой ключ [REDACTED_AWS_KEY] для теста \\| ok",
		"decoded (derived)",
		"| `f00000000001` | aws_access_key | 2: r1, r2 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown lacks %q", want)
		}
	}
	var rep auditreport.Report
	b, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Records != 3 || len(rep.Intercepted) != 4 || rep.Tokens.EstimatedShare != 0.5 {
		t.Fatalf("json report %+v", rep.Tokens)
	}
}

func TestRunErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{{"no audit", nil}, {"no match", []string{"--audit", "testdata/none-*.jsonl"}}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := run(tt.args, &bytes.Buffer{}); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
