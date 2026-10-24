# Конфигурация weatherbot

**Аудитория:** операторы, которые разворачивают бота, и разработчики, которые меняют настройки.

Связанные документы: [ARCHITECTURE.md](ARCHITECTURE.md), [OPERATIONS.md](OPERATIONS.md),
[TESTING.md](TESTING.md), [README](../README.md).

Бот настраивается только переменными окружения. Флагов командной строки и файлов
конфигурации нет. Чтение и проверка — `config.Load` в
[internal/config/config.go](../internal/config/config.go).

## Переменные окружения

| Переменная | Обязательна | По умолчанию | Правила проверки |
|------------|-------------|--------------|------------------|
| `TELEGRAM_BOT_TOKEN` | да | — | формат `<цифры>:<секрет>`; секрет — только латинские буквы, цифры, `_` и `-` |
| `OPENWEATHER_API_KEY` | да | — | непустая строка |
| `TELEGRAM_BASE_URL` | нет | `https://api.telegram.org` | абсолютный URL со схемой `http`/`https`, непустым хостом, без query и fragment |
| `OPENWEATHER_BASE_URL` | нет | `https://api.openweathermap.org` | те же правила, что для `TELEGRAM_BASE_URL` |
| `LOG_LEVEL` | нет | `info` | `debug`, `info`, `warn` или `error`, регистр не важен |

Источники: значения по умолчанию — [config.go:13-17](../internal/config/config.go), чтение —
[config.go:32-37](../internal/config/config.go) и [config.go:54](../internal/config/config.go),
проверки — [config.go:39-57](../internal/config/config.go), [config.go:74-125](../internal/config/config.go).

Общие правила:

- Пробелы по краям значения обрезаются ([config.go:67-72](../internal/config/config.go)).
- Пустое значение (или только пробелы) равносильно отсутствию переменной: для необязательных
  подставляется значение по умолчанию, для обязательных это ошибка. Поэтому пустые строки из
  [.env.example](../.env.example) безопасны для необязательных переменных.
- Базовый URL может содержать путь (например, `http://localhost:8081/proxy`): пути API
  добавляются к нему через `url.JoinPath`.
- `TELEGRAM_BASE_URL` и `OPENWEATHER_BASE_URL` нужны в основном для локальных заглушек и
  прокси, например `http://localhost:8081`.
- `LOG_LEVEL=debug` допустим, но сейчас в коде нет сообщений уровня DEBUG, поэтому вывод такой
  же, как при `info`.

Где взять значения:

- `TELEGRAM_BOT_TOKEN` выдаёт @BotFather в Telegram.
- `OPENWEATHER_API_KEY` выдаётся в личном кабинете OpenWeatherMap.

## Поведение при ошибках конфигурации

- `config.Load` собирает все найденные проблемы и возвращает их одной ошибкой
  (`errors.Join`, [config.go:60-62](../internal/config/config.go)).
- Значения секретов в текст ошибки не попадают.
- Процесс пишет запись `weatherbot stopped` уровня `ERROR` с полем `err` через стандартный
  логгер `slog` и завершается с кодом 1 ([main.go:21-31](../cmd/weatherbot/main.go)). Это
  происходит до создания JSON-логгера, поэтому запись попадает в **stderr в текстовом
  формате**, а не в JSON в stdout.

Пример значения поля `err`, когда ошибок несколько (части разделены переводом строки, тексты из
[config.go](../internal/config/config.go)):

```
load config: TELEGRAM_BOT_TOKEN is required
OPENWEATHER_API_KEY is required
LOG_LEVEL: unknown level "verbose", want debug, info, warn or error
```

Прочие варианты: `TELEGRAM_BOT_TOKEN has invalid format, want <digits>:<secret>`,
`<VAR>: scheme must be http or https, got "..."`, `<VAR>: host is empty`,
`<VAR>: query and fragment are not allowed`, `<VAR>: parse url: ...`.

Формат токена проверяется только при старте. То, что Telegram принимает этот токен, видно лишь
по первому `getUpdates`: если Telegram ответит 401 или 404, бот завершится (см.
[OPERATIONS.md](OPERATIONS.md#коды-выхода)).

## Файл .env и `make run`

- Шаблон: [.env.example](../.env.example) — все пять переменных с пустыми значениями.
- `.env` указан в [.gitignore](../.gitignore) и в репозиторий не попадает.
- Сам бот `.env` не читает. Его подгружает цель `run` в [Makefile](../Makefile): если файл
  есть, он выполняется через `.` с `set -a`, поэтому **значения из `.env` перекрывают уже
  заданные переменные окружения**. Это касается и пустых строк вида `LOG_LEVEL=`: они
  затирают значение из окружения, и бот берёт значение по умолчанию (а для обязательных
  переменных сообщает об ошибке). Без `.env` используется текущее окружение.
- `.env` выполняется как shell-скрипт: значения со спецсимволами shell нужно брать в кавычки.

Пример `.env`:

```sh
TELEGRAM_BOT_TOKEN=<TELEGRAM_BOT_TOKEN>
OPENWEATHER_API_KEY=<OPENWEATHER_API_KEY>
TELEGRAM_BASE_URL=
OPENWEATHER_BASE_URL=
LOG_LEVEL=info
```

## Параметры, заданные в коде

Эти значения не настраиваются через окружение; для изменения нужна правка кода.

| Параметр | Значение | Где |
|----------|----------|-----|
| Таймаут HTTP-клиента OpenWeatherMap | 10 с | [main.go:38](../cmd/weatherbot/main.go) |
| Таймаут HTTP-клиента Telegram | 40 с (должен быть больше long-poll таймаута) | [main.go:40](../cmd/weatherbot/main.go) |
| Long-poll таймаут `getUpdates` | 30 с | `longPollTimeout`, [client.go:20](../internal/delivery/telegram/client.go) |
| Начальный / максимальный backoff `getUpdates` | 1 с / 30 с, удвоение | `minBackoff`, `maxBackoff`, [poller.go:10-13](../internal/delivery/telegram/poller.go) |
| Лимит частоты на чат | 1 сообщение в 2 с | `perChatInterval`, [handler.go:16](../internal/delivery/telegram/handler.go) |
| Порог очистки карты лимитера | 10 000 чатов | `maxTrackedChats`, [handler.go:17](../internal/delivery/telegram/handler.go) |
| Максимальная длина названия города | 64 руны | `maxCityRunes`, [weather.go:14](../internal/usecase/weather.go) |
| Максимальный размер читаемого тела ответа | 1 MiB | `maxBodyBytes`, [telegram/client.go:19](../internal/delivery/telegram/client.go), [openweather/client.go:16](../internal/repository/openweather/client.go) |
| Единицы и язык OpenWeatherMap | `units=metric`, `lang=ru` | [openweather/client.go:58-59](../internal/repository/openweather/client.go) |
| Типы апдейтов Telegram | `allowed_updates=["message"]` | [client.go:83](../internal/delivery/telegram/client.go) |
