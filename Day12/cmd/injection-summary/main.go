// Command injection-summary builds the prompt-injection report from the
// latest e2e run of every (prompt version, guard mode) pair: summary.html,
// summary-matrix.html (compact), summary.json and their PNG screenshots.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"agile-consultant/e2e/injection"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "injection-summary: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	runsDir := flag.String("runs", "reports/injection", "directory with e2e run directories")
	promptsDir := flag.String("prompts", "configs/prompts", "directory with prompt templates (<name>.txt)")
	casesPath := flag.String("cases", "e2e/injection/testdata/cases.json", "path to cases.json")
	outDir := flag.String("out", "", "output directory (required)")
	copyTo := flag.String("copy-to", "", "optional directory replaced with a copy of the output")
	flag.Parse()
	if *outDir == "" {
		return errors.New("--out is required")
	}

	cases, err := injection.LoadCases(*casesPath)
	if err != nil {
		return err
	}
	runs, warnings := injection.DiscoverRuns(*runsDir, cases)
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "warning: skip %v\n", w)
	}
	latest, err := injection.SelectLatest(runs, *promptsDir)
	if err != nil {
		return err
	}
	if len(latest) == 0 {
		return fmt.Errorf("no complete runs found in %s", *runsDir)
	}
	for _, r := range latest {
		status := "current"
		switch {
		case r.PromptMissing:
			status = "prompt missing"
		case r.Stale:
			status = "STALE sha"
		}
		fmt.Printf("%s: %s (%s)\n", r.Key(), filepath.Base(r.Dir), status)
	}

	s := injection.Summary{Cases: cases, Runs: latest, GeneratedAt: time.Now()}
	if err := injection.CopyCards(latest, *outDir); err != nil {
		return err
	}
	htmlPath, err := injection.WriteSummary(*outDir, s)
	if err != nil {
		return err
	}
	if chrome, err := injection.FindChrome(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: PNG screenshots skipped: %v\n", err)
	} else {
		shots := []struct {
			html, png string
			height    int
		}{
			{htmlPath, "summary.png", injection.SummaryHeight(s)},
			{filepath.Join(*outDir, injection.SummaryMatrixFile), "summary-matrix.png", injection.MatrixHeight(s)},
		}
		for _, sh := range shots {
			if err := injection.Screenshot(context.Background(), chrome, sh.html, filepath.Join(*outDir, sh.png), sh.height); err != nil {
				fmt.Fprintf(os.Stderr, "warning: %s: %v\n", sh.png, err)
			}
		}
	}
	fmt.Println("summary:", htmlPath)

	if *copyTo != "" {
		if err := safeCopyTarget(*copyTo, *outDir); err != nil {
			return err
		}
		if err := injection.CopyTree(*outDir, *copyTo); err != nil {
			return fmt.Errorf("copy to %s: %w", *copyTo, err)
		}
		fmt.Println("copied to:", *copyTo)
	}
	return nil
}

// safeCopyTarget refuses obviously dangerous --copy-to values, since the
// target directory is removed before copying.
func safeCopyTarget(target, out string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	outAbs, _ := filepath.Abs(out)
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()
	if abs == "/" || abs == home || abs == cwd || abs == outAbs || filepath.Dir(abs) == "/" {
		return fmt.Errorf("refusing to replace --copy-to %s", abs)
	}
	return nil
}
