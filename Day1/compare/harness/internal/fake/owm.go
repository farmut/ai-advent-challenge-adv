package fake

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	slowDelay = 25 * time.Second
	hugeSize  = 50 << 20
)

// OWMRecord — зафиксированный запрос к фейковому OpenWeatherMap.
type OWMRecord struct {
	Scenario    int
	Path        string
	RawQuery    string
	Q           []string
	AppID       []string
	Status      int
	BytesSent   int64
	Duration    time.Duration
	ClientAbort bool
}

// OWM — фейковый OpenWeatherMap Current Weather API (/data/2.5/weather).
type OWM struct {
	key      string
	log      *log.Logger
	scenario atomic.Int64

	mu      sync.Mutex
	records []OWMRecord
}

// NewOWM создаёт фейк, который принимает только ключ key.
func NewOWM(key string, logger *log.Logger) *OWM {
	return &OWM{key: key, log: logger}
}

// SetScenario задаёт номер сценария, к которому относятся следующие запросы.
func (o *OWM) SetScenario(id int) { o.scenario.Store(int64(id)) }

// Records возвращает запросы, относящиеся к сценарию id.
func (o *OWM) Records(id int) []OWMRecord {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []OWMRecord
	for _, r := range o.records {
		if r.Scenario == id {
			out = append(out, r)
		}
	}
	return out
}

func (o *OWM) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	rec := OWMRecord{Scenario: int(o.scenario.Load()), Path: r.URL.Path, RawQuery: r.URL.RawQuery}
	q := r.URL.Query() // url.ParseQuery: '&' и ';' — разделители, как у реального API
	rec.Q, rec.AppID = q["q"], q["appid"]

	cw := &countingWriter{ResponseWriter: w}
	rec.Status = o.respond(r.Context(), cw, r, q.Get("q"))
	rec.BytesSent = cw.n
	rec.Duration = time.Since(start)
	rec.ClientAbort = r.Context().Err() != nil || cw.err != nil

	o.mu.Lock()
	o.records = append(o.records, rec)
	o.mu.Unlock()
	o.log.Printf("OWM scenario=%d %s?%s -> %d bytes=%d dur=%s abort=%v",
		rec.Scenario, rec.Path, redactAppID(rec.RawQuery), rec.Status, rec.BytesSent, rec.Duration.Round(time.Millisecond), rec.ClientAbort)
}

func (o *OWM) respond(ctx context.Context, w http.ResponseWriter, r *http.Request, city string) int {
	if !strings.HasSuffix(r.URL.Path, "/data/2.5/weather") {
		return owmJSON(w, http.StatusNotFound, map[string]any{"cod": "404", "message": "Internal error"})
	}
	if appid := r.URL.Query()["appid"]; len(appid) != 1 || appid[0] != o.key {
		return owmJSON(w, http.StatusUnauthorized, map[string]any{"cod": 401, "message": "Invalid API key. Please see https://openweathermap.org/faq#error401 for more info."})
	}
	switch strings.ToLower(strings.TrimSpace(city)) {
	case "moscow", "москва":
		return owmJSON(w, http.StatusOK, moscow())
	case "london":
		return owmJSON(w, http.StatusOK, london())
	case "error500":
		return owmJSON(w, http.StatusInternalServerError, map[string]any{"cod": 500, "message": "Internal server error"})
	case "ratelimit":
		return owmJSON(w, http.StatusTooManyRequests, map[string]any{"cod": 429, "message": "Your account is temporary blocked due to exceeding of requests limitation of your subscription type. Please choose the proper subscription https://openweathermap.org/price"})
	case "slow":
		t := time.NewTimer(slowDelay)
		defer t.Stop()
		select {
		case <-t.C:
			return owmJSON(w, http.StatusOK, moscow())
		case <-ctx.Done():
			return 0 // клиент сдался раньше
		}
	case "huge":
		return huge(ctx, w)
	default:
		return owmJSON(w, http.StatusNotFound, map[string]any{"cod": "404", "message": "city not found"})
	}
}

// huge отдаёт 200 и ~50 MiB «JSON»: одно бесконечное строковое поле.
// Бот без ограничения размера тела прочитает всё в память.
func huge(ctx context.Context, w http.ResponseWriter) int {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte(`{"cod":200,"name":"`)); err != nil {
		return http.StatusOK
	}
	chunk := bytes.Repeat([]byte("garbage-"), 8192) // 64 KiB
	for sent := 0; sent < hugeSize && ctx.Err() == nil; sent += len(chunk) {
		if _, err := w.Write(chunk); err != nil {
			return http.StatusOK
		}
	}
	_, _ = w.Write([]byte(`"}`))
	return http.StatusOK
}

func owmJSON(w http.ResponseWriter, status int, v any) int {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
	return status
}

type countingWriter struct {
	http.ResponseWriter
	n   int64
	err error
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.ResponseWriter.Write(p)
	c.n += int64(n)
	if err != nil && c.err == nil {
		c.err = err
	}
	return n, err
}

func moscow() map[string]any {
	now := time.Now().Unix()
	return map[string]any{
		"coord":      map[string]any{"lon": 37.6156, "lat": 55.7522},
		"weather":    []map[string]any{{"id": 802, "main": "Clouds", "description": "переменная облачность", "icon": "03d"}},
		"base":       "stations",
		"main":       map[string]any{"temp": 12.3, "feels_like": 10.1, "temp_min": 11.2, "temp_max": 13.4, "pressure": 1012, "humidity": 71, "sea_level": 1012, "grnd_level": 993},
		"visibility": 10000,
		"wind":       map[string]any{"speed": 4.2, "deg": 315, "gust": 7.1},
		"clouds":     map[string]any{"all": 40},
		"dt":         now,
		"sys":        map[string]any{"type": 2, "id": 2094500, "country": "RU", "sunrise": now - 20000, "sunset": now + 15000},
		"timezone":   10800,
		"id":         524901,
		"name":       "Москва",
		"cod":        200,
	}
}

func london() map[string]any {
	now := time.Now().Unix()
	return map[string]any{
		"coord":      map[string]any{"lon": -0.1257, "lat": 51.5085},
		"weather":    []map[string]any{{"id": 500, "main": "Rain", "description": "небольшой дождь", "icon": "10d"}},
		"base":       "stations",
		"main":       map[string]any{"temp": 9.8, "feels_like": 7.4, "temp_min": 8.9, "temp_max": 10.6, "pressure": 1004, "humidity": 87},
		"visibility": 9000,
		"wind":       map[string]any{"speed": 5.7, "deg": 240},
		"clouds":     map[string]any{"all": 90},
		"dt":         now,
		"sys":        map[string]any{"type": 2, "id": 2075535, "country": "GB", "sunrise": now - 18000, "sunset": now + 12000},
		"timezone":   0,
		"id":         2643743,
		"name":       "Лондон",
		"cod":        200,
	}
}

var appidRe = regexp.MustCompile(`(?i)(appid=)[^&;]*`)

// redactAppID скрывает значение appid в логе стенда.
func redactAppID(q string) string { return appidRe.ReplaceAllString(q, "${1}<REDACTED>") }
