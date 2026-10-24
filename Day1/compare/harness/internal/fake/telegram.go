// Package fake содержит фейковые Telegram Bot API, OpenWeatherMap и
// MITM HTTPS-прокси для black-box тестирования ботов.
package fake

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
)

// longPollHold — сколько фейк держит пустой getUpdates (вместо timeout бота).
const longPollHold = time.Second

// SentMessage — зафиксированный вызов sendMessage.
type SentMessage struct {
	ChatID    int64
	Text      string
	ParseMode string
	At        time.Time
}

// Telegram — фейковый Bot API.
type Telegram struct {
	token string
	log   *log.Logger

	mu       sync.Mutex
	nextID   int64
	pending  []json.RawMessage // ещё не подтверждённые offset'ом апдейты
	ids      []int64
	wake     chan struct{} // закрывается при появлении новых апдейтов
	sent     []SentMessage
	methods  map[string]int
	firstReq chan struct{}
	gotFirst bool
}

// NewTelegram создаёт фейк, принимающий только указанный токен.
func NewTelegram(token string, logger *log.Logger) *Telegram {
	return &Telegram{
		token:    token,
		log:      logger,
		nextID:   500000,
		wake:     make(chan struct{}),
		methods:  map[string]int{},
		firstReq: make(chan struct{}),
	}
}

// FirstRequest закрывается при первом обращении к Bot API с верным токеном.
func (t *Telegram) FirstRequest() <-chan struct{} { return t.firstReq }

// Enqueue добавляет входящие сообщения и возвращает момент их публикации.
func (t *Telegram) Enqueue(chatID int64, texts ...string) time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for _, text := range texts {
		t.nextID++
		id := t.nextID
		msg := map[string]any{
			"message_id": id,
			"date":       now.Unix(),
			"chat":       map[string]any{"id": chatID, "type": "private", "first_name": "Tester"},
			"from":       map[string]any{"id": chatID, "is_bot": false, "first_name": "Tester", "username": "tester", "language_code": "ru"},
			"text":       text,
		}
		if strings.HasPrefix(text, "/") {
			head, _, _ := strings.Cut(text, " ")
			msg["entities"] = []map[string]any{{"type": "bot_command", "offset": 0, "length": len(utf16.Encode([]rune(head)))}}
		}
		raw, _ := json.Marshal(map[string]any{"update_id": id, "message": msg})
		t.pending = append(t.pending, raw)
		t.ids = append(t.ids, id)
	}
	close(t.wake)
	t.wake = make(chan struct{})
	return now
}

// Sent возвращает копию отправленных ботом сообщений, начиная с from.
func (t *Telegram) Sent(from int) []SentMessage {
	t.mu.Lock()
	defer t.mu.Unlock()
	if from >= len(t.sent) {
		return nil
	}
	return append([]SentMessage(nil), t.sent[from:]...)
}

// SentCount — число вызовов sendMessage.
func (t *Telegram) SentCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.sent)
}

// Methods — счётчик вызовов методов Bot API.
func (t *Telegram) Methods() map[string]int {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]int, len(t.methods))
	for k, v := range t.methods {
		out[k] = v
	}
	return out
}

func (t *Telegram) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	prefix := "/bot" + t.token + "/"
	path := r.URL.Path
	if !strings.HasPrefix(path, prefix) {
		t.log.Printf("TG %s %s -> 404 (wrong token or path)", r.Method, redactPath(path, t.token))
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error_code": 404, "description": "Not Found"})
		return
	}
	method := strings.TrimPrefix(path, prefix)
	params, err := readParams(r)
	if err != nil {
		t.log.Printf("TG %s %s -> 400 %v", r.Method, method, err)
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error_code": 400, "description": "Bad Request: " + err.Error()})
		return
	}

	t.mu.Lock()
	t.methods[method]++
	if !t.gotFirst {
		t.gotFirst = true
		close(t.firstReq)
	}
	t.mu.Unlock()

	switch strings.ToLower(method) {
	case "getupdates":
		t.getUpdates(w, r, params)
	case "sendmessage":
		t.sendMessage(w, params)
	case "getme":
		t.log.Printf("TG getMe")
		writeOK(w, map[string]any{"id": 123456, "is_bot": true, "first_name": "WeatherBot", "username": "WeatherBot"})
	default:
		t.log.Printf("TG %s params=%v", method, params)
		writeOK(w, true)
	}
}

