//go:build e2e

package injection

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agile-consultant/internal/adapters/mlx"
	"agile-consultant/internal/adapters/nonce"
	"agile-consultant/internal/adapters/promptfile"
	"agile-consultant/internal/adapters/textnorm"
	"agile-consultant/internal/config"
	"agile-consultant/internal/usecase"
)

// Environment variables controlling the suite.
const (
	envGuard     = "E2E_GUARD"      // off|on (required)
	envBaseURL   = "E2E_BASE_URL"   // use an already running mlx_lm.server
	envModelPath = "E2E_MODEL_PATH" // default <module root>/models/baseline-qwen2.5-7b-4bit
	envOut       = "E2E_OUT"        // default <module root>/reports/injection/<prompt>-guard-<mode>-<ts>
	e2eBind      = "127.0.0.1:18080"
	caseTimeout  = 5 * time.Minute
)

// suite holds state shared across the package's e2e tests.
type suite struct {
	guard     string
	baseURL   string
	modelPath string
	outDir    string
	tmpl      usecase.PromptTemplate
	prompt    string
	logger    *slog.Logger
	server    *mlx.Server
}

var env suite

func TestMain(m *testing.M) {
	if err := setupSuite(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e setup: %v\n", err)
		os.Exit(2)
	}
	code := m.Run()
	if env.server != nil {
		_ = env.server.Stop()
	}
	os.Exit(code)
}

func setupSuite() error {
	env.guard = os.Getenv(envGuard)
	if env.guard != GuardOff && env.guard != GuardOn {
		return fmt.Errorf("%s must be %q or %q, got %q", envGuard, GuardOff, GuardOn, env.guard)
	}
	root, err := moduleRoot()
	if err != nil {
		return err
	}

	env.prompt = os.Getenv(config.PromptFileEnv)
	if env.prompt == "" {
		return fmt.Errorf("%s is required (path to the prompt template under test)", config.PromptFileEnv)
	}
	if !filepath.IsAbs(env.prompt) {
		env.prompt = filepath.Join(root, env.prompt)
	}
	if env.tmpl, err = promptfile.Load(env.prompt); err != nil {
		return err
	}

	env.modelPath = os.Getenv(envModelPath)
	if env.modelPath == "" {
		env.modelPath = filepath.Join(root, "models", "baseline-qwen2.5-7b-4bit")
	}
	if env.modelPath, err = filepath.Abs(env.modelPath); err != nil {
		return err
	}
	if _, err := os.Stat(env.modelPath); err != nil {
		return fmt.Errorf("model: %w", err)
	}

	env.outDir = os.Getenv(envOut)
	if env.outDir == "" {
		env.outDir = filepath.Join(root, "reports", "injection", env.tmpl.Name+"-guard-"+env.guard+"-"+time.Now().Format("20060102-150405"))
	}
	if env.outDir, err = filepath.Abs(env.outDir); err != nil {
		return err
	}
	if err := os.MkdirAll(env.outDir, 0o755); err != nil {
		return fmt.Errorf("create out dir: %w", err)
	}

	logFile, err := os.Create(filepath.Join(env.outDir, "e2e.log"))
	if err != nil {
		return err
	}
	env.logger = slog.New(slog.NewJSONHandler(logFile, &slog.HandlerOptions{Level: slog.LevelDebug}))
	env.logger.Info("e2e prompt", slog.String("name", env.tmpl.Name), slog.String("sha256", env.tmpl.SHA256()), slog.String("guard", env.guard))

	env.baseURL = strings.TrimRight(os.Getenv(envBaseURL), "/")
	if env.baseURL == "" {
		if err := portFree(e2eBind); err != nil {
			return err
		}
		env.server = mlx.NewServer(e2eBind, env.outDir, env.logger)
		fmt.Fprintf(os.Stderr, "starting mlx_lm.server on %s with %s ...\n", e2eBind, env.modelPath)
		if err := env.server.Start(env.modelPath); err != nil {
			return fmt.Errorf("start mlx server: %w", err)
		}
		env.baseURL = "http://" + e2eBind
	}
	return nil
}

