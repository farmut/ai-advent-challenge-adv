# Day 14: execution loop, профили ролей и оркестратор саб-агентов

Агент Day14 — два режима поверх одного ядра:

- **Оркестратор** (Day31–Day33, сохранён): конфиг-ориентированный цикл `spawn / ask_user / finish`, саб-агенты из профилей, RAG/MCP, TUI.
- **Execution loop** (новое в Day14): автономный контур «цель → план → код → тесты → security-ревью → приёмка» с docker-песочницей, бюджетами и профилями ролей с правилами против prompt injection (Day12).

Конфиг — один YAML (`agent.config.yaml`): память, RAG, MCP, оркестратор, профили, sandbox, pipeline, loop. Ключ — только из env, `llm.model` обязателен.

```
пользователь → [оркестратор: цикл планирования, ≤ max_rounds]
                 │  каждый раунд — один JSON-экшен:
                 │   {"action":"spawn","agent","task","context"}   запустить саб-агента
                 │   {"action":"ask_user","question","plan"}       согласовать план (human-in-the-loop)
                 │   {"action":"finish","answer"}                  выдать итоговый ответ
                 ▼
             саб-агенты (researcher / coder / reviewer …)
                 │  свой промпт-роль, свой доступ (RAG / MCP), своя эфемерная память
                 ▼
             результаты возвращаются в транскрипт оркестратора → следующий раунд
```

**Легаси-пути одиночного агента не тронуты**: без `--config` бинарь работает как в Day30 (RAG с реранкингом и цитированием, MCP-инструменты, 3-слойная память, профиль, инварианты, интерактивный FSM). `--config` (или env `AGENT_CONFIG`) включает режим оркестратора.

## Быстрый старт

```bash
cd Day31/agent
make build                 # бинарь ai-adv-agent-agent

# секреты — в окружении, не в YAML
export LLM_PROVIDER=openrouter
export LLM_API_KEY=sk-...
export LLM_MODEL=openai/gpt-4o-mini

# один запрос
make run-orchestrator AGENT_TASK="Проанализируй репозиторий и предложи 3 улучшения"

# интерактивный TUI-сеанс
make run-orchestrator-interactive
```

Прямой вызов:

```bash
./ai-adv-agent-agent --config agent.config.yaml --query "…"
./ai-adv-agent-agent --config agent.config.yaml --interactive
```

## Конфиг `agent.config.yaml`

Один файл включает и выключает возможности. **Каждый слой памяти включён по умолчанию** — слой указывают только чтобы выключить или настроить. RAG и MCP — opt-in.

Приоритет (низкий → высокий): **встроенные дефолты → YAML → явные CLI-флаги**. `config.Load` мержит файл поверх `config.Default()` (пропущенный `enabled:` оставляет слой включённым), `ResolveEnv` заполняет пустые LLM-поля из `LLM_*`, а `config.Overrides` (собран из `flag.Visit`, указатель-на-поле) даёт победить только тем флагам, которые пользователь реально ввёл.

