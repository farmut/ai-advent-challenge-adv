# Отчёт: Day3 — сайт-визитка и два уровня тестов
Дата: 2026-10-25. Профиль: Бизнес-фича. Каталог: `Day3/frontend/`.

## 1. Задача
Построить статический сайт-визитку вымышленного разработчика по `Day3/AGENTS.md` (Vue 3 + TypeScript + Vite + vite-ssg + PrimeVue 4) и подготовить два уровня тестирования:
- **Уровень 1:** unit/integration-тесты на 3+ модуля.
- **Уровень 2:** 3–5 smoke-сценариев, которые агент прогоняет сам через браузер, со скриншотом на каждом шаге и отчётом; при падении указать место проблемы.

## 2. Итоги Research (консилиум)
**Архитектор**
- Порядок слоёв: каркас → `content/types.ts` → `build/content-core.ts` (чистые функции) → `build/content-plugin.ts` (тонкий хук) → контент `.md` и `content/index.ts` → роутинг и `main.ts` → `useContactForm` → UI → секции → страницы → `App.vue` → тесты.
- Frontmatter валидируется вручную (без zod, чтобы не добавлять зависимость): ошибка сообщает имя файла и поле.
- `includedRoutes` строится чистой функцией из списка проектов, ручной список путей запрещён.
- Vite 8 (rolldown) и `@unhead/vue` должны быть в одном экземпляре; PrimeVue в styled mode при SSR может дать FOUC.

**Фронтенд-эксперт (vue-expert)**
- `useContactForm({ delayMs, simulate })`: состояния `idle → sending → success/error`, `error` достижим через параметр для тестов; повторный submit во время `sending` игнорируется.
- Неизвестный slug должен давать 404 на клиенте, не пустую страницу.
- Якорная навигация учитывает высоту липкой шапки (`scrollBehavior`).
- Селекторы для тестов: роли и `data-testid`.

**UI-дизайнер**
- Шапка: PrimeVue Menubar, breakpoint 640; пункты меню не ниже 44px.
- Primary-цвет indigo-600 (emerald-500 даёт ~2.5:1, не проходит AA).
- Форма: метки над полями, ошибка под полем, `aria-invalid`/`aria-describedby`, фокус на первое невалидное поле, `role=status` с `aria-live`, подпись «Это демо» всегда видна.
- A11y: skip-link, один `h1`, `focus-visible`, `prefers-reduced-motion`.

**Безопасность**
- Markdown собирается только на этапе сборки: `markdown-it` с `html: false`, `sanitize-html` с белым списком тегов и жёстким фильтром `href` (https, mailto, `#`).
- Запрет `---js` во frontmatter (gray-matter выполняет `eval`); h1 и картинки в теле `.md` дают ошибку сборки.
- ESLint: запреты `fetch`, XHR, `sendBeacon`, storage, cookie; `v-html` только в `MarkdownContent.vue`.
- CSP meta только в build (`connect-src 'none'`, `form-action 'none'`).

**Решения пользователя**
- Шапка: PrimeVue Menubar.
- CSP meta: да, только при build.
- h1 и картинки в `.md` — ошибка сборки; h5/h6 — понижаются до h4.
- Smoke: сначала Chrome MCP. Инструментов `mcp__claude-in-chrome__*` в сессии не оказалось, поэтому по решению пользователя smoke выполнен через Playwright MCP. `Day3/CLAUDE.md` обновлён.
- PrimeVue 5.0.4 → 4.5.5 (см. раздел 8).

## 3. План (Э1–Э7)
- **Э1 Каркас:** package.json, npm ci, eslint, tsconfig, vite, playwright.
- **Э2 Контент-ядро:** типы, `parseContentFile`, валидаторы, рендер Markdown с санитизацией, content-plugin.
- **Э3 Контент:** профиль, 3 проекта, 3 места работы, `content/index.ts` (сортировка, поиск по slug).
- **Э4 Роутинг и SSG:** routes, scrollBehavior, includedRoutes, main.ts, тема PrimeVue, CSP meta.
- **Э5 Форма:** `useContactForm`.
- **Э6 UI:** tokens.css, компоненты ui/, 6 секций, 3 страницы, App.vue.
- **Э7 E2E:** home, project, prerender, not-found, contact, layout.

