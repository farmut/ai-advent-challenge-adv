# weatherbot — правила проекта

Telegram-бот: по запросу пользователя присылает текущую погоду из OpenWeatherMap.
Правила ниже обязательны. Если требование противоречит удобству — побеждает требование.

## 1. Стек

- Go 1.26, модуль `weatherbot` (`go.mod`: `module weatherbot`, `go 1.26`).
- Только стандартная библиотека: `net/http`, `encoding/json`, `log/slog`, `net/url`, `net/http/httptest`.
- Сторонние модули запрещены (никаких telegram-bot-api, godotenv, testify, viper). `go.sum` должен отсутствовать.
- Бот — HTTP-клиент (long polling), входящий HTTP-сервер не нужен. Если когда-нибудь появится
  входящий HTTP (webhook, healthcheck) — только через Gin в `internal/delivery/http`, это единственное исключение.

## 2. Архитектура

- Clean Architecture, зависимости строго внутрь: `delivery → usecase → domain`; `repository` реализует порты usecase.
- `domain` импортирует только stdlib и без `net/http`, `log/slog`, `encoding/json`: только сущности и ошибки.
- `usecase` не знает про Telegram, HTTP и JSON. Бизнес-правила (валидация города) живут здесь.
- `delivery/telegram`: разбор команд, форматирование, отправка. Бизнес-логики нет.
- Интерфейсы объявляет потребитель (в пакете, который их использует), маленькие, 1–2 метода.
- Сборка зависимостей (DI) только в `cmd/weatherbot/main.go`. Других мест, где создаются адаптеры, нет.
- Без глобальных `var` (исключение — sentinel-ошибки `ErrXxx`) и без `init()`.
- `panic`, `log.Fatal`, `os.Exit` — только в `main`.

## 3. Структура папок

```
cmd/weatherbot/main.go                  точка входа, DI, сигналы
internal/config/config.go               чтение env, валидация (+config_test.go)
internal/domain/weather.go              сущность Weather
internal/domain/errors.go               sentinel-ошибки
internal/usecase/weather.go             WeatherService + порт WeatherProvider (+weather_test.go)
internal/repository/openweather/client.go   адаптер OWM (+client_test.go)
internal/delivery/telegram/client.go    Bot API: getUpdates, sendMessage (+client_test.go)
internal/delivery/telegram/poller.go    цикл long polling (+poller_test.go)
internal/delivery/telegram/handler.go   команды, rate limit, recover (+handler_test.go)
internal/delivery/telegram/messages.go  ВСЕ тексты для пользователя
internal/delivery/telegram/format.go    форматирование погоды (+format_test.go)
.env.example  Makefile  README.md
```

## 4. Naming

- Пакеты: короткие, lower, одно слово. Запрещены `util`, `utils`, `common`, `helpers`, `base`.
- Без stutter: `openweather.Client`, `telegram.Poller`, а не `openweather.OpenWeatherClient`.
- Конструкторы `NewX`, возвращают конкретный тип (`*Client`), принимают интерфейсы.
- Ошибки: `ErrXxx`; текст ошибки lowercase, без точки в конце: `errors.New("city not found")`.
- Инициализмы целиком: `baseURL`, `chatID`, `HTTPClient`, `APIKey` (не `baseUrl`, `chatId`).
- Файлы `snake_case.go`, тесты рядом с кодом: `client.go` + `client_test.go`.
- Обёртка ошибок: `fmt.Errorf("get current weather: %w", err)` — что делали, без «failed to».

## 5. Конфигурация

| Переменная             | Обязательна | Default                          |
|------------------------|-------------|----------------------------------|
| `TELEGRAM_BOT_TOKEN`   | да          | —                                |
| `OPENWEATHER_API_KEY`  | да          | —                                |
| `TELEGRAM_BASE_URL`    | нет         | `https://api.telegram.org`       |
| `OPENWEATHER_BASE_URL` | нет         | `https://api.openweathermap.org` |
| `LOG_LEVEL`            | нет         | `info` (`debug/info/warn/error`) |

- `config.Load()` собирает все проблемы через `errors.Join` и возвращает их разом (fail-fast при старте).
- `os.Getenv` / `os.LookupEnv` вызываются только в `internal/config`. Секреты никогда не логируются.
- `.env.example` содержит все переменные с пустыми значениями. `.env` в `.gitignore`.

## 6. Контракты

**OpenWeatherMap:** `GET {base}/data/2.5/weather?q=<city>&units=metric&lang=ru&appid=<key>`,
query собирается только через `url.Values`. Решение принимается по HTTP-статусу; поле `cod`
не парсить (при ошибке это строка, при успехе число).