```yaml
llm:
  provider: ""            # env LLM_PROVIDER (openai | openrouter | gigachat)
  model: ""               # env LLM_MODEL
  base_url: ""            # env LLM_BASE_URL (дефолт провайдера, если пусто)
  temperature: null       # null = дефолт провайдера
  max_tokens: 0           # 0 = без лимита
  context_window: 0       # 0 = без клиентской обрезки промпта
  ca_cert: ""             # PEM для self-signed HTTPS (приватный LiteLLM, Day30)

memory:
  dir: chat_history.json  # база; путь каждого слоя выводится из неё
  auto_update: false      # обновлять WM+LTM доп. LLM-вызовом после хода
  stm:  { enabled: true, limit: 10, summary: false }
  wm:   { enabled: true }
  ltm:  { enabled: true }
  facts: { enabled: true }
  profile: { enabled: true }
  task_memory: { enabled: true }
  # выключить слой:  ltm: { enabled: false }

rag:
  enabled: false          # true + db/embed_* → включить RAG
  db: rag.db
  embed_url: http://localhost:11434
  embed_model: nomic-embed-text
  top_k: 20               # размер пула ДО фильтрации
  threshold: 0.5          # отсечка по релевантности
  top_k_final: 10         # оставить ПОСЛЕ фильтра
  rerank: { enabled: false, model: "", mode: auto }  # mode: api | chat | auto

mcp:
  enabled: false
  servers: []             # инлайн-серверы или file: путь к MCP-YAML
  #   - { name: petstore, type: sse, url: "http://127.0.0.1:8931/sse" }
  fs_guard:               # клиентский guard файловых операций (internal/policy)
    enabled: true         # дефолт — включён: «забыли настроить» = «запрещено»
    read_root: ".."       # корень чтения; резолвится от каталога КОНФИГА
    servers: ["fs"]       # серверы, к тулам которых guard применяет проверку пути
    deny: [".env*", ".git/**", "**/*.key"]   # пусто → policy.DefaultDenyGlobs()

invariants:
  enabled: false
  path: ""                # пусто → выводится из memory.dir

orchestrator:
  enabled: true
  max_rounds: 8
  subagents:
    - name: researcher     # аналитик: собирает факты и контекст
      prompt: "Ты аналитик-исследователь. Собираешь факты…"
      rag: true            # доступ к RAG (требует rag.enabled)
      mcp: ["git", "fs"]   # напр. ["petstore"] или ["*"] — все серверы
      tools:               # гейт по ИМЕНАМ тулов + корень записи
        allow: ["read_text_file", "grep"]   # пусто → все тулы выданных серверов
        deny: []                            # всегда побеждает allow
        # write_root не задан → запись запрещена (fail-closed)
      memory: { wm: true }
    - name: coder          # инженер: пишет и правит код
      prompt: "Ты инженер. Пишешь и правишь код по контексту…"
      rag: false
      mcp: ["fs"]
      tools:
        allow: ["read_text_file", "write_file", "edit_file"]
        write_root: "../workspace"   # единственный каталог, куда можно писать
      memory: { wm: true }
    - name: reviewer       # ревьюер: проверяет результат
      prompt: "Ты ревьюер. Проверяешь корректность и риски…"
      rag: false
      mcp: []
      memory: { wm: false }
```

Доступ каждого саб-агента к инструментам гейтится ролью: `rag: true` даёт RAG-груундинг, `mcp: ["*"]` — все MCP-серверы, `mcp: ["petstore"]` — по allow-list, `mcp: []` — только LLM.

### `subagents[].tools` — гейт по инструментам и корень записи

`tools` (тот же блок доступен и у `consultant:`) сужает выданные `mcp:` серверы до конкретных инструментов и объявляет, куда роль вправе писать:

| Поле | Смысл |
|---|---|
| `allow` | Белый список **имён** тулов. Пусто = все тулы выданных серверов |
| `deny` | Чёрный список; всегда побеждает `allow` |
| `write_root` | Единственный подкаталог, доступный на запись. **Пусто = запись запрещена** |
| `read_root` | Переопределяет `mcp.fs_guard.read_root` для этой роли; пусто = наследует |

Ключевые свойства:

- **Fail-closed по умолчанию.** Роль без секции `tools:` остаётся read-only: `write_root` пуст, поэтому write-тулы ей не показываются и не выполняются. Старые конфиги (`mcp: ["git"]` без `tools:`) ведут себя ровно как раньше.
- **Валидация конфига.** `config.Validate` отвергает роль, у которой тул одновременно в `allow` и `deny`, а также роль, которой выдан write-тул при пустом `write_root` — такое право guard всё равно не смог бы исполнить.
- **Пути резолвятся от каталога конфига**, а не от cwd: `config.ResolvePaths` делает `read_root`/`write_root` абсолютными и подставляет токены `${READ_ROOT}` / `${WRITE_ROOT}` в `args` MCP-серверов (оба файловых сервера принимают каталог аргументом и наследуют cwd агента). Поэтому агент ведёт себя одинаково, откуда бы его ни запустили.
- **Два уровня проверки.** `Toolbelt.MCPToolsForGrant` фильтрует список тулов, которые вообще предлагаются модели (UX — не жечь раунды на заведомо отказные вызовы), а `Toolbelt.ToolExecutorForGrant` проверяет то же самое ещё раз перед вызовом — это и есть граница безопасности, через которую придётся пройти модели, выдумавшей имя тула.

### `mcp.fs_guard` — guard файловых операций

Внешний `server-filesystem` умеет только **симметричные** права (allowed-dir = и чтение, и запись). Асимметрию «читать весь `Day34/`, писать только в `Day34/workspace/`» обеспечивает наш guard — пакет **`internal/policy`**, встроенный в единую точку `Toolbelt.ToolExecutorForGrant`.

| Поле | Смысл |
|---|---|
| `enabled` | Включён по умолчанию (`config.Default()`), «забыли настроить» = «запрещено» |
| `read_root` | Корень, относительно которого адресуются все пути. Резолвится от каталога конфига |
| `servers` | Имена MCP-серверов, к тулам которых применяется проверка пути |
| `deny` | Глобы запрещённых путей; пусто → `policy.DefaultDenyGlobs()` |

Как это работает:

- **Таблица тулов fail-closed** (`policy.DefaultSpecs`, `internal/policy/table.go`): у каждого файлового тула объявлены вид (`read`/`write`/нейтральный), имена аргументов с путями и должен ли путь уже существовать. Тул, пришедший с сервера из `fs_guard.servers`, но отсутствующий в таблице, **запрещается**, а не пропускается. Именно поэтому версия npm-пакета в `agent.config.yaml` прибита: таблица привязана к конкретному релизу.
- **Резолв пути** идёт через `filepath.EvalSymlinks` + `filepath.Rel`, а не через `strings.HasPrefix` — иначе каталог-близнец `/root-evil` прошёл бы проверку на корень `/root`. Абсолютные пути и `..` отвергаются, а не «перебазируются».
- **Deny-глобы** сравниваются по итоговому относительному пути в нижнем регистре (ФС macOS регистронезависима, иначе `.ENV` обошёл бы `.env*`) и применяются одинаково к чтению и записи.
- **Текст ошибки — это промпт.** Отказ возвращается модели как результат тула, поэтому он в формате `КОД: пояснение + следующий шаг`: `PERMISSION_DENIED`, `READ_ONLY_PATH`, `DENIED_BY_POLICY`, `BAD_ARGUMENT`. Модель должна из ответа понять, что делать дальше (сохранить в `workspace/`), а не пытаться обойти запрет.

`fs_guard.servers` перечисляет только `fs`: у тулов fff-mcp (`grep`, `find_files`, `multi_grep`) аргумента пути нет вовсе — область поиска фиксируется при старте процесса, — так что проверять у них нечего, им достаточно гейта «выдан роли или нет».

## Память

Оркестратор — **memory-aware**: инжектит общие блоки профиль / LTM / WM / память-задачи в промпт планирования и **подгружает диалоговую историю (STM)**, чтобы помнить прошлые задачи сеанса (уточнения вроде «теперь добавь…» видят прошлый результат). Саб-агенты работают на **эфемерной** памяти (in-memory STM+WM, засеянная контекстом от оркестратора; общие LTM/профиль — **только на чтение** через `storage.ReadOnlyLTM`), поэтому не затирают сессионную память.

