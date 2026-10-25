# WeatherBot

Telegram-бот для получения информации о погоде из OpenWeatherMap API.

## Требования

- Go 1.26

## Переменные окружения

| Переменная             | Обязательна | Default                          |
|------------------------|-------------|----------------------------------|
| `TELEGRAM_BOT_TOKEN`   | да          | —                                |
| `OPENWEATHER_API_KEY`  | да          | —                                |
| `TELEGRAM_BASE_URL`    | нет         | `https://api.telegram.org`       |
| `OPENWEATHER_BASE_URL` | нет         | `https://api.openweathermap.org` |
| `LOG_LEVEL`            | нет         | `info` (`debug/info/warn/error`) |

## Команды

- `/start` — приветствие
- `/help` — показать помощь
- `/weather <город>` — показать погоду в городе
- Просто текст без `/` — название города

## Запуск

```bash
# Установка зависимостей
go mod tidy

# Запуск бота
TELEGRAM_BOT_TOKEN="your_telegram_token" OPENWEATHER_API_KEY="your_openweather_key" go run cmd/weatherbot/main.go
```

## Тестирование

```bash
go test ./...
```