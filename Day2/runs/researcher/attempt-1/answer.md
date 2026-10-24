Токен Telegram-бота и ключ OpenWeatherMap читаются только из переменных окружения. На отсутствие проверяются оба, а формат проверяется только у токена. Из текстов ошибок их вычищают функции `redact` в каждом HTTP-клиенте. Мест, где значение секрета сейчас реально попадает в логи, я не нашёл. Основной риск в другом: конфиг разрешает `http://`, и тогда секреты идут по сети открытым текстом. Код я не запускал, всё ниже получено чтением.

## Краткий ответ
- **Откуда читаются.** Только из окружения: `TELEGRAM_BOT_TOKEN` и `OPENWEATHER_API_KEY` в `config.Load`, `internal/config/config.go:33-34`. Пробелы по краям обрезаются в `env`, `internal/config/config.go:68`. Файл `.env` читает не программа, а `make run`: он выполняет `.env` как shell-скрипт (`Makefile:10`).
- **Как проверяются.** Токен проверяется на непустоту и формат `<цифры>:<[A-Za-z0-9_-]+>` в `validBotToken`, `internal/config/config.go:39-44,108-125`. Ключ OpenWeatherMap проверяется только на непустоту, `internal/config/config.go:45-47`. Тексты ошибок конфига значений не содержат.
- **Чистка, Telegram.** `(*Client).redact` убирает токен в трёх видах: как есть, после `PathEscape` и после `QueryEscape` (`internal/delivery/telegram/client.go:155-163`). Применяется к ошибкам сборки запроса и транспорта, а также к полю `description` из ответа Telegram (`internal/delivery/telegram/client.go:117,124,138,144`).
- **Чистка, OpenWeatherMap.** `(*Client).redact` убирает ключ как есть и после `QueryEscape` (`internal/repository/openweather/client.go:111-117`). Применяется к ошибкам сборки запроса и транспорта (`internal/repository/openweather/client.go:65,69`).
- **Логи.** Ошибки логируются на уровне `Warn`, и в лог попадают уже очищенные ошибки: `internal/delivery/telegram/handler.go:56,79` и `internal/delivery/telegram/poller.go:61`. Пользователю внутренняя ошибка не уходит, он получает `msgUnavailable` (`internal/delivery/telegram/handler.go:80`).
- **Главный риск.** `validateBaseURL` разрешает схему `http` (`internal/config/config.go:79`). Тогда токен (часть пути URL) и `appid` (параметр запроса) передаются открытым текстом.

## Файлы (file:line)
| `путь:строка` | идентификатор и что там | подвопрос |
|---|---|---|
| `internal/config/config.go:20-26` | `Config`: секреты лежат в обычных `string`, своих `String`/`LogValue` нет | хранение |
| `internal/config/config.go:33-34` | `env("TELEGRAM_BOT_TOKEN")`, `env("OPENWEATHER_API_KEY")` | источник |
| `internal/config/config.go:67-72` | `env`: `os.Getenv` + `TrimSpace` | источник |
| `internal/config/config.go:39-47` | проверки: «required» и «invalid format», значение не подставляется | валидация |
| `internal/config/config.go:106-125` | `validBotToken`: id из цифр, секрет из `[A-Za-z0-9_-]` | валидация |
| `internal/config/config.go:74-89` | `validateBaseURL`: http/https, хост обязателен, query и fragment запрещены; userinfo не запрещён | валидация, риски |
| `cmd/weatherbot/main.go:38,40` | `openweather.NewClient(..., cfg.OpenWeatherAPIKey, ...)`, `telegram.NewClient(..., cfg.TelegramBotToken, ...)` | поток |
| `cmd/weatherbot/main.go:23,31` | `slog.Error("weatherbot stopped", "err", err)`; ошибка конфига оборачивается в `load config: %w` | логи |
| `internal/delivery/telegram/client.go:108` | `u.JoinPath("bot"+c.token, apiMethod)`: токен идёт в путь URL | поток |
| `internal/delivery/telegram/client.go:117,124` | `c.redact(err.Error())` для ошибок сборки запроса и транспорта | чистка |
| `internal/delivery/telegram/client.go:134-135` | 401/404 превращаются в `ErrUnauthorized` с одним кодом статуса | чистка |
| `internal/delivery/telegram/client.go:138,144` | `c.redact(r.Description)` | чистка |
| `internal/delivery/telegram/client.go:155-163` | `redact`: замена на `[REDACTED]` | чистка |
| `internal/repository/openweather/client.go:60-61` | `q.Set("appid", c.apiKey)`: ключ идёт в query | поток |
| `internal/repository/openweather/client.go:65,69` | `c.redact(err.Error())` | чистка |
| `internal/repository/openweather/client.go:78` | ошибка декодирования через `%v` без `redact` | чистка (пробел) |
| `internal/repository/openweather/client.go:111-117` | `redact`: ключ как есть и после `url.QueryEscape` | чистка |
| `internal/delivery/telegram/handler.go:48` | `h.log.Error("handle message panicked", ..., "panic", r)`: значение паники пишется без чистки | логи, риски |
| `internal/delivery/telegram/handler.go:56,79-80` | логирование ошибок `Send` и погоды; пользователю уходит `msgUnavailable` | логи |
| `internal/delivery/telegram/poller.go:51-52,61` | `ErrUnauthorized` останавливает бота; остальные ошибки логируются | логи |
| `Makefile:10` | `. ./.env`: файл выполняется как shell-скрипт | источник |
| `.gitignore:1` | игнорируется только `.env` | риски |

