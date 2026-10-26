# 13. Тесты: непокрытые ветки HTTP-клиентов

**Тип:** tests
**Код:** `Day5/src`
**Где:** `internal/delivery/telegram/client.go`, `internal/repository/openweather/client.go`

## Сейчас
Не покрыто (`go tool cover -func`):
- `telegram.Client.Send` — 83.3%: нет теста ошибки от API (обёртка `send message: ...`);
- `telegram.Client.redact` — 80%: нет кейса с пустым токеном;
- `openweather.Client.Current` — 91.3%; `openweather.Client.redact` — 75%: нет кейса с пустым ключом и ключом,
  содержащим символы, которые меняются при `url.QueryEscape` (например `a+b/c`);
- нет теста, что тело ответа > 1 MiB (`maxBodyBytes`) не читается целиком и даёт `ErrProviderUnavailable`;
- нет теста `toDomain` при пустом массиве `weather` (описание пустое, паники нет).

## Критерии «сделано»
- [ ] `Send` при ответе 400 `{"ok":false,"description":"chat not found"}` возвращает ошибку, содержащую
      `send message` и `chat not found`, без токена.
- [ ] Ключ `a+b/c` не встречается в тексте ошибки транспорта ни в сыром, ни в экранированном виде.
- [ ] Ответ 200 с телом > 1 MiB → `errors.Is(err, domain.ErrProviderUnavailable)`.
- [ ] Ответ 200 с `"weather":[]` → `Description == ""`, ошибки нет.
- [ ] Покрытие `Send`, `redact` (оба пакета) и `Current` (openweather) — 100%.
- [ ] Продакшен-код не изменён.
- [ ] `go test -race ./...`, `go vet ./...`, `gofmt -l .` — чисто.
