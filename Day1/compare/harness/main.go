// Command harness — внешний black-box стенд для двух вариантов Telegram-бота
// погоды: поднимает фейковые Telegram Bot API и OpenWeatherMap, собирает и
// запускает бота, прогоняет сценарии и пишет transcripts/{a,b}.json.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"harness/internal/fake"
	"harness/internal/transcript"
)

const (
	fakeToken   = "123456:TEST_FAKE_TOKEN_abcdef"
	fakeKey     = "TEST_OWM_KEY_123"
	wrongKey    = "TEST_OWM_KEY_WRONG_999"
	tokenMarker = "TEST_FAKE_TOKEN"
	keyMarker   = "TEST_OWM_KEY"
	dockerImage = "alpine:latest"

	variantBudget = 4 * time.Minute
	firstReqWait  = 10 * time.Second
)

type options struct {
	variant string
	src     string
	compare string
	binDir  string
	wait    time.Duration
	noDock  bool
}

func main() {
	var o options
	flag.StringVar(&o.variant, "variant", "all", "a | b | all")
	flag.StringVar(&o.src, "src", "", "каталог исходников варианта (по умолчанию ../../variant-<v>/src)")
	flag.StringVar(&o.compare, "compare", "..", "каталог Day1/compare (сюда пишутся transcripts/)")
	flag.StringVar(&o.binDir, "bindir", filepath.Join(os.TempDir(), "weatherbot-harness"), "куда собирать бинари и сырые логи")
	flag.DurationVar(&o.wait, "wait", 5*time.Second, "сколько ждать ответа в обычном сценарии")
	flag.BoolVar(&o.noDock, "no-docker", false, "не пробовать запуск в docker через MITM-прокси")
	flag.Parse()

	variants := []string{o.variant}
	if o.variant == "all" {
		variants = []string{"a", "b"}
	}
	failed := false
	for _, v := range variants {
		if err := runVariant(o, v); err != nil {
			log.Printf("variant %s: %v", v, err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

// harness хранит состояние прогона одного варианта.
type harness struct {
	o       options
	variant string
	src     string
	run     *transcript.Run
	hlog    *log.Logger
	tg      *fake.Telegram
	owm     *fake.OWM
	tgSrv   *httptest.Server
	owmSrv  *httptest.Server
	proxy   *fake.MITMProxy
	prxSrv  *httptest.Server
	rawLog  *os.File
	lastMsg map[int64]time.Time
	launch  func(env []string, key string) (*botProc, error) // выбранный способ запуска
}

func runVariant(o options, v string) error {
	ctx, cancel := context.WithTimeout(context.Background(), variantBudget)
	defer cancel()

	src := o.src
	if src == "" {
		src = filepath.Join("..", "..", "variant-"+v, "src")
	}
	src, _ = filepath.Abs(src)
	compare, _ := filepath.Abs(o.compare)
	binDir, _ := filepath.Abs(o.binDir)
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(compare, "transcripts"), 0o755); err != nil {
		return err
	}

	hlogFile, err := os.Create(filepath.Join(binDir, v+"-harness.log"))
	if err != nil {
		return err
	}
	defer hlogFile.Close()
	hlog := log.New(io.MultiWriter(hlogFile, os.Stderr), "["+v+"] ", log.Ltime|log.Lmicroseconds)

	rawLog, err := os.Create(filepath.Join(binDir, v+"-bot.raw.log"))
	if err != nil {
		return err
	}
	defer rawLog.Close()

	h := &harness{
		o: o, variant: v, src: src, hlog: hlog, rawLog: rawLog,
		lastMsg: map[int64]time.Time{},
		run:     &transcript.Run{Variant: v, StartedAt: time.Now(), Source: src},
	}
	h.tg = fake.NewTelegram(fakeToken, hlog)
	h.owm = fake.NewOWM(fakeKey, hlog)
	h.tgSrv = httptest.NewServer(h.tg)
	defer h.tgSrv.Close()
	h.owmSrv = httptest.NewServer(h.owm)
	defer h.owmSrv.Close()

	defer func() {
		h.run.DurationMs = time.Since(h.run.StartedAt).Milliseconds()
		h.run.TGRequests = h.tg.Methods()
		if err := h.finish(compare, binDir); err != nil {
			hlog.Printf("finish: %v", err)
		}
	}()

	bin := filepath.Join(binDir, "weatherbot-"+v)
	if err := h.build(ctx, bin, "", ""); err != nil {
		return err
	}
	h.detectEnv()

	proc, err := h.connect(ctx, bin, binDir)
	if err != nil {
		h.run.Mode = "untestable"
		return err
	}
	if proc == nil {
		h.run.Mode = "untestable"
		return nil
	}

	for _, def := range scenarios() {
		if ctx.Err() != nil {
			h.run.Notes = append(h.run.Notes, "прогон прерван по бюджету времени перед сценарием "+def.title)
			break
		}
		h.run.Scenarios = append(h.run.Scenarios, h.runScenario(ctx, def))
	}
	h.writeMarker("SIGTERM (main run)")
	h.run.Shutdown = proc.Stop()
	hlog.Printf("shutdown: %+v", *h.run.Shutdown)

	// Сценарий 15: отдельный процесс с неверным ключом.
	h.writeMarker("run 2: wrong OWM key")
	before := h.tgTotal()
	proc2, err := h.launch(nil, wrongKey)
	if err != nil {
		return err
	}
	if h.waitTG(ctx, before, 20*time.Second) {
		h.run.Scenarios = append(h.run.Scenarios, h.runScenario(ctx, wrongKeyScenario()))
	} else {
		h.run.Notes = append(h.run.Notes, "прогон с неверным ключом: бот не обратился к Telegram")
	}
	h.writeMarker("SIGTERM (wrong key run)")
	h.run.Shutdown2 = proc2.Stop()
	return nil
}

// build собирает бинарь из каталога варианта.
func (h *harness) build(ctx context.Context, out, goos, goarch string) error {
	cmd := exec.CommandContext(ctx, "go", "-C", h.src, "build", "-o", out, "./cmd/weatherbot")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if goos != "" {
		cmd.Env = append(cmd.Env, "GOOS="+goos, "GOARCH="+goarch)
	}
	output, err := cmd.CombinedOutput()
	if goos == "" {
		h.run.Build = transcript.Build{OK: err == nil, Output: string(output), Binary: filepath.Base(out)}
	}
	if err != nil {
		return fmt.Errorf("go build (%s): %w: %s", goos, err, output)
	}
	return nil
}

var envLiteral = regexp.MustCompile(`"([A-Z][A-Z0-9]*(?:_[A-Z0-9]+)+)"`)

// detectEnv ищет имена переменных в исходниках и выбирает имена для base URL.
func (h *harness) detectEnv() {
	found := map[string]bool{}
	_ = filepath.WalkDir(h.src, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		for _, m := range envLiteral.FindAllStringSubmatch(string(b), -1) {
			found[m[1]] = true
		}
		return nil
	})
	names := make([]string, 0, len(found))
	for k := range found {
		names = append(names, k)
	}
	slices.Sort(names)
	h.run.EnvNames.Found = names

	// Кандидаты из исходников + распространённые имена.
	tg := []string{"TELEGRAM_BASE_URL", "TELEGRAM_API_URL", "TELEGRAM_API_BASE_URL", "TELEGRAM_URL", "TG_API_URL", "BOT_API_URL"}
	owm := []string{"OPENWEATHER_BASE_URL", "OPENWEATHER_API_URL", "OPENWEATHERMAP_BASE_URL", "OWM_BASE_URL", "OWM_API_URL", "WEATHER_API_URL", "WEATHER_BASE_URL"}
	for _, n := range names {
		if !strings.Contains(n, "URL") && !strings.Contains(n, "ENDPOINT") && !strings.Contains(n, "HOST") {
			continue
		}
		switch {
		case strings.Contains(n, "TELEGRAM") || strings.HasPrefix(n, "TG_") || strings.Contains(n, "BOT"):
			tg = appendUnique(tg, n)
		case strings.Contains(n, "WEATHER") || strings.Contains(n, "OWM"):
			owm = appendUnique(owm, n)
		}
	}
	h.run.EnvNames.TelegramURL, h.run.EnvNames.OWMURL = tg, owm
}

func appendUnique(s []string, v string) []string {
	if slices.Contains(s, v) {
		return s
	}
	return append(s, v)
}

func (h *harness) secretsEnv(key string) []string {
	if key == "" {
		key = fakeKey
	}
	return []string{"TELEGRAM_BOT_TOKEN=" + fakeToken, "OPENWEATHER_API_KEY=" + key, "LOG_LEVEL=info"}
}

// connect пробует способы направить бота на фейки и возвращает работающий процесс.
func (h *harness) connect(ctx context.Context, bin, binDir string) (*botProc, error) {
	// Прокси стенда. В попытке 1 он в режиме deny и служит предохранителем:
	// бот, игнорирующий base URL, не уйдёт в настоящий Telegram/OWM
	// (localhost прокси не затрагивает, поэтому подмена base URL работает).
	p, err := fake.NewMITMProxy(map[string]http.Handler{"api.telegram.org": h.tg, "api.openweathermap.org": h.owm}, h.hlog)
	if err != nil {
		return nil, err
	}
	p.SetDeny(true)
	h.proxy = p
	h.prxSrv = httptest.NewServer(p)
	proxyPort := h.prxSrv.URL[strings.LastIndex(h.prxSrv.URL, ":")+1:]
	guard := []string{"HTTPS_PROXY=" + h.prxSrv.URL, "HTTP_PROXY=" + h.prxSrv.URL, "NO_PROXY="}

	// 1. Переменные окружения base URL.
	envMode := func(extra []string, key string) (*botProc, error) {
		env := append(baseEnv(), h.secretsEnv(key)...)
		for _, n := range h.run.EnvNames.TelegramURL {
			env = append(env, n+"="+h.tgSrv.URL)
		}
		for _, n := range h.run.EnvNames.OWMURL {
			env = append(env, n+"="+h.owmSrv.URL)
		}
		env = append(env, guard...)
		return startNative(bin, append(env, extra...), h.rawLog)
	}
	h.writeMarker("attempt 1: base URL via env")
	start := time.Now()
	before := h.tgTotal()
	proc, err := envMode(nil, "")
	if err != nil {
		return nil, err
	}
	if h.waitTG(ctx, before, firstReqWait) {
		h.addAttempt("env base URL", true, "бот обратился к фейку Telegram по "+strings.Join(h.run.EnvNames.TelegramURL, "/"), start)
		h.run.Mode = "env"
		h.run.ModeNote = "base URL подменён переменными окружения"
		h.launch = envMode
		return proc, nil
	}
	exited := proc.Exited()
	proc.Stop()
	detail := "не тестируемо black-box: base URL не подменяется (за 10 с ни одного запроса к фейку Telegram)"
	if c := uniq(p.Connects()); len(c) > 0 {
		detail += "; бот пытался выйти наружу на " + strings.Join(c, ", ") + " (заблокировано прокси-предохранителем)"
	}
	if exited {
		detail += "; процесс завершился сам — см. лог"
	}
	h.addAttempt("env base URL", false, detail, start)

	// 2. Нативно через HTTPS_PROXY: http.Client без своего Transport читает прокси из окружения.
	p.SetDeny(false)

	h.writeMarker("attempt 2: native HTTPS_PROXY (macOS trust store)")
	start = time.Now()
	before = h.tgTotal()
	connectsBefore := len(p.Connects())
	proc, err = envMode(nil, "")
	if err != nil {
		return nil, err
	}
	if h.waitTG(ctx, before, 8*time.Second) {
		// Маловероятно: значит, системе доверен наш CA.
		h.addAttempt("native HTTPS_PROXY", true, "бот пошёл через прокси, TLS принят", start)
		h.run.Mode = "native-https-proxy-mitm"
		h.launch = func(_ []string, key string) (*botProc, error) {
			return envMode(nil, key)
		}
		return proc, nil
	}
	proc.Stop()
	h.addAttempt("native HTTPS_PROXY", false, fmt.Sprintf(
		"прокси подхватился (CONNECT: %s), но TLS не прошёл: %s. На macOS Go проверяет сертификаты через системное хранилище, SSL_CERT_FILE игнорируется, а менять Keychain стенд не должен",
		strings.Join(uniq(p.Connects()[connectsBefore:]), ", "), firstOr(p.TLSErrors(), "нет данных")), start)

	if h.o.noDock {
		return nil, nil
	}
	// 3. Linux-бинарь в контейнере: HTTPS_PROXY + SSL_CERT_FILE с CA стенда.
	arch, err := dockerArch(ctx)
	if err != nil {
		h.addAttempt("docker HTTPS_PROXY+SSL_CERT_FILE", false, "docker недоступен: "+err.Error(), time.Now())
		return nil, nil
	}
	linuxBin := "weatherbot-" + h.variant + "-linux"
	if err := h.build(ctx, filepath.Join(binDir, linuxBin), "linux", arch); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(binDir, "harness-ca.pem"), p.CAPEM(), 0o644); err != nil {
		return nil, err
	}
	n := 0
	dockerMode := func(_ []string, key string) (*botProc, error) {
		n++
		env := append(h.secretsEnv(key), "HTTPS_PROXY=http://host.docker.internal:"+proxyPort, "SSL_CERT_FILE=/w/harness-ca.pem")
		name := fmt.Sprintf("weatherbot-harness-%s-%d-%d", h.variant, os.Getpid(), n)
		return startDocker(name, binDir, linuxBin, env, h.rawLog)
	}
	h.writeMarker("attempt 3: docker linux/" + arch + " + HTTPS_PROXY + SSL_CERT_FILE")
	start = time.Now()
	before = h.tgTotal()
	proc, err = dockerMode(nil, "")
	if err != nil {
		return nil, err
	}
	if h.waitTG(ctx, before, 25*time.Second) {
		h.addAttempt("docker HTTPS_PROXY+SSL_CERT_FILE", true,
			"тот же исходник, собранный под linux/"+arch+", без изменений кода; трафик к api.telegram.org и api.openweathermap.org перехвачен MITM-прокси стенда с одноразовым CA", start)
		h.run.Mode = "docker-https-proxy-mitm"
		h.run.ModeNote = "base URL зашит в коде; бот запущен в alpine-контейнере с HTTPS_PROXY и SSL_CERT_FILE, запросы к настоящим хостам перехвачены прокси стенда"
		h.launch = dockerMode
		return proc, nil
	}
	proc.Kill()
	h.addAttempt("docker HTTPS_PROXY+SSL_CERT_FILE", false, "бот не обратился к Telegram за 25 с; TLS: "+firstOr(p.TLSErrors(), "нет ошибок"), start)
	return nil, nil
}

func dockerArch(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.Architecture}}").Output()
	if err != nil {
		return "", err
	}
	switch strings.TrimSpace(string(out)) {
	case "aarch64", "arm64":
		return "arm64", nil
	case "x86_64", "amd64":
		return "amd64", nil
	default:
		return "", fmt.Errorf("unknown docker arch %q", out)
	}
}

