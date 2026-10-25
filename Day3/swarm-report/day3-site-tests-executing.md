# Executing: Day3 — сайт-визитка + тесты (2026-10-25)
Исполнитель: vue-expert. Каталог: `Day3/frontend/`. Коммитов нет.

## 0. БЛОКЕР — решение пользователя (PrimeVue 5 требует лицензионный ключ)
`primevue@5.0.4` (как и `@primeuix/themes@3`, `@primevue/auto-import-resolver@5`) распространяется по **PrimeUI License**, а не MIT.
При `app.use(PrimeVue)` вызывается `verifyLicense` (`@primevue/core/config/index.mjs`, офлайн, сети нет). Без ключа:
- `console.warn("[PrimeUI] PrimeUI license is not configured.")` — 6 раз в выводе `npm run build` (SSR каждой страницы) и в консоли браузера;
- красный баннер «Invalid PrimeUI License» в правом нижнем углу каждой страницы (closed shadow root, `pointer-events:none`, тесты не ломает, но виден на скриншотах).
Community License бесплатна (физлица, учебные проекты), но ключ нужно получить на primeui.dev. Обходить проверку запрещает сама лицензия («may not … remove its license mechanisms»), поэтому я её не трогал.
Варианты (решает пользователь):
1. Получить бесплатный Community-ключ и передать `license` в `src/theme/primevue.ts`. Ключ проверяется офлайн и всё равно попадает в бандл, то есть это не секрет. Но AGENTS.md запрещает «API-ключи», поэтому нужно явное разрешение.
2. Перейти на `primevue@4.5.5` (MIT, без проверки лицензии) + `@primeuix/themes@2.x`. Это меняет таблицу версий в AGENTS.md и потребует сверить API v4.
3. Оставить как есть и зафиксировать: критерий «без предупреждений» формально не выполнен из-за этой строки.
Кроме этой строки, во всех 5 командах нет ни ошибок, ни предупреждений.