func (t *Telegram) getUpdates(w http.ResponseWriter, r *http.Request, p map[string]string) {
	offset, _ := strconv.ParseInt(p["offset"], 10, 64)
	timeout, _ := strconv.Atoi(p["timeout"])

	deadline := time.NewTimer(min(time.Duration(timeout)*time.Second, longPollHold))
	defer deadline.Stop()
	for {
		t.mu.Lock()
		if offset > 0 { // подтверждённые апдейты больше не отдаются
			i := 0
			for i < len(t.ids) && t.ids[i] < offset {
				i++
			}
			t.ids, t.pending = t.ids[i:], t.pending[i:]
		}
		batch := append([]json.RawMessage(nil), t.pending...)
		wake := t.wake
		t.mu.Unlock()

		if len(batch) > 0 || timeout <= 0 {
			t.log.Printf("TG getUpdates offset=%d timeout=%d -> %d updates", offset, timeout, len(batch))
			if batch == nil {
				batch = []json.RawMessage{}
			}
			writeOK(w, batch)
			return
		}
		select {
		case <-wake:
		case <-deadline.C:
			timeout = 0
		case <-r.Context().Done():
			return
		}
	}
}

func (t *Telegram) sendMessage(w http.ResponseWriter, p map[string]string) {
	chatID, err := strconv.ParseInt(p["chat_id"], 10, 64)
	if err != nil || p["text"] == "" {
		t.log.Printf("TG sendMessage bad params chat_id=%q text_len=%d", p["chat_id"], len(p["text"]))
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error_code": 400, "description": "Bad Request: message text is empty or chat_id invalid"})
		return
	}
	now := time.Now()
	t.mu.Lock()
	t.sent = append(t.sent, SentMessage{ChatID: chatID, Text: p["text"], ParseMode: p["parse_mode"], At: now})
	n := len(t.sent)
	t.mu.Unlock()
	t.log.Printf("TG sendMessage chat_id=%d parse_mode=%q text=%q", chatID, p["parse_mode"], p["text"])
	writeOK(w, map[string]any{
		"message_id": 900000 + n,
		"date":       now.Unix(),
		"chat":       map[string]any{"id": chatID, "type": "private"},
		"from":       map[string]any{"id": 123456, "is_bot": true, "first_name": "WeatherBot", "username": "WeatherBot"},
		"text":       p["text"],
	})
}

// readParams собирает параметры из query, JSON или form-тела в строки.
func readParams(r *http.Request) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	if r.Body == nil {
		return out, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if len(body) == 0 {
		return out, nil
	}
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch ct {
	case "application/json":
		var m map[string]any
		dec := json.NewDecoder(strings.NewReader(string(body)))
		dec.UseNumber()
		if err := dec.Decode(&m); err != nil {
			return nil, fmt.Errorf("decode json: %w", err)
		}
		for k, v := range m {
			switch x := v.(type) {
			case string:
				out[k] = x
			case json.Number:
				out[k] = x.String()
			default:
				b, _ := json.Marshal(x)
				out[k] = string(b)
			}
		}
	case "application/x-www-form-urlencoded", "":
		vals, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, fmt.Errorf("decode form: %w", err)
		}
		for k, v := range vals {
			if len(v) > 0 {
				out[k] = v[0]
			}
		}
	default:
		return nil, fmt.Errorf("unsupported content type %q", ct)
	}
	return out, nil
}

func writeOK(w http.ResponseWriter, result any) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func redactPath(path, token string) string {
	return strings.ReplaceAll(path, token, "<TOKEN>")
}
