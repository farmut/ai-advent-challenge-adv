# Day15: стенд Day14 «под ключ» — руководство по развёртыванию

Этот каталог — операционная обвязка над Day14: здесь собрано пошаговое описание и
Makefile-команды для поднятия **полной связки** на чистой машине:

```
┌─────────────────────────────────────────────────────────────────────┐
│  агент Day14 (agent/)   TUI/CLI: /loop, --loop, оркестратор        │
│   │  OpenAI-совместимый клиент (tool calling, retry, budget)        │
│   ▼                                                                 │
│  guard-прокси Day13 (bin/llm-proxy)   ← единственный путь к модели │
│   │  input/output guard, маскирование секретов, аудит, rate limit,  │
│   │  cost tracking (X-Guard-Cost-USD), X-Upstream-Model             │
│   ▼                                                                 │
│  backend модели (один из):                                          │
│    · Ollama  (qwen2.5:7b-instruct)          — локально, нужен pull  │
│    · LM Studio (своя instruct-модель)       — локально, GUI/CLI     │
│    · MLX (Apple Silicon, mlx_lm.server)     — локально              │
│    · OpenRouter (deepseek/deepseek-v4.1-flash-1017 и др.) — облако  │
│                                                                     │
│  docker-песочница ai-adv-sandbox:1 — тесты/SAST кода из loop        │
│  (make sandbox-image, --network none, read-only, uid 65532)         │
└─────────────────────────────────────────────────────────────────────┘
```

Читают также: [`../Day14/README.md`](../Day14/README.md) (устройство loop и стенда),
[`../Day14/agent/README.md`](../Day14/agent/README.md) (конфиги, профили, e2e).

---

## 1. Требования

| Компонент | Минимум | Проверка | Зачем |
|---|---|---|---|
| macOS 14+ или Linux | arm64/x86_64 | `uname -a` | прокси и агент собираются под обе |
| Go | 1.26 (с `GOTOOLCHAIN=auto` скачается сам) | `go version` | сборка прокси и агента |
| GNU make | любой | `make --version` | все команды ниже |
| docker | Desktop 4.x+ | `docker version` | песочница loop (без неё loop не стартует, exit 4) |
| Бэкенд-модель | один из четырёх (см. §4) | — | inference |
| Ключи | `LLM_API_KEY` (см. §3) | — | агент → прокси; облако → OpenRouter |

Опционально: `lms` CLI (LM Studio), Python 3.10+ (только MLX), `chrome-headless-shell` (PNG-отчёты прокси).

## 2. Лayout репозитория (что где)

| Путь | Роль |
|---|---|
| `Day13/` | guard-прокси: `bin/llm-proxy`, `configs/proxy.yaml` (бэкенды и алиасы моделей), `logs/audit/` |
| `Day14/` | агент: `agent/` (бинарь `ai-adv-agent-agent`), `sandbox/` (Dockerfile), `Makefile` (stand-up/stand-down/…), `scripts/openrouter_e2e.py` |
| `Day15/` | этот каталог: гайд + Makefile-обёртки |

## 3. Ключи и секреты (сначала это)

1. **Ключ прокси** — агент ходит в прокси с ключом клиента. Сгенерируйте и положите в `Day13/.env`:

   ```bash
   cd Day13
   [ -e .env ] || (umask 077; sed \
     -e "s/^PROXY_API_KEYS=$/PROXY_API_KEYS=$(openssl rand -hex 32)/" \
     -e "s/^AUDIT_PEPPER=$/AUDIT_PEPPER=$(openssl rand -hex 32)/" \
     .env.example > .env)
   ```

   `PROXY_API_KEYS` — ключ(и) клиентов прокси через запятую (минимум 32 символа),
   `AUDIT_PEPPER` — HMAC-соль аудита. Файл в `.gitignore`, права 0600.

