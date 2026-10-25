# Day3: сайт-визитка на Vue

Правила для любого агента (pi-agent, Claude Code и других). Специфика Claude Code вынесена в `CLAUDE.md`.

Учебный стенд: статический сайт-визитка вымышленного разработчика. Цель — проверить, как агент строит фронтенд по правилам.
Реального владельца, реальной отправки данных и деплоя нет.

## Связь с корневым AGENTS.md

- Корневые инварианты Frontend (TypeScript, Vue 3) действуют.
- Backend в Day3 **нет**. Go/Gin и Clean Architecture из корня здесь не применяются, Go-код в Day3 не создавать.
- При противоречии с корнем по Day3 главнее этот файл.

## Инварианты стека (нельзя нарушать)

| Что | Решение |
|---|---|
| Runtime | Node 24 LTS (`engines.node: ">=24"`, `.nvmrc` = `24`), пакетный менеджер npm, `package-lock.json` в git |
| Язык | TypeScript, `strict: true`, `noUncheckedIndexedAccess: true`. `any` запрещён, `as` — только с комментарием почему |
| Фреймворк | Vue 3, только Composition API и `<script setup lang="ts">`. Options API запрещён |
| Сборка | Vite + `vite-ssg`: каждая страница пререндерится в HTML на этапе сборки |
| Роутинг | `vue-router` (через `vite-ssg`), history mode |
| UI | PrimeVue (styled mode, пресет Aura из `@primeuix/themes`), авто-импорт через `unplugin-vue-components` + `@primevue/auto-import-resolver` |
| Head/SEO | `useHead` из `@unhead/vue` — той же мажорной версии, что тянет `vite-ssg` (сейчас 2.x), иначе будет два экземпляра |
| Контент | Markdown + YAML frontmatter, разбор только на этапе сборки (`gray-matter`, `markdown-it`, `sanitize-html`) |
| Тесты | Vitest + Vue Test Utils (окружение jsdom), Playwright по собранной статике |
| Линт | ESLint (flat config) + `eslint-plugin-vue` + `typescript-eslint`, типы — `vue-tsc --noEmit` |

Альтернативы (Nuxt, Tailwind, другие UI-киты, Pinia, axios, i18n-библиотеки, CSS-in-JS) не предлагать и не ставить.
Новая runtime-зависимость сверх таблицы — только с согласия пользователя.

### Версии (проверены 2026-10-25)

Ставить с `^`, кроме TypeScript. Перед изменением версий — сверить совместимость peerDependencies.

| Пакет | Версия | Примечание |
|---|---|---|
| vue | 3.5.44 | |
| vite | 8.3.5 | |
| vite-ssg | 28.3.0 | |
| vue-router | 5.4.0 | `vite-ssg` поддерживает `^4 \|\| ^5` |
| @unhead/vue | 2.1.17 | мажор должен совпадать с зависимостью `vite-ssg` |
| primevue, @primevue/auto-import-resolver | 4.5.5 | **не 5.x**: с 5.0 лицензия PrimeUI (нужен ключ, иначе баннер). 4.x — MIT |
| @primeuix/themes | 2.0.3 | пара к primevue 4.x, MIT |
| @vitejs/plugin-vue | 6.0.10 | |
| unplugin-vue-components | 32.1.0 | |
| typescript | ~6.0.3 | **не 7.x**: `typescript-eslint` требует `<6.1.0` |
| vue-tsc | 3.3.13 | |
| vitest | 5.0.5 | |
| @vue/test-utils | 2.5.1 | |
| jsdom | 30.1.3 | окружение Vitest |
| @playwright/test | 1.64.1 | |
| eslint | 10.13.0 | |
| eslint-plugin-vue | 10.11.1 | |
| typescript-eslint | 8.72.0 | |
| markdown-it | 15.0.3 | только build-time |
| gray-matter | 4.0.3 | только build-time |
| sanitize-html | 2.18.1 | только build-time |

## Структура

Весь код — в `Day3/frontend/`. Команды запускаются оттуда.
Отчёты, сценарии и скриншоты по Day3 — в `Day3/swarm-report/`, не в корневом `swarm-report/`. Инструкция по запуску сайта и тестов — `Day3/README.md`, обновлять при изменении команд.

