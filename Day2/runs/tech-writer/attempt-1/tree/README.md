# weatherbot

**Аудитория:** все, кто впервые знакомится с проектом: разработчики и операторы.

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

## Документация

| Документ | Для кого | О чём |
|----------|----------|-------|
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | разработчики | слои и пакеты, поток сообщения, интеграции с Telegram и OpenWeatherMap, обработка ошибок, backoff |
| [docs/CONFIGURATION.md](docs/CONFIGURATION.md) | операторы, разработчики | переменные окружения, проверки, `.env`, параметры, заданные в коде |
| [docs/OPERATIONS.md](docs/OPERATIONS.md) | операторы | сборка, запуск, остановка, коды выхода, логи, поведение при сбоях, ограничения, диагностика |
| [docs/TESTING.md](docs/TESTING.md) | разработчики | запуск тестов и линтера, работа тестов без сети, покрытие |

## Команды бота

| Ввод | Ответ |
|------|-------|
| `/start` | приветствие |
| `/help` (и любая неизвестная команда) | список команд |
| `/weather Казань` | погода в Казани |
| `Казань` | то же самое: текст без `/` считается названием города |

Команды можно писать в любом регистре и с суффиксом `@ИмяБота`. Название города: от 1 до 64
символов, только буквы, пробел, дефис, точка и запятая.

Каждый чат может отправлять не больше одного сообщения в 2 секунды, включая `/start` и
`/help`. На более частые сообщения бот отвечает «⏳ Не так быстро, попробуй через пару
секунд».

## Переменные окружения

| Переменная | Обязательна | По умолчанию |
|------------|-------------|--------------|
| `TELEGRAM_BOT_TOKEN` | да | — |
| `OPENWEATHER_API_KEY` | да | — |
| `TELEGRAM_BASE_URL` | нет | `https://api.telegram.org` |
| `OPENWEATHER_BASE_URL` | нет | `https://api.openweathermap.org` |
| `LOG_LEVEL` | нет | `info` (`debug`/`info`/`warn`/`error`) |

Токен бота выдаёт @BotFather в Telegram, ключ API — личный кабинет OpenWeatherMap. При ошибках
конфигурации бот перечисляет все проблемы сразу **в stderr** и завершается с кодом 1. Правила
проверки описаны в [docs/CONFIGURATION.md](docs/CONFIGURATION.md).

## Запуск

```sh
cp .env.example .env   # заполнить TELEGRAM_BOT_TOKEN и OPENWEATHER_API_KEY
make run               # подгружает .env; его значения перекрывают переменные окружения
```

или напрямую:

```sh
export TELEGRAM_BOT_TOKEN=<TELEGRAM_BOT_TOKEN> OPENWEATHER_API_KEY=<OPENWEATHER_API_KEY>
go run ./cmd/weatherbot
```

Рабочие логи пишутся в stdout в формате JSON. Запись об аварийном завершении пишется в stderr
обычным текстом. Остановка — Ctrl+C или SIGTERM, код выхода 0. Если Telegram отвергает токен,
бот завершается с кодом 1. Подробнее — в [docs/OPERATIONS.md](docs/OPERATIONS.md).

## Разработка

```sh
make test    # go test -race -cover ./...  (без внешней сети: httptest и фейки)
make lint    # go vet ./... + проверка gofmt -l
make build   # bin/weatherbot
```

Подробнее — в [docs/TESTING.md](docs/TESTING.md).

## Структура

```
cmd/weatherbot                    точка входа, сборка зависимостей, обработка SIGINT/SIGTERM
internal/config                   чтение и валидация env
internal/domain                   сущность Weather и доменные ошибки
internal/usecase                  WeatherService: валидация города, порт WeatherProvider
internal/repository/openweather   адаптер OpenWeatherMap
internal/delivery/telegram        клиент Bot API, long polling, команды, форматирование, тексты
```

Подробнее — в [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).
