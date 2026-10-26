# 11. Рефакторинг: нормализация города дублируется в delivery-слое

**Тип:** refactor
**Код:** `Day5/src`
**Где:** `internal/delivery/telegram/handler.go:77`, `internal/usecase/weather.go:48`

## Проблема
Правило «схлопнуть пробелы» реализовано дважды: в `usecase.normalizeCity` и в handler
(`strings.Join(strings.Fields(city), " ")`) — для текста «Не нашёл город «…»». Если правило нормализации
поменяется в usecase, сообщение пользователю разойдётся с тем, что реально ушло провайдеру. Бизнес-правило
живёт в delivery — нарушение Clean Architecture.

## Нужно
Handler получает нормализованное имя города из usecase. Варианты (выбрать один и обосновать в комментарии):
- доменная ошибка-тип `CityNotFoundError{City string}`, совместимая с `errors.Is(err, domain.ErrCityNotFound)`;
- или usecase экспортирует нормализацию, и handler вызывает её, а не свою копию.

## Критерии «сделано»
- [ ] В пакете `internal/delivery/telegram` нет `strings.Fields` для нормализации города
      (`grep -n "strings.Fields" internal/delivery/telegram/handler.go` — пусто).
- [ ] `errors.Is(err, domain.ErrCityNotFound)` продолжает работать (тест).
- [ ] Ответ на `"  Нью   Йорк  "` при ненайденном городе: `🤔 Не нашёл город «Нью Йорк»…` (тест).
- [ ] `domain` не импортирует ничего, кроме стандартной библиотеки.
- [ ] `go test -race ./...`, `go vet ./...`, `gofmt -l .` — чисто.