| Статус                           | Ошибка                          |
|----------------------------------|---------------------------------|
| 200                              | decode → `domain.Weather`       |
| 404                              | `domain.ErrCityNotFound`        |
| 401                              | `domain.ErrProviderAuth`        |
| 429                              | `domain.ErrRateLimited`         |
| 5xx, прочие, timeout, битый JSON | `domain.ErrProviderUnavailable` |

**Telegram Bot API:** `getUpdates` с `offset=last_update_id+1`, `timeout=30`,
`allowed_updates=["message"]`; `http.Client.Timeout = 40s` (больше long-poll timeout).
На 429 ждать `parameters.retry_after` секунд (с учётом `ctx`), на прочие ошибки — backoff.
`sendMessage` без `parse_mode` (plain text, ввод пользователя не интерпретируется как разметка).

**Порты:** `usecase.WeatherProvider.Current(ctx, city string) (domain.Weather, error)` и
`telegram.Sender.Send(ctx, chatID int64, text string) error` (см. 9.2 и раздел 11). Адаптеры принимают `baseURL` и `*http.Client` в конструкторе — это делает их тестируемыми через `httptest`.

## 7. Безопасность

- Перед логированием ошибки HTTP-клиента вычищать токен и ключ: `*url.Error` содержит полный URL,
  а в URL Telegram токен стоит прямо в пути (`/bot<token>/getUpdates`).
- Никаких `fmt.Sprintf` и конкатенации для URL с пользовательским вводом или секретами: `url.Values`, `url.JoinPath`.
- Валидация города в usecase до внешнего вызова: trim и схлопывание пробелов, 1..64 руны, только буквы,
  пробел, `-`, `.`, `,`; управляющие символы и невалидный UTF-8 отвергаются (`domain.ErrInvalidCity`).
- Тело ответа читать через `io.LimitReader(body, 1<<20)`. `StatusCode` проверять до decode.
- Per-chat rate limit: простой token bucket на stdlib (`sync.Mutex` + `map[int64]time.Time`), 1 запрос / 2 с.
- Обработка каждого апдейта под `recover`: паника в одном сообщении не роняет бота.
- Пользователю — только обобщённые тексты из `messages.go`, никогда не `err.Error()`.

## 8. UX

- Команды: `/start`, `/help`, `/weather <город>`. `/weather@MyBot Казань` нормализуется в `/weather`.
- Просто текст без `/` = название города. Неизвестная команда → текст `/help`.
- Эталонный ответ (plain text):

```
🌤 Москва, RU
🌡 +12 °C, ощущается как +10 °C
☁️ Переменная облачность
💧 Влажность 71 %
💨 Ветер 4 м/с, СЗ
```

- Температура округляется `math.Round`, знак обязателен (`+12`, `-3`, `0` без знака); ветер — округлённые м/с
  и 8 румбов `С, СВ, В, ЮВ, Ю, ЮЗ, З, СЗ`; описание с заглавной буквы (по рунам); город и страна — из ответа API.
- Тексты:
  - `/start`: «Привет! Я подскажу погоду. Просто напиши название города, например: Казань»
  - `/help`: список команд и пример `/weather Казань`
  - не найден: «🤔 Не нашёл город «X». Проверь написание или попробуй по-английски»
  - недоступен (unavailable, auth, rate limited): «⚠️ Сервис погоды сейчас недоступен. Попробуй через пару минут»
  - пустой ввод: «Напиши город после команды, например: /weather Пермь»
  - некорректный город: «Название города может содержать только буквы, пробел, дефис, точку и запятую»
  - слишком часто: «⏳ Не так быстро, попробуй через пару секунд»
- Все тексты — только в `messages.go`, константами или функциями `msgXxx`.

## 9. Примеры хорошего кода

**9.1. Domain: сущность и sentinel-ошибки.** Почему так: domain без зависимостей, ошибки сравниваются
через `errors.Is`, а delivery выбирает текст пользователю, не зная про HTTP-коды.

```go
// Package domain содержит сущности и ошибки предметной области.
package domain

import "errors"

// Weather — текущая погода в городе.
type Weather struct {
	City        string  // название из ответа провайдера
	Country     string  // ISO 3166-1 alpha-2, например "RU"
	TempC       float64 // температура, °C
	FeelsLikeC  float64 // ощущается как, °C
	Description string  // описание на русском, как прислал провайдер
	Humidity    int     // влажность, %
	WindSpeed   float64 // скорость ветра, м/с
	WindDeg     int     // направление ветра, градусы, 0 = север
}

var (
	ErrInvalidCity         = errors.New("invalid city")
	ErrCityNotFound        = errors.New("city not found")
	ErrProviderAuth        = errors.New("weather provider auth failed")
	ErrRateLimited         = errors.New("weather provider rate limited")
	ErrProviderUnavailable = errors.New("weather provider unavailable")
)
```

