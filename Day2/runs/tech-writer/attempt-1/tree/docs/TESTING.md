# Тестирование weatherbot

**Аудитория:** разработчики, которые запускают и пишут тесты.

Связанные документы: [ARCHITECTURE.md](ARCHITECTURE.md), [CONFIGURATION.md](CONFIGURATION.md),
[OPERATIONS.md](OPERATIONS.md), [README](../README.md).

## Команды

Цели [Makefile](../Makefile):

```sh
make test   # go test -race -cover ./...
make lint   # go vet ./... и проверка gofmt -l .; при неотформатированных файлах печатает их и выходит с ошибкой
```

Запуск отдельного пакета или теста:

```sh
go test ./internal/usecase/
go test -run TestPollerBackoffAndRetryAfter ./internal/delivery/telegram/
go test -run 'TestClientCurrent$' ./internal/repository/openweather/
```

`make test` включает race detector (`-race`), поэтому окружению нужна его поддержка.

## Как тесты обходятся без внешних сервисов

Тесты не обращаются ни к Telegram, ни к OpenWeatherMap, и им не нужны настоящие токены:

- **HTTP-клиенты** (`telegram.Client`, `openweather.Client`) проверяются на локальном
  `httptest.NewServer`, адрес которого передаётся как базовый URL. Сервер проверяет метод, путь
  и параметры запроса и возвращает подготовленные статусы и тела.
- **Обработчик и poller** работают с фейками интерфейсов: `fakeWeather`, `fakeSender`
  ([handler_test.go](../internal/delivery/telegram/handler_test.go)), `fakeUpdates`,
  `fakeHandler` ([poller_test.go](../internal/delivery/telegram/poller_test.go)).
- **Usecase** использует `fakeProvider` ([weather_test.go](../internal/usecase/weather_test.go)).
- **Время.** Паузу backoff в `Poller` можно подменить через поле `wait`
  ([poller.go:30](../internal/delivery/telegram/poller.go)): тесты записывают запрошенные
  задержки и не ждут их. Лимит частоты проверяется прямым вызовом `allow(chatID, now)` с
  заданным временем. Единственный тест с реальным ожиданием — `TestPollerStopsWhileWaiting`:
  он проверяет, что пауза в 1 с прерывается отменой контекста примерно через 20 мс.
- **Конфигурация.** Переменные задаются через `t.Setenv`. Хелпер `setEnv` выставляет все пять
  переменных, поэтому окружение разработчика не влияет на результат
  ([config_test.go:13-18](../internal/config/config_test.go)).
- В тестах используются фиктивные значения (`123456:test-token`, `test-key`).

## Что покрыто

| Пакет | Файл | Что проверяется |
|-------|------|-----------------|
| `internal/config` | [config_test.go](../internal/config/config_test.go) | значения по умолчанию; переопределения и обрезка пробелов; `LOG_LEVEL` без учёта регистра; сбор нескольких ошибок сразу; формат токена; секреты не попадают в текст ошибки |
| `internal/usecase` | [weather_test.go](../internal/usecase/weather_test.go) | нормализация города (пробелы, дефис, точка, запятая, `Ё`, ровно 64 руны); отказ для пустой строки, 65 рун, цифр, `&`, `/`, управляющих символов, невалидного UTF-8, эмодзи — без вызова провайдера; ошибка провайдера оборачивается |
| `internal/repository/openweather` | [client_test.go](../internal/repository/openweather/client_test.go) | путь и параметры запроса; отображение 200/404/401/429/5xx/418/битого JSON в доменные ошибки; отображение полей в `domain.Weather`; экранирование города в query; ключ скрыт в ошибке таймаута; отменённый контекст |
| `internal/delivery/telegram` | [client_test.go](../internal/delivery/telegram/client_test.go) | `getUpdates` (параметры, игнор `edited_message`); `sendMessage` (JSON-тело, без `parse_mode`); 429 с `retry_after`, 401, 400, 502, `ok:false`, битый JSON; токен скрыт в транспортных ошибках |
| | [poller_test.go](../internal/delivery/telegram/poller_test.go) | продвижение `offset` и пропуск апдейтов без текста; последовательность backoff (1 с, 2 с, `retry_after`, 4 с, сброс на 1 с); потолок 30 с; остановка на `ErrUnauthorized`; прерывание паузы отменой |
| | [handler_test.go](../internal/delivery/telegram/handler_test.go) | ответы на команды, `@BotName`, регистр, перевод строки как разделитель; отображение ошибок в тексты; внутренние ошибки не видны пользователю; лимит частоты и очистка карты чатов; восстановление после паники; ошибка отправки; `parseCommand` |
| | [format_test.go](../internal/delivery/telegram/format_test.go) | итоговый текст погоды; пропуск пустых полей; округление температуры; румбы ветра, включая отрицательные и больше 360°; `capitalize` |

Тестов для [cmd/weatherbot](../cmd/weatherbot/main.go) (сборка зависимостей, сигналы, коды
выхода) нет. Интеграционных тестов с настоящими API тоже нет.

## Соглашения для новых тестов

- Не обращайтесь к сети: для HTTP используйте `httptest.NewServer`, для слоёв выше —
  фейки интерфейсов из [ARCHITECTURE.md](ARCHITECTURE.md#слои-и-пакеты).
- Не ждите реальное время: подменяйте `Poller.wait` или передавайте время явно, как в `allow`.
- Для контекста используйте `t.Context()`, для логгера — `slog.New(slog.DiscardHandler)`, как в
  существующих тестах.
- Проверяйте ошибки через `errors.Is` / `errors.As` с доменными ошибками, а не по тексту, кроме
  случаев, где проверяется сам текст (например, отсутствие секретов).
- Перед коммитом выполните `make lint` и `make test`.