## Связи / поток
**Токен Telegram.**
1. Читается из окружения в `env` (`internal/config/config.go:33,68`).
2. Проверяется в `validBotToken` (`internal/config/config.go:42`).
3. Передаётся в клиент: `telegram.NewClient` (`cmd/weatherbot/main.go:40`).
4. Подставляется в путь URL: `call` → `JoinPath("bot"+c.token, ...)` (`internal/delivery/telegram/client.go:108`).
5. Уходит в запрос: `httpClient.Do` (`internal/delivery/telegram/client.go:122`).
6. При ошибке транспорта: `*url.Error` с URL внутри → `redact` (`internal/delivery/telegram/client.go:124`) → обёртки `get updates: %w` и `send message: %w` (`internal/delivery/telegram/client.go:86,98`).
7. Дальше ошибка попадает в лог: `p.log.Warn` (`internal/delivery/telegram/poller.go:61`) или `h.log.Warn` (`internal/delivery/telegram/handler.go:56`).
8. При 401/404 бот останавливается: `ErrUnauthorized` → `Poller.Run` возвращает ошибку (`internal/delivery/telegram/poller.go:51-52`) → `run poller: %w` (`cmd/weatherbot/main.go:46`) → `slog.Error` (`cmd/weatherbot/main.go:23`). Токена в этой ошибке нет.

**Ключ OpenWeatherMap.**
1. Читается из окружения в `env` (`internal/config/config.go:34`).
2. Проверяется только на непустоту (`internal/config/config.go:45`).
3. Передаётся в клиент: `openweather.NewClient` (`cmd/weatherbot/main.go:38`).
4. Подставляется в query: `appid` (`internal/repository/openweather/client.go:60-61`).
5. Уходит в запрос: `httpClient.Do` (`internal/repository/openweather/client.go:67`).
6. При ошибке: `redact` (`internal/repository/openweather/client.go:69`) → `get current weather: %w` (`internal/usecase/weather.go:39`) → `h.log.Warn("weather request failed")` (`internal/delivery/telegram/handler.go:79`). Пользователь получает `msgUnavailable` (`internal/delivery/telegram/handler.go:80`).
7. При 401 возвращается `domain.ErrProviderAuth` без подробностей (`internal/repository/openweather/client.go:89-90`, `internal/domain/errors.go:8`). Бот продолжает работать и каждый раз пишет предупреждение в лог (это вывод из кода `respond`).

**Тесты, подтверждающие поведение:**
- `TestLoadDoesNotLeakSecrets`, `internal/config/config_test.go:103-109`: ошибка конфига не содержит токен.
- `TestLoadErrors`, кейс «bad token format», `internal/config/config_test.go:70-72`.
- `TestLoadOverrides`, `internal/config/config_test.go:42`: пробелы вокруг ключа обрезаются.
- `TestClientRedactsTokenFromTransportErrors`, `internal/delivery/telegram/client_test.go:120-143`: при таймауте `Send` и `GetUpdates` ни часть секрета, ни id в ошибке не встречаются, зато есть `[REDACTED]`.
- `TestClientErrors`, кейс «unauthorized», `internal/delivery/telegram/client_test.go:80-82`.
- `TestClientCurrentTimeoutRedactsKey`, `internal/repository/openweather/client_test.go:96-114`: ключ со спецсимволами (`"s3cret key/+"`) удаляется из ошибки.
- `TestClientCurrentEscapesCity`, `internal/repository/openweather/client_test.go:81-94`: подставить второй `appid` через название города нельзя.
- `TestHandlerResponses`, кейс «unknown error», `internal/delivery/telegram/handler_test.go:72,85`: внутренний текст ошибки не отправляется пользователю.
- `TestPollerStopsOnUnauthorized`, `internal/delivery/telegram/poller_test.go:132`.

