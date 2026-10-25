# Research: Day3 — сайт-визитка + 2 уровня тестов
Дата: 2026-10-25. Профиль: Бизнес-фича. Консилиум: Архитектор, Фронтенд-эксперт (vue-expert), UI-дизайнер, Безопасность.

## Запрос
Построить сайт по Day3/AGENTS.md, затем:
- Уровень 1: unit/integration на 3+ модуля, проходят с первого запуска.
- Уровень 2: 5 smoke-сценариев через Chrome MCP (выбор пользователя вместо Playwright MCP), скриншот на шаг, отчёт, локализация проблем.
  1. Якорная навигация по 6 разделам. 2. Карточка → /projects/<slug> → «Назад». 3. Неизвестный slug → 404.
  4. Форма: ошибки на пустой отправке, успех без сетевых запросов. 5. 360px без горизонтального скролла.
- Плюс Playwright E2E из AGENTS.md.

## Архитектура
- Порядок: каркас (package.json, 3 tsconfig: app/node/vitest, eslint, vite) → content/types.ts → build/content-core.ts (чистые функции, без vite) → build/content-plugin.ts (тонкий хук) → контент .md + content/index.ts (чистые sort/find + glob-обвязка) → router/routes.ts + main.ts → composables/useContactForm.ts → ui → sections → pages → App.vue → тесты.
- parseContentFile(kind, fileName, raw): gray-matter → ручные валидаторы (ошибка «файл: поле») → markdown-it → sanitize-html. Без zod (новая зависимость).
- gray-matter кэширует по строке без options — передавать options.
- Vite 8/Rolldown: возможно нужен `moduleType: 'js'` из transform/load [проверить context7].
- md.d.ts + `import.meta.glob<Project>(..., { eager: true, import: 'default' })`, дискриминант `kind`.
- buildIncludedRoutes(paths, slugs) чистая: убрать `:`-пути, добавить /projects/<slug>, /404.
- vite-ssg 28: экспорт includedRoutes из entry, dirStyle flat, ClientOnly [проверить].
- Неизвестный slug: beforeEnter → not-found.
- vite preview на неизвестный путь отдаёт index.html/200 → E2E 404 проверяет клиентский рендер; dist/404.html проверяется отдельно.
- TS 6: types явно (vite/client, node), moduleResolution bundler.
- @unhead/vue: один экземпляр (npm ls), resolve.dedupe.
- PrimeVue styled + SSG: возможный FOUC — проверить сбор стилей в пререндер.

## Фронтенд
- useContactForm({ delayMs=800, simulate='success'|'error' }): values, touched/onBlur, errors + visibleErrors (touched || submitAttempted), status, isValid, submit, reset. Trim, длина по code points. Email regex + ≤254. Повторный submit в sending игнорируется. Таймер — vi.useFakeTimers.
- Vitest: контент-тесты в node env, компоненты в jsdom; PrimeVue через global.plugins.
- Playwright: webServer build && preview :4173, проекты desktop 1280×800 / mobile 360×740.
- Селекторы: роли + data-testid (section-<id>, project-card-<slug>, contact-form, field-error-*, form-status, back-link).

## UI
- Hero (h1, CTA Button as a), О себе, Навыки (Tag), Проекты (Card, stretched link, сетка 1/2/3), Опыт (Timeline align=left), Контакты (форма ≤560px).
- Шапка: PrimeVue Menubar breakpoint 640 (правило «не писать аналоги PrimeVue», встроенный бургер — без primeicons). Пункты ≥44px через definePreset. scroll-margin-top на секциях.
- Проект: «← К проектам» на /#projects (не history.back). 404: h1, «На главную», noindex.
- Токены: space 4..96, container 1120, system fonts, clamp-шкала. Primary indigo-600 (emerald-500 ≈2.5:1 — провал AA).
- Форма: метки над полями, Message под полем, aria-invalid/describedby, фокус на первое невалидное, role=status aria-live, демо-подпись всегда.
- A11y: skip-link, один h1, focus-visible, reduced-motion.

## Безопасность
- sanitize-html: теги из AGENTS.md, a[href, rel], schemes https/mailto, allowProtocolRelative false; transformTags.a — жёсткий фильтр href (https/mailto/#якорь), rel="noopener noreferrer" на https.
- h1 в Markdown → ошибка сборки; h5/h6 → h4; картинки → ошибка сборки.
- markdown-it: html false, linkify false, свой validateLink.
- gray-matter: запретить `---js` engine (eval!).
- Frontmatter URL: new URL, https://example.com / mailto:@example.com / #; cover — путь в public, файл существует, coverAlt обязателен.
- ESLint: no-console, no-restricted-globals/properties/syntax (fetch, XHR, WebSocket, sendBeacon, storage, cookie), no-restricted-imports build-библиотек в src, vue/no-v-html с исключением MarkdownContent.
- Форма: @submit.prevent, без action/method, кнопка disabled до mounted.
- Бандл: generateBundle проверяет moduleIds + тест grep dist.
- CSP meta только при build (connect-src 'none', form-action 'none').
- npm audit, lockfile, engine-strict.

## Решения пользователя
- Шапка: PrimeVue Menubar.
- CSP meta: да, только при build.
- h1 и картинки в теле .md → ошибка сборки; h5/h6 → h4.
- Chrome MCP: пользователь говорит «подключён», но в сессии инструментов mcp__claude-in-chrome__* нет — перепроверить перед Validation.

## Решения по ходу (Executing/Validation)
- PrimeVue 5.0.4 → 4.5.5 (+ @primeuix/themes 2.0.3): v5 под PrimeUI License, требует ключ. AGENTS.md/CLAUDE.md обновлены.
- Smoke: Playwright MCP вместо Chrome MCP (Chrome MCP в сессии не появился). Day3/CLAUDE.md обновлён.