## 1. Сверка API (Шаг 0). Источник — node_modules после install (context7 недоступен)
| Вопрос | Что выяснил | Откуда |
|---|---|---|
| vite-ssg 28: includedRoutes | Экспорт `includedRoutes(paths, routes)` из entry имеет приоритет над `ssgOptions`. По умолчанию отбрасываются пути с `:` и `*` | README, `dist/shared/vite-ssg.D5Fdxh1E.mjs` (`serverEntryIncludedRoutes`) |
| vite-ssg: dirStyle / имена файлов | `flat` по умолчанию: `/projects/x` → `projects/x.html`, `/404` → `404.html` | `.d.mts` ViteSSGOptions + build() |
| vite-ssg: гидрация | По умолчанию `hydration: false`: клиент делает `createApp` + полный ререндер. `router.isReady()` ждётся ДО mount | `dist/index.mjs` |
| vite-ssg: initialState | При пустом состоянии `serializeState` → `null`, инлайн-`<script>` нет (важно для CSP `script-src 'self'`) | `vite-ssg.C6pK7rvr.mjs` |
| vite-ssg: beasties | optional peer, не установлен → выключен явно `beastiesOptions: false` | `.d.mts`, `getBeasties` |
| vite-ssg: ClientOnly | регистрируется глобально (`registerComponents`); не понадобился | `dist/index.mjs` |
| @unhead/vue | Один экземпляр 2.1.17 (`npm ls`: deduped под vite-ssg). Серверный `createHead` ставит `lang: "en"` → перекрывает index.html, нужен `useHead({ htmlAttrs: { lang: 'ru' } })` | `unhead/dist/server.mjs:26`, проверено по dist |
| Vite 8 (rolldown): load + moduleType | `load` может вернуть `moduleType`; для `.md` возвращаю `moduleType: 'js'` | `vite/dist/node/index.d.ts`, `rolldown/.../define-config*.d.mts` |
| Vite 8: modulePreload | `build.modulePreload.polyfill` есть; выключен (полифил использует fetch) | `index.d.ts:2827/3053` |
| Vite 8: configLoader | Предупреждает об импортах без расширения в конфиге (будущий `configLoader: 'native'`) → импорты с `.ts` | вывод build |
| Vite 8: import.meta.glob | без generic тип `unknown` → сужаю рантайм-гардом по `kind`, без `as` | `types/importGlob.d.ts` |
| vite preview | `/projects/x` отдаёт `projects/x.html`; неизвестный путь → index.html, 200 (SPA-fallback) | curl по preview |
| PrimeVue 5: Menubar | props `model`, `breakpoint`, `buttonProps`; слот `#item="{ item, props }"` c `props.action/label`; клик по пункту закрывает мобильное меню | `primevue/menubar/index.d.ts`, `index.mjs` |
| PrimeVue 5: Button/Message/Timeline/Card/Tag/InputText/Textarea | Button `as`/`asChild`; Message `severity/size/variant`, корень `role="alert" aria-live="assertive"` (переопределяю через `pt`); Timeline слоты `content{item:any,index}`; InputText эмитит `string \| undefined` | `*/index.d.ts`, `message/index.mjs` |
| PrimeVue 5: SSR-стили | `useStyle` без `document` ничего не инжектит → в пререндере стилей PrimeVue нет (FOUC до загрузки JS) | `@primevue/core/usestyle/index.mjs` |
| PrimeVue 5: config | `theme`, `license`, `csp` и др.; лицензия — см. блокер | `@primevue/core/config/index.d.ts` |
| @primeuix/themes 3.1.0 | `definePreset` из `@primeuix/themes`, пресет `@primeuix/themes/aura`; токены menubar: `item.padding`, `mobileButton.size` | README, `dist/aura/menubar` |
| darkModeSelector | По умолчанию `"system"`; `false` выключает. Проверено E2E: цвет кнопки в light и dark одинаковый | `@primeuix/styled` chunk + layout.spec |
| auto-import-resolver | `PrimeVueResolver()` для `unplugin-vue-components`, `dts` генерирует типы для vue-tsc | README, `index.d.mts` |
| Vitest 5 | `projects` есть, но выбран докблок `// @vitest-environment jsdom` (поддерживается) — проще, по фолбэку плана | `vitest/dist/chunks/*.d.ts` |
| TS 6 | `types` указаны явно (`vite/client`, `node`), `moduleResolution: bundler`, `paths` без `baseUrl` | tsconfig + vue-tsc |
| ESLint 10 | `eslint.config.ts` требует `jiti` или флаг `unstable_native_nodejs_ts_config`; `@eslint/js` не ставится вместе с eslint | `eslint/lib/config/config-loader.js`, `ls node_modules/@eslint` |
| typescript-eslint 8.72 | peer `typescript >=4.8.4 <6.1.0` → `~6.0.3` подходит | `npm view` |
| gray-matter 4 | Движок `javascript` выполняет `eval`; кэш только без options | `lib/engines.js`, `index.js` |
| markdown-it 15 | Свои типы (`MarkdownIt`, `Token`), default export — callable | `dist/markdown-it.d.mts` |
| Playwright 1.64 | `webServer`, `projects`, `contextOptions.reducedMotion`, `test.info()` | типы `@playwright/test` |

peerDependencies: конфликтов нет, `--force` / `--legacy-peer-deps` не использовались. Все версии строго по таблице AGENTS.md.

## 2. Созданные файлы (`Day3/frontend/`)
Каркас: `package.json`, `package-lock.json`, `.npmrc` (engine-strict), `.nvmrc` (24), `.gitignore`, `index.html`, `tsconfig.json`, `vite.config.ts` (включая `test`), `playwright.config.ts`, `eslint.config.js`.
Build: `build/content-core.ts` (чистые функции: detectContentKind, splitFrontmatter, parseContentFile, validate*, renderMarkdown, sanitizeContentHtml, assertSafeUrl, ContentError), `build/content-plugin.ts` (load-хук, generateBundle-проверка, CSP-плагин `apply: 'build'`).
src: `main.ts`, `App.vue`, `md.d.ts`, `components.d.ts` (генерируется), `content/{types.ts,index.ts,profile.md,projects/*.md ×3,experience/*.md ×3}`, `router/{routes.ts,index.ts,includedRoutes.ts,scrollBehavior.ts}`, `theme/{preset.ts,primevue.ts}`, `composables/{useContactForm.ts,formatPeriod.ts}`, `styles/tokens.css`, `pages/{HomePage,ProjectPage,NotFoundPage}.vue`, `components/sections/×6`, `components/ui/{AppHeader,AppFooter,PageSection,ProjectCard,ExperienceEntry,MarkdownContent}.vue`.
public: `favicon.svg`, `images/task-tracker.svg`.
Тесты: `tests/unit/{content-core,content-index,useContactForm,included-routes,sections,router}.spec.ts`, `tests/e2e/{helpers.ts,home,project,prerender,not-found,contact,layout}.spec.ts`.

