# E2E Scenario: Day3 — сайт-визитка, smoke
Платформы: Web
Сервер: `npm run preview` (http://localhost:4173), каталог `Day3/frontend/`
Ширины: 360 (mobile), 1280 (desktop). Готовность страницы: `html[data-app-mounted="true"]`.
Скриншоты: `swarm-report/day3-screenshots/<w>-s<N>-<step>.png`

## Шаг 0. Подготовка
- [x] 0.1 Инструменты Playwright MCP (`mcp__playwright__*`) доступны (решение пользователя: Playwright MCP вместо Chrome MCP). Иначе сообщить пользователю, молча не пропускать ✅ (mcp__playwright__* загружены после перезапуска)
- [x] 0.2 Unit + build: `npm run lint`, `npm run typecheck`, `npm test`, `npm run build` — без ошибок и предупреждений; в dist есть index.html, 404.html, projects/{task-tracker,weather-dashboard,markdown-notes}.html ✅ (чистый npm ci; lint/typecheck чисто; 137/137 unit в 6 файлах; build без warnings, 5 страниц)
- [x] 0.3 `npm run test:e2e` — зелёный ✅ (57 passed, 0 failed, 5 skipped по условию)
- [x] 0.4 `npm run preview` запущен, `/` отдаёт 200 ✅ (свежий build; / и /projects/task-tracker → 200)

## S1. Якорная навигация по 6 разделам
- [x] 1.1 [1280] Открыть `/` → видны шапка (`<nav>`) и h1 Hero ✅ (nav и h1 «Алексей Вымыслов», data-app-mounted=true; day3-screenshots/1280-s1-1.1-home.png)
- [x] 1.2 [1280] Клик по `nav-<id>` для hero, about, skills, projects, experience, contact → URL `#<id>`, заголовок `section-<id>` виден сразу под шапкой ✅ (URL #id верный для всех 6; шапка 52px; заголовок top=124 для about/skills/projects/experience; hero=181 (scrollY 0) и contact=166 (конец страницы, нельзя прокрутить выше) — вне окна <132 из-за границ страницы, не дефект; day3-screenshots/1280-s1-1.2-<id>.png)
- [x] 1.3 [360] Открыть `/` → кнопка «Открыть меню» видна, пункты меню скрыты ✅ (бургер виден, nav-about скрыт; day3-screenshots/360-s1-1.3-home.png)
- [x] 1.4 [360] Для каждого из 6 разделов: открыть меню → клик по пункту → URL `#<id>`, раздел под шапкой, меню закрылось ✅ (все 6: пункт виден после открытия, URL #id, заголовок top≈108 при шапке 52 (hero 150, это верх страницы), меню закрылось, aria-expanded=false; day3-screenshots/360-s1-1.4-<id>.png, 360-s1-1.4-menu-open.png)

## S2. Карточка проекта → страница → «Назад»
- [x] 2.1 [1280] На `/#projects` видны 3 карточки `project-card-*` в порядке task-tracker, weather-dashboard, markdown-notes ✅ (порядок task-tracker, weather-dashboard, markdown-notes; day3-screenshots/1280-s2-2.1-cards.png)
- [x] 2.2 [1280] Клик по `project-card-task-tracker` → URL `/projects/task-tracker`, `project-title` = заголовок проекта, описание из Markdown видно ✅ (URL /projects/task-tracker, project-title «Трекер задач для небольших команд», описание из Markdown видно; 1280-s2-2.2-project.png)
- [x] 2.3 [1280] Клик `back-link` → URL `/#projects`, раздел «Проекты» на экране ✅ (URL /#projects, заголовок «Проекты» top=124 при шапке 52; 1280-s2-2.3-back.png)
- [x] 2.4 [360] Повторить 2.2–2.3 на 360px ✅ (URL/title верны, back → /#projects, h2 top=108; 360-s2-2.4-cards/project/back.png)

## S3. Неизвестный slug → 404
- [x] 3.1 [1280] Открыть `/projects/unknown-slug` → `not-found` виден, h1 «Страница не найдена», кнопка «На главную» ✅ (not-found виден, h1 «Страница не найдена», кнопка «На главную»; 1280-s3-3.1-404.png)
- [x] 3.2 [1280] Клик «На главную» → `/` ✅ (URL /, h1 Hero; 1280-s3-3.2-home.png)
- [x] 3.3 [360] Открыть `/foo/bar` → `not-found`, без горизонтального скролла ✅ (/foo/bar → not-found, scrollWidth 360 = innerWidth; 360-s3-3.3-404.png)

## S4. Форма обратной связи
- [x] 4.1 [1280] Перейти к `#contact`, `demo-note` «Это демо: сообщение никуда не отправляется» виден ✅ (demo-note «Это демо: сообщение никуда не отправляется» в viewport; 1280-s4-4.1-contact.png)
- [x] 4.2 [1280] Клик `contact-submit` с пустыми полями → видны `field-error-name`, `field-error-email`, `field-error-message` ✅ («Укажите имя», «Укажите email», «Напишите сообщение»; status idle; 1280-s4-4.2-errors.png)
- [x] 4.3 [1280] Ввести имя «Тест», email «test@example.com», сообщение «Проверка формы обратной связи» → ошибки исчезли ✅ (все 3 ошибки исчезли; 1280-s4-4.3-filled.png)
- [x] 4.4 [1280] Очистить лог сети → клик `contact-submit` → `form-status` data-status=sending, затем success ✅ (sending пойман сразу после клика, затем success «Спасибо! Сообщение «отправлено».»; 1280-s4-4.4-sending.png, -success.png; лог сети: baseline 4 запроса — doc/js/css/svg)
- [x] 4.5 [1280] Сетевых запросов после клика нет; localStorage/sessionStorage пусты, URL не изменился ✅ (после submit page.on(request) = 0, browser_network_requests тот же список из 4, localStorage=0, sessionStorage=0, cookie пуст, URL /#contact без изменений)
- [x] 4.6 [360] Повторить 4.2–4.5 на 360px ✅ (то же на 360: ошибки, исчезновение, sending→success, 0 запросов, storage/cookie пусты; 360-s4-4.6-*.png)

## S5. Мобильная вёрстка 360px
- [x] 5.1 [360] `/`: `document.documentElement.scrollWidth <= innerWidth` ✅ (scrollWidth 345 ≤ innerWidth 360; 360-s5-5.1.png)
- [x] 5.2 [360] `/projects/task-tracker`: то же ✅ (345 ≤ 360; 360-s5-5.2.png)
- [x] 5.3 [360] `/404` (или неизвестный путь): то же ✅ (/404: 360 ≤ 360, not-found виден; 360-s5-5.3.png)
- [x] 5.4 [360] Бургер открывается и закрывается, тап-цели меню ≥ 44×44 ✅ (бургер 44×44, 6 пунктов 303×44, закрытие работает; замечание: aria-label остаётся «Открыть меню» при открытом меню; 360-s5-5.4-open/closed.png)
- [x] 5.5 [360] Подвал: «Демонстрационный сайт, персона вымышлена» виден ✅ (подвал «Демонстрационный сайт, персона вымышлена» виден; 360-s5-5.5-footer.png)

## Итог
| Сценарий | 1280 | 360 | Проблема / вероятное место |
|---|---|---|---|
| S1 | ✅ | ✅ | Замечаний нет (hero/contact на 1280 не ниже шапки из-за границ страницы) |
| S2 | ✅ | ✅ | — |
| S3 | ✅ | ✅ | — |
| S4 | ✅ | ✅ | — |
| S5 | — | ✅ | Минор: aria-label бургера статичный «Открыть меню» (components/ui/AppHeader.vue:54) |