func (h *harness) addAttempt(name string, ok bool, detail string, start time.Time) {
	h.hlog.Printf("attempt %q ok=%v: %s", name, ok, detail)
	h.run.Attempts = append(h.run.Attempts, transcript.Attempt{Name: name, OK: ok, Detail: detail, ElapsedMs: time.Since(start).Milliseconds()})
}

func (h *harness) writeMarker(s string) {
	fmt.Fprintf(h.rawLog, "===== harness: %s =====\n", s)
}

func (h *harness) tgTotal() int {
	n := 0
	for _, v := range h.tg.Methods() {
		n += v
	}
	return n
}

// waitTG ждёт, пока число обращений к фейку Telegram превысит before.
func (h *harness) waitTG(ctx context.Context, before int, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		if h.tgTotal() > before {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func (h *harness) runScenario(ctx context.Context, def scenarioDef) transcript.Scenario {
	wait := def.wait
	if wait == 0 {
		wait = h.o.wait
	}
	if last, ok := h.lastMsg[def.chat]; ok {
		if d := perChatGap - time.Since(last); d > 0 {
			time.Sleep(d)
		}
	}
	h.owm.SetScenario(def.id)
	sentBefore := h.tg.SentCount()
	h.hlog.Printf("scenario %d %q: sending %d message(s)", def.id, def.title, len(def.texts))
	t0 := h.tg.Enqueue(def.chat, def.texts...)
	h.lastMsg[def.chat] = t0

	deadline := t0.Add(wait)
	var settleUntil time.Time
	settle := 500 * time.Millisecond
	if def.replies > 1 {
		settle = 1500 * time.Millisecond
	}
	for ctx.Err() == nil {
		now := time.Now()
		n := h.tg.SentCount() - sentBefore
		if n >= def.replies && settleUntil.IsZero() {
			settleUntil = now.Add(settle)
		}
		if now.After(deadline) || (!settleUntil.IsZero() && now.After(settleUntil)) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	waited := time.Since(t0)

	sc := transcript.Scenario{
		ID: def.id, Title: def.title, ChatID: def.chat, Inputs: def.texts, WaitedMs: waited.Milliseconds(),
		Replies: []transcript.Reply{}, OWMQueries: []transcript.OWMQuery{},
	}
	for _, m := range h.tg.Sent(sentBefore) {
		sc.Replies = append(sc.Replies, transcript.Reply{ChatID: m.ChatID, Text: m.Text, ParseMode: m.ParseMode, AtMs: m.At.Sub(t0).Milliseconds()})
		if strings.Contains(m.Text, tokenMarker) || strings.Contains(m.Text, keyMarker) {
			sc.Flags.LeakInReply = true
		}
	}
	if len(sc.Replies) > 0 {
		first := sc.Replies[0].AtMs
		sc.FirstReplyMs = &first
	}
	for _, r := range h.owm.Records(def.id) {
		valid := len(r.AppID) == 1 && r.AppID[0] == fakeKey
		sc.OWMQueries = append(sc.OWMQueries, transcript.OWMQuery{
			Path: r.Path, RawQuery: redactSecrets(r.RawQuery), Q: r.Q, AppIDCount: len(r.AppID), AppIDValid: valid,
			Status: r.Status, BytesSent: r.BytesSent, DurationMs: r.Duration.Milliseconds(), ClientAbort: r.ClientAbort,
		})
	}
	sc.Flags.Replied = len(sc.Replies) > 0
	if def.check != nil {
		def.check(&sc)
	}
	h.hlog.Printf("scenario %d done: replies=%d owm=%d waited=%s", def.id, len(sc.Replies), len(sc.OWMQueries), waited.Round(time.Millisecond))
	return sc
}

// finish считает утечки по сырому логу и пишет transcript + отредактированный лог.
func (h *harness) finish(compare, binDir string) error {
	_ = h.rawLog.Sync()
	raw, err := os.ReadFile(filepath.Join(binDir, h.variant+"-bot.raw.log"))
	if err != nil {
		return err
	}
	logText := string(raw)
	h.run.Leaks.TokenInLog = strings.Count(logText, tokenMarker)
	h.run.Leaks.KeyInLog = strings.Count(logText, keyMarker)
	for _, sc := range h.run.Scenarios {
		for _, r := range sc.Replies {
			h.run.Leaks.TokenInReplies += strings.Count(r.Text, tokenMarker)
			h.run.Leaks.KeyInReplies += strings.Count(r.Text, keyMarker)
		}
	}
	if h.prxSrv != nil {
		h.prxSrv.Close()
	}

	if err := os.WriteFile(filepath.Join(compare, "transcripts", h.variant+"-bot.log"), []byte(redactSecrets(logText)), 0o644); err != nil {
		return err
	}
	b, err := json.MarshalIndent(h.run, "", "  ")
	if err != nil {
		return err
	}
	out := filepath.Join(compare, "transcripts", h.variant+".json")
	if err := os.WriteFile(out, append(b, '\n'), 0o644); err != nil {
		return err
	}
	h.hlog.Printf("wrote %s (leaks: %+v)", out, h.run.Leaks)
	return nil
}

var secretReplacer = strings.NewReplacer(
	fakeToken, "<TOKEN>",
	"123456%3ATEST_FAKE_TOKEN_abcdef", "<TOKEN>",
	tokenMarker, "<TOKEN-PART>",
	wrongKey, "<WRONG_KEY>",
	fakeKey, "<KEY>",
	keyMarker, "<KEY-PART>",
)

func redactSecrets(s string) string { return secretReplacer.Replace(s) }

func uniq(s []string) []string {
	slices.Sort(s)
	return slices.Compact(s)
}

func firstOr(s []string, def string) string {
	if len(s) == 0 {
		return def
	}
	return s[0]
}
