# Day1: ручное code review, вариант A и вариант B

Обе задачи: Telegram-бот погоды (OpenWeatherMap) на Go, только stdlib.
A сгенерирован по общему `variant-a/CLAUDE.md` (14 общих правил), B по подробному `variant-b/CLAUDE.md`
(стек, архитектура, папки, naming, контракты, 5 примеров кода, 5 антипаттернов, шаблон файла, DoD).
Код прочитан полностью: 13 `.go` файлов A и 18 `.go` файлов B. Ссылки ниже даны относительно `variant-a/src` и `variant-b/src`.
Метрики собраны скриптом `compare/metrics.sh`, результаты в `compare/metrics-{a,b}.txt`.

## Сводка метрик

| Метрика                                  | A                                   | B                                      |
|------------------------------------------|-------------------------------------|----------------------------------------|
| `go build ./...`                         | PASS                                | PASS                                   |
| `go vet ./...`                           | PASS                                | PASS                                   |
| `gofmt -l .`                             | 0 файлов                            | 0 файлов                               |
| golangci-lint 2.15 (default)             | 3 issues                            | 3 issues                               |
| golangci-lint (+revive,gosec,errorlint…) | 4                                   | 5                                      |
| `go test -race`                          | PASS                                | PASS                                   |
| coverage total                           | 84.5%                               | 86.9%                                  |
| `func Test` / `t.Run(` / `_test.go`      | 24 / 3 / 5                          | 33 / 6 / 7                             |
| LOC prod / test                          | 726 / 567                           | 812 / 852                              |
| файлов / пакетов / модулей               | 20 / 6 / 1                          | 23 / 6 / 1                             |
| `go.mod`                                 | `go 1.25`                           | `go 1.26`                              |
| прогон: turns / время / стоимость        | 20 / 214.7 s / $0.888               | 13 / 525.5 s / $1.421                  |

Про lint: системный `golangci-lint 2.4.0` собран go1.25 и падает с `panic: file requires newer Go version go1.26`
на обоих вариантах. Падает он на stdlib тулчейна 1.26.5, `go.mod` тут ни при чём. Поэтому метрики сняты бинарём
golangci-lint 2.15.0, собранным go1.26.5 в scratchpad (`GOLANGCI_LINT=...`).
Default-набор у обоих находит одно и то же: 2 непроверенных `resp.Body.Close` (errcheck) и `QF1002 tagged switch`.
Extended у B добавляет `errorlint` на `%v` (openweather/client.go:78) и `revive: exported var ErrInvalidCity should have comment`
(domain/errors.go:6). Это наследие примера 9.1, в котором у sentinel-ошибок тоже нет комментариев.
У A extended добавляет `errorlint` в тесте (httpx/redact_test.go:26).

## Оценки (0–2)

