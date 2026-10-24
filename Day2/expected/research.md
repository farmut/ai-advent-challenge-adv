# Эталон для профиля researcher (ground truth)

Вопрос: `tasks/research.txt` (как защищены секреты: откуда читаются, как валидируются, где вычищаются из ошибок и логов, где риски утечки).

Файл агенту НЕ показывается. Все `путь:строка` проверены по `Day2/src` (`sed -n`) перед прогонами.

Покрытие R4 считает `checks/research.sh`: факт засчитан, если регулярка (Python `re`, IGNORECASE) из блока `facts` находит совпадение в `answer.md`. Затем каждое совпадение подтверждается вручную в `verdict.txt` (регулярка ловит маркер, а не смысл). Порог: ≥ 80% фактов, 0 неверных утверждений (ручная разметка).

## Факты (человекочитаемо)

| ID | Факт | Где |
|---|---|---|
| F01 | Секреты читаются из env через `env()`: `strings.TrimSpace(os.Getenv(key))`, пустое → default | internal/config/config.go:67-72 |
| F02 | Токен и ключ: `TELEGRAM_BOT_TOKEN`, `OPENWEATHER_API_KEY`, default пустой | internal/config/config.go:33-34 |
| F03 | Тексты ошибок конфига не содержат значений секретов (комментарий и сами ошибки) | internal/config/config.go:28-29, 40-47 |
| F04 | Тест `TestLoadDoesNotLeakSecrets` закрепляет это | internal/config/config_test.go:103-109 |
| F05 | `validBotToken`: формат `<digits>:<[A-Za-z0-9_-]+>` | internal/config/config.go:106-125 |
| F06 | Причина валидации: токен встаёт в путь URL (`JoinPath("bot"+c.token, …)`) | internal/config/config.go:106-107, internal/delivery/telegram/client.go:108 |
| F07 | Ключ OWM проверяется только на непустоту, формат не валидируется | internal/config/config.go:45-47 |
| F08 | `validateBaseURL`: только http/https, host обязателен, без query/fragment (защита от подмены адреса, куда уходят секреты) | internal/config/config.go:74-89 |
| F09 | Telegram `redact` заменяет сырой токен, `PathEscape` и `QueryEscape` формы на `[REDACTED]` | internal/delivery/telegram/client.go:155-163 |
| F10 | Где применяется telegram `redact`: build request :117, transport `*url.Error` :124, Description :138, :144 | internal/delivery/telegram/client.go:117,124,138,144 |
| F11 | Тест `TestClientRedactsTokenFromTransportErrors` | internal/delivery/telegram/client_test.go:120 |
| F12 | Ключ OWM передаётся в query-параметре `appid` | internal/repository/openweather/client.go:60 |
| F13 | OWM `redact`: сырой ключ и `QueryEscape` (без PathEscape), применяется в :65, :69 | internal/repository/openweather/client.go:111-117 |
| F14 | Тест `TestClientCurrentTimeoutRedactsKey` | internal/repository/openweather/client_test.go:96 |
| F15 | `statusError` формирует ошибку без URL | internal/repository/openweather/client.go:83-96 |
| F16 | `.env` в `.gitignore`, `.env.example` с пустыми значениями | .gitignore:1, .env.example:1-5 |
| F17 | Куда уходят ошибки: логи handler (send :56, weather :79) | internal/delivery/telegram/handler.go:56,79 |
| F18 | Пользователь не видит текст внутренней ошибки: неизвестная ошибка → `msgUnavailable` | internal/delivery/telegram/handler.go:78-80 |
| F19 | Ошибка конфига логируется в main | cmd/weatherbot/main.go:23 |
| F20 | Риск: `redact` строковый, зависит от формы экранирования; `%s` вместо `%w` теряет цепочку ошибки | internal/delivery/telegram/client.go:117,124 |
| F21 | Риск: у `Config` нет `LogValue`/`String`, логирование `cfg` целиком выдаст секреты (сейчас так не делается) | internal/config/config.go:20-26 |
| F22 | Риск: значение паники логируется как есть | internal/delivery/telegram/handler.go:48 |
| F23 | Риск (низкий): decode-ошибки не проходят через redact (URL в них нет) | internal/repository/openweather/client.go:78; internal/delivery/telegram/client.go:141,148 |
| F24 | Риск: `make run` подгружает `.env` в окружение процесса | Makefile:10 |

## Машиночитаемые маркеры

```facts
F01 ::: TrimSpace
F02 ::: TELEGRAM_BOT_TOKEN.{0,400}OPENWEATHER_API_KEY|OPENWEATHER_API_KEY.{0,400}TELEGRAM_BOT_TOKEN
F03 ::: config\.go:(2[89]|4[0-7])|не (содерж|попада|включа)\w*.{0,60}(значени|секрет|токен)|без значени
F04 ::: TestLoadDoesNotLeakSecrets
F05 ::: validBotToken
F06 ::: JoinPath|"bot"\s*\+|bot<|пут[ьиеё]\w* URL|URL[- ]пут|в путь|in the path
F07 ::: (OPENWEATHER_API_KEY|ключ\w*( OWM| OpenWeather)?).{0,160}(только|лишь|не валидир|не провер|формат не)|config\.go:4[5-7]
F08 ::: validateBaseURL
F09 ::: PathEscape
F10 ::: client\.go:1(17|24|38|44)|\*url\.Error
F11 ::: TestClientRedactsTokenFromTransportErrors
F12 ::: appid
F13 ::: QueryEscape|openweather/client\.go:(6[59]|11[1-7])
F14 ::: TestClientCurrentTimeoutRedactsKey
F15 ::: statusError
F16 ::: \.gitignore
F17 ::: handler\.go:(5[56]|79)
F18 ::: msgUnavailable
F19 ::: main\.go:2[2-4]
F20 ::: (%s|%v).{0,200}%w|%w.{0,200}(%s|%v)|errors\.(Is|As|Unwrap).{0,120}(тер|lost|не работ)
F21 ::: LogValue|LogValuer|String\(\)|Config.{0,80}(цел|целиком|весь)
F22 ::: handler\.go:4[78]|panic\w*.{0,120}(лог|log)|паник\w*.{0,120}(лог|log)
F23 ::: decode.{0,200}(redact|маскир|не очища|без очист)|(redact|маскир).{0,200}decode|client\.go:(78|141|148)
F24 ::: Makefile|make run
```
