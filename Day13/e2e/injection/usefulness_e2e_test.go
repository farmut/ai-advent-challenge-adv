//go:build e2e

package injection

import (
	"agile-consultant/internal/report/htmlkit"
	"context"
	"path/filepath"
	"testing"
	"time"

	"agile-consultant/internal/adapters/mlx"
	"agile-consultant/internal/config"
	"agile-consultant/internal/usecase"
)

// TestUsefulness guards against over-refusal and guard false positives:
// legitimate requests (including HTML in code, emoji ZWJ, NBSP/soft hyphen
// and an attached document) must be answered in the 4-field format, without
// a block and without a refusal, in both guard modes. Results are written to
// <E2E_OUT>/usefulness.json for the summary report.
func TestUsefulness(t *testing.T) {
	cases, err := LoadUsefulnessCases(filepath.Join("testdata", "usefulness.json"))
	if err != nil {
		t.Fatal(err)
	}
	inference := &config.Inference{Temperature: 0, TopP: 1, MaxTokens: 512}
	client := mlx.NewClient(env.baseURL, env.modelPath, inference, env.logger)
	consultant, err := newConsultant(client)
	if err != nil {
		t.Fatal(err)
	}

	run := UsefulnessRun{Guard: env.guard, StartedAt: time.Now()}
	t.Cleanup(func() {
		if _, err := WriteUsefulnessJSON(env.outDir, run); err != nil {
			t.Errorf("usefulness.json: %v", err)
		}
	})

	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), caseTimeout)
			defer cancel()
			resp, err := consultant.Ask(ctx, usecase.Request{Question: c.Question, Context: c.Context})
			r := EvaluateUsefulness(c, resp, err)
			run.Cases = append(run.Cases, r)
			t.Logf("%s guard=%s blocked=%v stage=%s reasons=%v format4=%v refused=%v on_topic=%v",
				c.ID, env.guard, r.Blocked, r.BlockStage, r.Reasons, r.Format4, r.Refused, r.OnTopic)
			if r.Error != "" {
				t.Fatalf("%s: ask: %s", c.ID, r.Error)
			}
			if !r.OK {
				t.Errorf("%s (%s) answered badly: blocked=%v stage=%s reasons=%v format4=%v refused=%v\n%s",
					c.ID, c.Title, r.Blocked, r.BlockStage, r.Reasons, r.Format4, r.Refused, htmlkit.RevealInvisible(r.Answer))
			}
		})
	}
}
