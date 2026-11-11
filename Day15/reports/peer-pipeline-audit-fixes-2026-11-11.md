# Исправления по аудиту `2026-11-11_peer-pipeline-audit.md` — статус и результаты

Дата: 2026-11-11
Источник findings: `Day15/2026-11-11_peer-pipeline-audit.md` (аудит пайплайна напарника: agent → proxy → Docker sandbox, 27 HTTP-наблюдений, 6 component probes, полный execution loop).
Модули, в которые внесены исправления: `Day13` (прокси-гард), `Day14` (агент/execution loop).

## Сводная таблица

| Finding | Severity | Суть | Статус | Где исправлено |
|---|---|---|---|---|
| PEER-01 | High | 4 структурных carrier (`block_comment`, `array`, `base64_plus`, `comment_plus`) обходили входной guard в обоих режимах, доходили до upstream побайтно | Исправлено, регрессии зелёные | `Day13/internal/usecase/secrets/{scanner,views}.go` |
| PEER-02 | High | 6 кодированных/составных carrier в ответе модели доставлялись клиенту без редактирования (распознавался только прямой ключ) | Исправлено, регрессии зелёные | `Day13/internal/usecase/proxy/output.go` |
| PEER-03 | Low | Финальный Markdown-отчёт loop сохранял «Статус: running» и нулевые usage counters | Исправлено, регрессия зелёная | `Day14/agent/internal/usecase/execloop/loop.go` |
| A4 (hardening) | — | Невидимые символы (zero-width/bidi/tags): прокси стирает carrier молча, скрытая инструкция становится видимой и легитимной | Исправлено | `Day14/agent/internal/usecase/{datawrap,chat}.go` |
| A6 (hardening) | — | У ролей не было детектора утечки инструкций (boundary forgery / prompt echo) | Исправлено | `Day14/agent/internal/usecase/datawrap.go`, `internal/app/{rolerunner,subagent}.go` |

## PEER-01 — структурные обходы входного guard

**Наблюдение аудита:** `block_comment`, `array`, `base64_plus`, `comment_plus` → HTTP 200, `x-guard-input: pass`, upstream вызван, synthetic carrier сохранён побайтно. Возможна передача реального секрета в любой из этих форм.

**Исправление:**
- `scanner.go`: decoded-представление теперь строится и от **joined**-текста, а не только от canonical: секрет, разрезанный конкатенацией и при этом закодированный (`base64_plus`: `"…" + "…"` из двух base64-половин), находится на глубине ≤ 2.
- `views.go`: новые bounded canonical views (`encodedPartsViews`): удаление block comments (`/* … */`) и объединение flat string arrays. Redaction покрывает исходный carrier целиком; прямое совпадение значения с direct-находкой не дублируется; dedupe по (type, span).
- Превышение decode-бюджета — fail closed.

**Регрессионные тесты** (`secrets_test.go` → `TestViews`, блок «PEER-01 regression»):
- `peer block_comment`: `sk-/* split */proj-…` → joined, blocked;
- `peer line comment`: `sk- // note\n proj-…` → joined, blocked;
- `peer base64_plus`: конкатенация двух base64-половин ключа → decoded, blocked;
- `peer comment_plus`: `sk-/* x */ + /* y */proj-…` → joined, blocked.

Все используют явно синтетический ключ корпуса аудита (`PEERauditSynthetic7xQ2z9Lm4vW8`).

**Дополнительно закрыт смежный known gap** (входил в рекомендацию аудита «recursive Base64→join» и таблицу known gaps): шифрованная тайнопись. `cipherCandidates` перебирает все сдвиги Caesar/ROT13 (≤8 runs × 256 символов) и отдаёт их decoded-скану: `fx-cebw-nop123KLM789` (ROT13 от `sk-proj-abc123XYZ789`) теперь blocked как derived. Словарный детектор инструкций (`detectCiphers`) аннотирует запрос: e2e-кейс IN-13 переведён из `known_gap` в `blocked`.

## PEER-02 — обходы выходного guard

**Наблюдение аудита:** выходной guard распознал только прямой ключ; Base64, quoted-plus, block-comment, array, base64-plus, comment-plus — доставлены клиенту с `x-guard-output: pass`.

**Исправление:** `output.go` — ответ модели прогоняется через **тот же** view-pipeline, что и вход (raw/normalized/joined/decoded). Derived-находки редактируют кодированный carrier точечно: placeholder вместо блоба, block не требуется.

