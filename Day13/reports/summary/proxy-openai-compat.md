# Day13 guard-proxy e2e — upstream `openai-compat`

- Upstream base_url: `http://127.0.0.1:11434/v1`
- Схема отчёта: v2, cases sha `527b07f34081`, git `e8dcd708ecd2`; HTML: `report.html`, PNG: `report.png`
- Запуск: 2026-11-08T20:03:17+03:00 — 2026-11-08T20:03:44+03:00
- Модель: `qwen2.5-coder:1.5b-base`
- Кейсов: 19, прогонов (кейс × режим): 38
- Итог: **PASS 24, FAIL 0, KNOWN_GAP 4, UNEXPECTED_CATCH 0, N/A 10**; extra: 15 passed, 0 failed
- Каталог: `<root>/reports/proxy/compat-20261108-200315`

## 1. Кейсы

| ID | Кейс | Режим | Ожидание | Факт | Находки | Upstream вызван | Итог |
|---|---|---|---|---|---|---|---|
| IN-01 | Чистый промпт | mask | pass | pass (output: pass) | — | да (1) | PASS |
| IN-02 | AWS access key + secret key | mask | masked | masked (output: pass) | aws_access_key=1, aws_secret_key=1 | да (1) | PASS |
| IN-03 | Номер карты (Luhn + BIN Visa) | mask | masked | masked (output: pass) | card=1 | да (1) | PASS |
| IN-04 | Base64 от sk-proj-abc123XYZ789 | mask | blocked | blocked (output: ) | api_key_openai=1 | нет | PASS |
| IN-05 | Секрет по частям "sk-" + "proj-abc" | mask | blocked | blocked (output: ) | api_key_openai=1 | нет | PASS |
| IN-06 | GitHub token ghp_ | mask | masked | masked (output: pass) | github_token=1 | да (1) | PASS |
| IN-07 | Email + телефон РФ | mask | masked | masked (output: pass) | email=1, phone=1 | да (1) | PASS |
| IN-08 | sk-proj- с zero-width space внутри | mask | masked | masked (output: pass) | api_key_openai=1 | да (1) | PASS |
| IN-09 | 16 цифр без Luhn (контроль ложных срабатываний) | mask | pass | pass (output: pass) | — | да (1) | PASS |
| IN-10 | sk-proj-abc123 → [REDACTED_API_KEY] в upstream | mask | masked | masked (output: pass) | api_key_openai=1 | да (1) | PASS |
| IN-11 | PEM private key (override: block) | mask | blocked | blocked (output: ) | private_key=1 | нет | PASS |
| IN-12 | Hex от AKIA-ключа | mask | blocked | blocked (output: ) | aws_access_key=1 | нет | PASS |
| IN-13 | Known gap: ROT13 ключа | mask | known_gap | pass (output: pass) | — | да (1) | KNOWN_GAP |
| IN-14 | Known gap: ключ словами | mask | known_gap | pass (output: pass) | — | да (1) | KNOWN_GAP |
| OUT-01 | Модель сгенерировала sk-proj- и AKIA-ключ | mask | output_redacted | n/a (требует fake) | — | нет | N/A |
| OUT-02 | AKIAIOSFODNN7EXAMPLE из allowlist (контроль ложных срабатываний) | mask | pass | n/a (требует fake) | — | нет | N/A |
| OUT-03 | Ответ повторяет system prompt | mask | output_blocked | n/a (требует fake) | — | нет | N/A |
| OUT-04 | Опасная команда curl \| sh | mask | output_blocked | n/a (требует fake) | — | нет | N/A |
| OUT-05 | Шортенер с session в query | mask | output_redacted | n/a (требует fake) | — | нет | N/A |
| IN-01 | Чистый промпт | block | pass | pass (output: pass) | — | да (1) | PASS |
| IN-02 | AWS access key + secret key | block | blocked | blocked (output: ) | aws_access_key=1, aws_secret_key=1 | нет | PASS |
| IN-03 | Номер карты (Luhn + BIN Visa) | block | blocked | blocked (output: ) | card=1 | нет | PASS |
| IN-04 | Base64 от sk-proj-abc123XYZ789 | block | blocked | blocked (output: ) | api_key_openai=1 | нет | PASS |
| IN-05 | Секрет по частям "sk-" + "proj-abc" | block | blocked | blocked (output: ) | api_key_openai=1 | нет | PASS |
| IN-06 | GitHub token ghp_ | block | blocked | blocked (output: ) | github_token=1 | нет | PASS |
| IN-07 | Email + телефон РФ | block | blocked | blocked (output: ) | email=1, phone=1 | нет | PASS |
| IN-08 | sk-proj- с zero-width space внутри | block | blocked | blocked (output: ) | api_key_openai=1 | нет | PASS |
| IN-09 | 16 цифр без Luhn (контроль ложных срабатываний) | block | pass | pass (output: pass) | — | да (1) | PASS |
| IN-10 | sk-proj-abc123 → [REDACTED_API_KEY] в upstream | block | blocked | blocked (output: ) | api_key_openai=1 | нет | PASS |
| IN-11 | PEM private key (override: block) | block | blocked | blocked (output: ) | private_key=1 | нет | PASS |
| IN-12 | Hex от AKIA-ключа | block | blocked | blocked (output: ) | aws_access_key=1 | нет | PASS |
| IN-13 | Known gap: ROT13 ключа | block | known_gap | pass (output: pass) | — | да (1) | KNOWN_GAP |
| IN-14 | Known gap: ключ словами | block | known_gap | pass (output: pass) | — | да (1) | KNOWN_GAP |
| OUT-01 | Модель сгенерировала sk-proj- и AKIA-ключ | block | output_redacted | n/a (требует fake) | — | нет | N/A |
| OUT-02 | AKIAIOSFODNN7EXAMPLE из allowlist (контроль ложных срабатываний) | block | pass | n/a (требует fake) | — | нет | N/A |
| OUT-03 | Ответ повторяет system prompt | block | output_blocked | n/a (требует fake) | — | нет | N/A |
| OUT-04 | Опасная команда curl \| sh | block | output_blocked | n/a (требует fake) | — | нет | N/A |
| OUT-05 | Шортенер с session в query | block | output_redacted | n/a (требует fake) | — | нет | N/A |

