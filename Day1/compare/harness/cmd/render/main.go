// Command render строит из transcripts/{a,b}.json HTML-«скриншоты» переписки
// в стиле Telegram и (с -screenshots) PNG через headless Chrome.
package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"image"
	"image/png"
	"log"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"harness/internal/transcript"
)

//go:embed page.html.tmpl
var tmplFS embed.FS

const (
	chromeDefault = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	maxShotHeight = 16000
	bgR, bgG, bgB = 0xc9, 0xd8, 0xe3 // цвет фона страницы, по нему обрезаем PNG
)

var titles = map[string]string{
	"a": "Вариант A — общий CLAUDE.md",
	"b": "Вариант B — подробный CLAUDE.md",
}

func main() {
	compare := flag.String("compare", "..", "каталог Day1/compare")
	shots := flag.Bool("screenshots", false, "снять PNG через headless Chrome")
	chrome := flag.String("chrome", chromeDefault, "путь к Chrome")
	flag.Parse()

	dir, err := filepath.Abs(*compare)
	if err != nil {
		log.Fatal(err)
	}
	runs := map[string]*transcript.Run{}
	for _, v := range []string{"a", "b"} {
		r, err := load(filepath.Join(dir, "transcripts", v+".json"))
		if err != nil {
			log.Fatalf("load %s: %v", v, err)
		}
		runs[v] = r
	}
	tmpl := template.Must(template.New("page.html.tmpl").Funcs(template.FuncMap{
		"mark": func(ok bool) string {
			if ok {
				return "✓"
			}
			return "✗"
		},
	}).ParseFS(tmplFS, "page.html.tmpl"))

	pages := []struct {
		name     string
		variants []string
		width    int
	}{
		{"chat-a", []string{"a"}, 600},
		{"chat-b", []string{"b"}, 600},
		{"chat-side-by-side", []string{"a", "b"}, 1200},
	}
	for _, p := range pages {
		all := buildPage(runs, p.variants, nil, "")
		htmlPath := filepath.Join(dir, p.name+".html")
		if err := writePage(tmpl, htmlPath, all); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %s", htmlPath)
		if !*shots {
			continue
		}
		if err := os.MkdirAll(filepath.Join(dir, "screenshots"), 0o755); err != nil {
			log.Fatal(err)
		}
		pngPath := filepath.Join(dir, "screenshots", p.name+".png")
		fits, err := screenshot(*chrome, htmlPath, pngPath, p.width)
		if err != nil {
			log.Fatal(err)
		}
		if fits {
			log.Printf("wrote %s", pngPath)
			continue
		}
		// Не влезло в один кадр — делим сценарии пополам.
		_ = os.Remove(pngPath)
		ids := scenarioIDs(runs[p.variants[0]])
		half := len(ids) / 2
		for i, part := range [][]int{ids[:half], ids[half:]} {
			suffix := fmt.Sprintf("-part%d", i+1)
			partHTML := filepath.Join(dir, p.name+suffix+".html")
			label := fmt.Sprintf("часть %d из 2 (сценарии %d–%d)", i+1, part[0], part[len(part)-1])
			if err := writePage(tmpl, partHTML, buildPage(runs, p.variants, part, label)); err != nil {
				log.Fatal(err)
			}
			partPNG := filepath.Join(dir, "screenshots", p.name+suffix+".png")
			if ok, err := screenshot(*chrome, partHTML, partPNG, p.width); err != nil || !ok {
				log.Fatalf("screenshot %s: fits=%v err=%v", partPNG, ok, err)
			}
			log.Printf("wrote %s", partPNG)
		}
	}
}