```
frontend/
  index.html
  vite.config.ts               плагины: vue, Components(PrimeVueResolver), content; ssgOptions
  build/content-plugin.ts      Vite-плагин: *.md → типизированный JS-модуль (frontmatter + очищенный HTML)
  src/
    main.ts                    export const createApp = ViteSSG(...); PrimeVue; export includedRoutes
    App.vue                    каркас: шапка с навигацией, <RouterView>, подвал
    router/routes.ts           маршруты
    pages/
      HomePage.vue             / — все разделы главной
      ProjectPage.vue          /projects/:slug
      NotFoundPage.vue         404
    components/
      sections/                HeroSection, AboutSection, SkillsSection, ProjectsSection,
                               ExperienceSection, ContactSection — по одному на раздел
      ui/                      мелкие переиспользуемые компоненты поверх PrimeVue
    composables/               useContactForm.ts и прочая логика без разметки
    content/
      profile.md               имя, роль, био, навыки, ссылки
      experience/*.md          места работы, по файлу на место
      projects/*.md            проекты, по файлу на проект, имя файла = slug
      types.ts                 интерфейсы Profile, ExperienceItem, Project
      index.ts                 загрузка через import.meta.glob, сортировка, поиск по slug
    styles/
      tokens.css               CSS-переменные проекта (отступы, брейкпоинты) поверх токенов PrimeVue
  tests/
    unit/                      *.spec.ts (Vitest)
    e2e/                       *.spec.ts (Playwright)
  public/                      favicon, изображения проектов
```

Правила слоёв:
- `pages/` собирают страницу из `sections/`, сами разметки почти не содержат.
- `sections/` и `ui/` получают данные через props, не импортируют `content/` напрямую. Исключение — страницы.
- Логика (валидация, состояние формы) — в `composables/`, тестируется без монтирования компонента.
- Тексты интерфейса, не являющиеся контентом (подписи кнопок, ошибки валидации), — в компонентах, на русском.

## Контент

- Весь контент — вымышленный. В `profile.md` поле `fictional: true`, в подвале сайта фраза «Демонстрационный сайт, персона вымышлена».
  Не использовать имена, фото и контакты реальных людей и компаний. Ссылки — на `example.com` или `#`.
- Компоненты не содержат контент-строк (имён, описаний проектов, дат). Правка контента = правка `.md`.
- Frontmatter валидируется при сборке. Нет обязательного поля или неверный тип — сборка падает с понятной ошибкой
  (имя файла + поле). Тихие значения по умолчанию запрещены.

Схемы frontmatter:

```yaml
# profile.md — тело: биография (Markdown)
name: string
role: string
location: string
fictional: true
skills:            # группы навыков
  - group: string
    items: string[]
links:
  - label: string
    url: string      # https://example.com/... или mailto:...@example.com
```

```yaml
# experience/<id>.md — тело: обязанности и результаты
company: string
position: string
start: YYYY-MM
end: YYYY-MM | null   # null = по настоящее время
```

```yaml
# projects/<slug>.md — тело: подробное описание для /projects/:slug
title: string
summary: string       # 1–2 предложения для карточки на главной
tags: string[]
year: number
cover: string | null  # путь в public/, alt обязателен если указан
coverAlt: string | null
repo: string | null
demo: string | null
order: number         # порядок на главной
```

Slug — имя файла, `^[a-z0-9-]+$`. Другой slug — ошибка сборки.

### Рендер Markdown (безопасность)

- Markdown превращается в HTML **только при сборке** в `build/content-plugin.ts`: `gray-matter` → `markdown-it` (`html: false`) → `sanitize-html` (белый список тегов: абзацы, заголовки h2–h4, списки, ссылки, `code`, `pre`, `strong`, `em`, `blockquote`; у ссылок только `href` со схемами `https`/`mailto`, внешним добавлять `rel="noopener noreferrer"`).
- В браузерный бандл не попадают `markdown-it`, `gray-matter`, `sanitize-html`. Проверка: после `npm run build` их нет в `dist/assets/*.js`.
- `v-html` разрешён **только** для поля `html` из контент-модуля и только в одном компоненте `ui/MarkdownContent.vue`. Любой другой `v-html` запрещён (включить правило `vue/no-v-html` с исключением для этого файла).

## Маршруты и SSG

| Путь | Страница | Источник |
|---|---|---|
| `/` | HomePage: Hero, О себе, Навыки, Проекты, Опыт, Контакты | `profile.md`, `experience/*`, `projects/*` |
| `/projects/:slug` | ProjectPage | `projects/<slug>.md` |
| `/:pathMatch(.*)*` | NotFoundPage | — |

- `includedRoutes` экспортируется из `src/main.ts` и разворачивает `/projects/:slug` в пути по списку проектов из `content/index.ts`. Ручной список путей запрещён.
  Маршруты с параметрами `vite-ssg` по умолчанию пропускает, поэтому catch-all заменяется явным путём `/404`. Он попадает в catch-all и даёт `dist/404.html`.
- Неизвестный slug на клиенте — редирект на 404, не пустая страница.
- Навигация по разделам главной — якоря `#about`, `#skills`, `#projects`, `#experience`, `#contact`; `scrollBehavior` в роутере учитывает хэш и высоту липкой шапки.
- Каждая страница задаёт `title` и `description` через `useHead`. `<html lang="ru">`.
- Код, которому нужен `window`/`document`, — только в `onMounted` или внутри `<ClientOnly>`. На верхнем уровне модулей обращения к браузерным API запрещены: сломают пререндер.

## Форма обратной связи (заглушка)