**9.2. Usecase: порт объявлен у потребителя, валидация до внешнего вызова.** Почему так: usecase
тестируется фейком за 3 строки, мусорный ввод не уходит во внешний API.

```go
// Package usecase содержит бизнес-логику бота.
package usecase

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"weatherbot/internal/domain"
)

const maxCityRunes = 64

// WeatherProvider — источник текущей погоды.
type WeatherProvider interface {
	Current(ctx context.Context, city string) (domain.Weather, error)
}

// WeatherService отдаёт погоду по названию города.
type WeatherService struct {
	provider WeatherProvider
}

// NewWeatherService создаёт сервис поверх провайдера погоды.
func NewWeatherService(provider WeatherProvider) *WeatherService {
	return &WeatherService{provider: provider}
}

// Current валидирует город и запрашивает погоду у провайдера.
func (s *WeatherService) Current(ctx context.Context, rawCity string) (domain.Weather, error) {
	city, err := normalizeCity(rawCity)
	if err != nil {
		return domain.Weather{}, err
	}
	w, err := s.provider.Current(ctx, city)
	if err != nil {
		return domain.Weather{}, fmt.Errorf("get current weather: %w", err)
	}
	return w, nil
}

func normalizeCity(raw string) (string, error) {
	city := strings.Join(strings.Fields(raw), " ")
	if n := utf8.RuneCountInString(city); n == 0 || n > maxCityRunes {
		return "", domain.ErrInvalidCity
	}
	for _, r := range city {
		if !unicode.IsLetter(r) && !strings.ContainsRune(" -.,", r) {
			return "", domain.ErrInvalidCity // заодно отсекает utf8.RuneError и управляющие
		}
	}
	return city, nil
}
```

**9.3. Адаптер OpenWeatherMap.** Почему так: URL собирается безопасно, тело ограничено, статус проверяется
до decode, наружу уходят только доменные ошибки, ключ не попадает в логи.

```go
// Package openweather — адаптер к OpenWeatherMap Current Weather API.
package openweather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"weatherbot/internal/domain"
)

const maxBodyBytes = 1 << 20 // 1 MiB

// Client запрашивает текущую погоду у OpenWeatherMap.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewClient создаёт клиент. httpClient обязан иметь Timeout.
func NewClient(baseURL, apiKey string, httpClient *http.Client) *Client {
	return &Client{baseURL: baseURL, apiKey: apiKey, httpClient: httpClient}
}

type currentResponse struct {
	Name string `json:"name"`
	Sys  struct {
		Country string `json:"country"`
	} `json:"sys"`
	Main struct {
		Temp      float64 `json:"temp"`
		FeelsLike float64 `json:"feels_like"`
		Humidity  int     `json:"humidity"`
	} `json:"main"`
	Weather []struct {
		Description string `json:"description"`
	} `json:"weather"`
	Wind struct {
		Speed float64 `json:"speed"`
		Deg   int     `json:"deg"`
	} `json:"wind"`
}

// Current возвращает текущую погоду в городе.
func (c *Client) Current(ctx context.Context, city string) (domain.Weather, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return domain.Weather{}, fmt.Errorf("parse base url: %w", err)
	}
	u = u.JoinPath("data", "2.5", "weather")
	q := url.Values{}
	q.Set("q", city)
	q.Set("units", "metric")
	q.Set("lang", "ru")
	q.Set("appid", c.apiKey)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return domain.Weather{}, fmt.Errorf("build request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return domain.Weather{}, fmt.Errorf("%w: %s", domain.ErrProviderUnavailable, c.redact(err.Error()))
	}
	defer resp.Body.Close()

	if err := statusError(resp.StatusCode); err != nil {
		return domain.Weather{}, err
	}
	var body currentResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&body); err != nil {
		return domain.Weather{}, fmt.Errorf("%w: decode response: %v", domain.ErrProviderUnavailable, err)
	}
	return toDomain(body), nil
}

func statusError(code int) error {
	switch {
	case code == http.StatusOK:
		return nil
	case code == http.StatusNotFound:
		return domain.ErrCityNotFound
	case code == http.StatusUnauthorized:
		return domain.ErrProviderAuth
	case code == http.StatusTooManyRequests:
		return domain.ErrRateLimited
	default:
		return fmt.Errorf("%w: status %d", domain.ErrProviderUnavailable, code)
	}
}

func toDomain(r currentResponse) domain.Weather {
	w := domain.Weather{
		City: r.Name, Country: r.Sys.Country,
		TempC: r.Main.Temp, FeelsLikeC: r.Main.FeelsLike, Humidity: r.Main.Humidity,
		WindSpeed: r.Wind.Speed, WindDeg: r.Wind.Deg,
	}
	if len(r.Weather) > 0 {
		w.Description = r.Weather[0].Description
	}
	return w
}

// redact убирает ключ (в сыром и URL-кодированном виде) из текста ошибки.
func (c *Client) redact(s string) string {
	if c.apiKey == "" {
		return s
	}
	s = strings.ReplaceAll(s, c.apiKey, "[REDACTED]")
	return strings.ReplaceAll(s, url.QueryEscape(c.apiKey), "[REDACTED]")
}
```