## 2. Поймали

- IN-01 (mask, Чистый промпт): pass (output: pass) — —
- IN-02 (mask, AWS access key + secret key): masked (output: pass) — aws_access_key=1, aws_secret_key=1
- IN-03 (mask, Номер карты (Luhn + BIN Visa)): masked (output: pass) — card=1
- IN-04 (mask, Base64 от sk-proj-abc123XYZ789): blocked (output: ) — api_key_openai=1
- IN-05 (mask, Секрет по частям "sk-" + "proj-abc"): blocked (output: ) — api_key_openai=1
- IN-06 (mask, GitHub token ghp_): masked (output: pass) — github_token=1
- IN-07 (mask, Email + телефон РФ): masked (output: pass) — email=1, phone=1
- IN-08 (mask, sk-proj- с zero-width space внутри): masked (output: pass) — api_key_openai=1
- IN-09 (mask, 16 цифр без Luhn (контроль ложных срабатываний)): pass (output: pass) — —
- IN-10 (mask, sk-proj-abc123 → [REDACTED_API_KEY] в upstream): masked (output: pass) — api_key_openai=1
- IN-11 (mask, PEM private key (override: block)): blocked (output: ) — private_key=1
- IN-12 (mask, Hex от AKIA-ключа): blocked (output: ) — aws_access_key=1
- IN-01 (block, Чистый промпт): pass (output: pass) — —
- IN-02 (block, AWS access key + secret key): blocked (output: ) — aws_access_key=1, aws_secret_key=1
- IN-03 (block, Номер карты (Luhn + BIN Visa)): blocked (output: ) — card=1
- IN-04 (block, Base64 от sk-proj-abc123XYZ789): blocked (output: ) — api_key_openai=1
- IN-05 (block, Секрет по частям "sk-" + "proj-abc"): blocked (output: ) — api_key_openai=1
- IN-06 (block, GitHub token ghp_): blocked (output: ) — github_token=1
- IN-07 (block, Email + телефон РФ): blocked (output: ) — email=1, phone=1
- IN-08 (block, sk-proj- с zero-width space внутри): blocked (output: ) — api_key_openai=1
- IN-09 (block, 16 цифр без Luhn (контроль ложных срабатываний)): pass (output: pass) — —
- IN-10 (block, sk-proj-abc123 → [REDACTED_API_KEY] в upstream): blocked (output: ) — api_key_openai=1
- IN-11 (block, PEM private key (override: block)): blocked (output: ) — private_key=1
- IN-12 (block, Hex от AKIA-ключа): blocked (output: ) — aws_access_key=1