2. **Ключ модели** — зависит от бэкенда:
   - локальные бэкенды (ollama/lmstudio/mlx): ключ не нужен, прокси работает с пустым `api_key_env`;
   - OpenRouter: `export LLM_API_KEY=sk-or-…` в shell перед `stand-up` (прокси берёт его из env как `OPENROUTER_API_KEY`-fallback; см. §6).

   Секреты **только в env / .env**. Кладётся ли ключ в YAML — нет: `llm.api_key` в конфиге агента запрещён, загрузка упадёт.

## 4. Поднятие стенда

### Путь А — одной командой (рекомендуется)

```bash
cd Day15
make stand-up UPSTREAM=ollama     # или lmstudio | mlx; облако — см. §6
make stand-agent                  # TUI агента через прокси
make stand-down                   # остановить всё поднятое
```

Что делает `stand-up`: проверяет `.env`, стартует бэкенд (если не жив), собирает и стартует прокси на `127.0.0.1:8090`, собирает агента, печатает команды. Идемпотентен: живые процессы не трогает, модели не качает повторно. Логи и PID — в `Day14/.stand/`.

### Путь Б — вручную, три терминала (для понимания)

**Терминал 1, бэкенд** (один из):

```bash
# ollama
ollama serve &                    # если ещё не запущен
ollama pull qwen2.5:7b-instruct   # один раз, ~4.7 ГБ
# lmstudio
lms server start --port 1234      # модель должна быть скачана в приложении
# mlx (только Apple Silicon)
cd Day13 && make setup model && make proxy-mlx-upstream
```

**Терминал 2, прокси:**

```bash
cd Day13 && set -a && . ./.env && set +a && make proxy
# слушает 127.0.0.1:8090, конфиг configs/proxy.yaml
```

**Терминал 3, агент:**

```bash
cd Day14/agent && make build
LLM_API_KEY="${PROXY_API_KEYS%%,*}" ./ai-adv-agent-agent \
  --config agent.config.yaml --model qwen2.5-7b-ollama --interactive
```

> Ключ агента = **ключ прокси** (`PROXY_API_KEYS`), не ключ провайдера. Порт и алиас
> модели должны совпадать с тем, что в `Day13/configs/proxy.yaml`.

### Проверка каждого звена

```bash
make doctor     # preflight: go/docker/.env/порты — из Day15
make verify     # 1) бэкенд жив 2) прокси /readyz 3) chat через прокси с X-Guard-* заголовками
```

Ожидаемый результат `verify`: `backend: OK`, `proxy /readyz: OK`, `chat via proxy: 200`,
в заголовках ответа `X-Guard-Input/Output`, `X-Guard-Cost-USD`, `X-Upstream-Model`.

## 5. Execution loop (смысл стенда)

```bash
cd Day15
make sandbox-image              # один раз, нужен docker (~10-20 мин)
make run-loop GOAL='Добавь в Go-сервис на Gin endpoint GET /users?name= с поиском в БД и тест'
# или цель файлом: GOAL='@agent/testdata/loop/sqli_goal.md'
```

Цикл: планировщик (`plan.v1`) → генератор пишет код инструментами → тесты/gosec в docker → security-ревью (read-only, строгий `security_findings.v1`, вердикт считает код) → судья `step_result.v1` → код в `Day14/workspace/<run>/base`. Коды выхода: `0` done, `2` failed, `3` бюджет, `4` нет docker, `5` конфиг. Отчёт в stdout и `Day14/agent/workspace-loop-e2e/<run>/`.

Детерминированный прогон без модели: `make e2e-loop-fake` (fake upstream + прокси + агент + sandbox, SQLi-сценарий, exit 0). Тесты: `cd Day14/agent && go test -tags e2e ./e2e/ -v`.

**Требование к модели:** instruct + tool calling + следование strict JSON (`plan.v1`).
7B instruct локально проходит оркестратор; для loop нужен устойчивый `plan.v1`
(проверено: qwen2.5-7b-instruct и deepseek-v4.1-flash справляются плохо; берите 20B+ или облако).

## 6. Облако через OpenRouter

