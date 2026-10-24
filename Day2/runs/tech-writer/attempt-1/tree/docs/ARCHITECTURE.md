# Архитектура weatherbot

**Аудитория:** разработчики, которые меняют или расширяют код бота.

Связанные документы: [CONFIGURATION.md](CONFIGURATION.md), [OPERATIONS.md](OPERATIONS.md),
[TESTING.md](TESTING.md), [README](../README.md).

## Обзор

weatherbot — один процесс на Go (модуль `weatherbot`, `go 1.26`, см. [go.mod](../go.mod)) без
внешних зависимостей: только стандартная библиотека. Бот получает сообщения из Telegram через
long polling (`getUpdates`), запрашивает текущую погоду у OpenWeatherMap и отвечает в чат
обычным текстом. Входящий HTTP-сервер не поднимается, состояние на диске не хранится.

## Слои и пакеты

| Пакет | Роль | Ключевые типы |
|-------|------|---------------|
| [cmd/weatherbot](../cmd/weatherbot/main.go) | точка входа: загрузка конфигурации, сборка зависимостей, обработка сигналов | `main`, `run` |
| [internal/config](../internal/config/config.go) | чтение и валидация переменных окружения | `Config`, `Load` |
| [internal/domain](../internal/domain/weather.go) | сущность и доменные ошибки ([errors.go](../internal/domain/errors.go)) | `Weather`, `ErrInvalidCity`, `ErrCityNotFound`, `ErrProviderAuth`, `ErrRateLimited`, `ErrProviderUnavailable` |
| [internal/usecase](../internal/usecase/weather.go) | бизнес-логика: нормализация и валидация названия города | `WeatherService`, порт `WeatherProvider` |
| [internal/repository/openweather](../internal/repository/openweather/client.go) | адаптер OpenWeatherMap Current Weather API | `Client` |
| [internal/delivery/telegram](../internal/delivery/telegram/client.go) | клиент Bot API, цикл long polling, разбор команд, форматирование, тексты ответов | `Client`, `Poller`, `Handler` |

Направление зависимостей: `cmd` → `config`, `delivery/telegram`, `repository/openweather`,
`usecase`; `delivery` и `repository` → `domain`; `usecase` → `domain`. Слои связаны через
интерфейсы, объявленные на стороне потребителя:

- `usecase.WeatherProvider` — реализует `openweather.Client` ([weather.go:17](../internal/usecase/weather.go));
- `telegram.WeatherGetter` — реализует `usecase.WeatherService` ([handler.go:21](../internal/delivery/telegram/handler.go));
- `telegram.Sender` и `telegram.UpdatesGetter` — реализует `telegram.Client`
  ([handler.go:26](../internal/delivery/telegram/handler.go), [poller.go:16](../internal/delivery/telegram/poller.go));
- `telegram.MessageHandler` — реализует `telegram.Handler` ([poller.go:21](../internal/delivery/telegram/poller.go)).

Сборка зависимостей — в `run` ([main.go:28-50](../cmd/weatherbot/main.go)):

```
config.Load → openweather.NewClient (http.Client, Timeout 10s)
            → usecase.NewWeatherService
            → telegram.NewClient (http.Client, Timeout 40s)
            → telegram.NewHandler(service, bot, logger)
            → telegram.NewPoller(bot, handler, logger).Run(ctx)
```

## Поток обработки сообщения

1. `Poller.Run` вызывает `GetUpdates(ctx, offset)` в бесконечном цикле
   ([poller.go:39-76](../internal/delivery/telegram/poller.go)). Начальный `offset` — 0, после
   каждого апдейта `offset = max(offset, update_id+1)`.
2. Апдейты без `message` или с пустым `text` пропускаются (но `offset` продвигается)
   ([poller.go:69-72](../internal/delivery/telegram/poller.go)).
3. Для каждого текстового сообщения синхронно вызывается `Handler.Handle(ctx, chatID, text)`.
4. `Handle` ([handler.go:45-58](../internal/delivery/telegram/handler.go)):
   - проверяет лимит частоты на чат (`allow`); при превышении отвечает `msgTooFrequent`;
   - иначе разбирает текст (`parseCommand`) и формирует ответ (`respond`);
   - отправляет ответ через `Sender.Send`; ошибка отправки только логируется;
   - паника внутри обработки перехватывается `recover` и логируется, бот продолжает работу;
     в этом случае пользователь ответа не получает.
