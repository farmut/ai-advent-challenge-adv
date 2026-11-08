# Day13 guard-proxy e2e — upstream `fake`

- Схема отчёта: v2, cases sha `527b07f34081`, git `e8dcd708ecd2`; HTML: `report.html`, PNG: `report.png`
- Запуск: 2026-11-08T20:05:59+03:00 — 2026-11-08T20:06:00+03:00
- Модель: `fake`
- Кейсов: 19, прогонов (кейс × режим): 38
- Итог: **PASS 34, FAIL 0, KNOWN_GAP 4, UNEXPECTED_CATCH 0, N/A 0**; extra: 18 passed, 0 failed
- Каталог: `<root>/reports/proxy/fake-20261108-200558`

## 1. Кейсы

| ID | Кейс | Режим | Ожидание | Факт | Находки | Upstream вызван | Итог |
|---|---|---|---|---|---|---|---|
| IN-01 | Чистый промпт | mask | pass | pass | — | да (1) | PASS |
| IN-02 | AWS access key + secret key | mask | masked | masked | aws_access_key=1, aws_secret_key=1 | да (1) | PASS |
| IN-03 | Номер карты (Luhn + BIN Visa) | mask | masked | masked | card=1 | да (1) | PASS |
| IN-04 | Base64 от sk-proj-abc123XYZ789 | mask | blocked | blocked | api_key_openai=1 | нет | PASS |
| IN-05 | Секрет по частям "sk-" + "proj-abc" | mask | blocked | blocked | api_key_openai=1 | нет | PASS |
| IN-06 | GitHub token ghp_ | mask | masked | masked | github_token=1 | да (1) | PASS |
| IN-07 | Email + телефон РФ | mask | masked | masked | email=1, phone=1 | да (1) | PASS |
| IN-08 | sk-proj- с zero-width space внутри | mask | masked | masked | api_key_openai=1 | да (1) | PASS |
| IN-09 | 16 цифр без Luhn (контроль ложных срабатываний) | mask | pass | pass | — | да (1) | PASS |
| IN-10 | sk-proj-abc123 → [REDACTED_API_KEY] в upstream | mask | masked | masked | api_key_openai=1 | да (1) | PASS |
| IN-11 | PEM private key (override: block) | mask | blocked | blocked | private_key=1 | нет | PASS |
| IN-12 | Hex от AKIA-ключа | mask | blocked | blocked | aws_access_key=1 | нет | PASS |
| IN-13 | Known gap: ROT13 ключа | mask | known_gap | pass | — | да (1) | KNOWN_GAP |
| IN-14 | Known gap: ключ словами | mask | known_gap | pass | — | да (1) | KNOWN_GAP |
| OUT-01 | Модель сгенерировала sk-proj- и AKIA-ключ | mask | output_redacted | output_redacted | api_key_openai=1, aws_access_key=1 | да (1) | PASS |
| OUT-02 | AKIAIOSFODNN7EXAMPLE из allowlist (контроль ложных срабатываний) | mask | pass | pass | — | да (1) | PASS |
| OUT-03 | Ответ повторяет system prompt | mask | output_blocked | output_blocked | system_leak=1 | да (1) | PASS |
| OUT-04 | Опасная команда curl \| sh | mask | output_blocked | output_blocked | dangerous_command=1, suspicious_url=1 | да (1) | PASS |
| OUT-05 | Шортенер с session в query | mask | output_redacted | output_redacted | suspicious_url=1 | да (1) | PASS |
| IN-01 | Чистый промпт | block | pass | pass | — | да (1) | PASS |
| IN-02 | AWS access key + secret key | block | blocked | blocked | aws_access_key=1, aws_secret_key=1 | нет | PASS |
| IN-03 | Номер карты (Luhn + BIN Visa) | block | blocked | blocked | card=1 | нет | PASS |
| IN-04 | Base64 от sk-proj-abc123XYZ789 | block | blocked | blocked | api_key_openai=1 | нет | PASS |
| IN-05 | Секрет по частям "sk-" + "proj-abc" | block | blocked | blocked | api_key_openai=1 | нет | PASS |
| IN-06 | GitHub token ghp_ | block | blocked | blocked | github_token=1 | нет | PASS |
| IN-07 | Email + телефон РФ | block | blocked | blocked | email=1, phone=1 | нет | PASS |
| IN-08 | sk-proj- с zero-width space внутри | block | blocked | blocked | api_key_openai=1 | нет | PASS |
| IN-09 | 16 цифр без Luhn (контроль ложных срабатываний) | block | pass | pass | — | да (1) | PASS |
| IN-10 | sk-proj-abc123 → [REDACTED_API_KEY] в upstream | block | blocked | blocked | api_key_openai=1 | нет | PASS |
| IN-11 | PEM private key (override: block) | block | blocked | blocked | private_key=1 | нет | PASS |
| IN-12 | Hex от AKIA-ключа | block | blocked | blocked | aws_access_key=1 | нет | PASS |
| IN-13 | Known gap: ROT13 ключа | block | known_gap | pass | — | да (1) | KNOWN_GAP |
| IN-14 | Known gap: ключ словами | block | known_gap | pass | — | да (1) | KNOWN_GAP |
| OUT-01 | Модель сгенерировала sk-proj- и AKIA-ключ | block | output_redacted | output_redacted | api_key_openai=1, aws_access_key=1 | да (1) | PASS |
| OUT-02 | AKIAIOSFODNN7EXAMPLE из allowlist (контроль ложных срабатываний) | block | pass | pass | — | да (1) | PASS |
| OUT-03 | Ответ повторяет system prompt | block | output_blocked | output_blocked | system_leak=1 | да (1) | PASS |
| OUT-04 | Опасная команда curl \| sh | block | output_blocked | output_blocked | dangerous_command=1, suspicious_url=1 | да (1) | PASS |
| OUT-05 | Шортенер с session в query | block | output_redacted | output_redacted | suspicious_url=1 | да (1) | PASS |