testid (для smoke на стадии Validation): `section-<hero|about|skills|projects|experience|contact>`, `nav-<id>` (пункты меню; на 360px сначала нажать кнопку с aria-label «Открыть меню»), `project-card-<slug>`, `project-title`, `project-body`, `back-link`, `not-found`, `home-link`, `contact-form`, `field-name|email|message`, `field-error-name|email|message`, `contact-submit`, `form-status` (+ `data-status` = idle|sending|success|error), `demo-note`, `fictional-note`. Готовность: `html[data-app-mounted="true"]`.

## 3. Отклонения от плана и почему
1. **`eslint.config.js` вместо `.ts`**: TS-конфиг требует `jiti` (вне стека) или флаг CLI, а скрипт обязан быть ровно `eslint .`.
2. **Один `tsconfig.json` вместо четырёх**: `vue-tsc --noEmit` на solution-style конфиге с `files: []` ничего не проверяет. Node-типы в src скомпенсированы ESLint: `process`/`Buffer`/`require` и импорты `node:*`/`fs`/`path` запрещены в `src/**`.
3. **`@eslint/js` не используется**: его нет ни в таблице, ни в зависимостях eslint 10. Ключевые core-правила включены явно (`no-console`, `no-eval`, `no-implied-eval`, `no-new-func`, `no-script-url`, `eqeqeq` и т.д.).
4. **Добавлены dev-зависимости только с типами**: `@types/node@^24.19.2` (fs/path в build и конфигах) и `@types/sanitize-html@^2.16.2` (у пакета нет своих типов). Runtime-зависимостей сверх таблицы нет.
5. **Guard неизвестного slug**: вместо `beforeEnter` параметр ограничен regex в пути `/projects/:slug(a|b|c)`. Причина — баг, найденный первым прогоном (п. 4).
6. **Vitest-окружение**: докблок `@vitest-environment jsdom` в `sections.spec.ts` вместо `projects`.
7. **`html[data-app-mounted]`** выставляется в `App.vue onMounted`. Это сигнал готовности для E2E/smoke: без гидрации DOM пересоздаётся после загрузки JS.
8. **`src/components.d.ts`** генерирует unplugin и он должен лежать в git, иначе typecheck на чистом клоне не увидит типы PrimeVue. В ESLint он в ignore: в файле генератора есть свои `eslint-disable`/`@ts-nocheck`.
9. **E2E на порту 4174** (`reuseExistingServer: false`), чтобы не конфликтовать с ручным `npm run preview` (4173) на стадии Validation.
10. Для `tests/**` выключено `no-script-url`: строки `"javascript:alert(1)"` — это XSS-фикстуры, а не код. В src правило работает.

## 4. Первый прогон unit-тестов (честно, до исправлений)
`npm test` → **Test Files 1 failed | 5 passed (6); Tests 1 failed | 134 passed (135)**.
Упал `router.spec.ts › маршруты › реальные маршруты: каждый проект из контента открывается, чужой slug — 404`:
`expected 'project' to be 'not-found'`.
**Причина — код, тест верный.** `beforeEnter` в vue-router не вызывается при смене параметра того же маршрута (`/projects/a` → `/projects/unknown`), поэтому клиентская навигация показала бы пустую страницу проекта вместо 404.
**Исправление в коде**: `createRoutes(slugs)` строит путь `/projects/:slug(<slug1>|<slug2>|…)`, и неизвестный slug сам падает в catch-all при любой навигации.
**Что поменялось в тестах**: только вызов фабрики (`makeRouter(['known'])` вместо предиката), assertions прежние. Добавлены 2 теста: регрессия смены параметра и пустой список проектов.
Второй прогон: **6/6 файлов, 137/137 тестов**. Потом в тестах были исправлены только типы (async-хелпер для `scrollBehavior`, типизированный `mount`), без изменения проверок.