5. `respond` ([handler.go:60-82](../internal/delivery/telegram/handler.go)):

| Ввод | Результат |
|------|-----------|
| `/start` | `msgStart` |
| любая другая команда, кроме `/weather` (в т.ч. `/help`, `/foo`) | `msgHelp` |
| `/weather` без аргумента или только пробелы | `msgEmptyCity` |
| `/weather <город>` или текст без `/` | запрос погоды |

   Команда приводится к нижнему регистру, суффикс `@BotName` отбрасывается, разделителем
   команды и аргумента служит любой пробельный символ, включая перевод строки
   ([handler.go:102-113](../internal/delivery/telegram/handler.go)).
6. `WeatherService.Current` нормализует город ([weather.go:44-58](../internal/usecase/weather.go)):
   строка должна быть валидным UTF-8, пробелы схлопываются в один, длина после нормализации
   1–64 руны (`maxCityRunes`), допустимы только буквы (`unicode.IsLetter`), пробел, `-`, `.`, `,`.
   Иначе — `domain.ErrInvalidCity` без обращения к провайдеру.
7. `openweather.Client.Current` выполняет HTTP-запрос и отображает ответ в `domain.Weather`.
8. `formatWeather` ([format.go:14-27](../internal/delivery/telegram/format.go)) собирает текст:
   температуры округляются до целых со знаком `+` для положительных, ветер — до целых м/с,
   направление — один из 8 румбов (`С`, `СВ`, `В`, `ЮВ`, `Ю`, `ЮЗ`, `З`, `СЗ`), первая буква
   описания делается заглавной; строки со страной и описанием опускаются, если поля пусты.

Отображение ошибок в ответ пользователю ([handler.go:71-81](../internal/delivery/telegram/handler.go)):

| Ошибка | Ответ | Лог |
|--------|-------|-----|
| `ErrInvalidCity` | `msgInvalidCity` | нет |
| `ErrCityNotFound` | `msgCityNotFound(<город>)` | нет |
| любая другая (`ErrProviderAuth`, `ErrRateLimited`, `ErrProviderUnavailable`, прочие) | `msgUnavailable` | `WARN weather request failed` |

Все тексты ответов — в [messages.go](../internal/delivery/telegram/messages.go).

## Внешние интеграции

### Telegram Bot API

[internal/delivery/telegram/client.go](../internal/delivery/telegram/client.go)

- URL: `<TELEGRAM_BASE_URL>/bot<token>/<method>` (через `url.JoinPath`, [client.go:108](../internal/delivery/telegram/client.go)).
- `getUpdates`: `GET`, параметры `offset`, `timeout=30` (`longPollTimeout`),
  `allowed_updates=["message"]` ([client.go:79-89](../internal/delivery/telegram/client.go)).
- `sendMessage`: `POST`, тело JSON `{"chat_id": ..., "text": ...}`, `Content-Type: application/json`,
  `parse_mode` не задаётся — ответы уходят как обычный текст ([client.go:92-101](../internal/delivery/telegram/client.go)).
- Тело ответа читается не более 1 MiB (`maxBodyBytes`, [client.go:19](../internal/delivery/telegram/client.go)).
- Обработка статусов ([client.go:129-150](../internal/delivery/telegram/client.go)):

| Статус | Ошибка |
|--------|--------|
| 200, `ok: true` | нет; `result` декодируется в целевой тип |
| 200, `ok: false` | `api error: <description>` |
| 200, невалидный JSON | `decode response: ...` |
| 429 | `*retryAfterError` с `parameters.retry_after` секунд |
| 401, 404 | `ErrUnauthorized` (токен отвергнут) |
| прочие | `status <код>: <description>` |

### OpenWeatherMap

[internal/repository/openweather/client.go](../internal/repository/openweather/client.go)