| #  | Критерий | A | B | Комментарий |
|----|----------|---|---|-------------|
| 1  | Слои и направление зависимостей | 1 | 2 | A: пакет на интеграцию (`bot → telegram, weather → httpx`), граф ацикличен, интерфейсы объявлены у потребителя (bot/bot.go:15-27). Но `bot` завязан на транспортные типы `telegram.Update`/`telegram.Message` (bot/bot.go:16, 136) и на `weather.Weather`, домена и usecase нет. B: `domain` импортирует только `errors`, `usecase` только stdlib+domain (проверено скриптом), DI только в cmd/weatherbot/main.go:38-42. |
| 2  | Тестируемость (baseURL, клиент, порты) | 2 | 2 | A: `NewClient(httpClient, baseURL, …)` (telegram/client.go:65, weather/client.go:54), `config.Load(getenv func)` (config/config.go:26) тестируется без `t.Setenv`, это чище, чем у B. Backoff подменяется через поля (bot/bot_test.go:96). B: baseURL приходит из env (config/config.go:35-36), `Poller.wait` подменяется (poller.go:30), порты по 1 методу. |
| 3  | Покрытие и качество тестов | 1 | 2 | A: 84.5%, хорошие тесты на offset, retry и утечку секретов (telegram/client_test.go:89, weather/client_test.go:103). Не проверены 5xx с не-JSON телом, 429 Telegram, отмена ctx и панический путь. B: 86.9%, usecase 100%. Табличные тесты: маппинг 8 статусов (openweather/client_test.go:28-35), инъекция `a&appid=x` (client_test.go:81), redaction на таймауте, точное расписание backoff/retry_after (poller_test.go:94-118), recover (handler_test.go:135), вытеснение из rate-limit карты (handler_test.go:123). |
| 4  | vet / gofmt / lint | 2 | 2 | Оба чистые по vet и gofmt, по 3 одинаковых замечания default-lint. |
| 5  | Ошибки (%w, sentinel, статусы OWM, `cod`) | 1 | 2 | A: sentinel `ErrCityNotFound/ErrUnauthorized/ErrRateLimited` (weather/client.go:23-30), `errors.Is` в боте, `cod` не парсится. Но тело декодируется до проверки статуса (weather/client.go:106-111), поэтому 404/401/5xx с не-JSON телом превращаются в `decode weather response` и маппинг теряется. B: статус проверяется до decode (openweather/client.go:73), всё остальное оборачивается в `ErrProviderUnavailable` через `%w` (client.go:94), usecase оборачивает (`get current weather: %w`, usecase/weather.go:39). Минус: decode-ошибка через `%v` (client.go:78). |
| 6  | Таймауты, context, DefaultClient | 1 | 2 | Ни у кого нет `DefaultClient` и `http.Get`, `NewRequestWithContext` есть везде. A: таймауты есть (main.go:37-38, poll 30 s → client 40 s), но есть баг с общим дедлайном, см. «Реальные баги» №1. B: 10 s для OWM и 40 s для Telegram (main.go:38,40), ctx прокинут до HTTP. Таймаута на обработку одного сообщения нет, но его фактически задаёт `http.Client.Timeout`. |
| 7  | Graceful shutdown | 2 | 1 | A: `signal.NotifyContext` (main.go:47), обработчики идут на `context.WithoutCancel` (bot/bot.go:93) и дожидаются через `wg.Wait` (bot.go:89), начатые ответы доставляются. B: `NotifyContext` (main.go:35), poller выходит сразу. Но `Handle` получает тот же ctx (poller.go:73), поэтому SIGTERM во время запроса к OWM обрывает и запрос, и `sendMessage`: пользователь остаётся без ответа. |
| 8  | Конфиг и секреты (env, fail-fast) | 2 | 2 | Оба собирают все ошибки через `errors.Join`, хардкода нет, `.env.example` есть. A валидирует units и timeout (config.go:42-49). B валидирует формат токена, потому что токен встаёт в путь URL (config.go:106-125), и схему/host base URL (config.go:74-89). Ещё у B есть тест, что токен не попадает в ошибку (config_test.go:103). |
| 9  | Redact токена и ключа | 2 | 2 | A: `httpx.RedactURLError` подменяет URL в `*url.Error` и сохраняет цепочку `errors.Is` (httpx/redact.go:14-20). Это структурно надёжнее строковой замены. B: `redact` заменяет сырое и escaped значение (telegram/client.go:155-163, openweather/client.go:111-117), есть тест с ключом `"s3cret key/+"`. Цена: транспортная ошибка уходит через `%s`, цепочка рвётся (telegram/client.go:124). |
| 10 | Построение URL, валидация города | 1 | 2 | A: OWM через `url.Values` (weather/client.go:117-124), Telegram через `fmt.Sprintf("%s/bot%s/%s", …)` с непроверенным токеном (telegram/client.go:105). Город проверяется только по пустоте и длине ≤100 рун (bot/handler.go:58). B: `url.Values` + `JoinPath` везде (openweather/client.go:55-61, telegram/client.go:108-109). Валидация в usecase: UTF-8, 1..64 руны, буквы и ` -.,` (usecase/weather.go:44-58). Она слишком строгая, см. «Реальные баги» №6. |
| 11 | LimitReader, StatusCode до decode | 1 | 2 | A: `io.LimitReader` есть (1 MiB OWM, 10 MiB TG), но decode идёт до статуса (weather/client.go:107, telegram/client.go:119). B: LimitReader 1 MiB, статус проверяется до decode (openweather/client.go:73-77, telegram/client.go:129-140). |
| 12 | Rate limit, flood, recover | 0 | 2 | A: per-chat лимита нет, есть только семафор на 16 параллельных обработчиков (bot.go:78, 120-124). `recover` нет: паника в горутине `wg.Go` (bot.go:125) роняет процесс. B: per-chat 1 запрос / 2 s с вытеснением старых записей (handler.go:84-99), `recover` в `Handle` (handler.go:46-50). |
| 13 | Telegram: offset+1, 429 retry_after | 1 | 2 | A: `offset = UpdateID+1` (bot.go:115), экспоненциальный backoff 1→30 s. `retry_after` игнорируется (`APIError` без `parameters`, telegram/client.go:47-50). На 401 бот ретраит бесконечно. B: `offset = max(offset, id+1)` (poller.go:69), `retry_after` учитывается (client.go:131-133, poller.go:55-57), на 401/404 бот останавливается (poller.go:51). Не учтено: 429 на `sendMessage` только логируется (handler.go:55-57). |
| 14 | UX текстов и формата | 2 | 1 | A: подробный ответ (описание, температура и ощущается, влажность, ветер, давление), подписи единиц для metric/imperial/standard (format.go:18-22), разные тексты для «не найден», «перегружен» и «ошибка», отдельный текст для неизвестной команды. Мелкие дефекты: №3 и №4. B: формат точно по эталону (знак температуры, румбы, format.go:14-43), но валидация отвергает реальные названия (№6). Последовательная обработка (№5) даёт всем пользователям задержку до 10 s, если OWM тормозит. |
| 15 | Naming, идиоматичность, stutter | 2 | 2 | A: `telegram.Client`, `weather.Client`, `bot.Bot` (лёгкий stutter), `wg.Go` и `min` из Go 1.25. Пакет `httpx` получился «утилитным». `weather.NewClient(httpClient, baseURL, apiKey, units, lang)` принимает 4 строки подряд, их легко перепутать. B: без stutter, `msgXxx`, ошибки в нижнем регистре, конструкторы на 3 параметра. Мелочи: у экспортируемых sentinel нет doc-комментариев, у `domain/errors.go` нет package doc. |
| 16 | Соответствие своему CLAUDE.md | 1 | 2 | A по общим правилам: секретов в коде нет, README с запуском есть, тесты есть, логи есть. Пункты «безопасно» и «готово к продакшену» выполнены частично: нет recover и flood-защиты, текст сообщений пользователя пишется в лог на уровне Info (bot.go:140, PII). B: структура папок 1:1 с разделом 3, DoD (раздел 12) выполнен, контракты (раздел 6) соблюдены. |
|    | **Итого из 32** | **22** | **30** | |