В `Day13/configs/proxy.yaml` уже заведены upstream `openrouter` и алиас
`deepseek-flash` → `deepseek/deepseek-v4.1-flash-1017` (цены в конфиге из каталога
OpenRouter; смена модели — env `PROXY_MODEL_DEEPSEEK_FLASH_UPSTREAM_MODEL=<id>`).

```bash
export LLM_API_KEY=sk-or-...
cd Day15 && make stand-up UPSTREAM=cloud
make stand-agent STAND_MODEL=deepseek-flash ARGS="--interactive"
```

Облачный конфиг агента с лимитом $1 на loop-прогон: `Day14/agent/agent.openrouter.yaml`.
Облачные e2e (инъекции + loop-цели, отчёты в `swarm-report/`):

```bash
cd Day14 && python3 scripts/openrouter_e2e.py [--phase injection|loop]
```

## 7. Переключение моделей и бэкендов

| Что | Как |
|---|---|
| Алиас агента | `--model <alias>` или `STAND_MODEL=<alias>` (make stand-agent) |
| Реальная модель за алиасом | env `PROXY_MODEL_<ALIAS>_UPSTREAM_MODEL=<id>` **на старте прокси** (после смены — `make stand-down && make stand-up`) |
| Модель ollama | `OLLAMA_MODEL=<id> make stand-up UPSTREAM=ollama` |
| Свой конфиг агента | `make stand-agent AGENT_CONFIG=agent.openrouter.yaml` |

Какой алиас у какого бэкенда по умолчанию: `qwen2.5-7b-ollama`, `qwen2.5-7b-lmstudio`,
`qwen2.5-7b-local` (mlx), `gpt-4.1-mini` (OpenAI, нужен `OPENAI_API_KEY`), `deepseek-flash` (OpenRouter).

### Подставить свою модель (примеры по бэкендам)

Полностью через параметры, без правки кода:

```bash
# OpenRouter — любой id из каталога
PROXY_MODEL_DEEPSEEK_FLASH_UPSTREAM_MODEL=vendor/своя-модель make stand-up UPSTREAM=cloud
make stand-agent STAND_MODEL=deepseek-flash ARGS="--interactive"

# Ollama — любая скачанная модель
OLLAMA_MODEL=gemma3:4b \
PROXY_MODEL_QWEN2_5_7B_OLLAMA_UPSTREAM_MODEL=gemma3:4b \
  make stand-up UPSTREAM=ollama

# LM Studio — id загруженной в приложении модели
PROXY_MODEL_QWEN2_5_7B_LMSTUDIO_UPSTREAM_MODEL=<id> make stand-up UPSTREAM=lmstudio
```

Оверрайд читается прокси **на старте** — после смены: `make stand-down` и повторный `stand-up` с env. Проверка подстановки: `make verify` показывает `X-Upstream-Model` с реальным именем модели.

### Свой сервер со своим base_url (vLLM, llama.cpp, корпоративный gateway)

Через параметры нельзя — нужно 5 строк в `../Day13/configs/proxy.yaml` (upstream + алиас), дальше всё как обычно:

```yaml
upstreams:
  myserver:
    base_url: https://my-llm.example.com/v1    # или http://127.0.0.1:8000/v1 (loopback)
    api_key_env: MY_LLM_KEY                     # ключ из env, никогда из файла
models:
  my-model:
    upstream: myserver
    upstream_model: my-model-name
    price: { input_micro_usd_per_1m: 0, output_micro_usd_per_1m: 0, ref: mine }
```

### Нюанс: цена модели и денежный бюджет

`loop.max_cost_usd` (обрыв по `$1`) считается по заголовку `X-Guard-Cost-USD`, который прокси вычисляет из **цены алиаса** (`price:` в конфиге). Если подставленная модель имеет нулевую/отсутствующую цену — денежный лимит фактически не работает; остаются лимиты `max_llm_calls`, `max_tokens`, `deadline`. Цены OpenRouter — в публичном каталоге `https://openrouter.ai/api/v1/models` (поле `pricing`), перевод в конфиг: `$ за 1M токенов × 1_000_000` = `micro_usd_per_1m`.

