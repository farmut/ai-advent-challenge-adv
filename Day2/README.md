# Day2: три агентных профиля Claude Code

Три профиля для headless-режима (`claude -p --agent <name>`), каждый проверен на реальной задаче по коду
`src/` (копия `Day1/variant-b/src`, не правится):

| Профиль | Файл | Что делает | Задача проверки |
|---|---|---|---|
| Bug Fix | [profiles/bug-fixer.md](profiles/bug-fixer.md) + [settings](profiles/bug-fixer.settings.json) | по симптому находит причину, чинит, доказывает тестом, прогоняет всё | [tasks/bugfix.txt](tasks/bugfix.txt): бот отвечает «Не так быстро» на каждое сообщение всплеска |
| Research | [profiles/researcher.md](profiles/researcher.md) + [settings](profiles/researcher.settings.json) | отвечает на вопрос по коду, только чтение, ссылки `путь:строка` | [tasks/research.txt](tasks/research.txt): как защищены секреты |
| Technical writer | [profiles/tech-writer.md](profiles/tech-writer.md) + [settings](profiles/tech-writer.settings.json) | пишет docs/*.md и README по коду | [tasks/writer.txt](tasks/writer.txt): ARCHITECTURE/CONFIGURATION/OPERATIONS/TESTING + README |

**Результат:** все три профиля прошли проверку. researcher — со 2-й попытки (v2: факты отделены от знания stdlib), tech-writer и
bug-fixer — с 1-й, без доработок. Финальные вердикты: `runs/<profile>/attempt-N/verdict.txt`.

**Итоги, таблица доработок профилей и вердикты:** [REPORT.md](REPORT.md). Версии, флаги и sha256: [env.txt](env.txt).
Скриншоты финальных прогонов: [screenshots/](screenshots/).

## Профиль = промпт + права

- `profiles/<name>.md` — субагент Claude Code (frontmatter `name/description/tools/model` + системный промпт:
  роль, алгоритм, ДОЛЖЕН / НЕ ДОЛЖЕН, безопасность, критерий готовности, строгий формат ответа).
- `profiles/<name>.settings.json` — права, передаются через `--settings`: `defaultMode: dontAsk`
  (всё, что не разрешено явно, отклоняется без вопроса), общий deny (секреты, сеть, git-история, `make`, `go run` …),
  профильный allow. Bug Fix дополнительно работает в sandbox ОС (Seatbelt): запись только в рабочий каталог,
  сеть закрыта, `.env` не читается даже кодом тестов.
- `profiles/history/` — все версии профилей по попыткам.

## Как запустить

Нужны Go 1.26, Claude Code 2.1.310 (вход через OAuth), `jq`, `python3`, Google Chrome (для PNG).

```sh
Day2/harness/smoke.sh                    # S0–S7: авторизация, подхват --agent, права, sandbox, сеть
Day2/harness/run.sh researcher 1         # один прогон → Day2/runs/researcher/attempt-1/
Day2/harness/run.sh tech-writer 1
Day2/harness/run.sh bug-fixer 1
```

`run.sh` создаёт одноразовую копию `src/` в `/tmp/d2run.XXXX` (git init + тег `baseline`), кладёт туда только
`.claude/agents/<name>.md` и запускает:

```sh
env -i HOME=… USER=… PATH=… GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=readonly GOCACHE=<run>/.gocache … \
  claude -p "$(cat tasks/<task>.txt)" --agent <name> --settings Day2/profiles/<name>.settings.json \
    --setting-sources project --strict-mcp-config --no-session-persistence \
    --permission-mode dontAsk --model claude-opus-5-5 --output-format stream-json --verbose --max-budget-usd N
```

После прогона собираются артефакты, запускаются проверки и рендер:

| Файл попытки | Что это |
|---|---|
| `cmd.txt`, `task.txt`, `profile.sha256` | точная команда, задача, sha256 профиля/settings/задачи |
| `transcript.jsonl`, `stderr.log`, `run.json` | полный stream-json, итоговое событие (turns, $, время, permission_denials) |
| `answer.md` | итоговый ответ агента |
| `diff.patch`, `changed.txt`, `status.txt`, `tree/` | что агент изменил относительно baseline |
| `audit.txt` | G1–G5: успех, без вопросов, без подглядывания, формат, профиль не подогнан |
| `checks.log` | профильные критерии (B1–B8 / R1–R4 / W1–W5) |
| `verdict.txt` | итог PASS/FAIL + ручные подтверждения |
| `summary.html`, `transcript.html`, `transcript.png`, `transcript-log-partN.png` | рендер транскрипта |

## Структура

```
profiles/            три профиля + settings + history/
tasks/               тексты задач (единственное сообщение агенту)
expected/            эталоны и скрытый приёмочный тест — агенту не показываются
harness/run.sh       изолированный прогон + сбор артефактов
harness/smoke.sh     smoke S0–S7 (smoke-probe.md — нейтральный зонд прав), smoke_eval.py — оценка
harness/checks/      audit (G1–G5), bugfix (B1–B8), research (R1–R4), writer (W1–W5), verdict
harness/render/      stream-json → HTML → PNG (Go, stdlib, headless Chrome)
runs/                все попытки, smoke и калибровка проверок
screenshots/         PNG финальных прогонов
src/                 baseline-код (не меняется, sha256 в env.txt)
```

Ограничение: финальная версия каждого профиля подтверждена одним прогоном (n = 1).
