# Day4: локальная LLM как код-ассистент в AstroNvim

Локальная модель (LM Studio) подключена в AstroNvim через codecompanion.nvim (чат, inline, агент) и minuet-ai.nvim (FIM-автодополнение).
Правила проекта из Дня 1 стоят в system prompt. Те же задачи, что у облачного ассистента в Днях 1 и 2, прогнаны на двух локальных моделях.

**Итоги:** [REPORT.md](REPORT.md). Коротко: qwen3.8-27b без thinking прошла агентную задачу Дня 2 с первого раза (7 мин против 1.5 мин у облака).
Генерацию проекта Дня 1 не довела до рабочего состояния ни одна локальная модель. Версии и sha256 лежат в [env.txt](env.txt).

## Установка в IDE

Нужны LM Studio с `lms` в PATH, модели `qwen/qwen3-coder-30b` и/или `qwen/qwen3.8-27b` и `qwen2.5-coder-1.5b` (FIM), AstroNvim v5.

```sh
Day4/nvim/install.sh          # ~/.config/nvim/lua/plugins/ai.lua -> Day4/nvim/ai.lua (старый файл уходит в .bak.<время>)
lms server start
```

В nvim:

| Команда | Что делает |
|---|---|
| `:AILoad` | загружает чат-модель в LM Studio с окном из пресета (`-c 32768`) |
| `<Leader>ac` | чат; `@{agent}` в сообщении включает агентный режим (чтение/правка файлов, команды) |
| `<Leader>ai` / `<Leader>aa` | inline-правка / меню действий, в нём `/bugfix` — профиль bug-fixer из Дня 2 |
| `<C-]>` | ручной вызов FIM-подсказки (minuet) |

Модель выбирается через `AI_CHAT_MODEL=qwen/qwen3.8-27b nvim` или `ga` в чате. Пресеты (temperature, top_p, top_k, max_tokens, окно, thinking) задаются в таблице `models` в `ai.lua`.

Правила проекта: файл `.ai/system-prompt.md` в каталоге проекта или выше (поиск идёт до `$HOME`). Здесь это [.ai/system-prompt.md](.ai/system-prompt.md), копия `Day1/variant-b/CLAUDE.md`.
Предустановленные правила codecompanion (`CLAUDE.md`, `AGENTS.md`, `~/.claude/CLAUDE.md` и т.п.) в локальную модель **не** отправляются.

## Воспроизведение прогонов

Нужны Go 1.26, LM Studio, python3. Команды запускаются из корня репозитория.

```sh
Day4/harness/run.sh bugfix  qwen/qwen3.8-27b     32768 0.2 0.9 my-label   # агентная задача Дня 2
Day4/harness/run.sh feature qwen/qwen3-coder-30b 32768 0.2 0.9 my-label   # генерация бота Дня 1
AI_REASONING=none Day4/harness/run.sh ...                                 # thinking выкл. (LM Studio reasoning_effort)
Day4/harness/queue.sh <файл>                                              # очередь: строки "<env> -- <аргументы run.sh>"
```

`run.sh` делает следующее:
1. Создаёт одноразовую копию в `$TMPDIR/d4run.*` и перезагружает модель в LM Studio с нужным контекстом. **Все загруженные модели выгружаются.**
2. Поднимает логирующий прокси и запускает headless nvim с конфигом IDE под Seatbelt: запись только в каталог прогона, сеть только localhost.
3. Собирает артефакты и запускает проверки Дня 1 (`metrics.sh`, black-box) или Дня 2 (`bugfix.sh`) без изменений.

## Структура

```
.ai/system-prompt.md       правила Дня 1 (= Day1/variant-b/CLAUDE.md) — system prompt локальной модели
nvim/ai.lua                конфиг IDE (источник правды, symlink в ~/.config/nvim)
nvim/install.sh            бэкап + symlink
prompts/bug-fixer.md       профиль Дня 2 без frontmatter (prompt library /bugfix)
tasks/                     feature.txt = Day1/prompt.txt, agent-bugfix.txt = Day2/tasks/bugfix.txt
harness/run.sh             один изолированный прогон + проверки
harness/driver.lua         headless-драйвер codecompanion (одно сообщение, без подсказок)
harness/sandbox.sb         Seatbelt-профиль
harness/proxy.py           логирующий прокси перед LM Studio (тела запросов, usage, тайминги)
harness/summarize.py       сводка прогона
harness/queue.sh           последовательная очередь прогонов
runs/<task>/<label>/       артефакты: cmd.txt, meta.json, events.jsonl, messages.json, transcript.md, answer.md,
                           requests.jsonl.gz, diff.patch, tree/, checks.log | metrics.txt + blackbox.json,
                           summary.txt, manual.txt (ручной вердикт)
```

Ограничения: по 1 прогону на комбинацию (coder30b на bugfix — 3). Подбор temperature и размера контекста не делался, подробнее в REPORT.md.
