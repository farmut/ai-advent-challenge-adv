## Me

> Context:
> - <group>agent</group>

Бот заваливает чат сообщениями «Не так быстро» и ловит лимиты Telegram

Окружение: weatherbot (код в текущем каталоге), обычный личный чат.

Шаги:
1) Отправить боту «Москва».
2) Сразу, быстрее чем за 2 секунды, отправить ещё 10 сообщений подряд (любой текст или вставить пачку).

Ожидаемо: на первое сообщение пришла погода. На всплеск пришло не больше одного предупреждения «⏳ Не так быстро…» за окно ограничения, остальные сообщения тихо проигнорированы. Бот не должен генерировать исходящий трафик пропорционально спаму.

Фактически: на каждое из 10 сообщений бот отвечает отдельным «⏳ Не так быстро, попробуй через пару секунд». При активном спаме в логах появляются WARN `send message … telegram rate limited, retry after …`, и часть ответов не доставляется даже нормальным запросам.

Требование: исправить, добавить регрессионный тест, убедиться что остальные тесты, `go vet` и `gofmt` чистые.

## CodeCompanion (LM Studio)

run_command: find . -type f -name "*.go" | sort && echo --- && git status --short

read_file: internal/delivery/telegram/handler.go

read_file: internal/delivery/telegram/client.go

read_file: internal/delivery/telegram/handler_test.go

read_file: internal/delivery/telegram/messages.go

read_file: internal/delivery/telegram/poller.go

## Me

> Context:
> - <group>agent</group>

