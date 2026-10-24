## Краткий ответ

- **Откуда берутся секреты.** Оба читаются только из переменных окружения: `TELEGRAM_BOT_TOKEN` и `OPENWEATHER_API_KEY`. Функция `env` обрезает пробелы по краям (`internal/config/config.go:33-34`, `internal/config/config.go:67-72`). Сами значения передаются только в конструкторы двух HTTP-клиентов (`cmd/weatherbot/main.go:38`, `cmd/weatherbot/main.go:40`). Файл `.env` подхватывает только `make run` (`Makefile:10`), сам бинарник его не читает.
- **Как проверяются.** У токена строгий формат `<цифры>:<[A-Za-z0-9_-]+>` (`validBotToken`, `internal/config/config.go:108-125`). У ключа OpenWeatherMap проверяется только, что он не пустой (`internal/config/config.go:45-47`). Тексты ошибок конфигурации — константы, значения секретов в них не подставляются (`internal/config/config.go:41-46`).
- **Как вычищаются из ошибок.** В каждом клиенте есть свой `redact`, он заменяет секрет на `[REDACTED]`. Для токена Telegram — в сыром виде, после `PathEscape` и после `QueryEscape` (`internal/delivery/telegram/client.go:155-163`). Для ключа — в сыром виде и после `QueryEscape` (`internal/repository/openweather/client.go:111-117`). `redact` применяется к ошибкам построения запроса и транспортным ошибкам (`internal/delivery/telegram/client.go:117,124`, `internal/repository/openweather/client.go:65,69`), а в Telegram ещё к полю `Description` из ответа API (`internal/delivery/telegram/client.go:138,144`).
- **Что пишется в логи.** Логируются только ошибки, которые уже прошли через клиенты: `handler.go:56`, `handler.go:79`, `poller.go:61`, `main.go:23`. Пользователю в чат при сбое уходит общий текст `msgUnavailable` (`internal/delivery/telegram/handler.go:78-80`).
- **Где остаются риски.** Можно задать `http://` в базовых URL, тогда секреты пойдут открытым текстом (`internal/config/config.go:79`). У `Config` нет защиты от случайного вывода целиком в лог (`internal/config/config.go:20-26`). Несколько веток ошибок не проходят через `redact` (`internal/delivery/telegram/client.go:141,148`, `internal/repository/openweather/client.go:78`). Ключ OpenWeatherMap по формату не проверяется. Подробности и оценки — в «Выводах».

## Файлы (file:line)

