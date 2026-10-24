// Command render превращает артефакты одной попытки (stream-json транскрипт
// claude -p, verdict.txt, run.json) в HTML и PNG через headless Chrome.
//
//	go -C harness/render run . -in runs/<profile>/attempt-N [-title ...] [-screenshot]
//
// Пишет в каталог попытки:
//   - transcript.html      — полный лог (сообщения, вызовы инструментов, результаты);
//   - summary.html         — шапка, вердикт, задача, итоговый ответ;
//   - transcript.png       — снимок summary.html;
//   - transcript-log-partN.png — снимки полного лога, порезанные по высоте.
package main

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"image"
	"image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

//go:embed page.html.tmpl
var tmplFS embed.FS

const (
	chromeDefault = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	maxShotHeight = 16000
	shotWidth     = 1100
	bgR, bgG, bgB = 0xee, 0xf1, 0xf5 // фон страницы, по нему обрезаем PNG
	maxInput      = 800
	maxOutput     = 1200
)

// Маскирование на случай, если в транскрипт что-то попало.
var masks = []*regexp.Regexp{
	regexp.MustCompile(`\d{6,}:[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\b[0-9a-f]{32}\b`),
}

func main() {
	in := flag.String("in", "", "каталог попытки (runs/<profile>/attempt-N)")
	title := flag.String("title", "", "заголовок")
	shots := flag.Bool("screenshot", false, "снять PNG через headless Chrome")
	chrome := flag.String("chrome", chromeDefault, "путь к Chrome")
	flag.Parse()
	if *in == "" {
		log.Fatal("-in is required")
	}
	dir, err := filepath.Abs(*in)
	if err != nil {
		log.Fatal(err)
	}
	if *title == "" {
		*title = filepath.Base(filepath.Dir(dir)) + " " + filepath.Base(dir)
	}
	pv, err := build(dir, *title)
	if err != nil {
		log.Fatal(err)
	}
	tmpl := template.Must(template.New("page.html.tmpl").ParseFS(tmplFS, "page.html.tmpl"))

	summary := pv
	summary.Events = nil
	summary.Mode = "summary"
	full := pv
	full.Mode = "full"

	sumHTML := filepath.Join(dir, "summary.html")
	fullHTML := filepath.Join(dir, "transcript.html")
	for path, v := range map[string]pageView{sumHTML: summary, fullHTML: full} {
		if err := writePage(tmpl, path, v); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %s", path)
	}
	if !*shots {
		return
	}
	if err := shoot(*chrome, sumHTML, filepath.Join(dir, "transcript.png")); err != nil {
		log.Fatal(err)
	}
	// Полный лог режем на части так, чтобы каждая влезала в кадр.
	old, _ := filepath.Glob(filepath.Join(dir, "transcript-log-part*.*"))
	for _, f := range old {
		_ = os.Remove(f)
	}
	for parts := 1; parts <= 12; parts++ {
		ok := true
		chunks := split(pv.Events, parts)
		for i, ch := range chunks {
			v := pv
			v.Mode = "log"
			v.Events = ch
			v.PartLabel = fmt.Sprintf("лог, часть %d из %d", i+1, len(chunks))
			h := filepath.Join(dir, fmt.Sprintf("transcript-log-part%d.html", i+1))
			p := filepath.Join(dir, fmt.Sprintf("transcript-log-part%d.png", i+1))
			if err := writePage(tmpl, h, v); err != nil {
				log.Fatal(err)
			}
			fits, err := screenshot(*chrome, h, p)
			if err != nil {
				log.Fatal(err)
			}
			if !fits {
				ok = false
				break
			}
		}
		if ok {
			log.Printf("log split into %d part(s)", parts)
			return
		}
		old, _ := filepath.Glob(filepath.Join(dir, "transcript-log-part*.*"))
		for _, f := range old {
			_ = os.Remove(f)
		}
	}
	log.Fatal("log does not fit into 12 screenshots")
}