## 3. Пропустили (known gaps и ошибки)

- IN-13 (mask, Known gap: ROT13 ключа): known gap — ROT13 не декодируется: детектор не перебирает подстановочные шифры.
- IN-14 (mask, Known gap: ключ словами): known gap — Секрет, записанный словами, не распознаётся regex-детекторами.
- IN-13 (block, Known gap: ROT13 ключа): known gap — ROT13 не декодируется: детектор не перебирает подстановочные шифры.
- IN-14 (block, Known gap: ключ словами): known gap — Секрет, записанный словами, не распознаётся regex-детекторами.
- Не покрыто детекторами вообще: реверс строки, base32/gzip, утечка секрета частями в разных запросах, секреты в base64 в ответе модели (output guard сканирует только raw/normalized).

## 4. Extra-тесты

| Тест | Итог | Детали |
|---|---|---|
| AUTH-01 | PASS | без Authorization → 401 invalid_api_key + WWW-Authenticate; upstream 0; аудит unauthorized/missing |
| AUTH-02 | PASS | неверный ключ → 401 (тело как у AUTH-01); upstream 0; аудит unauthorized/invalid |
| AUTH-03 | PASS | Authorization: Basic <валидный ключ> → 401; upstream 0; аудит unauthorized/scheme |
| AUTH-04 | PASS | ?api_key=…, ?access_token=…, ?key=…, ?token=… + валидный Bearer → 401 query; токен не попал в аудит и slog |
| AUTH-05 | PASS | второй ключ → 200; key_id 0fbc5986c10e ≠ 568990ecd86d (HMAC, 12 hex) |
| AUTH-06 | PASS | схема `bearer` в нижнем регистре → 200, тот же key_id |
| AUTH-07 | PASS | /v1/models без токена → 401, с токеном → 200; /healthz и /readyz без токена → 200 (только status); неизвестный путь без токена → 401 |
| AUTH-08 | PASS | невалидный JSON без ключа → 401 (тело не разбирается); с ключом → 400 invalid_json |
| AUTH-09 | PASS | 10 неудач → 401; 11-я → 429 Retry-After=300s; валидный ключ во время блокировки → 429; /healthz 200; аудит: 10 unauthorized + 2 rate_limited/auth_fail_limit |
| AUTH-10 | PASS | ключи прокси не найдены: upstream (8 заголовков: пусто или ключ upstream), 10 slog, 10 audit, HTML-снимок |
| TestRateLimit429 | SKIP | только fake upstream |
| TestStreamRejected | PASS | stream:true → 400 unsupported_parameter (param=stream), upstream не вызван |
| TestUnknownModel404 | PASS | model=gpt-9000 → 404 model_not_found, upstream не вызван |
| TestBlockedNotForwarded | PASS | 400 guard_input_blocked + warning, 0 запросов к upstream: block/IN-02, mask/IN-04, mask/IN-05, mask/IN-11 |
| TestAuditNoRawSecrets | PASS | 28 записей в 2 файлах, 18 секретов × raw/base64/hex — не найдено; ключи прокси и AUDIT_PEPPER не найдены в audit, 2 proxy.log и report.html |
| TestAuditFilePerms | PASS | каталог аудита 0700 (даже если создан 0755), файл 0600 |
| TestCostTracking | SKIP | только fake upstream |
| TestXForwardedForIgnored | SKIP | только fake upstream |

