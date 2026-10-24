# x

Telegram-бот, который присылает текущую погоду в городе из
[OpenWeatherMap](https://openweathermap.org/current). Go 1.26, только стандартная библиотека,
long polling (входящий HTTP-сервер не нужен).

```
🌤 Москва, RU
🌡 +12 °C, ощущается как +10 °C
☁️ Переменная облачность
💧 Влажность 71 %
💨 Ветер 4 м/с, СЗ
```

## Команды

| Ввод              | Ответ                              |
|-------------------|------------------------------------|
| `/start`          | приветствие                        |
| `/help`           | список команд                      |
| `/weather Казань` | погода в Казани                    |
| `Казань`          | то же самое — текст = город        |

Не чаще одного запроса в 2 секунды на чат.

## Переменные окружения

| Переменная             | Обязательна | По умолчанию                     |
|------------------------|-------------|----------------------------------|
| `TELEGRAM_BOT_TOKEN`   | да          | —                                |
| `OPENWEATHER_API_KEY`  | да          | —                                |
| `TELEGRAM_BASE_URL`    | нет         | `https://api.telegram.org`       |
| `OPENWEATHER_BASE_URL` | нет         | `https://api.openweathermap.org` |
| `LOG_LEVEL`            | нет         | `info` (`debug/info/warn/error`) |

Токен бота выдаёт [@BotFather](https://t.me/BotFather), ключ API — личный кабинет OpenWeatherMap
(новый ключ активируется не сразу, до пары часов). При ошибках конфигурации бот перечисляет все
проблемы сразу и завершается с кодом 1.

## Запуск

```sh
cp .env.example .env   # заполнить TELEGRAM_BOT_TOKEN и OPENWEATHER_API_KEY
make run               # подхватит .env
```

или напрямую:

```sh
export TELEGRAM_BOT_TOKEN=... OPENWEATHER_API_KEY=...
go run ./cmd/weatherbot
```

Логи — JSON в stdout. Остановка — Ctrl+C или SIGTERM.

## Разработка

```sh
make test    # go test -race -cover ./...  (без сети: всё через httptest)
make lint    # go vet + gofmt -l
make build   # bin/weatherbot
```

## Структура

```
cmd/weatherbot             точка входа, DI, graceful shutdown
internal/config            чтение и валидация env
internal/domain            сущность Weather и доменные ошибки
internal/usecase           WeatherService: валидация города, порт WeatherProvider
internal/repository/openweather   адаптер OpenWeatherMap
internal/delivery/telegram клиент Bot API, long polling, команды, форматирование, тексты
```
