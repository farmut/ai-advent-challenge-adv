# weatherbot

Telegram-бот: по запросу пользователя присылает текущую погоду в городе из OpenWeatherMap.

## Команды

- `/start` — приветствие
- `/help` — справка
- `/weather <город>` — погода в городе (также работает просто текст с названием города)

## Переменные окружения

| Переменная             | Обязательна | Default                          |
|------------------------|-------------|----------------------------------|
| `TELEGRAM_BOT_TOKEN`   | да          | —                                |
| `OPENWEATHER_API_KEY`  | да          | —                                |
| `TELEGRAM_BASE_URL`    | нет         | `https://api.telegram.org`       |
| `OPENWEATHER_BASE_URL` | нет         | `https://api.openweathermap.org` |
| `LOG_LEVEL`            | нет         | `info` (`debug/info/warn/error`) |

Скопируйте `.env.example` в `.env` и заполните значения (бот читает переменные окружения,
файл `.env` подгружайте сами, например через `direnv` или `export`).

## Запуск

```sh
export TELEGRAM_BOT_TOKEN=...
export OPENWEATHER_API_KEY=...
go run ./cmd/weatherbot
```

## Тесты и линт

```sh
make test   # go test -race -cover ./...
make lint   # go vet + gofmt -l
```

Тесты не ходят в сеть: все HTTP-вызовы проверяются через `httptest` и фейки.
