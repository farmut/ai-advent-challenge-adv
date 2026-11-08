//go:build e2e

package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"agile-consultant/internal/adapters/clock"
	"agile-consultant/internal/adapters/mlx"
	"agile-consultant/internal/app"
	"agile-consultant/internal/config"
	"agile-consultant/internal/ports"
	"agile-consultant/internal/report/htmlkit"
)

// Environment variables controlling the suite.
const (
	envUpstream  = "E2E_UPSTREAM"   // fake (default) | mlx | openai-compat
	envOut       = "E2E_OUT"        // default <root>/reports/proxy/<ts>-<upstream>
	envCopyTo    = "E2E_COPY_TO"    // copy report.md to <dir>/proxy-<upstream>.md
	envBaseURL   = "E2E_BASE_URL"   // mlx: running mlx_lm.server (http://host:port); openai-compat: required ".../v1"
	envModelPath = "E2E_MODEL_PATH" // mlx mode: model dir (default from configs/proxy.yaml)
	envModel     = "E2E_MODEL"      // openai-compat mode (required): upstream model id, e.g. qwen2.5:7b-instruct

	upstreamFake   = UpstreamFake
	upstreamMLX    = "mlx"
	upstreamCompat = "openai-compat"
	mlxBind        = "127.0.0.1:18081"
	e2ePepper      = "e2e-fixed-pepper-0123456789"
	// Synthetic proxy API keys (PROXY_API_KEYS); post sends e2eKey1.
	e2eKey1    = "e2e-proxy-key-one-7fQ2mX9pL4vR8sT1wZ6yB3nC"
	e2eKey2    = "e2e-proxy-key-two-K5hJ8dG2fA0qW7eR4tY1uI9oP"
	localModel = "qwen2.5-7b-local"
	paidModel  = "paid-fake"
)

var fixedNow = time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)

type suiteState struct {
	upstream string
	// baseURL is the openai-compat upstream (without userinfo) for the report.
	baseURL string
	// recBase is the recorder URL the proxy routes are pointed at.
	recBase   string
	root      string
	cfgPath   string
	outDir    string
	modelPath string
	fake      *FakeUpstream
	rec       *Recorder
	server    *mlx.Server
	cases     []Case
	started   time.Time

	mu          sync.Mutex
	results     []Result
	extras      []ExtraResult
	auditByMode map[string]string
	// extraAudits are the audit files of extra-test harnesses (rate limit,
	// auth, ...), used for the log examples of the HTML report.
	extraAudits []string
}

var st = suiteState{auditByMode: map[string]string{}}

func TestMain(m *testing.M) {
	if err := setup(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e setup: %v\n", err)
		teardown()
		os.Exit(2)
	}
	code := m.Run()
	teardown()
	casesSHA, _ := FileSHA256(filepath.Join(st.root, "e2e", "proxy", "testdata", "cases.json"))
	meta := Meta{SchemaVersion: ReportSchemaVersion, Upstream: st.upstream, BaseURL: st.baseURL, Started: st.started, Finished: time.Now(), OutDir: st.outDir,
		Model: st.modelPath, Cases: len(st.cases), GoTestRun: fmt.Sprint(code), CasesSHA: casesSHA, GitSHA: GitSHA(st.root)}
	path, err := WriteReport(meta, st.cases, st.results, st.extras, st.auditByMode, st.extraAudits)
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e report: %v\n", err)
		code = 1
	} else {
		fmt.Printf("e2e proxy report: %s\n", path)
		fmt.Printf("e2e proxy html: %s\n", filepath.Join(st.outDir, ReportHTML))
		if leaks := finalLeakSweep(); len(leaks) > 0 {
			for _, l := range leaks {
				fmt.Fprintf(os.Stderr, "e2e LEAK: %s\n", l)
			}
			code = 1
		}
		if dir := os.Getenv(envCopyTo); dir != "" {
			if err := copyReport(dir); err != nil {
				fmt.Fprintf(os.Stderr, "e2e copy: %v\n", err)
			}
		}
	}
	os.Exit(code)
}