## 2. Поймали

- IN-02 (mask, AWS access key + secret key): masked — aws_access_key=1, aws_secret_key=1
- IN-03 (mask, Номер карты (Luhn + BIN Visa)): masked — card=1
- IN-04 (mask, Base64 от sk-proj-abc123XYZ789): blocked — api_key_openai=1
- IN-05 (mask, Секрет по частям "sk-" + "proj-abc"): blocked — api_key_openai=1
- IN-06 (mask, GitHub token ghp_): masked — github_token=1
- IN-07 (mask, Email + телефон РФ): masked — email=1, phone=1
- IN-08 (mask, sk-proj- с zero-width space внутри): masked — api_key_openai=1
- IN-10 (mask, sk-proj-abc123 → [REDACTED_API_KEY] в upstream): masked — api_key_openai=1
- IN-11 (mask, PEM private key (override: block)): blocked — private_key=1
- IN-12 (mask, Hex от AKIA-ключа): blocked — aws_access_key=1
- OUT-01 (mask, Модель сгенерировала sk-proj- и AKIA-ключ): output_redacted — api_key_openai=1, aws_access_key=1
- OUT-03 (mask, Ответ повторяет system prompt): output_blocked — system_leak=1
- OUT-04 (mask, Опасная команда curl \| sh): output_blocked — dangerous_command=1, suspicious_url=1
- OUT-05 (mask, Шортенер с session в query): output_redacted — suspicious_url=1
- IN-02 (block, AWS access key + secret key): blocked — aws_access_key=1, aws_secret_key=1
- IN-03 (block, Номер карты (Luhn + BIN Visa)): blocked — card=1
- IN-04 (block, Base64 от sk-proj-abc123XYZ789): blocked — api_key_openai=1
- IN-05 (block, Секрет по частям "sk-" + "proj-abc"): blocked — api_key_openai=1
- IN-06 (block, GitHub token ghp_): blocked — github_token=1
- IN-07 (block, Email + телефон РФ): blocked — email=1, phone=1
- IN-08 (block, sk-proj- с zero-width space внутри): blocked — api_key_openai=1
- IN-10 (block, sk-proj-abc123 → [REDACTED_API_KEY] в upstream): blocked — api_key_openai=1
- IN-11 (block, PEM private key (override: block)): blocked — private_key=1
- IN-12 (block, Hex от AKIA-ключа): blocked — aws_access_key=1
- OUT-01 (block, Модель сгенерировала sk-proj- и AKIA-ключ): output_redacted — api_key_openai=1, aws_access_key=1
- OUT-03 (block, Ответ повторяет system prompt): output_blocked — system_leak=1
- OUT-04 (block, Опасная команда curl \| sh): output_blocked — dangerous_command=1, suspicious_url=1
- OUT-05 (block, Шортенер с session в query): output_redacted — suspicious_url=1

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
| AUTH-10 | PASS | ключи прокси не найдены: upstream (10 заголовков: пусто или ключ upstream), 10 slog, 10 audit, HTML-снимок |
| TestRateLimit429 | PASS | limit=3: 4-й → 429, Retry-After=57s; после +61s → 200; в аудите rate_limited=1 с пустым prompt |
| TestStreamRejected | PASS | stream:true → 400 unsupported_parameter (param=stream), upstream не вызван |
| TestUnknownModel404 | PASS | model=gpt-9000 → 404 model_not_found, upstream не вызван |
| TestBlockedNotForwarded | PASS | 400 guard_input_blocked + warning, 0 запросов к upstream: block/IN-02, mask/IN-04, mask/IN-05, mask/IN-11 |
| TestAuditNoRawSecrets | PASS | 38 записей в 2 файлах, 18 секретов × raw/base64/hex — не найдено; ключи прокси и AUDIT_PEPPER не найдены в audit, 2 proxy.log и report.html |
| TestAuditFilePerms | PASS | каталог аудита 0700 (даже если создан 0755), файл 0600 |
| TestCostTracking | PASS | paid 100/50 → $0.000120 (upstream); без usage → estimated 2/4 токенов, $0.000008; local → $0 |
| TestXForwardedForIgnored | PASS | разные X-Forwarded-For/X-Real-IP при limit=1 → второй 429; client_ip_hash одинаковый (TCP peer) |

