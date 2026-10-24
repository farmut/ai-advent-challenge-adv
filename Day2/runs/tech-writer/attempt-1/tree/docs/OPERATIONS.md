# Эксплуатация weatherbot

**Аудитория:** операторы, которые собирают, запускают и сопровождают бота.

Связанные документы: [CONFIGURATION.md](CONFIGURATION.md), [ARCHITECTURE.md](ARCHITECTURE.md),
[TESTING.md](TESTING.md), [README](../README.md).

## Требования

- Go 1.26 (по [go.mod](../go.mod)); внешних Go-зависимостей нет.
- Исходящий HTTPS-доступ к Telegram Bot API и OpenWeatherMap (или к адресам из
  `TELEGRAM_BASE_URL` / `OPENWEATHER_BASE_URL`).
- Входящие порты не нужны: бот работает через long polling.
- Для бота не должен быть настроен webhook: пока он активен, Telegram отклоняет `getUpdates`.
  Бот сам webhook не снимает. Такие отказы он считает временной ошибкой и повторяет запрос с
  backoff (см. [Диагностика](#диагностика)).

## Сборка и запуск

Цели [Makefile](../Makefile):

```sh
make build   # go build -o bin/weatherbot ./cmd/weatherbot
make run     # подгружает .env (если есть) и выполняет go run ./cmd/weatherbot
```

Запуск собранного бинарника:

```sh
export TELEGRAM_BOT_TOKEN=<TELEGRAM_BOT_TOKEN> OPENWEATHER_API_KEY=<OPENWEATHER_API_KEY>
./bin/weatherbot
```

Переменные и их проверка описаны в [CONFIGURATION.md](CONFIGURATION.md). Бинарник `.env` не
читает: его подгружает только `make run`.

Запускайте **один экземпляр на токен**. В коде нет координации между экземплярами: два
процесса с одним токеном будут конкурировать за `getUpdates`.

## Остановка

- `SIGINT` (Ctrl+C) или `SIGTERM` отменяют корневой контекст
  (`signal.NotifyContext`, [main.go:35](../cmd/weatherbot/main.go)).
- Текущий запрос `getUpdates` или пауза backoff прерываются сразу.
- Активные запросы к OpenWeatherMap и `sendMessage` выполняются с тем же контекстом и тоже
  прерываются. Сообщения, которые обрабатывались в момент остановки, остаются без ответа, а в
  лог могут попасть записи `WARN weather request failed` / `WARN send message` с ошибкой отмены
  контекста.
- После выхода из цикла пишется `INFO weatherbot stopped`, код выхода 0
  ([main.go:45-49](../cmd/weatherbot/main.go)).

## Коды выхода

| Код | Когда | Источник |
|-----|-------|----------|
| 0 | остановка по `SIGINT`/`SIGTERM` | [main.go:45-49](../cmd/weatherbot/main.go) |
| 1 | ошибка конфигурации | [main.go:29-32](../cmd/weatherbot/main.go) |
| 1 | Telegram ответил на `getUpdates` статусом 401 или 404 (токен отвергнут или неверный `TELEGRAM_BASE_URL`) | [poller.go:51-53](../internal/delivery/telegram/poller.go), [client.go:134-135](../internal/delivery/telegram/client.go) |

Остальные ошибки Telegram и все ошибки OpenWeatherMap процесс не останавливают.

## Логи

- Рабочие логи — JSON (`slog.NewJSONHandler`) в **stdout**, уровень задаётся `LOG_LEVEL`
  ([main.go:33](../cmd/weatherbot/main.go)). Стандартные поля: `time`, `level`, `msg`.
- Финальная запись при аварийном завершении (`ERROR weatherbot stopped`, поле `err`) пишется
  стандартным логгером `slog` в **stderr в текстовом формате**, не в JSON
  ([main.go:23](../cmd/weatherbot/main.go)). Собирайте оба потока.
- Токен бота и ключ OpenWeatherMap в ошибках заменяются на `[REDACTED]`.
- Тексты сообщений пользователей не логируются. В записях `send message` и
  `handle message panicked` есть `chat_id`. В записи `weather request failed` есть только
  текст ошибки, но при сетевой ошибке или таймауте он включает URL запроса к OpenWeatherMap с
  названием города (ключ в нём заменён на `[REDACTED]`).

Все записи, которые пишет бот:

| Уровень | `msg` | Поля | Что означает | Источник |
|---------|-------|------|--------------|----------|
| INFO | `weatherbot started` | — | конфигурация загружена, начинается polling | [main.go:44](../cmd/weatherbot/main.go) |
| INFO | `weatherbot stopped` | — | штатная остановка по сигналу | [main.go:48](../cmd/weatherbot/main.go) |
| ERROR (stderr, текст) | `weatherbot stopped` | `err` | аварийный выход с кодом 1 | [main.go:23](../cmd/weatherbot/main.go) |
| WARN | `get updates` | `err`, `retry_in` | ошибка `getUpdates`, будет повтор через `retry_in` | [poller.go:61](../internal/delivery/telegram/poller.go) |
| WARN | `weather request failed` | `err` | OpenWeatherMap недоступен, вернул 401/429/5xx/невалидный ответ; пользователь получил «Сервис погоды сейчас недоступен» | [handler.go:79](../internal/delivery/telegram/handler.go) |
| WARN | `send message` | `chat_id`, `err` | ответ не доставлен, повторов нет | [handler.go:56](../internal/delivery/telegram/handler.go) |
| ERROR | `handle message panicked` | `chat_id`, `panic` | паника при обработке сообщения перехвачена, ответ не отправлен | [handler.go:48](../internal/delivery/telegram/handler.go) |

Сообщений уровня DEBUG в коде нет.

## Поведение при сбоях

| Ситуация | Поведение |
|----------|-----------|
| Telegram недоступен, 5xx, сетевая ошибка, невалидный ответ | повтор `getUpdates` с backoff 1 с → 2 с → 4 с … максимум 30 с; успешный запрос сбрасывает backoff ([poller.go](../internal/delivery/telegram/poller.go)) |
| Telegram 429 на `getUpdates` | пауза ровно `retry_after` секунд; если его нет — обычный backoff |
| Telegram 401/404 на `getUpdates` | выход с кодом 1 |
| Ошибка `sendMessage` (любая, включая 429 и 401/404) | `WARN send message`, ответ теряется, бот продолжает работу |
| OpenWeatherMap: неверный ключ (401) | пользователь получает «сервис недоступен», `WARN weather request failed`; бот **не** останавливается — следите за логами |
| OpenWeatherMap: 429, 5xx, таймаут 10 с, невалидный JSON, тело больше 1 MiB | «сервис недоступен» и `WARN weather request failed`; повторов нет |
| OpenWeatherMap: 404 | пользователь получает «Не нашёл город …», в лог не пишется |
| Паника в обработчике сообщения | перехватывается, `ERROR handle message panicked`, обработка следующих сообщений продолжается |

## Ограничения

- **Последовательная обработка.** Сообщения обрабатываются по одному. Пока идёт запрос к
  OpenWeatherMap (до 10 с), остальные чаты ждут.
- **Лимит частоты.** Не больше одного сообщения в 2 с на чат, считая любые сообщения, включая
  `/start` и `/help`. На лишнее сообщение бот отвечает «⏳ Не так быстро…». Лимит
  хранится в памяти и сбрасывается при перезапуске. Глобального лимита на все чаты нет.
- **Состояние только в памяти.** `offset` long polling не сохраняется. Telegram считает
  апдейты подтверждёнными только после следующего `getUpdates` с большим `offset`. Поэтому
  после перезапуска последняя обработанная пачка может прийти снова, и пользователи получат
  повторные ответы.
- **Только текстовые сообщения.** Запрашивается `allowed_updates=["message"]`; апдейты без
  текста пропускаются молча.
- **Название города:** 1–64 символа после схлопывания пробелов, только буквы, пробел, `-`,
  `.`, `,`. Цифры, апострофы и эмодзи отклоняются без запроса к OpenWeatherMap
  ([weather.go:44-58](../internal/usecase/weather.go)).
- Нет health-check эндпоинта и метрик: о работе бота можно судить только по логам.

## Диагностика

| Симптом | Что проверить |
|---------|---------------|
| Процесс сразу завершается, в stdout пусто | stderr: там текст ошибки конфигурации; см. [CONFIGURATION.md](CONFIGURATION.md) |
| Выход с кодом 1 после `weatherbot started` | stderr: `telegram bot token rejected: status 401/404` — неверный токен или `TELEGRAM_BASE_URL` |
| Бот не отвечает, в логах повторяется `WARN get updates` | поле `err`: сетевые ошибки и 5xx — проблемы доступа к Telegram API с хоста; `status 409: ...` — у бота активен webhook или запущен второй экземпляр с тем же токеном. Поле `retry_in` показывает паузу до повтора |
| Все запросы погоды — «сервис недоступен» | `WARN weather request failed`: `weather provider auth failed` — неверный или ещё не активированный ключ; `weather provider rate limited` — исчерпан лимит тарифа; `weather provider unavailable` — сеть, таймаут, 5xx |