## 8. Траблшутинг

| Симптом | Причина | Действие |
|---|---|---|
| `401 invalid_api_key` | агент шлёт не тот ключ (<32 символов / ключ провайдера) | ключ агента = значение `PROXY_API_KEYS` из `Day13/.env` |
| `401` сразу после смены `PROXY_MODEL_*` | прокси не перезапущен (env читается на старте) | `make stand-down && make stand-up` с env |
| `502 upstream_model_not_found` | за алиасом нет модели в бэкенде | `ollama pull <id>` / скачать в LM Studio / оверрайд алиаса |
| `502 upstream_error` один раз | LM Studio догружает модель | повторить запрос |
| `orchestrator protocol violation` | base/coder-модель не следует JSON-экшенам | взять instruct-модель |
| loop падает на «планирование», 0 вызовов | модель не выдаёт валидный `plan.v1` | модель 20B+ / облако; проверить `json_retries` |
| exit 4 при `--loop` | нет docker или образа | `docker version`; `make sandbox-image` |
| macOS: bind mount fails | workspace вне docker-shared пути | держать репо под `/Users/…`; не использовать `/tmp` для workspace |
| порт 8090 занят | старый прокси | `make stand-down` или `lsof -i :8090` |
| TUI: `model=qwen2.5-7b-ollama`, а бэкенд lmstudio | несовпадение алиаса и бэкенда | `STAND_MODEL` подстраивается автоматически при использовании `stand-up/stand-agent` |

## 9. Безопасность и бюджеты

- Секреты только в env: `PROXY_API_KEYS`, `AUDIT_PEPPER` (Day13/.env, 0600), `LLM_API_KEY`/`OPENROUTER_API_KEY` (shell). В YAML ключи запрещены.
- Аудит всех вызовов: `Day13/logs/audit/proxy-audit.jsonl` (маскированные промпты/ответы, guard-действия, стоимость). Отчёт: `make -C Day13 audit-report`.
- Лимиты: rate limit прокси (20/мин на IP, `proxy.yaml`), клиентский bucket агента (`llm.rate_limit`), ретраи (`llm.retry`), loop-бюджеты (`loop.max_cost_usd`, `max_llm_calls`, `deadline`).
- Профили ролей: права из `Day14/agent/profiles/`, правила против prompt injection (Day12), ревьюеры read-only.

## 10. Остановка и чистка

```bash
make stand-down                    # прокси + бэкенд, поднятые через stand-up
rm -rf Day14/.stand                # логи и PID (необязательно)
make -C Day14 clean                # бинарь агента (git-mcp не трогает)
```

Образ песочницы и скачанные модели не удаляются — переиспользуются между запусками.

---

## partner_bundle: автономный Python red-team стенд

Отдельный сервис в [`partner_bundle/`](partner_bundle/README.md) — локальная репетиция
контура «HTTP gateway → генерация → проверки → LLM security review → known-risk policy →
commit» без привязки к Go-стеку Day13/Day14. Связка с основным стендом не нужна:
свой gateway на `127.0.0.1:8093` (guards/audit/rate-limit настоящие, провайдер — fixture
или реальный GigaChat/Anthropic). Проверено на этой машине 2026-11-10: 347 тестов pytest
зелёные, loop 3/3 задачи в commit, redteam-after 33 наблюдения без успешных атак.