## Реальные баги

1. **A, общий дедлайн обработки и отправки** (bot/bot.go:137 + bot/handler.go:62 + cmd/weatherbot/main.go:38). У `handleMessage` таймаут `requestTimeout`, у `weatherReply` такой же дочерний таймаут, у HTTP-клиента OWM тоже `RequestTimeout`. Если OWM отвечает дольше 10 s, ctx истекает, и `SendMessage` вызывается с уже просроченным ctx: пользователь не получает даже `failureText`. Воспроизведено probe-тестом в scratchpad: `ctx err at SendMessage after slow weather: context deadline exceeded`.
2. **A, decode до проверки статуса** (weather/client.go:106-111, telegram/client.go:118-121). HTML или пустое тело на 404/401/429/5xx (например, от прокси или CDN) дают `decode … response`, а не sentinel. Пользователь получает общий текст, 429 не распознаётся. Тест A с 5xx проходит только потому, что тело в нём JSON (weather/client_test.go:77).
3. **A, команда с переводом строки** (bot/handler.go:52). `strings.Cut(text, " ")` не делит по `\n`: `"/weather\nМосква"` разбирается в `cmd="weather\nмосква"` и даёт «Неизвестная команда» (подтверждено probe). Тот же дефект есть в шаблоне раздела 11 B; B его исправил (handler.go:108 `IndexFunc(unicode.IsSpace)`) и покрыл тестом (handler_test.go:62).
4. **A, пустой город** (bot/handler.go:31-32 → weather/client.go:91-92). Текст из одних пробелов даёт ответ «Город «» не найден». Пустая строка маппится в `ErrCityNotFound` вместо приглашения ввести город.
5. **B, последовательная обработка** (delivery/telegram/poller.go:73). `Handle` вызывается синхронно внутри цикла polling, поэтому один медленный запрос к OWM (до 10 s) блокирует всех пользователей и откладывает следующий `getUpdates`.
6. **B, слишком строгая валидация города** (usecase/weather.go:52-55, правило взято из CLAUDE.md §7). Отвергаются `Val-d'Or`, `Ust'-Kut`, названия с цифрами, Devanagari (`मुंबई`: комбинирующие знаки не проходят `IsLetter`), а также NFD-формы кириллицы (`и` + U+0306 вместо `й`). Подтверждено probe. Такие пользователи видят `msgInvalidCity`, хотя город существует.
7. **B, обрыв ответа при SIGTERM** (poller.go:73, handler.go:55). При отмене ctx обрывается и текущий запрос к OWM, и `Send`.
8. **B, flood на исходящих** (handler.go:51-57). При превышении лимита бот всё равно отправляет `msgTooFrequent` на каждое входящее сообщение. Флудер провоцирует 1:1 исходящих и 429 от Telegram, а 429 на `sendMessage` не ретраится.
9. **Оба, повторная доставка после рестарта** (A bot.go:115, B poller.go:69). Offset подтверждается только следующим `getUpdates`. Если остановить бота после обработки пачки, Telegram при старте отдаст её снова, и пользователи получат дубли ответов. Для бота погоды это некритично.
10. **A, нет recover** (bot/bot.go:125-128). Паника в любом обработчике завершает весь процесс.