**9.4. Табличный тест адаптера на `httptest`.** Почему так: сеть не нужна, проверяется и запрос
(путь, параметры), и маппинг каждого статуса в доменную ошибку.

```go
package openweather_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"weatherbot/internal/domain"
	"weatherbot/internal/repository/openweather"
)

const okBody = `{"name":"Москва","sys":{"country":"RU"},"main":{"temp":12.4,"feels_like":9.6,"humidity":71},
"weather":[{"description":"переменная облачность"}],"wind":{"speed":4.2,"deg":315}}`

func TestClientCurrent(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		wantErr  error
		wantCity string
	}{
		{name: "ok", status: http.StatusOK, body: okBody, wantCity: "Москва"},
		{name: "not found", status: http.StatusNotFound, body: `{"cod":"404","message":"city not found"}`, wantErr: domain.ErrCityNotFound},
		{name: "bad key", status: http.StatusUnauthorized, body: `{"cod":401}`, wantErr: domain.ErrProviderAuth},
		{name: "rate limited", status: http.StatusTooManyRequests, body: `{}`, wantErr: domain.ErrRateLimited},
		{name: "server error", status: http.StatusInternalServerError, body: `oops`, wantErr: domain.ErrProviderUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				q := r.URL.Query()
				if r.URL.Path != "/data/2.5/weather" || q.Get("q") != "Москва" || q.Get("appid") != "test-key" {
					t.Errorf("unexpected request: %s", r.URL)
				}
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			t.Cleanup(srv.Close)

			c := openweather.NewClient(srv.URL, "test-key", srv.Client())
			got, err := c.Current(t.Context(), "Москва")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Current() err = %v, want %v", err, tt.wantErr)
			}
			if got.City != tt.wantCity {
				t.Errorf("Current() city = %q, want %q", got.City, tt.wantCity)
			}
		})
	}
}
```

**9.5. main: конфиг, DI, graceful shutdown, JSON-логи.** Почему так: всё связывание в одном месте,
`run()` возвращает ошибку, `os.Exit` ровно один, Ctrl+C / SIGTERM корректно останавливают polling.

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"weatherbot/internal/config"
	"weatherbot/internal/delivery/telegram"
	"weatherbot/internal/repository/openweather"
	"weatherbot/internal/usecase"
)