- Поля: имя (2–100 символов), email (формат), сообщение (10–2000 символов). Скрытое honeypot-поле не нужно — отправки нет.
- Валидация — в `composables/useContactForm.ts`, ошибки показываются под полем после blur или попытки отправки.
- «Отправка» — имитация: состояние `sending` ~800 мс, затем `success`. Состояние `error` должно быть достижимо в тестах (через параметр composable), чтобы проверить UI ошибки.
- Под формой постоянно видна подпись: «Это демо: сообщение никуда не отправляется».
- **Запрещено:** `fetch`/XHR/`navigator.sendBeacon`, сторонние сервисы форм, `mailto:`-отправка содержимого, сохранение введённых данных в `localStorage`/cookies, логирование введённых данных в консоль.

## UI и вёрстка

- Компоненты PrimeVue — для интерактива и форм (`Button`, `InputText`, `Textarea`, `Message`, `Card`, `Tag`, `Timeline` и т.п.). Не писать свои аналоги существующих компонентов PrimeVue.
- Только бесплатные компоненты из пакета `primevue`. PrimeBlocks и платные шаблоны не использовать.
- Тема: Aura через `app.use(PrimeVue, { theme: { preset, options: { darkModeSelector: false } } })`. Тёмной темы нет — без `false` PrimeVue перекрасит свои компоненты по системной теме, а собственные стили останутся светлыми.
- Кастомизация цветов — через `definePreset` или CSS-переменные PrimeVue, не через переопределение внутренних классов `.p-*`.
- Собственные стили — `<style scoped>`, значения из CSS-переменных (`tokens.css` и токены PrimeVue). Хардкод цветов в компонентах запрещён.
- Mobile-first: базовые стили — для 360px, расширение через `min-width` медиа-запросы (брейкпоинты 640 / 960 / 1200). На ширине 360–1440px нет горизонтального скролла, текст не обрезается, тап-цели не меньше 44×44px.
- Семантика: один `<h1>` на страницу, разделы — `<section>` с заголовком, навигация — `<nav>`, у изображений `alt`.

## Тесты

Unit (Vitest, `tests/unit/`), обязательно:
- `useContactForm`: валидация каждого поля (граничные длины, неверный email), переходы `idle → sending → success/error`.
- Контент-плагин: валидный файл → ожидаемый объект; пропущенное поле → ошибка с именем файла; `<script>`, `onerror=`, `javascript:` в Markdown вырезаются.
- `content/index.ts`: сортировка проектов по `order`, опыта по `start` (новые сверху), поиск по slug.
- Компоненты секций: рендер переданных props, пустой список проектов не ломает разметку.

E2E (Playwright, `tests/e2e/`), запуск по `vite preview` после сборки, проекты Chromium desktop (1280×800) и mobile (360×740):
- Главная открывается, все 6 разделов видны, якорная навигация прокручивает к разделу.
- Клик по карточке проекта → `/projects/<slug>`, заголовок и описание из Markdown, кнопка «Назад» возвращает на главную.
- Прямой заход на `/projects/<slug>` отдаёт пререндеренный HTML с заголовком проекта (проверка ответа сервера, без JS).
- Неизвестный slug → 404.
- Форма: ошибки при пустой отправке, успешная «отправка» валидных данных, ни одного сетевого запроса при отправке (`page.on('request')`).
- На 360px нет горизонтального скролла: `document.documentElement.scrollWidth <= innerWidth`.

Не трогать и не ослаблять существующие тесты ради зелёного прогона. Падающий тест — причина в коде или в тесте объяснить явно.

## Команды

Каталог: `Day3/frontend/`. Скрипты в `package.json` должны называться именно так:

| Задача | Команда | Скрипт |
|---|---|---|
| Установка | `npm ci` | — |
| Dev-сервер | `npm run dev` | `vite` |
| Сборка (SSG) | `npm run build` | `vite-ssg build` |
| Просмотр сборки | `npm run preview` | `vite preview` |
| Unit-тесты | `npm test` | `vitest run` |
| E2E | `npm run test:e2e` | `playwright test` (webServer: `npm run build && npm run preview`) |
| Линт | `npm run lint` | `eslint .` |
| Типы | `npm run typecheck` | `vue-tsc --noEmit` |

## Критерии готовности

Задача готова, когда в `Day3/frontend/` без ошибок и предупреждений проходят:
`npm run lint`, `npm run typecheck`, `npm test`, `npm run build`, `npm run test:e2e` —
и в `dist/` есть `index.html`, `projects/<slug>.html` на каждый проект и `404.html`.

## Запрещено

- Секреты, `.env`, API-ключи, аналитика, трекеры, внешние шрифты и скрипты с CDN.
- Любые сетевые запросы из приложения в рантайме.
- Отключение правил ESLint/TS через `eslint-disable`, `@ts-ignore`, `@ts-expect-error` без комментария с причиной.
- Коммит `dist/`, `node_modules/`, `test-results/`, `playwright-report/`.