| `путь:строка` | идентификатор и что там | подвопрос |
|---|---|---|
| `internal/config/config.go:20-26` | `Config`: поля `TelegramBotToken`, `OpenWeatherAPIKey`, без `String()`/`LogValue` | источник, риски |
| `internal/config/config.go:33-34` | `Load`: `env("TELEGRAM_BOT_TOKEN", "")`, `env("OPENWEATHER_API_KEY", "")` | источник |
| `internal/config/config.go:67-72` | `env`: `strings.TrimSpace(os.Getenv(key))` | источник |
| `internal/config/config.go:39-47` | проверки: пустой токен, неверный формат токена, пустой ключ; тексты ошибок — константы | валидация, ошибки |
| `internal/config/config.go:108-125` | `validBotToken`: id из цифр, secret из `[A-Za-z0-9_-]` | валидация |
| `internal/config/config.go:74-89` | `validateBaseURL`: схема http/https, host, запрет query/fragment | риски (http) |
| `cmd/weatherbot/main.go:22-24` | `main`: `slog.Error("weatherbot stopped", "err", err)` | логи |
| `cmd/weatherbot/main.go:38,40` | `openweather.NewClient(..., cfg.OpenWeatherAPIKey, ...)`, `telegram.NewClient(..., cfg.TelegramBotToken, ...)` | поток |
| `internal/delivery/telegram/client.go:108` | `call`: `u.JoinPath("bot"+c.token, apiMethod)` — токен в пути URL | поток |
| `internal/delivery/telegram/client.go:117,124` | `c.redact(err.Error())` для `build request` / `do request` | очистка |
| `internal/delivery/telegram/client.go:134-135` | 401/404 → `ErrUnauthorized` со статусом, без тела ответа | очистка |
| `internal/delivery/telegram/client.go:138,144` | `c.redact(r.Description)` | очистка |
| `internal/delivery/telegram/client.go:106,141,148` | `parse base url: %w`, `decode response: %w`, `decode result: %w` — без `redact` | риски |
| `internal/delivery/telegram/client.go:155-163` | `redact`: замена `c.token`, `url.PathEscape`, `url.QueryEscape` | очистка |
| `internal/repository/openweather/client.go:56-61` | `Current`: `q.Set("appid", c.apiKey)`, `u.RawQuery = q.Encode()` | поток |
| `internal/repository/openweather/client.go:65,69` | `c.redact(err.Error())` | очистка |
| `internal/repository/openweather/client.go:78` | `decode response: %v` — без `redact` | риски |
| `internal/repository/openweather/client.go:83-96` | `statusError`: только код и доменные ошибки | очистка |
| `internal/repository/openweather/client.go:111-117` | `redact`: сырой вид и `url.QueryEscape(c.apiKey)` | очистка |
| `internal/delivery/telegram/handler.go:48,56,79` | `h.log.Error(... "panic", r)`, `h.log.Warn("send message", ...)`, `h.log.Warn("weather request failed", ...)` | логи |
| `internal/delivery/telegram/poller.go:61` | `p.log.Warn("get updates", "err", err, ...)` | логи |
| `internal/usecase/weather.go:52-55` | `normalizeCity`: только буквы и ` -.,` (подмешать `appid` через город нельзя) | поток |
| `.gitignore:1` | `.env` | хранение |
| `Makefile:9-10` | `run`: `set -a; [ -f .env ] && . ./.env` | источник |
| `internal/config/config_test.go:103-109` | `TestLoadDoesNotLeakSecrets` | тесты |
| `internal/config/config_test.go:70-72` | кейс `"bad token format"` (`abc/../x`) | тесты |
| `internal/delivery/telegram/client_test.go:120-143` | `TestClientRedactsTokenFromTransportErrors` | тесты |
| `internal/repository/openweather/client_test.go:96-114` | `TestClientCurrentTimeoutRedactsKey` (ключ `"s3cret key/+"`) | тесты |
| `internal/repository/openweather/client_test.go:81-94` | `TestClientCurrentEscapesCity` (`a&appid=x`) | тесты |
| `internal/delivery/telegram/handler_test.go:72,85` | кейс `"unknown error"`: в чат не уходит `"secret"` | тесты |

## Связи / поток

**Токен Telegram:**
1. Переменная окружения → `env` с `TrimSpace` (`internal/config/config.go:33`, `internal/config/config.go:68`).
2. Проверка `validBotToken` (`internal/config/config.go:42`). При ошибке возвращается константный текст (`internal/config/config.go:43`) → `fmt.Errorf("load config: %w")` (`cmd/weatherbot/main.go:31`) → `slog.Error` (`cmd/weatherbot/main.go:23`).
3. `telegram.NewClient(..., cfg.TelegramBotToken, ...)` (`cmd/weatherbot/main.go:40`) → поле `Client.token` (`internal/delivery/telegram/client.go:75`).
4. `call` вставляет токен в путь `bot<token>/<method>` (`internal/delivery/telegram/client.go:108`) → `httpClient.Do` (`internal/delivery/telegram/client.go:122`).
5. Ошибки проходят через `redact` (`internal/delivery/telegram/client.go:117,124,138,144`) → оборачиваются в `get updates:` / `send message:` (`internal/delivery/telegram/client.go:86,98`).
6. Попадают в логи: `poller.go:61` (getUpdates), `handler.go:56` (sendMessage). Если ошибка — `ErrUnauthorized`, `Run` возвращает её (`internal/delivery/telegram/poller.go:51-53`) → `main.go:46` → `main.go:23`.