## 4. Что реализовано (`Day3/frontend/`)
- **Конфигурация:** `package.json`, `package-lock.json`, `.npmrc` (engine-strict), `.nvmrc` (24), `tsconfig.json`, `vite.config.ts`, `playwright.config.ts`, `eslint.config.js`, `index.html`.
- **Сборка контента:** `build/content-core.ts` (parseContentFile, validate*, renderMarkdown, sanitizeContentHtml, assertSafeUrl, ContentError), `build/content-plugin.ts` (load-хук, проверка `generateBundle`, CSP-плагин).
- **Точка входа и роутинг:** `src/main.ts`, `src/App.vue`, `src/router/{routes,index,includedRoutes,scrollBehavior}.ts`.
- **Тема:** `src/theme/{preset,primevue}.ts`.
- **Контент:** `src/content/{types.ts,index.ts,profile.md}`, `projects/*.md` (3), `experience/*.md` (3).
- **Логика:** `src/composables/{useContactForm,formatPeriod}.ts`.
- **Страницы:** `HomePage`, `ProjectPage`, `NotFoundPage`.
- **Секции:** 6 компонентов в `components/sections/`.
- **UI-компоненты:** `AppHeader`, `AppFooter`, `PageSection`, `ProjectCard`, `ExperienceEntry`, `MarkdownContent`.
- **Стили и ассеты:** `styles/tokens.css`, `public/favicon.svg`, `public/images/task-tracker.svg`.
- **Тесты:** `tests/unit/` (6 spec-файлов), `tests/e2e/` (6 spec-файлов и `helpers.ts`).

## 5. Уровень 1: unit/integration

| Spec-файл | Модуль | Тестов |
|---|---|---|
| `content-core.spec.ts` | `build/content-core.ts` | 50 |
| `useContactForm.spec.ts` | `composables/useContactForm.ts` | 39 |
| `content-index.spec.ts` | `content/index.ts` | 15 |
| `sections.spec.ts` | `components/sections/*` | 15 |
| `router.spec.ts` | `router/routes.ts` | 12 |
| `included-routes.spec.ts` | `router/includedRoutes.ts` | 6 |
| **Итого** | 6 файлов, 6 модулей | **137** |

**Первый прогон честно:** 134 из 135 тестов. Упал `router.spec.ts`, тест «реальные маршруты: каждый проект открывается, чужой slug — 404» (`expected 'project' to be 'not-found'`).
- **Место проблемы:** код, не тест. `beforeEnter` в vue-router не вызывается при смене параметра того же маршрута (`/projects/a` → `/projects/unknown`), поэтому клиент показал бы пустую страницу проекта.
- **Исправление в коде:** `createRoutes(slugs)` строит путь `/projects/:slug(a|b|…)`, и неизвестный slug падает в catch-all при любой навигации.
- **Тест не ослаблен:** менялся только вызов фабрики, assertions прежние. Добавлены 2 теста: регрессия смены параметра и пустой список проектов.
- **Итог:** 137 из 137.

## 6. Уровень 2: smoke через Playwright MCP
Сценарий: `day3-site-tests-e2e-scenario.md` (все шаги `[x]`). Отчёт: `day3-smoke-report-2026-10-25.md`. Скриншотов: 39 в `day3-screenshots/`.

| Сценарий | 1280×800 | 360×740 | Замечание |
|---|---|---|---|
| S1 Якорная навигация по 6 разделам | OK | OK | hero и contact на 1280 не ниже шапки из-за границ страницы |
| S2 Карточка → `/projects/<slug>` → «Назад» | OK | OK | |
| S3 Неизвестный slug → 404 | OK | OK | |
| S4 Форма: ошибки, успех, 0 сетевых запросов | OK | OK | состояние `sending` поймано |
| S5 Мобильная вёрстка, без горизонтального скролла | — | OK | scrollWidth 345 ≤ 360 на `/` и `/projects/task-tracker` |

Ключевые скриншоты:

![S1: список проектов на 1280](day3-screenshots/1280-s1-1.2-projects.png)
![S1: меню открыто на 360](day3-screenshots/360-s1-1.4-menu-open.png)
![S2: страница проекта на 1280](day3-screenshots/1280-s2-2.2-project.png)
![S4: ошибки пустой отправки на 1280](day3-screenshots/1280-s4-4.2-errors.png)
![S5: 360 без горизонтального скролла](day3-screenshots/360-s5-5.1.png)

Консоль браузера за сессию: 0 ошибок и 0 предупреждений.