func setup() error {
	st.started = time.Now()
	st.upstream = os.Getenv(envUpstream)
	if st.upstream == "" {
		st.upstream = upstreamFake
	}
	if st.upstream != upstreamFake && st.upstream != upstreamMLX && st.upstream != upstreamCompat {
		return fmt.Errorf("%s must be %q, %q or %q, got %q", envUpstream, upstreamFake, upstreamMLX, upstreamCompat, st.upstream)
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	st.root = config.ModuleRoot(wd)
	st.cfgPath = filepath.Join(st.root, "configs", "proxy.yaml")
	st.outDir = os.Getenv(envOut)
	if st.outDir == "" {
		st.outDir = filepath.Join(st.root, "reports", "proxy", time.Now().Format("20060102-150405")+"-"+st.upstream)
	}
	if st.outDir, err = filepath.Abs(st.outDir); err != nil {
		return err
	}
	if err := os.MkdirAll(st.outDir, 0o700); err != nil {
		return err
	}
	if st.cases, err = LoadCases(filepath.Join(st.root, "e2e", "proxy", "testdata", "cases.json")); err != nil {
		return err
	}
	cfg, err := config.LoadProxyEnv(st.cfgPath, func(string) string { return "" })
	if err != nil {
		return err
	}
	st.modelPath = cfg.Models[localModel].UpstreamModel
	if v := os.Getenv(envModelPath); v != "" {
		if st.modelPath, err = filepath.Abs(v); err != nil {
			return err
		}
	}

	var target string
	st.recBase = "/v1"
	switch st.upstream {
	case upstreamFake:
		st.fake = NewFakeUpstream()
		target = st.fake.URL
		st.modelPath = "fake"
	case upstreamMLX:
		target = os.Getenv(envBaseURL)
		if target == "" {
			if _, err := os.Stat(st.modelPath); err != nil {
				return fmt.Errorf("model %s: %w", st.modelPath, err)
			}
			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
			st.server = mlx.NewServer(mlxBind, st.outDir, logger)
			fmt.Fprintf(os.Stderr, "e2e: starting %s on %s with %s\n", mlx.ServerBin(), mlxBind, st.modelPath)
			if err := st.server.Start(st.modelPath); err != nil {
				return err
			}
			target = "http://" + mlxBind
		}
		target = strings.TrimSuffix(strings.TrimRight(target, "/"), "/v1")
	case upstreamCompat:
		// Any OpenAI-compatible API (Ollama, LM Studio, ...): no server is
		// started; the recorder forwards to the full base URL.
		model := strings.TrimSpace(os.Getenv(envModel))
		if model == "" {
			return fmt.Errorf("%s=%s needs %s (upstream model id)", envUpstream, upstreamCompat, envModel)
		}
		bu, err := CompatBaseURL(os.Getenv(envBaseURL))
		if err != nil {
			return fmt.Errorf("%s: %w", envBaseURL, err)
		}
		st.modelPath, st.baseURL, st.recBase = model, RedactURL(bu), ""
		fmt.Fprintf(os.Stderr, "e2e: openai-compat upstream %s, model %s\n", st.baseURL, model)
		target = bu.String()
	}
	u, err := url.Parse(target)
	if err != nil {
		return err
	}
	st.rec = NewRecorder(u)
	st.recBase = st.rec.URL + st.recBase
	return nil
}

// realModel reports whether the run talks to a real model (output cases
// are n/a: the answer is not deterministic).
func (s *suiteState) realModel() bool { return s.upstream != upstreamFake }

func teardown() {
	if st.rec != nil {
		st.rec.Close()
	}
	if st.fake != nil {
		st.fake.Close()
	}
	if st.server != nil {
		_ = st.server.Stop()
	}
}

// copyReport copies report.md/html/png to <dir>/proxy-<upstream>.* file by
// file; other files in dir are kept. A missing PNG (no Chrome) is skipped.
func copyReport(dir string) error {
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(st.root, dir)
	}
	files := map[string]string{}
	for _, ext := range []string{".md", ".html", ".png"} {
		src := filepath.Join(st.outDir, "report"+ext)
		if _, err := os.Stat(src); err == nil {
			files[src] = "proxy-" + st.upstream + ext
		}
	}
	return htmlkit.CopyFiles(dir, files)
}