func main() {
	if err := run(); err != nil {
		slog.Error("weatherbot stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	weather := openweather.NewClient(cfg.OpenWeatherBaseURL, cfg.OpenWeatherAPIKey, &http.Client{Timeout: 10 * time.Second})
	service := usecase.NewWeatherService(weather)
	bot := telegram.NewClient(cfg.TelegramBaseURL, cfg.TelegramBotToken, &http.Client{Timeout: 40 * time.Second})
	handler := telegram.NewHandler(service, bot, logger)
	poller := telegram.NewPoller(bot, handler, logger)

	logger.Info("weatherbot started")
	if err := poller.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("run poller: %w", err)
	}
	logger.Info("weatherbot stopped")
	return nil
}
```

## 10. Антипаттерны (запрещено)

**10.1. Клиент без таймаута, глобальное состояние.**
❌ `var client = http.DefaultClient` + `func init() { token = os.Getenv("TOKEN") }`
✅ `&http.Client{Timeout: 10 * time.Second}` создаётся в `main` и передаётся в `NewClient`; env читает `config.Load()`.

**10.2. URL через форматирование строк.**
❌ `fmt.Sprintf("%s/data/2.5/weather?q=%s&appid=%s", base, city, key)` — `city="a&appid=x"` ломает запрос.
✅ `url.Values{}` + `q.Encode()` + `u.JoinPath(...)` (пример 9.3).

**10.3. Утечка секретов и внутренностей.**
❌ `slog.Error("send failed", "err", err)` где `err` — `*url.Error` с `https://api.telegram.org/bot123:ABC/sendMessage`;
❌ `h.sender.Send(ctx, chatID, "Ошибка: "+err.Error())`.
✅ Логировать `redact(err.Error())`; пользователю — `msgUnavailable` из `messages.go`.

**10.4. Нарушение слоёв.**
❌ В `handler.go`: `http.Get(...)` к OWM или валидация города; в `domain`: `import "net/http"` / `"log/slog"`.
✅ Handler: разобрать команду → вызвать usecase → отформатировать. Валидация — usecase, HTTP — repository.

**10.5. Тесты с сетью, паника вне main.**
❌ `openweather.NewClient("https://api.openweathermap.org", os.Getenv("OPENWEATHER_API_KEY"), ...)` в тесте;
❌ `log.Fatal(err)` или `panic(err)` внутри `config`, `poller`, адаптеров.
✅ `httptest.NewServer` + фейковый ключ `"test-key"`; ошибки возвращаются вверх до `run()` в `main`.

## 11. Шаблон типичного файла

Порядок: package doc comment → импорты (группа stdlib, пустая строка, группа `weatherbot/...`) →
константы → типы → интерфейсы потребителя → конструктор `NewX` → экспортируемые методы →
неэкспортируемые хелперы внизу. Экспортировать только то, что нужно другим пакетам.

```go
// Package telegram — delivery-слой: long polling Telegram Bot API и обработка команд.
package telegram

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"weatherbot/internal/domain"
)

const perChatInterval = 2 * time.Second

// WeatherGetter — то, что handler'у нужно от usecase.
type WeatherGetter interface {
	Current(ctx context.Context, city string) (domain.Weather, error)
}

// Sender отправляет текст в чат.
type Sender interface {
	Send(ctx context.Context, chatID int64, text string) error
}

// Handler превращает входящее сообщение в ответ.
type Handler struct {
	weather WeatherGetter
	sender  Sender
	log     *slog.Logger
	mu      sync.Mutex
	lastReq map[int64]time.Time // token bucket ёмкостью 1 на чат
}

// NewHandler создаёт обработчик сообщений.
func NewHandler(weather WeatherGetter, sender Sender, log *slog.Logger) *Handler {
	return &Handler{weather: weather, sender: sender, log: log, lastReq: make(map[int64]time.Time)}
}

// Handle обрабатывает одно сообщение. Паника не роняет бота.
func (h *Handler) Handle(ctx context.Context, chatID int64, text string) {
	defer func() {
		if r := recover(); r != nil {
			h.log.Error("handle message panicked", "chat_id", chatID, "panic", r)
		}
	}()
	reply := msgTooFrequent
	if h.allow(chatID, time.Now()) {
		reply = h.respond(ctx, text)
	}
	if err := h.sender.Send(ctx, chatID, reply); err != nil {
		h.log.Warn("send message", "chat_id", chatID, "err", err) // Sender уже вернул redacted-ошибку
	}
}

func (h *Handler) respond(ctx context.Context, text string) string {
	cmd, city := parseCommand(text)
	switch {
	case cmd == "/start":
		return msgStart
	case cmd != "/weather" && cmd != "":
		return msgHelp
	case city == "":
		return msgEmptyCity
	}
	w, err := h.weather.Current(ctx, city)
	switch {
	case err == nil:
		return formatWeather(w)
	case errors.Is(err, domain.ErrInvalidCity):
		return msgInvalidCity
	case errors.Is(err, domain.ErrCityNotFound):
		return msgCityNotFound(city)
	default:
		h.log.Warn("weather request failed", "err", err)
		return msgUnavailable
	}
}

func (h *Handler) allow(chatID int64, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if last, ok := h.lastReq[chatID]; ok && now.Sub(last) < perChatInterval {
		return false
	}
	h.lastReq[chatID] = now
	return true
}

// parseCommand: "/weather@MyBot Казань" → ("/weather", "Казань"); "Казань" → ("", "Казань").
func parseCommand(text string) (cmd, arg string) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", text
	}
	head, rest, _ := strings.Cut(text, " ")
	head, _, _ = strings.Cut(head, "@")
	return strings.ToLower(head), strings.TrimSpace(rest)
}
```

## 12. Definition of Done

Задача сделана, только если: `go build ./...` и `go vet ./...` без ошибок; `gofmt -l .` пуст;
`go test -race -cover ./...` зелёный, тесты без сети и реальных токенов (покрыты config, usecase,
openweather, telegram client/poller/handler/format); `README.md` описывает env, `go run ./cmd/weatherbot`
и `make test`; `Makefile` с целями `build`, `run`, `test`, `lint` (`go vet` + `gofmt -l`).