| Слой | Флаг (легаси) | Файл | Назначение |
|---|---|---|---|
| STM | `--history` | `chat_history.json` | История диалога |
| WM | `--memory-wm` | `*.wm.json` | Факты текущей задачи |
| LTM | `--memory-ltm` | `*.ltm.json` | Профиль, стабильные предпочтения |
| Память задачи | — | `*.taskmem.json` | Цель / что уточнено / ограничения |
| Профиль | `--profile` | `*.profile.md` | Профиль пользователя (верхний слой) |

Пути WM/LTM/и пр. выводятся из `memory.dir` автоматически.

## Интерактивный режим (TUI)

Полноэкранный интерфейс (`tui.go`, gotui v5): шапка + прокручиваемый лог диалога + многострочный ввод. Задача выполняется в фоне, прогресс оркестратора/саб-агентов стримится в лог. Вне TTY или с `--no-tui` — обычный строчный REPL.

**Ввод** (многострочный `widgets.TextArea`):
- **Enter** — новая строка (набор/абзацы работают; вставка из буфера сохраняется целиком — gotui отдаёт вставленный `\n` как Enter)
- **Ctrl+S** — отправить (модификаторы на Enter в gotui/tcell не детектятся; если комбинация не доходит — `/keys` включает диагностику нажатий в логе)
- **↑/↓** — курсор между строками; на верхней/нижней границе — история ввода (shell-style, с сохранением черновика)

**Навигация и выделение**:
- **тачпад/колесо — прокрутка лога работает по умолчанию** (мышь захвачена приложением; без захвата терминал вообще не передаёт скролл — крутится его собственный буфер)
- **выделение текста при захваченной мыши** — модификатором терминала: Option+drag (iTerm2) / Fn+drag (Terminal.app); либо **`/select`** (или F2) — отпустить мышь для нативного выделения без модификаторов (ценой тачпад-скролла)
- **PgUp / PgDn / Home / End** — прокрутка лога (работает всегда, в обоих режимах); follow-bottom отпускается при скролле вверх и возвращается внизу
- **Ctrl+C** — выход

**Копирование**: `/copy` кладёт последний ответ в системный буфер (`pbcopy`/`wl-copy`/`xclip`/`xsel`, с фолбэком на OSC 52 — работает и по SSH).