**Ключ OpenWeatherMap:**
1. Переменная окружения → `env` (`internal/config/config.go:34`) → проверка только на пустоту (`internal/config/config.go:45`).
2. `openweather.NewClient` (`cmd/weatherbot/main.go:38`) → `Client.apiKey` (`internal/repository/openweather/client.go:27`).
3. Ключ попадает в query-параметр `appid` (`internal/repository/openweather/client.go:60-61`) → `Do` (`internal/repository/openweather/client.go:67`).
4. Ошибки: `redact` (`client.go:65,69`); статусы → доменные ошибки без тела ответа (`client.go:83-96`); ошибка декодирования — без `redact` (`client.go:78`).
5. Дальше `usecase.Current` оборачивает ошибку в `get current weather: %w` (`internal/usecase/weather.go:39`) → `handler.respond` логирует её (`internal/delivery/telegram/handler.go:79`), а пользователю отправляет `msgUnavailable` (`handler.go:80`).

## Выводы

**Факты:**
- Значения секретов не подставляются ни в один текст ошибки конфигурации (`internal/config/config.go:41,43,46`). Это проверяет `TestLoadDoesNotLeakSecrets` (`internal/config/config_test.go:103-109`).
- **Символы токена и экранирование.** `validBotToken` пропускает только цифры в id, а в secret — буквы `a-z`/`A-Z`, цифры, `_` и `-` (`internal/config/config.go:113-123`). Двоеточие-разделитель стоит в сыром токене. Комментарий в коде говорит, что символы вроде `/` и `?` недопустимы (`internal/config/config.go:106-107`), и проверка это обеспечивает. Сам `redact` перебирает три формы токена: сырую, `PathEscape` и `QueryEscape` (`internal/delivery/telegram/client.go:159`). *Вывод, не проверено по коду проекта:* буквы, цифры, `_` и `-` не меняются ни при `PathEscape`, ни при `QueryEscape`; `:` в пути не экранируется, а в query становится `%3A`. Значит, сырой формы достаточно для пути URL, а дополнительные формы — запас.
- **Символы ключа OpenWeatherMap.** После `TrimSpace` допускается любая непустая строка. `redact` покрывает сырой вид и `QueryEscape` (`internal/repository/openweather/client.go:115-116`), а в URL ключ кладётся через `q.Encode()` (`client.go:61`). Тест проверяет пробел, `/` и `+` (`internal/repository/openweather/client_test.go:97`). *Вывод, не проверено:* `url.Values.Encode` экранирует так же, как `QueryEscape`, и `*url.Error` выводит URL в той же экранированной форме. Тогда маскирование срабатывает для любого класса символов: буквы и цифры остаются как есть, пробел становится `+`, спецсимволы — `%XX`.
- Подставить второй `appid` через название города нельзя: `normalizeCity` пропускает только буквы и ` -.,` (`internal/usecase/weather.go:52-55`). Кроме того, значения кодирует `url.Values` — это проверяет `TestClientCurrentEscapesCity` (`internal/repository/openweather/client_test.go:81-94`).
- Неизвестные ошибки погоды в чат не попадают (`internal/delivery/telegram/handler.go:78-80`); это проверяет кейс `"unknown error"` (`internal/delivery/telegram/handler_test.go:72,85`).
- В коде нет ни одного вызова, который логирует `cfg` или клиента целиком: grep по `cfg`, `Printf`, `%+v` и `.Debug(` в `.go` находит только тесты и `main.go:29-40`.

**Риски:**