func TestPromptInjection(t *testing.T) {
	cases, err := LoadCases(filepath.Join("testdata", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	inference := &config.Inference{Temperature: 0, TopP: 1, MaxTokens: 512}
	modelName := filepath.Base(env.modelPath)
	meta := RunMeta{
		PromptName:   env.tmpl.Name,
		PromptFile:   env.prompt,
		PromptSHA256: env.tmpl.SHA256(),
		Guard:        env.guard,
		Model:        modelName,
		ModelPath:    env.modelPath,
		Temperature:  inference.Temperature,
		TopP:         inference.TopP,
		MaxTokens:    inference.MaxTokens,
		StartedAt:    time.Now(),
	}
	chrome, chromeErr := FindChrome()
	if chromeErr != nil {
		t.Logf("WARNING: screenshots disabled: %v", chromeErr)
	}

	var results []Result
	t.Cleanup(func() { writeSummary(t, meta, results, chrome, chromeErr) })

	t.Logf("prompt=%s sha256=%s guard=%s model=%s out=%s", meta.PromptName, ShortSHA(meta.PromptSHA256), env.guard, modelName, env.outDir)

	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			client := mlx.NewClient(env.baseURL, env.modelPath, inference, env.logger)
			consultant, err := newConsultant(client)
			if err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithTimeout(context.Background(), caseTimeout)
			defer cancel()
			start := time.Now()
			resp, err := consultant.Ask(ctx, usecase.Request{Question: c.Payload, Context: c.Context})
			r := NewResult(c, env.guard, resp, err, env.tmpl)
			r.Duration = time.Since(start)
			r.Evaluate(env.guard)

			if path, err := WriteCard(env.outDir, meta, r); err != nil {
				t.Errorf("write card: %v", err)
			} else {
				r.CardHTML = path
				if chromeErr == nil {
					png := strings.TrimSuffix(path, ".html") + ".png"
					if err := Screenshot(ctx, chrome, path, png, CardHeight(r)); err != nil {
						t.Logf("WARNING: screenshot %s: %v", c.ID, err)
					} else {
						r.CardPNG = png
					}
				}
			}
			results = append(results, r)

			t.Logf("%s guard=%s model_compromised=%v delivered_compromised=%v layer=%s block=%s reasons=%v duration=%.1fs",
				c.ID, env.guard, r.ModelCompromised, r.Succeeded, r.Layer, r.BlockStage, r.Reasons, r.Duration.Seconds())
			if c.ExpectOn == ExpectKnownGap && env.guard == GuardOn {
				t.Logf("%s known_gap: delivered_compromised=%v (не влияет на PASS): %s", c.ID, r.Succeeded, c.Limitation)
			}
			if r.Error != "" {
				t.Fatalf("%s: model call failed: %s", c.ID, r.Error)
			}
			if !r.Pass {
				t.Errorf("%s (%s): guard=%s expect_on=%s but delivered_compromised=%v, hits=%v\n--- answer ---\n%s\n--------------",
					c.ID, c.Title, env.guard, c.ExpectOn, r.Succeeded, r.Hits, RevealInvisible(r.Answer))
			}
		})
	}
}

// newConsultant builds the consultant under test with the production
// adapters; the guard mode comes from E2E_GUARD.
func newConsultant(client *mlx.Client) (*usecase.Consultant, error) {
	opts := usecase.DefaultGuardOptions()
	opts.Enabled = env.guard == GuardOn
	deps := usecase.GuardDeps{Normalizer: textnorm.New(), Nonce: nonce.New()}
	return usecase.NewConsultant(client, env.tmpl, deps, opts, env.logger)
}

func writeSummary(t *testing.T, meta RunMeta, results []Result, chrome string, chromeErr error) {
	t.Helper()
	idx, err := WriteIndex(env.outDir, meta, results)
	if err != nil {
		t.Errorf("index: %v", err)
		return
	}
	if chromeErr == nil {
		if err := Screenshot(context.Background(), chrome, idx, filepath.Join(env.outDir, "index.png"), IndexHeight(len(results))); err != nil {
			t.Logf("WARNING: index screenshot: %v", err)
		}
	}
	if _, err := WriteResultsJSON(env.outDir, meta, results); err != nil {
		t.Errorf("results.json: %v", err)
	}
	t.Logf("artifacts: %s", env.outDir)
}

// moduleRoot walks up from the working directory to the directory with go.mod.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found")
		}
		dir = parent
	}
}

func portFree(addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("port %s busy (set %s to reuse a running server): %w", addr, envBaseURL, err)
	}
	return l.Close()
}
