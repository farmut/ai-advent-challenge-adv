# Day10 — Agile-консультант (Go + MLX)

TUI-сервис «Agile-консультант» на Go 1.26 с интеграцией дообученной в Day9 MLX-модели.

## Стек

- Go 1.26
- `charmbracelet/bubbletea` — TUI
- `mlx-lm` 0.32.2 (`../Day6/.venv`) — инференс MLX
- Базовая модель: `mlx-community/Qwen2.5-7B-Instruct-4bit`
- Тюненая модель: `Day9/models/student-fused` → `models/agile-v1.0-4bit`

## Быстрый старт

```bash
cd /Users/amachulin/ai_advent_challenge_adv/ai-advent-challenge-adv/Day10
source ../Day6/.venv/bin/activate
make build
make run
```

В TUI:
- введите Agile-вопрос и нажмите `Enter`;
- `Tab` или `m` — переключить активную модель;
- `Ctrl+C` или `q` — выход.

## Команды

| Команда | Назначение |
|---|---|
| `make build` | собрать бинарник |
| `make run` | запустить TUI |
| `make test` | unit-тесты |
| `make vet` | `go vet` |
| `make bench` | бенчмарк tuned-модели |
| `make bench-compare` | A/b tuned vs baseline |
| `make preflight` | test + vet + build |
| `make clean` | удалить bin/ и логи |

## Headless-режим

```bash
nohup ./bin/agile-consultant server --config configs/app.yaml > logs/agile-consultant.log 2>&1 &
```

Пример запроса (поле `model` должно содержать абсолютный путь к модели для `mlx_lm.server`):

```bash
curl -s http://127.0.0.1:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "/Users/amachulin/ai_advent_challenge_adv/ai-advent-challenge-adv/Day10/models/agile-v1.0-4bit",
    "messages": [{"role": "user", "content": "Как провести ретроспективу?"}],
    "max_tokens": 512,
    "temperature": 0.7
  }'
```

## Переключение моделей без перезапуска

1. Отредактируйте `configs/app.yaml`: установите `default: true` нужной модели.
2. Отправьте `SIGHUP` процессу:
   ```bash
   kill -HUP $(pgrep -f "agile-consultant server")
   ```

Рекомендуется запускать через `nohup` или launchd/systemd, чтобы `SIGHUP` не завершал процесс.

## Выкат новой версии модели

```bash
# 1. Подготовить новую версию
cp -R models/agile-v1.0-4bit models/agile-v1.1-4bit

# 2. Добавить в configs/app.yaml
# models:
#   v1.1:
#     name: agile-v1.1-4bit
#     path: .../models/agile-v1.1-4bit
#     default: true

# 3. Применить без перезапуска
kill -HUP $(pgrep -f "agile-consultant server")
```

## Откат

1. Верните `default: true` на предыдущую версию в `configs/app.yaml`.
2. `kill -HUP $(pgrep -f "agile-consultant server")`.

## Структура

```
Day10/
├── cmd/agile-consultant/main.go
├── internal/
│   ├── config/          # YAML-конфиг
│   ├── ports/           # интерфейсы
│   ├── usecase/         # бизнес-логика + парсер ответа
│   ├── adapters/mlx/    # HTTP-клиент и lifecycle mlx_lm.server
│   ├── ui/              # bubbletea TUI
│   └── benchmark/       # метрики и бенчмарки
├── configs/app.yaml
├── testdata/agile_queries.jsonl
├── scripts/benchmark.sh
├── Makefile
└── swarm-report/
```

## Отчёт

Подробный Report: `swarm-report/day10-2026-11-02.md`.