// forbiddenEverywhere are values that must not appear in any file of the
// run (audit, slog, results, HTML): proxy keys and the pepper.
func forbiddenEverywhere() map[string]string {
	return map[string]string{"proxy key 1": e2eKey1, "proxy key 2": e2eKey2, "AUDIT_PEPPER": e2ePepper}
}

// finalLeakSweep runs after the report: no file of the run contains a
// proxy key or the pepper, the HTML reports contain no case secret (raw,
// base64, hex), and the upstream never received a proxy key.
func finalLeakSweep() []string {
	var leaks []string
	forbidden := forbiddenEverywhere()
	var secrets []string
	for _, c := range st.cases {
		secrets = append(secrets, c.Secrets...)
	}
	_ = filepath.WalkDir(st.outDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(st.outDir, path)
		for name, v := range forbidden {
			if strings.Contains(string(data), v) {
				leaks = append(leaks, fmt.Sprintf("%s contains %s", rel, name))
			}
		}
		if strings.HasSuffix(path, ".html") {
			for _, p := range HTMLProblems(string(data), secrets) {
				leaks = append(leaks, rel+": "+p)
			}
		}
		return nil
	})
	if st.rec != nil {
		for _, a := range st.rec.Auths() {
			if strings.Contains(a, e2eKey1) || strings.Contains(a, e2eKey2) {
				leaks = append(leaks, "upstream received a proxy key in Authorization")
			}
		}
	}
	return leaks
}

func (s *suiteState) addResult(r Result) {
	s.mu.Lock()
	s.results = append(s.results, r)
	s.mu.Unlock()
}

func (s *suiteState) addExtra(e ExtraResult) {
	s.mu.Lock()
	s.extras = append(s.extras, e)
	s.mu.Unlock()
}

// harness is one proxy instance built exactly like cmd/llm-proxy.
type harness struct {
	app       *app.App
	srv       *httptest.Server
	clk       *clock.Fake
	cfg       *config.Proxy
	auditPath string
}