**Регрессионные тесты** (`service_test.go` → `TestOutputGuard`, блок «PEER-02 regression»):
- `output base64 key`: `c2stcHJvai1HZW5FcjR0ZWRLM3k5ODc=` → `OutputRedacted`, `api_key_openai`, блоб `c2stcHJv` отсутствует в контенте;
- `output block comment key`: `sk-/* split */proj-GenEr4tedK3y987` → redacted, тело ключа отсутствует;
- `output quoted-plus key`: `"sk-" + "proj-GenEr4tedK3y987"` → redacted, тело ключа отсутствует.

## PEER-03 — устаревший статус в финальном отчёте loop

**Наблюдение аудита:** встроенный Markdown-отчёт сохранил `Статус: running` и нулевые counters, хотя session JSON — `status: done`, две попытки, 9 вызовов, 4255 synthetic tokens. Причина: рендер отчёта до записи терминального состояния.

**Исправление:** `execloop/loop.go` — `terminate` и `abort` пишут `run.Status` и `syncUsage` (meter → `run.Usage`, replans остаются run-owned, nil-meter в тестах — no-op) **до** рендера отчёта; `persist` тоже синхронизирует usage.

**Регрессионный тест:** `peer_fix_test.go` → `TestTerminateReportFinalState`: после завершения run сохранённый отчёт содержит терминальный статус и фактический usage (42 tokens, cost 0.01) из meter, а не `running` с нулями.

## A4 — невидимые символы

**Риск:** прокси стирает zero-width/bidi/tags из транспорта молча; скрытая инструкция превращается в обычный видимый текст, который модель принимает за легитимный.

**Исправление:** `datawrap.go` — `invisibleRune`/`countInvisible`/`stripInvisible`; top-level query (`chat.go: sanitizeInvisibleQuery`, применяется в `Execute` и `ExecuteWithTools`) и `WrapUntrusted`-блоки с невидимыми символами очищаются и предваряются `[guard]`-предупреждением с числом удалённых символов.

**Тесты:** `datawrap_test.go` → `TestWrapUntrustedAnnotatesInvisible` (warning есть, `\u200B` не выжил, очищенный контент на месте; чистый текст не аннотируется).

## A6 — утечка инструкций ролью

**Риск:** модель воспроизводит системные инструкции / маркеры границ по просьбе из данных (boundary forgery, prompt echo) — детектора не было.

**Исправление:** `DataCanary` встроена в `UntrustedDataRule` (каждый рольный prompt её несёт); `CanaryLeak` на ответе роли → `ErrCanaryLeak`; `rolerunner.go` и `subagent.go` переводят такой ход в fail closed, аналогично guard block.

**Тесты:** `datawrap_test.go` → `TestCanaryLeak` (канарейка в правиле; детект в ответе; нет false positive на чистом ответе).

## Результаты верификации (выполнено после всех правок)

| Проверка | Команда | Результат |
|---|---|---|
| Сборка прокси + бинарники | `make -C Day13 proxy-build fake-upstream` | OK (`bin/llm-proxy`, `bin/fake-upstream`) |
| Сборка агента и git-mcp-server | `make -C Day14 build` | OK (exit 0) |
| Вет | `go vet ./...` (Day13), `make -C Day14 vet` | чисто |
| Формат | `gofmt -l` (оба модуля) | чисто |
| Тесты прокси | `go test ./...` (Day13) | 24 пакета ok, 0 FAIL |
| Тесты агента | `go test ./...` (Day14/agent) | 17 пакетов ok, 0 FAIL |
| E2E через HTTP-стек | `go test ./e2e/proxy/` (fake upstream, оба режима mask/block, 20 кейсов) | ok; IN-13 blocked, IN-13a pass + `encoded_text` |

## Остаточные риски (зафиксированы)

- **Литература** (подстановка с сохранением гласных): по биграммному покрытию статистически неотделима от обычных слов (измеренный overlap 0.40–0.75 на обоих классах); аннотация давала бы false positive на каждой короткой заметке без словарных слов. Осознанно out-of-scope, документировано в `Day13/README.md`.
- Ключ словами, реверс строки, base32/gzip — прежние known gaps, вне PEER-findings.
- E2E против live-модели не прогонялся (fake upstream); логика покрыта unit/e2e-корпусом.

## Изменённые файлы

**Day13:** `internal/usecase/secrets/scanner.go`, `views.go`, `secrets_test.go`; `internal/usecase/proxy/service.go`, `output.go`, `service_test.go`; `e2e/proxy/proxy_e2e_test.go`, `e2e/proxy/html_test.go`, `e2e/proxy/testdata/cases.json`; `configs/proxy.yaml`; `README.md`.
**Day14:** `agent/internal/usecase/execloop/loop.go` (+ новый `peer_fix_test.go`), `agent/internal/usecase/datawrap.go`, `datawrap_test.go`, `agent/internal/usecase/chat.go`, `agent/internal/app/rolerunner.go`, `agent/internal/app/subagent.go`.