E2E, первый прогон: **52 passed, 3 failed, 5 skipped**.
- `layout › тап-цели` (mobile): 41.85px < 44. Причина в тесте: замер сделан во время анимации раскрытия Menubar. Фактически 45px (проверено computed style). Разовый замер заменён на `expect.poll`, порог 44 тот же.
- `project › растянутая ссылка` (×2): Playwright не кликает по `<p>`, перекрытому `::after` ссылки (actionability). Это как раз подтверждает оверлей. Теперь клик идёт по карточке в точке над текстом описания.
- По скриншотам найден баг кода, который тесты не ловили: прямой заход на `/#experience` прокручивал на ~272px ниже нужного (scrollBehavior срабатывает на пререндеренном DOM до mount). Фикс — повторная `scrollToHash` в `App.vue onMounted`. Тест ужесточён верхней границей (≤ header+16). Проверка мутацией: без фикса тест падает (272.5 / 272.9px).
- Бургер на mobile прилипал к логотипу: исправлено через `justify-content: space-between` на корне Menubar.

## 5. Итог: 5 команд (финальный прогон подряд)
| Команда | Результат |
|---|---|
| `npm run lint` | exit 0, 0 ошибок, 0 предупреждений |
| `npm run typecheck` | exit 0, без вывода |
| `npm test` | exit 0, 6 файлов, 137 passed |
| `npm run build` | exit 0; 5 страниц: `index.html`, `404.html`, `projects/{task-tracker,weather-dashboard,markdown-notes}.html`; JS 474 kB (gzip 121 kB). Предупреждение: только `[PrimeUI] PrimeUI license is not configured.` ×6 |
| `npm run test:e2e` | exit 0, 55 passed, 5 skipped (проверки файлов dist только в desktop; тест бургера только в mobile), 0 failed |
`grep -lE "markdown-it|gray-matter|sanitize-html" dist/assets/*.js` → пусто. Плюс `generateBundle` роняет сборку при утечке, плюс E2E `prerender.spec`.
Скриншоты на каждый E2E-тест (`screenshot: 'on'`) лежат в `Day3/frontend/test-results/`.

## 6. npm ls / audit
- `npm ci` — ок, lockfile консистентен. `npm audit` и `npm audit --omit=dev` — 0 vulnerabilities.
- Один экземпляр: vue 3.5.44, vue-router 5.4.0, vite 8.3.5, @unhead/vue 2.1.17 (deduped под vite-ssg), typescript 6.0.3.
- vite-ssg тянет свой `jsdom@28` (для SSG), Vitest использует `jsdom@30.1.3`, как в таблице. Конфликта нет.
- UNMET OPTIONAL: `jiti`, `@stylistic/eslint-plugin`, `@noble/hashes` — необязательные peer-зависимости, не нужны.

