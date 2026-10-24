# Эталонные факты для профиля tech-writer (W2)

Файл агенту НЕ показывается. Все значения сверены с `Day2/src`.

## Переменные окружения (internal/config/config.go:33-36, 54)

| Переменная | Обязательна | По умолчанию | Валидация |
|---|---|---|---|
| `TELEGRAM_BOT_TOKEN` | да | — | формат `<digits>:<[A-Za-z0-9_-]+>` (config.go:106-125) |
| `OPENWEATHER_API_KEY` | да | — | только непустота (config.go:45-47) |
| `TELEGRAM_BASE_URL` | нет | `https://api.telegram.org` (config.go:14) | http/https, host обязателен, без query/fragment (config.go:74-89) |
| `OPENWEATHER_BASE_URL` | нет | `https://api.openweathermap.org` (config.go:15) | то же |
| `LOG_LEVEL` | нет | `info` (config.go:16) | debug/info/warn/error без учёта регистра (config.go:91-104) |

Пробелы по краям обрезаются (config.go:67-72). Все ошибки сразу через `errors.Join` (config.go:60), выход с кодом 1 (main.go:22-25).

## Таймауты и лимиты

| Факт | Значение | Где |
|---|---|---|
| HTTP-клиент OWM | 10s | cmd/weatherbot/main.go:38 |
| HTTP-клиент Telegram | 40s | cmd/weatherbot/main.go:40 |
| long poll timeout | 30 s | internal/delivery/telegram/client.go:20 |
| лимит тела ответа | 1 MiB | telegram/client.go:19, openweather/client.go:16 |
| интервал на чат | 2s | internal/delivery/telegram/handler.go:16 |
| maxTrackedChats | 10000 | internal/delivery/telegram/handler.go:17 |
| длина города | 1..64 рун | internal/usecase/weather.go:14,49 |
| backoff getUpdates | 1s → 30s, удвоение; `retry_after` только для getUpdates | internal/delivery/telegram/poller.go:11-12,54-60 |

## Неточности исходного README (должны быть исправлены)

1. «Логи — JSON в stdout»: ошибка загрузки конфига пишется дефолтным `slog` (текст, stderr) — main.go:23; JSON-логгер в stdout создаётся только после загрузки конфига — main.go:33.
2. «graceful shutdown» (раздел «Структура»): при SIGTERM контекст отменяется, текущие запросы к OWM/Telegram обрываются, а не дожидаются завершения — main.go:35, poller.go:43-50, handler.go (ctx передаётся в Send/Current).

## Машиночитаемые проверки (используются checks/writer.sh)

```env
TELEGRAM_BOT_TOKEN
OPENWEATHER_API_KEY
TELEGRAM_BASE_URL
OPENWEATHER_BASE_URL
LOG_LEVEL
```

```defaults
TELEGRAM_BASE_URL ::: https://api\.telegram\.org
OPENWEATHER_BASE_URL ::: https://api\.openweathermap\.org
LOG_LEVEL ::: \binfo\b
```

```limits
OWM timeout 10s ::: \b10\s*(s|с|сек)
Telegram timeout 40s ::: \b40\s*(s|с|сек)
long poll 30s ::: \b30\s*(s|с|сек)
body 1 MiB ::: 1\s*MiB|1\s*МиБ|1\s*MB|1\s*МБ|1 << 20
per-chat 2s ::: \b2\s*(s|с|сек)
maxTrackedChats ::: 10[ _ ]?000
city 64 ::: \b64\b
backoff 1s..30s ::: \b1\s*(s|с|сек)\w*.{0,80}30\s*(s|с|сек)
```