func shoot(chrome, html, png string) error {
	fits, err := screenshot(chrome, html, png)
	if err != nil {
		return err
	}
	if !fits {
		log.Printf("warning: %s cropped at %dpx", png, maxShotHeight)
	}
	log.Printf("wrote %s", png)
	return nil
}

func split(ev []event, parts int) [][]event {
	if parts <= 1 || len(ev) == 0 {
		return [][]event{ev}
	}
	size := (len(ev) + parts - 1) / parts
	var out [][]event
	for i := 0; i < len(ev); i += size {
		out = append(out, ev[i:min(i+size, len(ev))])
	}
	return out
}

// --- модель страницы ---

type pageView struct {
	Title     string
	Mode      string // summary | full | log
	PartLabel string
	Generated string
	Init      string
	Stats     []string
	Hashes    []string
	Verdict   string
	Crits     []critView
	Task      string
	Answer    string
	Events    []event
}

type critView struct {
	ID, Status, Detail, Class string
}

type event struct {
	N     int
	Kind  string // text | tool | result | error
	Label string
	Body  string
}

func build(dir, title string) (pageView, error) {
	pv := pageView{Title: title, Generated: time.Now().Format("2006-01-02 15:04")}
	pv.Task = mask(readFile(filepath.Join(dir, "task.txt")))
	pv.Answer = mask(readFile(filepath.Join(dir, "answer.md")))
	for _, l := range strings.Split(strings.TrimSpace(readFile(filepath.Join(dir, "profile.sha256"))), "\n") {
		if f := strings.Fields(l); len(f) == 2 {
			pv.Hashes = append(pv.Hashes, f[0][:12]+"  "+f[1])
		}
	}
	verdict := readFile(filepath.Join(dir, "verdict.txt"))
	for _, l := range strings.Split(verdict, "\n") {
		switch {
		case strings.HasPrefix(l, "VERDICT") || strings.HasPrefix(l, "MANUAL") || strings.HasPrefix(l, "FINAL"):
			pv.Verdict += l + "\n"
		case strings.TrimSpace(l) == "":
		default:
			f := strings.Fields(l)
			if len(f) < 2 {
				continue
			}
			cls := map[string]string{"PASS": "ok", "FAIL": "bad", "MANUAL": "man", "INFO": "info"}[f[1]]
			if cls == "" {
				pv.Verdict += l + "\n"
				continue
			}
			detail := strings.Join(f[2:], " ")
			pv.Crits = append(pv.Crits, critView{ID: f[0], Status: f[1], Detail: trunc(detail, 300), Class: cls})
		}
	}

	f, err := os.Open(filepath.Join(dir, "transcript.jsonl"))
	if err != nil {
		return pv, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	names := map[string]string{}
	n := 0
	for sc.Scan() {
		var e map[string]any
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		switch e["type"] {
		case "system":
			if e["subtype"] == "init" {
				pv.Init = fmt.Sprintf("model=%v · tools=%v · mcp_servers=%v · permissionMode=%v · cwd=%v · claude %v",
					e["model"], e["tools"], e["mcp_servers"], e["permissionMode"], e["cwd"], e["claude_code_version"])
			}
		case "assistant", "user":
			msg, _ := e["message"].(map[string]any)
			content, _ := msg["content"].([]any)
			for _, c := range content {
				cm, _ := c.(map[string]any)
				switch cm["type"] {
				case "text":
					if s, _ := cm["text"].(string); strings.TrimSpace(s) != "" && e["type"] == "assistant" {
						n++
						pv.Events = append(pv.Events, event{N: n, Kind: "text", Label: "assistant", Body: mask(trunc(s, 4000))})
					}
				case "tool_use":
					n++
					name, _ := cm["name"].(string)
					id, _ := cm["id"].(string)
					names[id] = name
					pv.Events = append(pv.Events, event{N: n, Kind: "tool", Label: "→ " + name, Body: mask(trunc(inputSummary(name, cm["input"]), maxInput))})
				case "tool_result":
					id, _ := cm["tool_use_id"].(string)
					kind := "result"
					if b, _ := cm["is_error"].(bool); b {
						kind = "error"
					}
					pv.Events = append(pv.Events, event{N: n, Kind: kind, Label: "← " + names[id], Body: mask(trunc(resultText(cm["content"]), maxOutput))})
				}
			}
		case "result":
			pv.Stats = append(pv.Stats,
				fmt.Sprintf("subtype=%v", e["subtype"]),
				fmt.Sprintf("is_error=%v", e["is_error"]),
				fmt.Sprintf("turns=%v", e["num_turns"]),
				fmt.Sprintf("cost=$%.2f", num(e["total_cost_usd"])),
				fmt.Sprintf("time=%s", (time.Duration(num(e["duration_ms"]))*time.Millisecond).Round(time.Second)),
			)
			if d, ok := e["permission_denials"].([]any); ok {
				pv.Stats = append(pv.Stats, fmt.Sprintf("permission_denials=%d", len(d)))
			}
		}
	}
	if err := sc.Err(); err != nil {
		return pv, err
	}
	return pv, nil
}

func inputSummary(name string, in any) string {
	m, _ := in.(map[string]any)
	switch name {
	case "Bash":
		return fmt.Sprint(m["command"])
	case "Read":
		s := fmt.Sprint(m["file_path"])
		if o, ok := m["offset"]; ok {
			s += fmt.Sprintf(" (offset %v, limit %v)", o, m["limit"])
		}
		return s
	case "Edit":
		return fmt.Sprintf("%v\n--- old\n%v\n+++ new\n%v", m["file_path"], m["old_string"], m["new_string"])
	case "Write":
		return fmt.Sprintf("%v\n%v", m["file_path"], m["content"])
	}
	b, _ := json.Marshal(m)
	return string(b)
}

func resultText(c any) string {
	switch v := c.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, x := range v {
			if xm, ok := x.(map[string]any); ok {
				if s, ok := xm["text"].(string); ok {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}

func trunc(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + fmt.Sprintf("\n… [обрезано, всего %d символов]", len(r))
}

func mask(s string) string {
	for _, re := range masks {
		s = re.ReplaceAllString(s, "[MASKED]")
	}
	return s
}

func readFile(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}

func writePage(t *template.Template, path string, pv pageView) error {
	var buf bytes.Buffer
	if err := t.Execute(&buf, pv); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// screenshot снимает страницу высоким окном и обрезает фон снизу.
// fits=false — контент упёрся в нижний край.
func screenshot(chrome, htmlPath, pngPath string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, chrome, "--headless=new", "--disable-gpu", "--hide-scrollbars",
		"--force-device-scale-factor=1", "--no-first-run", "--no-default-browser-check",
		"--screenshot="+pngPath, fmt.Sprintf("--window-size=%d,%d", shotWidth, maxShotHeight), "file://"+htmlPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("chrome: %w: %s", err, out)
	}
	f, err := os.Open(pngPath)
	if err != nil {
		return false, err
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		return false, err
	}
	b := img.Bounds()
	last := -1
	for y := b.Max.Y - 1; y >= b.Min.Y && last < 0; y-- {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if r>>8 != bgR || g>>8 != bgG || bl>>8 != bgB {
				last = y
				break
			}
		}
	}
	if last < 0 {
		return false, errors.New(pngPath + ": пустой кадр")
	}
	fits := last < b.Max.Y-8
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	si, ok := img.(subImager)
	if !ok {
		return false, fmt.Errorf("png: unexpected image type %T", img)
	}
	cropped := si.SubImage(image.Rect(b.Min.X, b.Min.Y, b.Max.X, min(last+24, b.Max.Y)))
	out, err := os.Create(pngPath)
	if err != nil {
		return false, err
	}
	defer out.Close()
	if err := png.Encode(out, cropped); err != nil {
		return false, err
	}
	return fits, nil
}
