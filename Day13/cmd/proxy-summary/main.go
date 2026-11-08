// Command proxy-summary builds the combined Day13 guard-proxy report from
// the latest complete e2e run of every upstream found in meta.upstream
// (fake and mlx are always listed, then e.g. openai-compat):
// proxy-summary.html, proxy-summary-matrix.html, proxy-summary.png and
// proxy-summary.json. Runs of an older report schema or cases.json are
// marked STALE, runs whose go test failed are marked FAILED.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agile-consultant/e2e/proxy"
	"agile-consultant/internal/report/htmlkit"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "proxy-summary: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("proxy-summary", flag.ContinueOnError)
	runsDir := fs.String("runs", "reports/proxy", "directory with e2e run directories (results.json)")
	casesPath := fs.String("cases", "e2e/proxy/testdata/cases.json", "path to cases.json")
	outDir := fs.String("out", "", "output directory (required)")
	copyTo := fs.String("copy-to", "", "optional directory the output files are copied into (other files are kept)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *outDir == "" {
		return errors.New("--out is required")
	}
	cases, err := proxy.LoadCases(*casesPath)
	if err != nil {
		return err
	}
	casesSHA, err := proxy.FileSHA256(*casesPath)
	if err != nil {
		return err
	}
	latest, warns := proxy.SelectLatest(*runsDir, cases, casesSHA)
	for _, w := range warns {
		fmt.Fprintf(stderr, "warning: skip %v\n", w)
	}
	if len(latest) == 0 {
		return fmt.Errorf("no complete runs found in %s", *runsDir)
	}
	ups := proxy.UpstreamOrder(latest)
	for _, up := range ups {
		r := latest[up]
		switch {
		case r == nil:
			fmt.Fprintf(stdout, "%s: no run\n", up)
		default:
			status := "current"
			if r.Stale {
				status = "STALE: " + r.StaleReason
			}
			if r.Failed {
				status += ", FAILED"
			}
			fmt.Fprintf(stdout, "%s: %s (%s)\n", up, filepath.Base(r.Dir), status)
		}
	}
	files, err := proxy.WriteSummary(*outDir, proxy.PageInput{
		Title: "Day13 guard-proxy · общий отчёт (" + strings.Join(ups, " + ") + ")", Upstreams: ups, Runs: latest, Cases: cases, Generated: time.Now(),
	}, casesSHA)
	if err != nil {
		return err
	}
	for _, f := range files {
		fmt.Fprintln(stdout, "wrote:", f)
	}
	if *copyTo != "" {
		names := map[string]string{}
		for _, f := range files {
			names[f] = filepath.Base(f)
		}
		if err := htmlkit.CopyFiles(*copyTo, names); err != nil {
			return fmt.Errorf("copy to %s: %w", *copyTo, err)
		}
		fmt.Fprintln(stdout, "copied to:", *copyTo)
	}
	return nil
}
