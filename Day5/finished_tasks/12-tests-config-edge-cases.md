# 12. Тесты: граничные случаи config

**Тип:** tests
**Код:** `Day5/src`
**Где:** `internal/config/config.go`, `internal/config/config_test.go`

## Сейчас
Покрытие пакета 87.5%. Не покрыто (`go tool cover -func`):
- `parseLevel` — 66.7%: не проверены `debug`, `warn`, `error`, регистр (`DEBUG`);
- `validBotToken` — 81.8%: не проверены пустой id (`:abc`), нецифровой id (`12a:abc`), пустой secret (`123:`);
- `validateBaseURL` — 80%: не проверены ошибка `url.Parse` (`http://[::1`) и пустой хост (`https://`).

## Критерии «сделано»
- [ ] Табличные тесты на все перечисленные случаи; каждый кейс проверяет, что текст ошибки содержит имя
      переменной (`LOG_LEVEL`, `TELEGRAM_BOT_TOKEN`, `TELEGRAM_BASE_URL`).
- [ ] Тест на пробелы по краям: `TELEGRAM_BOT_TOKEN="  123:abc  "` → токен без пробелов.
- [ ] `go test -coverprofile=c.out ./internal/config && go tool cover -func=c.out` показывает 100% для
      `parseLevel`, `validBotToken`, `validateBaseURL`.
- [ ] Продакшен-код не изменён (`git diff --stat` затрагивает только `config_test.go`).
- [ ] `go test -race ./...`, `go vet ./...`, `gofmt -l .` — чисто.
