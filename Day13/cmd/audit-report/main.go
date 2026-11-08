// Command audit-report aggregates llm-proxy audit logs (JSONL) into a
// markdown report: totals, tokens and cost, findings, intercepted secrets
// (masked context only) and fingerprint repeats.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"agile-consultant/internal/auditreport"
)

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "audit-report: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("audit-report", flag.ContinueOnError)
	var audits multi
	fs.Var(&audits, "audit", "audit JSONL path or glob (repeatable)")
	out := fs.String("out", "", "markdown output file (default stdout)")
	jsonOut := fs.String("json", "", "optional JSON output file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(audits) == 0 {
		return errors.New("--audit is required")
	}
	recs, files, err := auditreport.Read(audits)
	if err != nil {
		return err
	}
	rep := auditreport.Build(recs, files)
	md := auditreport.Markdown(rep)
	if *out == "" {
		if _, err := io.WriteString(stdout, md); err != nil {
			return err
		}
	} else if err := os.WriteFile(*out, []byte(md), 0o600); err != nil {
		return err
	}
	if *jsonOut != "" {
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(*jsonOut, b, 0o600); err != nil {
			return err
		}
	}
	return nil
}