## Что B взял прямо из примеров CLAUDE.md

Code-блоки из `variant-b/CLAUDE.md` извлечены и сравнены с файлами через `diff` (scratchpad/blocks):

| Пример CLAUDE.md | Файл B | Отличия |
|------------------|--------|---------|
| 9.1 domain | `internal/domain/weather.go` + `errors.go` | Совпадает дословно, только разнесено на 2 файла по разделу 3 (doc-комментарий пакета остался в weather.go). |
| 9.2 usecase | `internal/usecase/weather.go` | 5 изменённых строк из 55: добавлена явная проверка `utf8.ValidString` (строки 45-47). |
| 9.3 openweather | `internal/repository/openweather/client.go` | 2 изменённые строки из 117: `build request: %w` → `%s` + `c.redact(...)` (строка 65), то есть B закрыл утечку, которую пропустил сам пример. |
| 9.4 тест адаптера | `internal/repository/openweather/client_test.go` | Каркас примера сохранён, добавлены 3 строки таблицы (502, 418, битый JSON) и 4 теста (mapping, escape, redact на таймауте, отменённый ctx). |
| 9.5 main | `cmd/weatherbot/main.go` | Совпадает дословно, добавлен только package doc. |
| 11 шаблон handler | `internal/delivery/telegram/handler.go` | 21 изменённая строка из 100: вытеснение `maxTrackedChats` (карта в шаблоне росла бесконечно), `parseCommand` через `IndexFunc(unicode.IsSpace)` (исправлен баг шаблона с `\n`), нормализация пробелов в `msgCityNotFound`. |
| 8 UX | `messages.go`, `format.go` | Тексты слово в слово, формат ответа совпадает с эталоном, что подтверждает format_test.go:9-22. |
| 6 контракты | `delivery/telegram/client.go`, `poller.go` | `retry_after`, 40 s, `allowed_updates`, без `parse_mode`. Кода в CLAUDE.md для них не было, B написал их сам по текстовому контракту. |

Примеры 9.x дают примерно 345 строк, это около 42% из 812 строк prod-кода B. Самостоятельно B написал Telegram-клиент, poller,
config и format, причём именно по жёстким правилам из разделов 6–7. Его качество во многом взято из подсказки.
Там, где шаблон содержал дефекты (`\n` в `parseCommand`, неограниченная карта, `%w` с URL-ошибкой), B их исправил,
а не скопировал. Дефекты самого правила (слишком строгий алфавит города, sentinel без doc) он унаследовал.

## Что A сделал лучше или по-другому

- **Graceful shutdown**: `context.WithoutCancel` + `WaitGroup` позволяют доделать начатые ответы (bot.go:89-93). B обрывает их.
- **Параллельная обработка** с ограничением через семафор (bot.go:120-128). У B один медленный запрос тормозит всех.
- **Redaction через `*url.Error`** (httpx/redact.go) не зависит от того, как именно секрет экранирован, и сохраняет `errors.Is/As`.
- **`config.Load(getenv)`**: конфиг тестируется чистой функцией, без глобального окружения.
- **Гибкость**: `OPENWEATHER_UNITS`, `OPENWEATHER_LANG`, `REQUEST_TIMEOUT` и подписи единиц. B жёстко зашил `metric`/`ru` и таймауты.
- **Отдельный текст для 429 OWM** («Сервис погоды перегружен», handler.go:71-73) и для неизвестной команды.
- **Мягкая валидация города** (только длина): реальные названия с апострофами и цифрами не отсекаются, а за безопасность отвечает `url.Values`.
- **Dockerfile** (distroless, nonroot, `-trimpath`) и Makefile `cover`. B ни Dockerfile, ни аналога не сделал.
- **Дешевле и быстрее**: $0.888 и 215 s против $1.421 и 525 s. Turns у A больше (20 против 13), но каждый из них короче.

## Итоговые факторы

Решающими оказались явные **контракты и запреты** в CLAUDE.md B: статус до decode, `url.Values`/`JoinPath`, `retry_after`,
rate limit + recover, redact, слои. За них B получил +2 по критериям 1, 5, 10, 11, 12, 13. **Примеры кода** почти
целиком перенеслись в результат (около 42% prod-кода), вместе с их сильными и слабыми сторонами. Общие правила A дали
грамотный и идиоматичный код с лучшей конкурентностью и shutdown, но без защитных механизмов, о которых правила не говорили явно.