```bash
cd partner_bundle
python3 -m venv .venv && . .venv/bin/activate   # Python 3.11+ (проверено 3.13/3.14)
pip install -r requirements.txt
mkdir -p .agent-tmp

# офлайн-прогон (scripted fixture, НЕ LLM):
python docs/day15-redteam/scripts/fixture_gateway.py &   # баннер OFFLINE_SCRIPTED_FIXTURE_NOT_LLM
curl http://127.0.0.1:8093/health
curl http://127.0.0.1:8093/v1/chat -H 'Content-Type: application/json' \
  -d '{"prompt":"Объясни HTTP кратко.","input_mode":"block"}'
python -m src.tools.security_loop --output .agent-tmp/partner-loop-1
python docs/day15-redteam/scripts/evaluate.py --phase after --output .agent-tmp/redteam-after
python -m pytest tests -q -p no:cacheprovider   # 347 тестов
# реальный провайдер (credentials свои, в чат/отчёты не включать):
# export GIGACHAT_SCOPE=GIGACHAT_API_PERS GIGACHAT_CA_BUNDLE=/path/to/ca.pem
# python -m src.tools.llm_gateway --config config/llm_gateway.gigachat.json --env-file /path/local.env
```

Контракт gateway: `POST /v1/chat` `{"prompt":"≤16000 символов","input_mode":"block|mask"}`;
200 — ответ прошёл guard, 422 — секрет/PII заблокирован до API, 502 — output guard,
429 — лимит, 503 — audit недоступен; до 10 POST/мин/IP; модель/system prompt из запроса
не задаются. Loop работает в отдельном git-репозитории внутри output-каталога, чужой
проект не трогает, push не делает. Границы пакета и формат отчёта партнёру — в
[`partner_bundle/README.md`](partner_bundle/README.md).

### Провайдеры bundle (после доработки 2026-11-10)

Помимо fixture/GigaChat/Anthropic добавлены два OpenAI-совместимых провайдера
(`provider: "openai_compatible"`, опциональные `base_url`/`api_key_env`/`api_key_required`):

| Конфиг | Что за ним | Ключ |
|---|---|---|
| `config/llm_gateway.local.json` | локальная модель: LM Studio `:1234` (или Ollama `:11434` — смените `base_url`), дефолт `qwen2.5-coder-7b-instruct` | не нужен (`api_key_required: false`) |
| `config/llm_gateway.deepseek.json` | облако: `deepseek/deepseek-v4.1-flash-1017` через OpenRouter (та, что тестировали с Go-агентом Day14), цены в конфиге из каталога | `OPENROUTER_API_KEY` (или `LLM_API_KEY`) |

```bash
# локальная модель (LM Studio должен быть запущен):
python -m src.tools.llm_gateway --config config/llm_gateway.local.json
# deepseek:
OPENROUTER_API_KEY=sk-or-... python -m src.tools.llm_gateway --config config/llm_gateway.deepseek.json
```

Проверено живьём: локальный провайдер ответил «Москва» через LM Studio (usage реальный,
cost `unknown` — тариф не задан); deepseek-конфиг валидируется, live-прогон — после
возврата ключа. Известный предел: слабые локальные модели не проходят строгий контракт
сигнатур loop (`invalid_signature` на всех попытках) — тот же класс, что `plan.v1` в Day14.

### E2E bundle (адаптированная копия Day14)

`partner_bundle/e2e/bundle_e2e_test.go` — копия `Day14/agent/e2e/loop_e2e_test.go`,
переделанная под Python-сервис (свой gateway вместо Day13, свой loop, контракт
`/v1/chat`). Три сценария: fixture (офлайн, строгие asserts), local (live LM Studio),
deepseek (live, skip без ключа). Полный loop против живой модели — по флагу:

```bash
cd partner_bundle/e2e && go test -tags bundle_e2e -v .          # быстрые сценарии
BUNDLE_E2E_FULL_LOOP=1 go test -tags bundle_e2e -run 'Local|Deepseek' -v .
```

Нюансы адаптации (зафиксированы в коде теста): артефакты — только внутри
`bundle/.agent-tmp` (runner отказывается по symlink-путям macOS `/tmp`/`t.TempDir`);
между фазами тест перезапускает gateway — лимит 10 POST/мин/IP хранится в памяти
процесса. Зелёный прогон 2026-11-10: fixture loop 9 запросов / 3 коммита,
redteam-after 33 кейса без успешных атак, local chat 200.