1. **Секреты по открытому каналу при `http://` в базовом URL** — `internal/config/config.go:79`, `validateBaseURL`. Проверка разрешает схему `http` для обоих базовых URL, и тест это закрепляет (`internal/config/config_test.go:43-44`). Если в продакшене задать `TELEGRAM_BASE_URL`/`OPENWEATHER_BASE_URL` с `http://` (по ошибке или через подменённое окружение), токен в пути и ключ в query уйдут незашифрованными. Если адрес указывает на чужой хост, секреты получит этот хост. **Средняя** (нужна неверная конфигурация).
2. **Нет защиты от случайного логирования `Config`** — `internal/config/config.go:20-26`. Секреты — обычные `string`-поля; методов `String()`/`LogValue()` и типа-обёртки нет. Сейчас утечки нет, но если кто-то добавит `logger.Info("config", "cfg", cfg)` или `%+v`, оба секрета попадут в stdout. Тесты уже печатают `cfg` через `%+v` при падении (`internal/config/config_test.go:35,54`) — там тестовые значения. **Низкая/средняя** (реализуется при будущем изменении кода).
3. **Ветки ошибок без `redact`** — `internal/delivery/telegram/client.go:106` (`parse base url`), `:141` (`decode response`), `:148` (`decode result`), а также `internal/repository/openweather/client.go:53,78`. *Вывод, не проверено:* ошибки JSON-декодера стандартной библиотеки не содержат URL. Ошибка разбора `baseURL` содержит базовый URL, но не токен: `JoinPath` с токеном вызывается после разбора. Сейчас утечки нет. Риск появится, если базовый URL будет содержать учётные данные в userinfo — `validateBaseURL` их не запрещает (`internal/config/config.go:74-89`). **Низкая.**
4. **Значение паники уходит в лог как есть** — `internal/delivery/telegram/handler.go:48`. Значение `r` логируется без `redact`. Паника, в текст которой попадёт секрет, оказалась бы в логе. В текущем коде пути от `Handle` до таких паник не видно. **Низкая.**
5. **Ключ OpenWeatherMap не проверяется по формату** — `internal/config/config.go:45`. Короткий или «бытовой» ключ (например, `a`) заставит `redact` заменить все вхождения этой подстроки в ошибке (`internal/repository/openweather/client.go:115`). Ошибка станет нечитаемой, но это не утечка. Неверный ключ выяснится только при первом запросе (401 → `ErrProviderAuth`, `client.go:89-90`), а ошибка залогируется как предупреждение (`handler.go:79`) — бот продолжит работать. **Низкая** (диагностика, а не утечка).
6. **Хранение `.env`** — `.gitignore:1` игнорирует только `.env`; варианты вроде `.env.local` и `.env.prod` не покрыты. Если такой файл создать с реальными значениями, его легко закоммитить. **Низкая.** `Makefile:10` выполняет `.env` как shell-скрипт (`. ./.env`). *Вывод, не проверено:* при значениях со спецсимволами shell (`$`, пробелы, кавычки) они исказятся или выполнятся как команды. Это касается целостности, а не утечки.
7. **Тест токена слабее, чем мог бы быть** — `internal/delivery/telegram/client_test.go:14`. Тестовый токен `123456:test-token` не содержит `_`, а тест маскирования проверяет только транспортные ошибки (`client_test.go:120-143`). Маскирование `Description` (`client.go:138,144`) отдельным тестом с токеном в описании не проверено. У ключа OpenWeatherMap нет теста для ветки `build request` (`openweather/client.go:65`). **Низкая** (пробел в покрытии).

**Подозрительный текст:** при просмотре файлов проекта текста, адресованного ИИ/агенту, не встретилось. Файл `.claude/agents/researcher.md` я не открывал: это служебный каталог, к коду проекта он не относится.

## Неопределённости

- Формат текста `*url.Error` и то, что `http.Client` включает в ошибку полный URL с путём и query, — знание стандартной библиотеки Go, а не код проекта. Косвенно это подтверждают тесты, ожидающие `[REDACTED]` в тексте ошибки (`internal/delivery/telegram/client_test.go:139`, `internal/repository/openweather/client_test.go:111`). Сами тесты я не запускал.
- Совпадение экранирования `url.Values.Encode` с `url.QueryEscape` и отсутствие URL в ошибках `encoding/json` — тоже вывод, по коду проекта не проверено.
- Как ведут себя редиректы: куда `http.Client` уходит по 3xx от базового URL и что тогда оказывается в тексте ошибки. В проекте собственного `CheckRedirect` нет (`cmd/weatherbot/main.go:38,40`); подробнее не исследовал.
- Утечки вне процесса не оценивал: окружение процесса, дампы, системные логи контейнера/оркестратора. Они зависят от окружения развёртывания, которого в репозитории нет.
- Допущение: под «логами» понимаются логи `slog` в stdout (`cmd/weatherbot/main.go:33`) и логгер по умолчанию в `main.go:23`. Других приёмников логов в коде нет.
- Файл `.env` (если он есть) я по правилам не читал; проверял только шаблон `.env.example` — значения в нём пустые (`.env.example:1-2`).