**Рендеринг лога** (`tui_render.go`): цветные теги стадий (`[orchestrator]`, `[subagent …]`, `[rag]`, `[mcp]`), markdown-lite для планов и ответов (заголовки `#`, списки `- `/`+ `, `**жирный**`, `` `код` ``, правила `---`), фенсы ```` ``` ```` — verbatim за гаттером. Разметка защищена от порчи «непроизвольным» кодом (флангинг-проверка для `**`, `* ` — не маркер списка) — корректность важнее цвета.

**Слэш-команды** (двуязычные): `/help`·`/помощь` (список команд **+ вход в режим консультанта по документации**), `/end`·`/конец` (выход из режима консультанта), `/agents`·`/агенты`, `/memory`·`/память`, `/mcp`·`/mcp-list`, `/tools`·`/инструменты`, `/copy`·`/копировать`, `/select`·`/выделение`, `/clear`·`/очистить`, `/exit`·`/выход`. Любая строка на `/` — команда, в LLM не уходит (неизвестная `/cmd` сообщается, не отправляется).

**Режим консультанта по документации** (`/help` → … → `/end`): вопросы отвечаются не оркестратором, а заземлённым Q&A по индексу документации (`consultant.rag` в конфиге, по умолчанию `../rag/docs.db`) с доступом к MCP-инструментам из `consultant.mcp` (по умолчанию `["git"]` — ветка/файлы/diff). История режима — эфемерная (сырые вопрос/ответ, окно 12 сообщений), общая история сессии не трогается. Embed-модель консультанта обязана совпадать с той, которой построен `docs.db`.

**Согласование плана** (human-in-the-loop): экшен `ask_user` ставит выполнение на паузу — оркестратор показывает план и ждёт, пока пользователь **согласует, прокомментирует или вернёт на доработку**; ответ вплетается в транскрипт. В одноразовом CLI (без prompter) оркестратор действует автономно.

## Архитектура (Clean Architecture)

| Пакет | Что |
|---|---|
| `internal/config` | YAML-документ, `Load` / `Default` / `ResolveEnv` / `Overrides` / `Validate` — контракт слияния и приоритетов |
| `internal/app` | Композиционный корень (держит `usecase` свободным от адаптеров): `Toolbelt` (общий LLM, RAG-пайплайн, пул MCP-инструментов + маршрутизация tool→server), `MemoryFactory` (shared vs ephemeral память), `Orchestrator`, `SubAgent` |
| `internal/usecase` | `ChatUseCase.ExecuteWithTools` (MCP tool-loop), `RAGUseCase` (retrieve→rerank→filter→grounded), память (`UpdateWM`/`UpdateLTM`/`TaskMemory…`) |
| `internal/adapter` | `llm/` (OpenAI-совместимый HTTP-клиент, `ca_cert`, gen-defaults), `rag/` (эмбеддер + SQLite-поиск + 2 реранкера), `storage/` (файловые + in-memory репозитории, `ReadOnlyLTM`) |
| `internal/domain` / `internal/port` | Сущности и контракты-интерфейсы |

Каждый `SubAgent` переиспользует тот же движок, что и остальной агент — `ChatUseCase.ExecuteWithTools` плюс опциональный RAG-груундинг через `RAGUseCase.BuildPrompt` — на своей эфемерной памяти, с доступом к инструментам по роли.

## Make-цели (Day31)

| Цель | Что делает |
|---|---|
| `make build` / `test` / `vet` / `clean` | Сборка / юнит-тесты / статанализ / очистка |
| `run-orchestrator AGENT_TASK="…"` | Один запрос через оркестратор (`AGENT_CONFIG=`, умолч. `agent.config.yaml`) |
| `run-orchestrator-interactive` | Интерактивный TUI-сеанс |
| `run-orchestrator-rag` | Включить RAG флагами поверх конфига (`RAG_DB=`, `RAG_TOP_K=`, `RAG_THRESHOLD=`, `RAG_TOP_K_FINAL=`) |
| `run-orchestrator-local LOCAL_LLM_BASE_URL=…` | Оркестратор на локальном OpenAI-совместимом эндпоинте (LM Studio / Ollama) |

Makefile также сохраняет RAG-набор (`run-rag*`, `run-rag-eval`/`grounded`/`compare`/`rerank-compare` + их `-local`), MCP-config-тесты, telegram-opt и LiteLLM-цели из Day30.

## Execution loop (Day14)

Полностью автономный цикл «цель → план → код → тесты → security-ревью → приёмка» поверх Day13-прокси. Генератор пишет код **только** инструментами в каталог подзадачи; тесты и SAST выполняются в docker-песочнице; ревьюер read-only и отвечает строгим JSON-контрактом; вердикт всегда считает код, а не модель.

```
plan.v1 (planner, цель в DATA-блоке)
  └─ подзадача (топологический порядок, workspace/<run>/<id> = копия base)
       generating ── coder: spec + feedback.v1 → файлы (diff = SHA256-снапшоты)
       testing ── go vet/test | vue-tsc/vitest (sandbox, exit code решает)
       reviewing ── gosec/eslint (SAST) + security-{go,vue}: security_findings.v1
       verifying ── researcher-судья: step_result.v1 по acceptance-критериям
       done → workspace/<run>/base (неудачная попытка базу не портит)
  fail → feedback по шаблону → повтор; нет прогресса / исчерпаны попытки → replan