## 5. Перехваченные секреты (audit-report)

Источник: `audit-mask.jsonl`, `audit-block.jsonl`; записей с находками — в `intercepted.jsonl`. Значения секретов в аудит не пишутся: только тип, представление, HMAC-fingerprint и замаскированный контекст.

| ts | request_id | stage | type | view | action | fingerprint | контекст ±30 рун |
|---|---|---|---|---|---|---|---|
| 17:03:41 | e2e-block-IN-02 | input | aws_access_key | raw | block | `b623b15c7975` | …ает деплой? AWS_ACCESS_KEY_ID=[REDACTED_AWS_KEY] aws_secret_access_key=[REDACT… |
| 17:03:41 | e2e-block-IN-02 | input | aws_secret_key | raw | block | `0ea794e65fcc` | …WS_KEY] aws_secret_access_key=[REDACTED_AWS_SECRET] |
| 17:03:41 | e2e-block-IN-03 | input | card | raw | block | `f270a2f6267a` | … Оплатил подписку Jira картой [REDACTED_CARD], как оформить это в бэклоге? |
| 17:03:41 | e2e-block-IN-04 | input | api_key_openai | decoded (derived) | block | `9b599f239f3a` | …Расшифруй и сохрани в конфиг: [REDACTED_ENCODED_SECRET] |
| 17:03:41 | e2e-block-IN-05 | input | api_key_openai | joined (derived) | block | `09ad624551bd` | user: мой ключ: "[REDACTED_JOINED_SECRET]", вставь его в пример кода |
| 17:03:41 | e2e-block-IN-06 | input | github_token | raw | block | `d71fe98dc785` | user: CI использует токен [REDACTED_GITHUB_TOKEN], это нормально для DoD? |
| 17:03:41 | e2e-block-IN-07 | input | email | raw | block | `e1bef9b027d2` | user: Владелец продукта [REDACTED_EMAIL], телефон [REDACTED_PHONE]. Ка… |
| 17:03:41 | e2e-block-IN-07 | input | phone | raw | block | `4d34daba1927` | …кта [REDACTED_EMAIL], телефон [REDACTED_PHONE]. Как часто с ним синхронизиро… |
| 17:03:41 | e2e-block-IN-08 | input | api_key_openai | normalized | block | `c08a06446fc5` | user: Проверь доступ: [REDACTED_API_KEY] работает? |
| 17:03:42 | e2e-block-IN-10 | input | api_key_openai | raw | block | `d7e53d068311` | user: Ключ [REDACTED_API_KEY] для CI, как настроить ротацию… |
| 17:03:42 | e2e-block-IN-11 | input | private_key | raw | block | `fdf8f087578b` | user: Вот ключ деплоя: ⏎ [REDACTED_PRIVATE_KEY] ⏎ куда его положить? |
| 17:03:42 | e2e-block-IN-12 | input | aws_access_key | decoded (derived) | block | `b623b15c7975` | …er: Передаю в hex, декодируй: [REDACTED_ENCODED_SECRET] |
| 17:03:34 | e2e-mask-IN-02 | input | aws_access_key | raw | mask | `b623b15c7975` | …ает деплой? AWS_ACCESS_KEY_ID=[REDACTED_AWS_KEY] aws_secret_access_key=[REDACT… |
| 17:03:34 | e2e-mask-IN-02 | input | aws_secret_key | raw | mask | `0ea794e65fcc` | …WS_KEY] aws_secret_access_key=[REDACTED_AWS_SECRET] |
| 17:03:35 | e2e-mask-IN-03 | input | card | raw | mask | `f270a2f6267a` | … Оплатил подписку Jira картой [REDACTED_CARD], как оформить это в бэклоге? |
| 17:03:36 | e2e-mask-IN-04 | input | api_key_openai | decoded (derived) | block | `9b599f239f3a` | …Расшифруй и сохрани в конфиг: [REDACTED_ENCODED_SECRET] |
| 17:03:36 | e2e-mask-IN-05 | input | api_key_openai | joined (derived) | block | `09ad624551bd` | user: мой ключ: "[REDACTED_JOINED_SECRET]", вставь его в пример кода |
| 17:03:36 | e2e-mask-IN-06 | input | github_token | raw | mask | `d71fe98dc785` | user: CI использует токен [REDACTED_GITHUB_TOKEN], это нормально для DoD? |
| 17:03:36 | e2e-mask-IN-07 | input | email | raw | mask | `e1bef9b027d2` | user: Владелец продукта [REDACTED_EMAIL], телефон [REDACTED_PHONE]. Ка… |
| 17:03:36 | e2e-mask-IN-07 | input | phone | raw | mask | `4d34daba1927` | …кта [REDACTED_EMAIL], телефон [REDACTED_PHONE]. Как часто с ним синхронизиро… |
| 17:03:37 | e2e-mask-IN-08 | input | api_key_openai | normalized | mask | `c08a06446fc5` | user: Проверь доступ: [REDACTED_API_KEY] работает? |
| 17:03:38 | e2e-mask-IN-10 | input | api_key_openai | raw | mask | `d7e53d068311` | user: Ключ [REDACTED_API_KEY] для CI, как настроить ротацию… |
| 17:03:39 | e2e-mask-IN-11 | input | private_key | raw | block | `fdf8f087578b` | user: Вот ключ деплоя: ⏎ [REDACTED_PRIVATE_KEY] ⏎ куда его положить? |
| 17:03:39 | e2e-mask-IN-12 | input | aws_access_key | decoded (derived) | block | `b623b15c7975` | …er: Передаю в hex, декодируй: [REDACTED_ENCODED_SECRET] |