- `GET <OPENWEATHER_BASE_URL>/data/2.5/weather?q=<город>&units=metric&lang=ru&appid=<ключ>`
  ([client.go:55-61](../internal/repository/openweather/client.go)). Параметры кодируются
  через `url.Values`, поэтому символы вроде `&` в названии не могут подменить параметры.
- Из ответа используются поля `name`, `sys.country`, `main.temp`, `main.feels_like`,
  `main.humidity`, `weather[0].description`, `wind.speed`, `wind.deg` ([client.go:30-47](../internal/repository/openweather/client.go)).
- Тело читается не более 1 MiB (`maxBodyBytes`, [client.go:16](../internal/repository/openweather/client.go)).
- Обработка статусов ([client.go:83-96](../internal/repository/openweather/client.go)):

| Статус / ситуация | Ошибка |
|-------------------|--------|
| 200 | нет |
| 200, невалидный JSON | `ErrProviderUnavailable` |
| 404 | `ErrCityNotFound` |
| 401 | `ErrProviderAuth` |
| 429 | `ErrRateLimited` |
| прочие статусы | `ErrProviderUnavailable` |
| сетевая ошибка, таймаут, отмена контекста | `ErrProviderUnavailable` |

Ретраев к OpenWeatherMap нет: одна попытка на сообщение.

## Конкурентность и состояние

- Обработка строго последовательная: один цикл `Poller.Run`, `Handle` вызывается синхронно
  ([poller.go:73](../internal/delivery/telegram/poller.go)). Медленный ответ OpenWeatherMap
  (до 10 с) задерживает ответы во всех чатах.
- Всё состояние в памяти и теряется при перезапуске:
  - `offset` long polling ([poller.go:40](../internal/delivery/telegram/poller.go));
  - карта `lastReq` для лимита частоты ([handler.go:36](../internal/delivery/telegram/handler.go)),
    защищена `sync.Mutex`.
- Лимит частоты ([handler.go:84-99](../internal/delivery/telegram/handler.go)): не чаще одного
  сообщения в 2 с (`perChatInterval`) на чат. Учитывается любое сообщение, включая `/start` и
  `/help`; отклонённое сообщение получает `msgTooFrequent` и не обновляет отметку времени.
  Когда в карте не меньше 10 000 чатов (`maxTrackedChats`), при следующем разрешённом
  запросе из неё удаляются записи старше 2 с. Жёсткого верхнего предела размера карты нет:
  если все записи свежие, карта продолжает расти.

## Повторы и backoff при ошибках Telegram

[poller.go:46-67](../internal/delivery/telegram/poller.go):

- `ErrUnauthorized` — цикл завершается с ошибкой, процесс выходит с кодом 1.
- 429 с `retry_after > 0` — пауза ровно `retry_after`, текущий backoff не меняется.
- Любая другая ошибка (в т.ч. 429 без `retry_after`) — пауза `backoff`, затем backoff
  удваивается: 1 с, 2 с, 4 с … до 30 с (`minBackoff`, `maxBackoff`, [poller.go:10-13](../internal/delivery/telegram/poller.go)).
- Успешный `getUpdates` сбрасывает backoff на 1 с.
- Пауза прерывается отменой контекста (`sleep`, [poller.go:79-88](../internal/delivery/telegram/poller.go)).

Ошибки `sendMessage` (включая 429 и 401/404) не повторяются: сообщение теряется, в лог пишется
`WARN send message` ([handler.go:55-57](../internal/delivery/telegram/handler.go)).

## Защита секретов и входных данных

- Токен бота и ключ API удаляются из текстов ошибок и заменяются на `[REDACTED]` в сыром и
  URL-экранированном виде (`redact`: [telegram/client.go:155-163](../internal/delivery/telegram/client.go),
  [openweather/client.go:111-117](../internal/repository/openweather/client.go)).
- `config.Load` не включает значения секретов в сообщения об ошибках
  ([config.go:28-29](../internal/config/config.go)).
- Формат токена проверяется при старте, потому что токен подставляется в путь URL
  ([config.go:106-125](../internal/config/config.go)).
- Ответы отправляются без `parse_mode`, поэтому пользовательский ввод не интерпретируется как
  разметка.
- Внутренние ошибки пользователю не показываются: он видит только тексты из `messages.go`.