```

Ключевые свойства:

- **Доверие.** Список изменённых файлов — разница SHA256-снапшотов, self-report генератора игнорируется. Входы ролей (код, вывод тестов, контекст) оборачиваются в nonce-DATA блоки: текст внутри — данные, не инструкции. Feedback генератору собирается кодом из полей находок по фиксированному шаблону (`исправь: CWE-89 … в main.go:19 — …`), сырой текст ревьюера до генератора не доходит.
- **Вердикт.** `ComputeVerdict` считает код: fail ⇔ есть Verified-находка High/Critical (проверяется file+line по снапшоту). Вердикт из ответа LLM игнорируется. Повтор тот же пары `file|cwe` — «нет прогресса», подзадача падает. `// #nosec` и `eslint-disable` запрещены профилем coder.
- **Бюджеты.** Каждый вызов LLM проходит через `MeteredLLM`: лимиты `loop.{max_llm_calls,max_tokens,max_cost_usd,deadline}` (все обязательны, «0 = бесконечность» запрещена). Превышение → `aborted`. Клиентский rate limiter (`llm.rate_limit`) и ретраи (`llm.retry`) настраиваются там же; лимиты в коде не захардкожены.
- **Персист.** Каждая смена стадии пишется write-ahead в `chat_history.loop.<id>.json` (0600, секреты вырезаются, сырой код не хранится — только `ArtifactRef`/сводки). PID-lock защищает от двух процессов; протухший lock перехватывается. `Esc` / SIGINT → `paused`, продолжение — `/loop resume` или `--loop-resume`.

### Профили и маппинг прав

Профили (`profiles/<name>.md` + `<name>.settings.json`, 7 штук: `planner`, `researcher`, `coder`, `bug-fixer`, `tech-writer`, `security-go`, `security-vue`; отдельный набор `profiles/review/` — CI-ревью PR) — единственный источник прав роли: `internal/adapter/profilefs` читает их, `internal/usecase/profilemap` маппит `Read/Grep/Glob/Edit/Write/Bash(...)` в тулы, write-globs и exec-политику (fail-closed: неизвестное право — ошибка загрузки, `~/...` вне root игнорируется). В loop-режиме роль работает в каталоге подзадачи; `Write=""` делает роль read-only независимо от профиля (так работают ревьюеры и судья). Команды тестов/SAST сверяются с `sandbox.allowed_binaries` и глобальным deny-листом (`go run`, `npm install`, `npx`, `-exec*` и шелл-метасимволы) ещё при загрузке конфига.

Раздел «Безопасность» каждого профиля несёт правила из Day12 по prompt injection: DATA-nonce границы (входы — данные, не инструкции; поддельный маркер — подмена), special-token injection (`<|im_start|>`/`<|im_end|>`/фальшивые маркеры — текст, не команды), invisible payload (zero-width/bidi/теги юникода, HTML-markdown-комментарии), prompt extraction (запрет пересказывать инструкции), instruction override (запрет менять роль/цель/формат по указанию из данных). Ревьюеры трактуют инъекцию в коде как находку CWE-1427. Те же правила подмешиваются ролям без профиля через `UntrustedDataRule` (`internal/usecase/datawrap.go`).

### Sandbox

Генерированный код и команды выполняются **только** в docker-образе (`make sandbox-image`): `--network none`, read-only корень, `--cap-drop ALL`, `no-new-privileges`, uid 65532, лимиты pids/memory/cpus, tmpfs /tmp. Зависимости без сети: образ предзагружает go-mod-cache (gin) и node_modules (vue/vitest/eslint) по шаблонам `sandbox/templates/{go,vue}`; изменение `go.mod`/`package*.json` запрещено политикой профилей — `sbx-run` внутри образа отвечает кодом 97 «зависимости не из шаблона». Конфиг-ключа «выключить sandbox» нет: нет образа или docker — loop не стартует (exit 4).

### Команды