**Найдено и исправлено по итогам smoke:**
- **a11y, бургер:** подпись `Открыть меню` оставалась при открытом меню. Заменена на статичную «Меню»; состояние передаёт `aria-expanded`. Селекторы в E2E обновлены, добавлены проверки `aria-expanded`.
- **Скриншоты успешной отправки:** первые кадры сняты во время анимации появления сообщения. Оркестратор переснял их через секунду после `success`. Дефекта кода нет.

## 7. Validation (финальная проверка оркестратора)
- `npm run lint`: exit 0, 0 ошибок, 0 предупреждений.
- `npm run typecheck`: exit 0.
- `npm run build`: exit 0, 0 предупреждений. В `dist/assets/*.js` нет `markdown-it`, `gray-matter`, `sanitize-html`.
- `npm test`: 137 из 137.
- `npm run test:e2e`: 57 passed, 5 skipped (штатно), 0 failed. После исправления бургера — 2 зелёных прогона подряд у оркестратора (ниже); у исполнителя 3 прогона из 4 упали по окружению (см. раздел 8).
- `dist/`: `index.html`, `404.html`, `projects/{task-tracker,weather-dashboard,markdown-notes}.html`.

**Стабильность E2E:** после исправления бургера оркестратор сам прогнал `npx playwright test` дважды подряд (Mac на батарее, браузер Playwright MCP закрыт). Оба прогона: 57 passed, 5 skipped, 0 failed, примерно 6 с. Поиск «Открыть меню» по `src` и `tests` пуст. Затем lint, typecheck и build: exit 0, предупреждений 0. Unit: 137/137. dist: index.html, 404.html, projects/{task-tracker,weather-dashboard,markdown-notes}.html. Падения исполнителя (прогоны 1, 3, 4) случились при параллельно открытом Chrome от MCP и во время maintenance sleep. Это окружение, а не код.

## 8. Проблемы и откаты
- **PrimeVue 5.0.4 → 4.5.5.** 5.x распространяется по PrimeUI License и требует ключ. Без ключа `npm run build` выводил 6 предупреждений `[PrimeUI] PrimeUI license is not configured`, а в браузере был баннер «Invalid PrimeUI License». Переведено на 4.5.5 и `@primeuix/themes` 2.0.3 (MIT). Код приложения не менялся. Добавлен E2E-тест на отсутствие лицензионного баннера. `Day3/AGENTS.md` и `Day3/CLAUDE.md` обновлены.
- **Chrome MCP не подключился.** Smoke выполнен через Playwright MCP по решению пользователя. `Day3/CLAUDE.md` обновлён.
- **context7 недоступен исполнителю.** API сверено по `node_modules` и `.d.ts`. Итоги сверки есть в `day3-site-tests-executing.md`.
- **Баг в коде, найденный E2E и скриншотами:** прямой заход на `/#experience` прокручивал на ~272px ниже нужного. Исправлено повторной `scrollToHash` в `App.vue onMounted`. Тест ужесточён; без фикса он падает.
- **Бургер прилипал к логотипу на mobile.** Исправлено через `justify-content: space-between`.
- **Первый E2E-прогон:** 52 passed, 3 failed, 5 skipped. Причины: тап-цель бургера замерялась во время анимации (замер заменён на `expect.poll`, порог 44px тот же); клик по `<p>`, перекрытому `::after` растянутой ссылки (клик перенесён на карточку). Оба падения проверены, код не менялся ради зелёного прогона.
- **Smoke-агент обновлял файл сценария пакетами шагов,** а не после каждого шага. Статусы в файле проставлены, но не по одному шагу за раз.
- **E2E флейкали из-за сна Mac на батарее.** Прогоны 1, 3 и 4 упали, `pmset` фиксирует maintenance sleep в те же окна. Прогон 2 (57 passed) прошёл. `caffeinate` от батарейного maintenance sleep не спасает.

## 9. Известные ограничения
- **FOUC:** PrimeVue в styled mode не отдаёт стили при SSR. До загрузки JS компоненты PrimeVue без темы. Исправление: собирать стили через `onPageRendered`, это заметная доработка.
- **`vite preview`** отдаёт 200 и `index.html` на неизвестный URL. 404 рисует клиент. `dist/404.html` проверяется отдельно. На хостинге нужна отдача 404 со статусом 404.
- **Анимация мобильного меню** PrimeVue (`@primeuix/motion`) не учитывает `prefers-reduced-motion`.
- **`Day3/` не закоммитан:** в git статус `??` (untracked).

## 10. Статус
**Done.**
