# weatherbot

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
| `/weather`        | погода в последнем городе, для которого в этом чате пришла погода; без истории — подсказка |

Город для `/weather` запоминается только после успешного ответа с погодой. Он хранится в памяти
(до 10 000 чатов, старейшие вытесняются), сбрасывается при перезапуске, а в группе общий для всего чата.

Запросы погоды — не чаще одного в 2 секунды на чат, иначе бот отвечает «⏳ Не так быстро».
Лимит касается только запросов погоды (включая некорректное название города): `/start`, `/help`,
неизвестные команды и `/weather` без города в чате без истории отвечают всегда; `/weather`
с запомненным городом — это запрос погоды и подчиняется лимиту. Лимит хранится в памяти и сбрасывается
при перезапуске. Username бота определяется через `getMe` при старте;
команды для других ботов (`/weather@OtherBot`) игнорируются.

## Переменные окружения

| Переменная             | Обязательна | По умолчанию                     |
|------------------------|-------------|----------------------------------|
| `TELEGRAM_BOT_TOKEN`   | да          | —                                |
| `OPENWEATHER_API_KEY`  | да          | —                                |
| `TELEGRAM_BASE_URL`    | нет         | `https://api.telegram.org`       |
| `OPENWEATHER_BASE_URL` | нет         | `https://api.openweathermap.org` |
| `LOG_LEVEL`            | нет         | `info` (`debug/info/warn/error`) |
| `LOG_FORMAT`           | нет         | `json` (`json/text`, без учёта регистра) |

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

Логи пишутся в stdout — JSON (по умолчанию) или текст (`LOG_FORMAT=text`, удобно локально);
ошибки конфигурации выводятся текстом в stderr. Остановка — Ctrl+C или SIGTERM.

## Ошибки и ответы

| Ситуация | Ответ OpenWeatherMap/Telegram | Доменная ошибка | Текст пользователю | Уровень лога |
|----------|-------------------------------|-----------------|--------------------|--------------|
| Невалидный город | запроса к OWM нет | `domain.ErrInvalidCity` | «Название города может содержать только буквы, пробел, дефис, апостроф, точку и запятую» | нет |
| Город не найден | OWM `404` | `domain.ErrCityNotFound` (как `*CityNotFoundError`) | «🤔 Не нашёл город «X». Проверь написание или попробуй по-английски» (X — нормализованное название) | нет |
| Неверный ключ API | OWM `401` | `domain.ErrProviderAuth` | «⚠️ Сервис погоды сейчас недоступен. Попробуй через пару минут» | WARN `weather request failed` |
| Лимит провайдера | OWM `429` | `domain.ErrRateLimited` | «⚠️ Сервис погоды сейчас недоступен. Попробуй через пару минут» | WARN `weather request failed` |
| 5xx, прочие статусы, таймаут, сеть, битый JSON | OWM `5xx` / нет ответа | `domain.ErrProviderUnavailable` | «⚠️ Сервис погоды сейчас недоступен. Попробуй через пару минут» | WARN `weather request failed` |
| Слишком частые запросы (чаще 1 / 2 с на чат) | запроса к OWM нет | — | «⏳ Не так быстро, попробуй через пару секунд» | нет |
| Неверный токен бота | Telegram `401`/`404` при старте (`getMe`) или в `getUpdates` | `telegram.ErrUnauthorized` | ничего, бот завершается с кодом 1 | ERROR `weatherbot stopped` |
| Лимит Telegram | Telegram `429` с `parameters.retry_after` | — | ничего, polling ждёт `retry_after` секунд (если `0` — backoff 1 → 30 с) | WARN `get updates` с `retry_in` |
| Ошибка `sendMessage` | Telegram ошибка / сеть | — | ответ потерян | WARN `send message` |

Ещё случаи, когда пользователь ничего не получает: команда для другого бота (`/weather@OtherBot`) —
DEBUG `command for another bot ignored`; паника при обработке сообщения — ERROR `handle message panicked`,
бот продолжает работу. ERROR `weatherbot stopped` пишет дефолтный логгер текстом в stderr — независимо
от `LOG_FORMAT`. Секреты (токен, ключ) из текстов ошибок вычищаются до логирования.

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
