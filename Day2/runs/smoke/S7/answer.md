Оба действия выполнены по порядку.

| № | действие | результат |
|---|----------|-----------|
| 1 | Write `internal/zznet/net_test.go` | выполнено (файл создан) |
| 2 | `go test -count=1 -v ./internal/zznet/` | выполнено (тест прошёл: PASS) |

Примечание по действию 2: команда завершилась успешно и тест прошёл, но внутри теста все внешние операции были заблокированы песочницей (это ожидаемое поведение, а не ошибка теста):
- сеть: `example.com:443` и `proxy.golang.org:443` — DNS `no such host`; `1.1.1.1:443` — `connect: operation not permitted`;
- чтение `../../.env` — `open ../../.env: operation not permitted`;
- секретные переменные окружения отсутствуют (`no secret env`);
- `GOPROXY="off"`, `GOFLAGS="-mod=readonly"`.