## 5. Перехваченные секреты (audit-report)

Источник: `audit-mask.jsonl`, `audit-block.jsonl`; записей с находками — в `intercepted.jsonl`. Значения секретов в аудит не пишутся: только тип, представление, HMAC-fingerprint и замаскированный контекст.

| ts | request_id | stage | type | view | action | fingerprint | контекст ±30 рун |
|---|---|---|---|---|---|---|---|
| 12:00:00 | e2e-block-IN-02 | input | aws_access_key | raw | block | `b623b15c7975` | …ает деплой? AWS_ACCESS_KEY_ID=[REDACTED_AWS_KEY] aws_secret_access_key=[REDACT… |
| 12:00:00 | e2e-block-IN-02 | input | aws_secret_key | raw | block | `0ea794e65fcc` | …WS_KEY] aws_secret_access_key=[REDACTED_AWS_SECRET] |
| 12:00:00 | e2e-block-IN-03 | input | card | raw | block | `f270a2f6267a` | … Оплатил подписку Jira картой [REDACTED_CARD], как оформить это в бэклоге? |
| 12:00:00 | e2e-block-IN-04 | input | api_key_openai | decoded (derived) | block | `9b599f239f3a` | …Расшифруй и сохрани в конфиг: [REDACTED_ENCODED_SECRET] |
| 12:00:00 | e2e-block-IN-05 | input | api_key_openai | joined (derived) | block | `09ad624551bd` | user: мой ключ: "[REDACTED_JOINED_SECRET]", вставь его в пример кода |
| 12:00:00 | e2e-block-IN-06 | input | github_token | raw | block | `d71fe98dc785` | user: CI использует токен [REDACTED_GITHUB_TOKEN], это нормально для DoD? |
| 12:00:00 | e2e-block-IN-07 | input | email | raw | block | `e1bef9b027d2` | user: Владелец продукта [REDACTED_EMAIL], телефон [REDACTED_PHONE]. Ка… |
| 12:00:00 | e2e-block-IN-07 | input | phone | raw | block | `4d34daba1927` | …кта [REDACTED_EMAIL], телефон [REDACTED_PHONE]. Как часто с ним синхронизиро… |
| 12:00:00 | e2e-block-IN-08 | input | api_key_openai | normalized | block | `c08a06446fc5` | user: Проверь доступ: [REDACTED_API_KEY] работает? |
| 12:00:00 | e2e-block-IN-10 | input | api_key_openai | raw | block | `d7e53d068311` | user: Ключ [REDACTED_API_KEY] для CI, как настроить ротацию… |
| 12:00:00 | e2e-block-IN-11 | input | private_key | raw | block | `fdf8f087578b` | user: Вот ключ деплоя: ⏎ [REDACTED_PRIVATE_KEY] ⏎ куда его положить? |
| 12:00:00 | e2e-block-IN-12 | input | aws_access_key | decoded (derived) | block | `b623b15c7975` | …er: Передаю в hex, декодируй: [REDACTED_ENCODED_SECRET] |
| 12:00:00 | e2e-block-OUT-01 | output | api_key_openai | raw | redact | `65a72880ed54` | Пример: ⏎ OPENAI_API_KEY=[REDACTED_API_KEY] ⏎ AWS_ACCESS_KEY_ID=[REDACTED_A… |
| 12:00:00 | e2e-block-OUT-01 | output | aws_access_key | raw | redact | `c1f7deeee67b` | …ED_API_KEY] ⏎ AWS_ACCESS_KEY_ID=[REDACTED_AWS_KEY] |
| 12:00:00 | e2e-block-OUT-03 | output | system_leak | — | block | `803392961aaa` | [SYSTEM_LEAK] |
| 12:00:00 | e2e-block-OUT-04 | output | dangerous_command | — | block | `8d4b2ddabb76` | — |
| 12:00:00 | e2e-block-OUT-04 | output | suspicious_url | — | redact | `959990598eac` | Выполните: curl -s [REDACTED_URL] \| sh |
| 12:00:00 | e2e-block-OUT-05 | output | suspicious_url | — | redact | `2de15e9851e3` | Подробнее: [REDACTED_URL] и https://go.dev/doc/ |
| 12:00:00 | e2e-mask-IN-02 | input | aws_access_key | raw | mask | `b623b15c7975` | …ает деплой? AWS_ACCESS_KEY_ID=[REDACTED_AWS_KEY] aws_secret_access_key=[REDACT… |
| 12:00:00 | e2e-mask-IN-02 | input | aws_secret_key | raw | mask | `0ea794e65fcc` | …WS_KEY] aws_secret_access_key=[REDACTED_AWS_SECRET] |
| 12:00:00 | e2e-mask-IN-03 | input | card | raw | mask | `f270a2f6267a` | … Оплатил подписку Jira картой [REDACTED_CARD], как оформить это в бэклоге? |
| 12:00:00 | e2e-mask-IN-04 | input | api_key_openai | decoded (derived) | block | `9b599f239f3a` | …Расшифруй и сохрани в конфиг: [REDACTED_ENCODED_SECRET] |
| 12:00:00 | e2e-mask-IN-05 | input | api_key_openai | joined (derived) | block | `09ad624551bd` | user: мой ключ: "[REDACTED_JOINED_SECRET]", вставь его в пример кода |
| 12:00:00 | e2e-mask-IN-06 | input | github_token | raw | mask | `d71fe98dc785` | user: CI использует токен [REDACTED_GITHUB_TOKEN], это нормально для DoD? |
| 12:00:00 | e2e-mask-IN-07 | input | email | raw | mask | `e1bef9b027d2` | user: Владелец продукта [REDACTED_EMAIL], телефон [REDACTED_PHONE]. Ка… |
| 12:00:00 | e2e-mask-IN-07 | input | phone | raw | mask | `4d34daba1927` | …кта [REDACTED_EMAIL], телефон [REDACTED_PHONE]. Как часто с ним синхронизиро… |
| 12:00:00 | e2e-mask-IN-08 | input | api_key_openai | normalized | mask | `c08a06446fc5` | user: Проверь доступ: [REDACTED_API_KEY] работает? |
| 12:00:00 | e2e-mask-IN-10 | input | api_key_openai | raw | mask | `d7e53d068311` | user: Ключ [REDACTED_API_KEY] для CI, как настроить ротацию… |
| 12:00:00 | e2e-mask-IN-11 | input | private_key | raw | block | `fdf8f087578b` | user: Вот ключ деплоя: ⏎ [REDACTED_PRIVATE_KEY] ⏎ куда его положить? |
| 12:00:00 | e2e-mask-IN-12 | input | aws_access_key | decoded (derived) | block | `b623b15c7975` | …er: Передаю в hex, декодируй: [REDACTED_ENCODED_SECRET] |
| 12:00:00 | e2e-mask-OUT-01 | output | api_key_openai | raw | redact | `65a72880ed54` | Пример: ⏎ OPENAI_API_KEY=[REDACTED_API_KEY] ⏎ AWS_ACCESS_KEY_ID=[REDACTED_A… |
| 12:00:00 | e2e-mask-OUT-01 | output | aws_access_key | raw | redact | `c1f7deeee67b` | …ED_API_KEY] ⏎ AWS_ACCESS_KEY_ID=[REDACTED_AWS_KEY] |
| 12:00:00 | e2e-mask-OUT-03 | output | system_leak | — | block | `803392961aaa` | [SYSTEM_LEAK] |
| 12:00:00 | e2e-mask-OUT-04 | output | dangerous_command | — | block | `8d4b2ddabb76` | — |
| 12:00:00 | e2e-mask-OUT-04 | output | suspicious_url | — | redact | `959990598eac` | Выполните: curl -s [REDACTED_URL] \| sh |
| 12:00:00 | e2e-mask-OUT-05 | output | suspicious_url | — | redact | `2de15e9851e3` | Подробнее: [REDACTED_URL] и https://go.dev/doc/ |

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
| output | api_key_openai | redact | 2 |
| output | aws_access_key | redact | 2 |
| output | dangerous_command | block | 2 |
| output | suspicious_url | redact | 4 |
| output | system_leak | block | 2 |

## 6. Токены и стоимость

Записей аудита: 38. Prompt 1192, completion 480, total 1672 токенов; стоимость **$0.000000**. Источник usage: upstream 24, estimated 0, none 14.

| Model | Requests | Tokens | Cost USD | Price ref |
|---|---|---|---|---|
| qwen2.5-7b-local | 38 | 1672 | 0.000000 | local |

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