## Выводы
**Факты:**
- Тексты ошибок конфига не содержат значений секретов. Это проверяет `TestLoadDoesNotLeakSecrets`.
- Значение секрета не попадает в логи ни в одном месте. `Config` нигде не логируется: вызовы логгера есть только в `cmd/weatherbot/main.go:23,44,48`, `internal/delivery/telegram/handler.go:48,56,79` и `internal/delivery/telegram/poller.go:61`.
- Формат токена ограничен `[A-Za-z0-9_-]`, поэтому все три варианта в `redact` совпадают, и подменить путь URL токеном нельзя. Комментарий в `internal/delivery/telegram/client.go:108` по коду подтверждается.
- Подозрительных инструкций, адресованных ИИ, в просмотренных файлах нет.

**Риски:**
1. **Передача секретов открытым текстом. Серьёзность средняя.** `validateBaseURL` пропускает `http` (`internal/config/config.go:79`), и тест прямо использует `http://localhost:...` (`internal/config/config_test.go:43-44`). Если в проде задать `TELEGRAM_BASE_URL` или `OPENWEATHER_BASE_URL` со схемой `http://` и нелокальным хостом, токен (в пути) и `appid` (в query) пойдут по сети незашифрованными. Кроме того, любой, кто может менять окружение, может направить секреты на свой хост. Это логично для локального Bot API-сервера, но ограничения на localhost в коде нет.
2. **Ключ в query-строке. Серьёзность низкая или средняя.** `internal/repository/openweather/client.go:60`. Так требует API OpenWeatherMap. Но полный URL с `appid` может попасть в логи прокси или middleware, если появится обёртка `http.Client` или `RoundTripper`, которая логирует URL. Сейчас такой обёртки нет: `&http.Client{Timeout: ...}` в `cmd/weatherbot/main.go:38`.
3. **Чистка по совпадению строки зависит от того, где оказался секрет. Серьёзность низкая.** `redact` ловит только известные формы (`internal/repository/openweather/client.go:115-116`). Ключ OpenWeatherMap по формату не проверяется (`internal/config/config.go:45`). Если ключ попадёт в текст ошибки в другой кодировке (например, в `%`-кодировке пути вместо query), чистка не сработает. Сейчас ключ кодируется только через `url.Values.Encode` и совпадает с `QueryEscape`. Ошибка JSON на `internal/repository/openweather/client.go:78` не чистится. По моему выводу URL в ней нет, так что утечки сейчас нет, но это неочищенный путь.
4. **Значение паники логируется как есть. Серьёзность низкая.** `internal/delivery/telegram/handler.go:48`. Если паника когда-нибудь будет вызвана со значением, содержащим URL запроса или ошибку транспорта до чистки, секрет попадёт в лог. Сейчас таких мест в коде нет, это вывод.
5. **В `Config` нет защиты от случайного логирования. Серьёзность низкая.** `internal/config/config.go:20-26`: у типа нет `String`/`LogValue`, который скрывал бы секреты. Любой будущий вызов вроде `logger.Info("cfg", "cfg", cfg)` или `%+v` выведет оба секрета. Пример такого вывода уже есть в тесте, правда с тестовыми значениями: `t.Errorf("Load() = %+v", cfg)` в `internal/config/config_test.go:54`.
6. **`.env` и учётные данные в URL. Серьёзность низкая.**
   - `.gitignore:1` игнорирует только `.env`. Файлы вроде `.env.local` или `.env.prod` могут случайно попасть в коммит.
   - `Makefile:10` выполняет `.env` как shell-скрипт, поэтому в нём сработает любой код.
   - `validateBaseURL` не запрещает `user:pass@` в base URL. При ошибке разбора текст от `url.Parse` по моему выводу содержит исходную строку (`internal/config/config.go:77`). Это не те два секрета, о которых вопрос, но так могут утечь учётные данные прокси.
7. **Ключ OpenWeatherMap отклонён (401): падения нет, только предупреждения в лог. Серьёзность низкая (вопрос эксплуатации).** `internal/repository/openweather/client.go:89-90`. Бот работает дальше и на каждый запрос отвечает `msgUnavailable`. Значение ключа при этом не утекает.

## Неопределённости
- Код не запускался. То, что `*url.Error` из `net/http` содержит полный URL с query и путём и что `json`-ошибки не содержат URL, я взял из знания стандартной библиотеки Go, а не из этого репозитория. Косвенно это подтверждают тесты на `[REDACTED]`: если бы URL в ошибке не было, они бы не проходили.
- Версию Go в `go.mod` я не проверял. Предполагаю, что поведение `url.Error` и `JoinPath` в ней стандартное.
- Содержимое `.env` я намеренно не читал, по правилам. Попал ли когда-нибудь реальный секрет в историю git, не проверено: каталог `.git` я не анализировал.
- Как приложение разворачивается в проде (контейнер, systemd, куда идёт stdout с JSON-логами) из репозитория не видно. От этого зависит, насколько серьёзны риски 1 и 2.