func load(path string) (*transcript.Run, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r transcript.Run
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func scenarioIDs(r *transcript.Run) []int {
	ids := make([]int, 0, len(r.Scenarios))
	for _, s := range r.Scenarios {
		ids = append(ids, s.ID)
	}
	return ids
}

// --- view model ---

type pageView struct {
	Title     string
	PartLabel string
	Wide      bool
	Columns   []columnView
	Rows      []rowView
	Generated string
}

type columnView struct {
	Title   string
	Summary []chip
	Notes   []string
}

type chip struct {
	OK   bool
	Text string
}

type rowView struct {
	ID    int
	Title string
	Cells []cellView
}

type cellView struct {
	Missing  bool
	Inputs   []bubble
	Replies  []bubble
	NoReply  string
	Marks    []chip
	Info     string
	ChatNote string
}

type bubble struct {
	Text  string
	Meta  string
	Count int
}

func buildPage(runs map[string]*transcript.Run, variants []string, only []int, part string) pageView {
	pv := pageView{Wide: len(variants) > 1, PartLabel: part, Generated: time.Now().Format("2006-01-02 15:04")}
	if pv.Wide {
		pv.Title = "Telegram-бот погоды: A vs B (black-box, фейковые Telegram и OWM)"
	} else {
		pv.Title = titles[variants[0]]
	}
	for _, v := range variants {
		pv.Columns = append(pv.Columns, summarize(v, runs[v]))
	}
	include := func(id int) bool {
		if only == nil {
			return true
		}
		for _, x := range only {
			if x == id {
				return true
			}
		}
		return false
	}
	for _, s := range runs[variants[0]].Scenarios {
		if !include(s.ID) {
			continue
		}
		row := rowView{ID: s.ID, Title: s.Title}
		for _, v := range variants {
			row.Cells = append(row.Cells, buildCell(find(runs[v], s.ID)))
		}
		pv.Rows = append(pv.Rows, row)
	}
	return pv
}

func find(r *transcript.Run, id int) *transcript.Scenario {
	for i := range r.Scenarios {
		if r.Scenarios[i].ID == id {
			return &r.Scenarios[i]
		}
	}
	return nil
}

func summarize(v string, r *transcript.Run) columnView {
	c := columnView{Title: titles[v]}
	modes := map[string]string{
		"env":                     "base URL через env",
		"docker-https-proxy-mitm": "base URL не подменяется → docker + HTTPS_PROXY (MITM)",
		"untestable":              "не тестируемо black-box",
	}
	c.Summary = append(c.Summary, chip{OK: r.Mode == "env", Text: "подмена: " + modes[r.Mode]})
	l := r.Leaks
	c.Summary = append(c.Summary,
		chip{OK: l.TokenInLog == 0, Text: fmt.Sprintf("токен в логе: %d", l.TokenInLog)},
		chip{OK: l.KeyInLog == 0, Text: fmt.Sprintf("ключ в логе: %d", l.KeyInLog)},
		chip{OK: l.TokenInReplies+l.KeyInReplies == 0, Text: fmt.Sprintf("секреты в ответах: %d", l.TokenInReplies+l.KeyInReplies)},
	)
	if sd := r.Shutdown; sd != nil {
		c.Summary = append(c.Summary, chip{OK: sd.Graceful, Text: fmt.Sprintf("SIGTERM: выход за %d мс, код %d", sd.ElapsedMs, sd.ExitCode)})
	}
	passed, total := 0, 0
	for _, s := range r.Scenarios {
		if s.Flags.Expect != nil {
			total++
			if *s.Flags.Expect {
				passed++
			}
		}
	}
	c.Summary = append(c.Summary, chip{OK: passed == total, Text: fmt.Sprintf("ожидаемое поведение: %d/%d", passed, total)})
	if r.Mode != "env" {
		for _, a := range r.Attempts {
			c.Notes = append(c.Notes, mark(a.OK)+" "+a.Name+": "+a.Detail)
		}
	}
	return c
}

func mark(ok bool) string {
	if ok {
		return "✓"
	}
	return "✗"
}

func buildCell(s *transcript.Scenario) cellView {
	if s == nil {
		return cellView{Missing: true}
	}
	var c cellView
	c.Inputs = group(s.Inputs, nil, true)
	if s.ChatID != 1001 {
		c.ChatNote = fmt.Sprintf("чат %d", s.ChatID)
	}
	texts := make([]string, len(s.Replies))
	metas := make([]string, len(s.Replies))
	for i, r := range s.Replies {
		texts[i] = r.Text
		metas[i] = fmt.Sprintf("+%s", fmtMs(r.AtMs))
		if r.ParseMode != "" {
			metas[i] += " · " + r.ParseMode
		}
	}
	c.Replies = group(texts, metas, false)
	if len(s.Replies) == 0 {
		c.NoReply = fmt.Sprintf("нет ответа за %s", fmtMs(s.WaitedMs))
	}

	c.Marks = append(c.Marks, chip{OK: s.Flags.Replied, Text: "ответ"})
	if s.Flags.Expect != nil {
		c.Marks = append(c.Marks, chip{OK: *s.Flags.Expect, Text: s.Flags.ExpectNote})
	}
	c.Marks = append(c.Marks, chip{OK: !s.Flags.LeakInReply, Text: "без утечек"})
	if inj := s.Flags.Injection; inj != nil {
		c.Marks = append(c.Marks, chip{OK: *inj != "VULNERABLE", Text: "инъекция: " + *inj})
	}

	var info []string
	switch n := len(s.OWMQueries); n {
	case 0:
		info = append(info, "в OWM не ходил")
	default:
		statuses := map[int]int{}
		for _, q := range s.OWMQueries {
			statuses[q.Status]++
		}
		parts := []string{}
		for _, st := range slices.Sorted(maps.Keys(statuses)) {
			k := statuses[st]
			label := fmt.Sprint(st)
			if st == 0 {
				label = "клиент оборвал"
			}
			if k > 1 {
				label += fmt.Sprintf("×%d", k)
			}
			parts = append(parts, label)
		}
		info = append(info, fmt.Sprintf("OWM: %d запр. (%s)", n, strings.Join(parts, ", ")))
		q := s.OWMQueries[0]
		if s.Flags.Injection != nil {
			info = append(info, fmt.Sprintf("raw query: %s · appid×%d · q=%q", q.RawQuery, q.AppIDCount, q.Q))
		}
		if q.BytesSent > 1<<20 {
			info = append(info, fmt.Sprintf("прочитано ≈%.1f MiB из 50", float64(q.BytesSent)/(1<<20)))
		}
	}
	if s.FirstReplyMs != nil && *s.FirstReplyMs > 1000 {
		info = append(info, "ответ через "+fmtMs(*s.FirstReplyMs))
	}
	if s.Note != "" {
		info = append(info, s.Note)
	}
	c.Info = strings.Join(info, " · ")
	return c
}

// group сворачивает подряд идущие одинаковые сообщения в одно с «×N».
func group(texts, metas []string, input bool) []bubble {
	var out []bubble
	for i, t := range texts {
		shown := t
		if input && utf8.RuneCountInString(t) > 60 {
			r := []rune(t)
			shown = string(r[:40]) + fmt.Sprintf("… (%d символов)", len(r))
		}
		if n := len(out); n > 0 && texts[i-1] == t {
			out[n-1].Count++
			continue
		}
		b := bubble{Text: shown, Count: 1}
		if metas != nil {
			b.Meta = metas[i]
		}
		out = append(out, b)
	}
	return out
}

func fmtMs(ms int64) string {
	if ms < 1000 {
		return fmt.Sprintf("%d мс", ms)
	}
	return fmt.Sprintf("%.1f с", float64(ms)/1000)
}

func writePage(t *template.Template, path string, pv pageView) error {
	var buf bytes.Buffer
	if err := t.Execute(&buf, pv); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// screenshot снимает страницу высоким окном и обрезает фон снизу.
// fits=false — контент упёрся в нижний край, нужно делить.
func screenshot(chrome, htmlPath, pngPath string, width int) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, chrome, "--headless=new", "--disable-gpu", "--hide-scrollbars",
		"--force-device-scale-factor=1", "--no-first-run", "--no-default-browser-check",
		"--screenshot="+pngPath, fmt.Sprintf("--window-size=%d,%d", width, maxShotHeight), "file://"+htmlPath)
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
		return false, fmt.Errorf("%s: пустой кадр", pngPath)
	}
	if last >= b.Max.Y-8 {
		return false, nil
	}
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
	return true, nil
}
