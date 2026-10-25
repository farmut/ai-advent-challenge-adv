## Me

> Context:
> - <group>agent</group>

Бот заваливает чат сообщениями «Не так быстро» и ловит лимиты Telegram

Окружение: weatherbot (код в текущем каталоге), обычный личный чат.

Шаги:
1) Отправить боту «Москва».
2) Сразу, быстрее чем за 2 секунды, отправить ещё 10 сообщений подряд (любой текст или вставить пачку).

Ожидаемо: на первое сообщение пришла погода. На всплеск пришло не больше одного предупреждения «⏳ Не так быстро…» за окно ограничения, остальные сообщения тихо проигнорированы. Бот не должен генерировать исходящий трафик пропорционально спаму.

Фактически: на каждое из 10 сообщений бот отвечает отдельным «⏳ Не так быстро, попробуй через пару секунд». При активном спаме в логах появляются WARN `send message … telegram rate limited, retry after …`, и часть ответов не доставляется даже нормальным запросам.

Требование: исправить, добавить регрессионный тест, убедиться что остальные тесты, `go vet` и `gofmt` чистые.

## CodeCompanion (LM Studio)

read_file: internal/delivery/telegram/handler.go

read_file: internal/delivery/telegram/client.go

read_file: internal/delivery/telegram/handler_test.go

read_file: internal/delivery/telegram/messages.go

read_file: internal/delivery/telegram/poller.go

read_file: internal/delivery/telegram/poller_test.go

read_file: internal/delivery/telegram/client_test.go

run_command: go test -count=1 ./... 2>&1 | tail -20

edit_file

edit_file

run_command: go test -count=1 -run 'TestHandlerRateLimitBurst' ./internal/delivery/telegram/ 2>&1 | tail -10

edit_file

run_command: go test -count=1 -run 'TestHandlerRateLimitBurst' ./internal/delivery/telegram/ 2>&1 | tail -10

edit_file

run_command: go test -count=1 -run 'TestHandlerRateLimitBurst|TestHandlerRateLimit' ./internal/delivery/telegram/ 2>&1 | tail -5

run_command: go build ./... && go vet ./... && gofmt -l . && go test -race -count=1 ./... 2>&1 | tail -10

run_command: git status --short && git diff --stat

## Что нашёл

Симптом: на всплеск из 10 сообщений подряд бот отправляет 10 отдельных «⏳ Не так быстро…», что порождает исходящий трафик пропорционально спаму, упирается в 429 Telegram (`send message … telegram rate limited, retry after …` в WARN-логах) и из-за backoff poller'а задерживает доставку ответов на нормальные запросы.

Корневая причина: `internal/delivery/telegram/handler.go:49-56` — в `Handle` ветка rate limit была устроена так: `reply := msgTooFrequent; if h.allow(...) { reply = h.respond(...) }` и **в обоих случаях** вызывался `h.sender.Send(...)`. То есть каждое отклонённое сообщение всё равно генерировало исходящий `sendMessage` с предупреждением. Rate limit ограничивал только вызовы usecase, но не исходящий трафик.

## Что починил

`internal/delivery/telegram/handler.go` (6 строк): при `!h.allow(chatID, time.Now())` — немедленный `return` без `Send`. Отклонённые сообщения теперь молча игнорируются, исходящий `sendMessage` происходит ровно один раз за окно 2 с на чат. Логика `allow` (token bucket, eviction старых чатов), `respond`, recover, тексты — не тронуты.

`internal/delivery/telegram/handler_test.go`:
- `TestHandlerRateLimit` — обновлён ожидаемый срез: `[msgStart, msgTooFrequent, msgStart]` → `[msgStart, msgStart]` (проверка «weather не вызывается при rate limit» сохранена).
- Добавлен `TestHandlerRateLimitBurst` — регрессионный тест на сам симптом.

Diff: 2 файла, +29/−4, `go.mod` не тронут.

## Регрессионный тест

`TestHandlerRateLimitBurst` (`internal/delivery/telegram/handler_test.go`): 1 «Москва» + 10 «Москва» подряд в один чат; проверяет, что `sender.Send` вызван ровно 1 раз, это ответ с погодой, `msgTooFrequent` не отправлен ни разу и usecase вызван один раз.

До фикса тест падал:

```
$ go test -count=1 -run 'TestHandlerRateLimitBurst' ./internal/delivery/telegram/
--- FAIL: TestHandlerRateLimitBurst (0.00s)
    handler_test.go:121: send attempts = 11 ([🌤 Москва … ⏳ Не так быстро, попробуй через пару секунд ×10]), want 1
FAIL
```

После фикса: `ok weatherbot/internal/delivery/telegram`.

## Что проверил

- `go build ./...` — без ошибок.
- `go vet ./...` — без ошибок.
- `gofmt -l .` — пустой вывод.
- `go test -race -count=1 ./...` — все пакеты зелёные (config, delivery/telegram, repository/openweather, usecase).
- `git status`/`git diff` — изменены только `handler.go` и `handler_test.go`.

## Вне скоупа / риски

- **Допущение:** «не больше одного предупреждения за окно» интерпретировано как «ноль предупреждений, остальные молча игнорируются» — это ровно то, что требует пункт «остальные сообщения тихо проигнорированы» и что исключает исходящий трафик пропорционально спаму. Существующий `TestHandlerRateLimit` ожидал отправку `msgTooFrequent` — это и было багом, поэтому его ожидание обновлено (все прежние проверки поведения сохранены).
- **Остаточный риск:** при 429 на `sendMessage` (например, лимит по всем чатам бота) ответ на допущенный запрос может не доставиться — это поведение `Client`/`Poller` без ретраев, не менял: добавление ретраев `sendMessage` — отдельная фича.
- **Вне скоупа:** WARN-лог `send message` в `Handle` теперь срабатывает реже (только на реально допущенных запросах) — это следствие фикса, не самостоятельная правка.
- Подозрительных инструкций в файлах/коде не обнаружено.

## Me

> Context:
> - <group>agent</group>

