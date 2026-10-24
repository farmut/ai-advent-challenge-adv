# weatherbot

Telegram-бот, который присылает текущую погоду в городе из [OpenWeatherMap](https://openweathermap.org/current).
Написан только на стандартной библиотеке Go, внешних зависимостей нет.

## Использование

| Сообщение                  | Ответ                       |
|----------------------------|-----------------------------|
| `/start`, `/help`          | справка                     |
| `/weather Москва`          | текущая погода в Москве     |
| `Москва` (обычный текст)   | то же самое                 |

Пример ответа:

```
Погода: Moscow, RU
Облачно
🌡 Температура: 5.3°C (ощущается как 2.1°C)
💧 Влажность: 80%
💨 Ветер: 3.5 м/с
🧭 Давление: 1012 гПа
```

## Настройка

1. Создайте бота у [@BotFather](https://t.me/BotFather) и получите токен.
2. Получите API-ключ на <https://home.openweathermap.org/api_keys> (новый ключ активируется до пары часов).
3. `cp .env.example .env` и заполните значения.

| Переменная             | Обязательна | По умолчанию | Описание                              |
|------------------------|-------------|--------------|---------------------------------------|
| `TELEGRAM_BOT_TOKEN`   | да          | —            | токен бота                            |
| `OPENWEATHER_API_KEY`  | да          | —            | ключ OpenWeatherMap                   |
| `OPENWEATHER_UNITS`    | нет         | `metric`     | `metric`, `imperial` или `standard`   |
| `OPENWEATHER_LANG`     | нет         | `ru`         | язык описания погоды                  |
| `REQUEST_TIMEOUT`      | нет         | `10s`        | таймаут обработки одного сообщения    |
| `LOG_LEVEL`            | нет         | `info`       | `debug`, `info`, `warn`, `error`      |

Секреты читаются только из окружения и никогда не попадают в логи
(URL с токеном/ключом вырезается из сетевых ошибок).

## Запуск

Требуется Go 1.25+.

```sh
make run                 # загрузит .env и запустит бота
# или
make build && TELEGRAM_BOT_TOKEN=... OPENWEATHER_API_KEY=... ./bin/weatherbot
```

Docker:

```sh
docker build -t weatherbot .
docker run --rm --env-file .env weatherbot
```

Бот работает через long polling (вебхук и публичный адрес не нужны) и корректно
завершается по `SIGINT`/`SIGTERM`, дожидаясь обработки уже принятых сообщений.
Логи пишутся в stdout в формате JSON.

## Тесты

```sh
make test    # go test -race ./...
make cover   # отчёт о покрытии
```

Тесты не ходят в сеть: HTTP-клиенты проверяются на `httptest.Server`,
логика бота — на фейковых реализациях интерфейсов.

## Структура

```
cmd/weatherbot/      точка входа, сборка зависимостей
internal/config/     загрузка и валидация переменных окружения
internal/weather/    клиент OpenWeatherMap
internal/telegram/   минимальный клиент Telegram Bot API (getUpdates, sendMessage)
internal/bot/        цикл long polling, разбор команд, форматирование ответа
internal/httpx/      общие HTTP-утилиты (скрытие секретов в ошибках)
```