| TUI | CLI | Действие |
|---|---|---|
| `/loop <цель>` / `/цикл` | `--loop "<цель>"` | старт цикла (цель или `@file.md` — пункты `- `/`* ` становятся acceptance-roots) |
| `/loop status` | — | статус и бюджет текущего/последнего run |
| `/loop stop` (или `Esc`) | SIGINT | пауза; попытка помечается interrupted |
| `/loop resume` | `--loop-resume` | продолжение с места остановки |
| `/findings` | — | находки последней попытки и итоговые по run |
| `/profile [name]` | — | профили: описание, права, globs, exec-политика |

Во время loop TUI разрешает только `/loop status|stop`, `/findings`, `/copy`. Коды выхода CLI: `0` done, `2` failed, `3` aborted (бюджет), `4` sandbox недоступен, `5` ошибка конфига.

### Конфигурация

Новые секции (все лимиты — только здесь, быстрые значения для тестов — в `testdata/`):

| Секция | Ключи |
|---|---|
| `pipeline` | `max_security_iterations`, `json_retries`, `generator_profile`, `reviewers{go,vue}`, `test_commands`, `sast`, `command_timeout`, `test_output_max_bytes`, `review_file_max_bytes`, `review_total_max_bytes`, `max_tool_rounds` |
| `loop` | `max_subtasks`, `max_attempts_per_subtask`, `max_llm_calls`, `max_tokens`, `max_cost_usd`, `deadline`, `max_replans`, `on_ask_user` (decide\|fail) |
| `sandbox` | + `templates{go,vue}` — шаблоны для засева workspace |
| `profiles` | `dir` — каталог профилей Day2 |

Прочее: `llm.api_key` в YAML запрещён (только env `LLM_API_KEY`), `llm.model` обязателен, `llm.base_url` — https или loopback-http. Свой сервер `local` (`internal/adapter/localtools`) даёт ролям файловые тулы без `npx`/сети: `read_text_file`, `write_file`, `edit_file`, `grep`, `run_command` и др.; `run_command` идёт в sandbox после проверки exec-политики.

### E2E

```bash
make sandbox-image    # образ песочницы (нужен docker)
make e2e-loop-fake    # fake upstream + прокси + агент: SQLi-сценарий из плана, section 4
go test -tags e2e ./e2e/ -count=1 -v   # то же + негативные варианты, из agent/
```

Сценарий (`testdata/loop/`): генератор пишет endpoint с SQLi (gosec G201 → High), ревьюер возвращает Critical CWE-89, feedback-код принуждает плейсхолдер `$1`; проверяются exit-коды, персист попыток (0600, без ключа), продвижение в `base` и отсутствие сырого ключа в аудите прокси. Негативы: «нет прогресса» → 2, сломанный docker → 4 (до любого вызова LLM), `max_llm_calls: 3` → 3. Ручной шаг: TUI `/loop @…` + `Esc` → `paused` → `/loop resume` → `done` (вариант г плана).

### Облачные прогоны (OpenRouter)

`agent.openrouter.yaml` — конфиг облачной модели через прокси (алиас `deepseek-flash` в `../Day13/configs/proxy.yaml`; ключ только из env `LLM_API_KEY`/`OPENROUTER_API_KEY`; лимит $1 на loop-прогон, `llm.retry.max_delay: 120s` — облачный провайдер просит длинные Retry-After). Прокси отдаёт реальное имя upstream-модели заголовком `X-Upstream-Model` — оркестратор один раз пишет в лог `[llm] upstream model: <real> (alias <alias>)`. Прогоны (6 prompt-injection атак + 3 loop-цели с нарушениями, классификация «прокси / ревью / проскочило», отчёты в `swarm-report/`):

```bash
export LLM_API_KEY=sk-or-...
cd .. && python3 scripts/openrouter_e2e.py [--phase injection|loop]
```