func newHarness(t testing.TB, mode, name string, mutate func(*config.Proxy)) *harness {
	t.Helper()
	dir := filepath.Join(st.outDir, name)
	env := map[string]string{
		config.EnvProxyGuardMode:      mode,
		config.EnvProxyRate:           "1000",
		config.EnvProxyAuditPath:      filepath.Join(dir, "audit.jsonl"),
		"PROXY_UPSTREAM_MLX_BASE_URL": st.recBase,
		"AUDIT_PEPPER":                e2ePepper,
		"PROXY_API_KEYS":              e2eKey1 + "," + e2eKey2,
	}
	if st.upstream == upstreamFake {
		env["PROXY_UPSTREAM_OPENAI_BASE_URL"] = st.recBase
		env["OPENAI_API_KEY"] = "e2e-fake-key"
	}
	getenv := func(k string) string { return env[k] }
	cfg, err := config.LoadProxyEnv(st.cfgPath, getenv)
	if err != nil {
		t.Fatal(err)
	}
	switch st.upstream {
	case upstreamMLX:
		m := cfg.Models[localModel]
		m.UpstreamModel = st.modelPath
		m.AuditModel = config.RelToRoot(cfg.Root, st.modelPath)
		cfg.Models[localModel] = m
	case upstreamCompat:
		m := cfg.Models[localModel]
		m.UpstreamModel, m.AuditModel, m.LocalPath = st.modelPath, "", false
		cfg.Models[localModel] = m
	}
	cfg.Models[paidModel] = config.ModelRoute{Upstream: "mlx", UpstreamModel: "fake-paid",
		Price: config.Price{InputMicroUSDPer1M: 400000, OutputMicroUSDPer1M: 1600000, Ref: "e2e-paid"}}
	if mutate != nil {
		mutate(cfg)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	logFile, err := os.OpenFile(filepath.Join(dir, "proxy.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{cfg: cfg, auditPath: cfg.Audit.Path}
	opt := app.Options{Getenv: getenv, Logger: slog.New(slog.NewJSONHandler(logFile, nil))}
	if st.upstream == upstreamFake {
		h.clk = clock.NewFake(fixedNow)
		opt.Clock = h.clk
	}
	if h.app, err = app.Build(cfg, opt); err != nil {
		t.Fatal(err)
	}
	h.srv = httptest.NewServer(h.app.Handler)
	if strings.HasPrefix(name, "extra/") {
		st.mu.Lock()
		st.extraAudits = append(st.extraAudits, h.auditPath)
		st.mu.Unlock()
	}
	t.Cleanup(func() {
		h.srv.Close()
		_ = h.app.Close()
		_ = logFile.Close()
	})
	return h
}

// response is a parsed proxy response.
type response struct {
	status  int
	header  http.Header
	body    []byte
	code    string
	param   string
	content string
	finish  string
}

// post sends a chat completion with "Authorization: Bearer e2eKey1" unless
// hdr sets Authorization (an empty value sends no Authorization header).
func (h *harness) post(t testing.TB, payload map[string]any, hdr map[string]string) response {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Set("Authorization", "Bearer "+e2eKey1)
	for k, v := range hdr {
		header.Set(k, v)
	}
	if header.Get("Authorization") == "" {
		header.Del("Authorization")
	}
	return h.raw(t, http.MethodPost, "/v1/chat/completions", b, header)
}

// raw sends a request with exactly the given headers (no default
// Authorization).
func (h *harness) raw(t testing.TB, method, path string, reqBody []byte, header http.Header) response {
	t.Helper()
	req, err := http.NewRequest(method, h.srv.URL+path, bytes.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	r := response{status: resp.StatusCode, header: resp.Header, body: body}
	var parsed struct {
		Error struct {
			Code  string  `json:"code"`
			Param *string `json:"param"`
		} `json:"error"`
		Choices []struct {
			Message      struct{ Content string } `json:"message"`
			FinishReason string                   `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil {
		r.code = parsed.Error.Code
		if parsed.Error.Param != nil {
			r.param = *parsed.Error.Param
		}
		if len(parsed.Choices) > 0 {
			r.content, r.finish = parsed.Choices[0].Message.Content, parsed.Choices[0].FinishReason
		}
	}
	return r
}

func (h *harness) chat(t testing.TB, model string, msgs []ports.ChatMessage, hdr map[string]string) response {
	t.Helper()
	payload := map[string]any{"model": model, "messages": msgs}
	if st.realModel() {
		payload["max_tokens"] = 128
		payload["temperature"] = 0.2
	}
	return h.post(t, payload, hdr)
}

// auditRecords reads all records of the harness audit log.
func (h *harness) auditRecords(t testing.TB) ([]ports.AuditRecord, string) {
	t.Helper()
	data, err := os.ReadFile(h.auditPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	var out []ports.AuditRecord
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var r ports.AuditRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("audit line: %v", err)
		}
		out = append(out, r)
	}
	return out, string(data)
}

func lastAuditLine(t testing.TB, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	return lines[len(lines)-1]
}

// extra records a scenario test result for the report.
func extra(t *testing.T, name string, fakeOnly bool, fn func(t *testing.T) string) {
	t.Helper()
	cat := ExtraCategory(name)
	if fakeOnly && st.upstream != upstreamFake {
		st.addExtra(ExtraResult{Name: name, Category: cat, Skipped: true, Details: "только fake upstream"})
		t.Skip("fake upstream only")
	}
	details := ""
	defer func() {
		st.addExtra(ExtraResult{Name: name, Category: cat, Passed: !t.Failed(), Details: details})
	}()
	details = fn(t)
}

// allFiles returns the paths of files named name under the run directory.
func allFiles(name string) []string {
	var out []string
	_ = filepath.WalkDir(st.outDir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() == name {
			out = append(out, path)
		}
		return nil
	})
	return out
}
