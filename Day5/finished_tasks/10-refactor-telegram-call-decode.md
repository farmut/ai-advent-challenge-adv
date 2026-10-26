# 10. Рефакторинг: убрать дублирование декодирования в telegram.Client.call

**Тип:** refactor
**Код:** `Day5/src`
**Где:** `internal/delivery/telegram/client.go:103-152` (`call`)

## Проблема
`json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(&r)` повторяется три раза (429, прочие
ошибки, 200). Функция `call` длиной ~50 строк смешивает сборку URL, HTTP-запрос и разбор ответа.

## Нужно
- Ответ декодируется один раз: helper вроде `decodeAPIResponse(io.Reader) (apiResponse, error)` или
  чтение тела до `switch`.
- Сборка URL вынесена в отдельный метод (`methodURL(apiMethod string, q url.Values) (string, error)`).

## Критерии «сделано»
- [ ] Выражение `json.NewDecoder(io.LimitReader(` встречается в `client.go` ровно один раз
      (`grep -c` = 1).
- [ ] `call` не длиннее 30 строк.
- [ ] Ошибки и их тексты не изменились: `TestClientErrors`, `TestClientRedactsTokenFromTransportErrors`
      и остальные тесты пакета зелёные без правок.
- [ ] Токен не попадает в тексты ошибок (существующие тесты на redaction зелёные).
- [ ] `go test -race ./...`, `go vet ./...`, `gofmt -l .` — чисто.