Итоги 2026-11-10 (`swarm-report/openrouter-e2e-FINAL-2026-11-10.md`, HTML: `../reports/openrouter-loop-e2e-2026-11-10.html`): 4/6 атак отражено (провалы — invisible payload и подделка DATA-границы с exfil); output guard прокси заблокировал 12+ опасных ответов; loop-прогоны до security review не дошли — flash-модель не выдаёт валидный `plan.v1` (для полноценных находок CWE нужна модель инструкт-20B+).

## RAG и MCP (сохранены из прошлых дней)

RAG-конвейер (`--rag`): вопрос → эмбеддинг → поиск top-K в `rag.db` → опциональный реранк выделенной моделью → фильтр по порогу → grounded LLM-вызов со **структурированным ответом** (ответ + источники + дословные цитаты; при релевантности ниже порога — честное «не знаю» без вызова LLM). Индекс строит компонент `../rag/` (`index`/`search`/`stats`).

> Запрос эмбеддится **той же моделью**, что и индекс. `../rag/rag.db` построен `text-embedding-nomic-embed-text-v2-moe` (768 dims) — другая модель даёт бессмысленную близость.

MCP (`mcp.enabled: true`): пул подключений к нескольким серверам (stdio + HTTP/SSE). Собственный Petstore MCP-сервер — в `../mcp-server/` (22 инструмента `pet_*`/`store_*`/`user_*`/`report_*`). Саб-агенты получают инструменты по allow-list роли; в TUI `/mcp` и `/tools` показывают живой набор серверов и инструментов.

## Переменные окружения

| Переменная | Обяз. | По умолчанию |
|---|---|---|
| `LLM_PROVIDER` | да | — (`openai` / `openrouter` / `gigachat`) |
| `LLM_API_KEY` | да | — |
| `LLM_MODEL` | нет | дефолт провайдера |
| `LLM_BASE_URL` | нет | дефолт провайдера |
| `LLM_CA_CERT` | нет | — (PEM для self-signed HTTPS) |
| `AGENT_CONFIG` | нет | `agent.config.yaml` |

Секреты держите в окружении — пустые LLM-поля в YAML берутся из `LLM_*`.

## Тесты

```bash
make test          # юнит-тесты
make vet
```

Юнит-тесты покрывают: контракт мерджа/оверрайда/валидации конфига, изоляцию эфемерной vs общей памяти, цикл оркестратора spawn→route→finish на скриптованном mock-LLM, TUI-рендеринг (markdown-lite, экранирование разметки, прокрутка/история), слэш-команды. Guard проверяет, что shipped `agent.config.yaml` валиден.

**`internal/policy`** тестируется в изоляции — пакет импортирует только stdlib и `internal/domain`, поэтому тесты guard'а не поднимают ни LLM, ни MCP. Фикстура строится через `t.TempDir()`; табличные кейсы закрывают обход через `..`, абсолютный путь, симлинк наружу, каталог-близнец (`root-evil` при корне `root`), регистр в deny-глобах, пустой и не-строковый аргумент, запись при пустом `write_root`, неизвестный тул с файлового сервера (fail-closed) и нейтральность git/fff-тулов. Отдельная проверка: любой вердикт начинается с одного из четырёх кодов — это контракт для промпта модели.

`internal/app/toolbelt_test.go` проверяет связку grant → policy на фейковом `port.MCPPool`, в том числе что при отказе guard'а `CallTool` **не вызывается** вовсе.

`TestExampleConfigIsValid` дополнительно стережёт файловую конфигурацию: серверы `fs`/`search` на месте, `fs_guard` включён, писать может ровно одна роль (`coder`, в `../workspace`), версия npm-пакета прибита, `${READ_ROOT}` после `ResolvePaths` не остаётся неподставленным, а **каждое имя тула** в `tools.allow`/`deny` реально существует в `policy.DefaultSpecs()` или среди git-тулов — опечатка в YAML иначе молча лишила бы роль инструмента. Интеграционные RAG-тесты (`-tags integration`) и `telegram-opt` требуют эндпоинт эмбеддингов / LLM-креды.