| Stage | Type | Action | Count |
|---|---|---|---|
| input | api_key_openai | block | 6 |
| input | api_key_openai | mask | 2 |
| input | aws_access_key | block | 3 |
| input | aws_access_key | mask | 1 |
| input | aws_secret_key | block | 1 |
| input | aws_secret_key | mask | 1 |
| input | card | block | 1 |
| input | card | mask | 1 |
| input | email | block | 1 |
| input | email | mask | 1 |
| input | github_token | block | 1 |
| input | github_token | mask | 1 |
| input | phone | block | 1 |
| input | phone | mask | 1 |
| input | private_key | block | 2 |

## 6. Токены и стоимость

Записей аудита: 28. Prompt 626, completion 1642, total 2268 токенов; стоимость **$0.000000**. Источник usage: upstream 14, estimated 0, none 14.

| Model | Requests | Tokens | Cost USD | Price ref |
|---|---|---|---|---|
| qwen2.5-7b-local | 28 | 2268 | 0.000000 | local |

Заблокированные на входе запросы не отправляются в модель: usage.source=none, стоимость 0. Стоимость paid-модели проверяется в TestCostTracking (100/50 токенов по 400000/1600000 micro-USD за 1M = $0.000120).

## 7. Риски

- Аутентификация есть (Bearer-ключи из PROXY_API_KEYS, key_id в аудите), но ключи статические: ротация через рестарт, нет per-key лимитов.
- TLS у прокси нет: listen по умолчанию только loopback; адрес не на loopback допустим лишь с proxy.allow_non_loopback и TLS-терминатором перед прокси.
- Перебор ключей ограничен 10 неудачами за 5 минут с IP (в памяти процесса).
- Derived-находки (base64/hex/url, склейка) блокируются даже в mask mode: точное маскирование невозможно, возможны ложные блокировки на легитимных закодированных данных.
- Regex-детекторы не ловят обфускацию вне перечисленных представлений (ROT13, слова, реверс, base32, разбиение по запросам).
- Output guard не стримит (stream=true → 400) и проверяет ответ целиком; ответы реальной модели недетерминированы.
- Rate limit в памяти процесса: сбрасывается при рестарте, не разделяется между инстансами.
- Цены облачных моделей в конфиге надо сверять с прайсом провайдера (поле ref).