## 7. Безопасность (реализовано)
gray-matter: первая строка строго `---` и отказные движки js/javascript/coffee. Тест проверяет, что `---js` не выполняется (маркер в globalThis не появляется).
markdown-it: `html: false`, `linkify: false`, свой `validateLink` (https/mailto/#).
Проверка токенов: h1 и картинки → ошибка сборки, h5/h6 → h4, ссылка не на example.com → ошибка.
sanitize-html: белый список, `transformTags.a` с жёстким фильтром href, `rel="noopener noreferrer"` у https.
URL во frontmatter через `new URL`, допустимы только `https://(*.)example.com`, `mailto:@example.com` и `#`. `cover` должен существовать в public, `coverAlt` обязателен. Неизвестные поля → ошибка.
CSP meta только в build: `connect-src 'none'`, `form-action 'none'`, `script-src 'self'`, `style-src 'self' 'unsafe-inline'` (PrimeVue инжектит `<style>`). E2E: ни одной ошибки консоли и CSP-нарушений, только same-origin запросы.
Форма: `@submit.prevent`, `novalidate`, без action/method, кнопка disabled до mount. E2E: 0 запросов при отправке, пустые localStorage/sessionStorage/cookies.
ESLint-запреты проверены пробным файлом: срабатывают все 15 нарушений.

## 8. Известные проблемы
1. **PrimeUI license** — см. блокер в п. 0.
2. **FOUC в пререндере**: PrimeVue в styled mode не отдаёт стили при SSR. До загрузки JS компоненты PrimeVue (Button, Card, Menubar) без темы; контент и собственные стили есть. Без гидрации vite-ssg после загрузки JS перерисовывает DOM. Исправить можно сбором `BaseStyle/*.getThemeStyleSheet()` в `onPageRendered`, но это заметная доработка.
3. **vite preview** на неизвестный URL отвечает 200 + index.html (SPA-fallback), 404 рисует клиент. На реальном хостинге нужно настроить отдачу `404.html` со статусом 404. Сейчас `dist/404.html` проверяется отдельно.
4. **Анимация мобильного меню PrimeVue** не учитывает `prefers-reduced-motion` (JS-анимация из `@primeuix/motion`).
5. Бандл 474 kB (121 kB gzip) — в основном PrimeVue вместе с проверкой лицензии. Ниже порога предупреждения Vite (500 kB).
6. Chrome MCP для smoke на Validation: в этой сессии исполнителя инструментов `mcp__claude-in-chrome__*` не было, это нужно перепроверить оркестратору.

---

## Миграция на PrimeVue 4.5.5 (решение пользователя по блокеру п. 0, 2026-10-25)
Блокер п. 0 **снят**: ключ не используется, PrimeVue переведён на ветку 4.x под MIT. Таблица версий в `Day3/AGENTS.md` обновлена оркестратором.

### Что изменилось
- `package.json`: `primevue` ^5.0.4 → **^4.5.5**, `@primevue/auto-import-resolver` ^5.0.4 → **^4.5.5**, `@primeuix/themes` ^3.1.0 → **^2.0.3**. `npm install` прошёл, lockfile обновлён, peer-конфликтов нет, `--force` и `--legacy-peer-deps` не использовались. После `npm ci` из node_modules ушли `@primeui/license-manager` и `@primeicons/vue`.
- **Код приложения не менялся.** Используемое API 4.x совпадает, а в `src/theme/primevue.ts` полей лицензии не было. `src/components.d.ts` перегенерирован, содержимое прежнее.
- Добавлен E2E-тест в `tests/e2e/layout.spec.ts`: нет `#p-license-host`, нет текста «Invalid PrimeUI License», нет console-warning `PrimeUI|license`. Это защита от возврата на 5.x. Проверки только добавлены, существующие не ослаблены.

### Лицензии установленных пакетов
| Пакет | Версия | license |
|---|---|---|
| primevue | 4.5.5 | MIT |
| @primevue/core, icons, metadata, auto-import-resolver | 4.5.5 | MIT |
| @primeuix/themes | 2.0.3 | MIT |
| @primeuix/styles | 2.0.3 | MIT |
| @primeuix/styled | 0.7.4 | MIT |
| @primeuix/utils | 0.6.4 | MIT |
`grep -rl "PrimeUI License" node_modules/@primeuix node_modules/@primevue node_modules/primevue` → пусто. `verifyLicense` и `license-manager` в primevue/@primevue не встречаются.

### Сверка API 4.x (по node_modules, d.ts)
| Что | 4.x / themes 2.x | Совпадает с кодом |
|---|---|---|
| `definePreset` | экспорт `@primeuix/themes` (`(...presets) => Preset`) | да |
| Пресет Aura | `@primeuix/themes/aura` через экспорт `"./*"` → `dist/aura/index.mjs` | да |
| Токены menubar | `baseItem.padding`, `item.padding`, `mobileButton.size` (по умолчанию 1.75rem), `borderRadius` | да |
| Message | `severity`, `size`, `variant: outlined\|simple`, корень `role="alert" aria-live="assertive"` (снимается через `pt`, как и раньше) | да |
| Button | `as`, `asChild`, `severity`, `variant`, `size` | да |
| Menubar | `model`, `breakpoint`, `buttonProps`; слоты `start/end/item/button/buttonicon/menubutton/menubuttonicon`; `item` scope с `props.action/label/icon` | да |
| Timeline | `value`, `align`, `dataKey`, `pt`; слоты `content/opposite/marker/connector` | да |
| Конфиг | `theme`, `csp`, `unstyled`…; `darkModeSelector` по умолчанию `"system"`, мы ставим `false` (E2E light и dark подтверждает) | да |

### Первый прогон после миграции (`npm ci` → 5 команд), без правок
| Команда | Результат |
|---|---|
| `npm run lint` | exit 0, 0 ошибок, 0 предупреждений |
| `npm run typecheck` | exit 0 |
| `npm test` | exit 0, 6 файлов, 137 passed |
| `npm run build` | exit 0, **ни одного предупреждения**; `grep -i "license\|warn"` по выводу пуст. JS 429 kB (gzip 107 kB, было 474/121) |
| `npm run test:e2e` | exit 0, 55 passed, 5 skipped (штатно) |
После добавления регрессионного теста лицензии: lint 0, typecheck 0, **e2e 57 passed, 5 skipped, 0 failed**.

dist: `index.html`, `404.html`, `projects/{markdown-notes,task-tracker,weather-dashboard}.html`. `grep -E "Invalid PrimeUI License|p-license-host|license-manager|verifyLicense" dist` → пусто, маркеров `markdown-it|gray-matter|sanitize-html` в `dist/assets/*.js` нет. На скриншотах E2E (360 и 1280) баннера нет.

### npm ls / audit
- Один экземпляр: vue 3.5.44, vue-router 5.4.0, vite 8.3.5, @unhead/vue 2.1.17, primevue 4.5.5, @primeuix/themes 2.0.3.
- `npm audit` и `npm audit --omit=dev` → 0 vulnerabilities. Неудовлетворённых обязательных peer нет (только optional: jiti, @stylistic).

### Что остаётся из «Известных проблем» (п. 8)
Пункт 1 (лицензия) закрыт. Пункты 2–6 (FOUC стилей PrimeVue в пререндере, SPA-fallback у `vite preview`, анимация меню без reduced-motion, Chrome MCP) остаются. Размер бандла уменьшился до 429 kB.

---

## Fix: aria-label бургера (по итогам smoke, 2026-10-25)
**Проблема.** `AppHeader.vue`: `:button-props="{ 'aria-label': 'Открыть меню' }"` — подпись «Открыть меню» не менялась при открытом меню. `aria-expanded` Menubar переключает сам, и правильно.
**Решение.** Статичная подпись `'Меню'`, состояние передаёт `aria-expanded` (стандартный паттерн disclosure). Динамическую подпись не делал: Menubar не отдаёт состояние `mobileActive` наружу без хаков.

### Изменения
- `src/components/ui/AppHeader.vue`: `'aria-label': 'Меню'`.
- Селекторы (имя элемента изменилось, проверки те же): `tests/e2e/helpers.ts` (`openMenuIfMobile`) и `tests/e2e/layout.spec.ts` (тап-цель бургера) → `getByRole('button', { name: 'Меню', exact: true })`. `exact`, чтобы не совпасть с подстрокой. `grep "Открыть меню" src tests` пуст.
- Новые проверки в `tests/e2e/home.spec.ts › мобильное меню закрывается после выбора пункта` (mobile): `aria-expanded="false"` до открытия → `"true"` после открытия, доступное имя ровно «Меню» → `"false"` после выбора пункта.

### Результаты
| Команда | Результат |
|---|---|
| `npm run lint` | exit 0, без предупреждений |
| `npm run typecheck` | exit 0 |
| `npm test` | exit 0, 137 passed |
| `npm run build` | exit 0, 0 предупреждений (поиск warn/license по выводу пуст) |
| `npm run test:e2e` | **прогон 2 (03:03): exit 0, 57 passed, 5 skipped**. Прогоны 1, 3, 4 частично упали из-за окружения (ниже) |

### E2E и сон машины (честно)
Четыре прогона после правки:
1. 2 failed (`project.spec`, mobile), длительность 2.3 мин;
2. 57 passed за 5.9 с;
3. 5 failed, 9.6 мин;
4. под `caffeinate -i -s -m`: 1 failed, 9.7 мин.

**Причина — окружение, не код.**
- `pmset -g log` показывает «Entering Sleep state due to 'Maintenance Sleep' … Using Batt» в окнах 02:55:57–03:03:37, 03:03:58–03:13:38 и 03:24:00–03:33:41. Это ровно время провальных прогонов. Между сном — DarkWake примерно на 20 с.
- 30-секундный таймаут теста «длился» 9.7 мин, то есть процесс был заморожен целиком.
- Каждый раз падают разные тесты, а сами по себе они идут меньше секунды.
- `caffeinate` maintenance sleep от батареи не блокирует.
- Параллельно работал Playwright MCP координатора (Chrome, load average ~7).

Новые проверки `aria-expanded` и тап-цели бургера прошли во всех прогонах, включая 3 и 4. Чтобы получить ещё один полностью зелёный прогон, его нужно запускать на машине, подключённой к питанию (или без maintenance sleep).
