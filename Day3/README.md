# Day3: сайт-визитка на Vue и два уровня тестов

Статический сайт-визитка вымышленного разработчика: Vue 3, TypeScript, vite-ssg, PrimeVue 4. Собран по правилам
[AGENTS.md](AGENTS.md). Тестирование в два уровня:

- **Уровень 1, код:** 6 файлов Vitest, 137 тестов на бизнес-логику (`frontend/tests/unit/`).
- **Уровень 2, UI:** 5 smoke-сценариев, которые агент сам прошёл через Playwright MCP на ширинах 1280 и 360,
  со скриншотом на каждом шаге. Плюс Playwright E2E (`frontend/tests/e2e/`): 57 тестов на desktop и mobile.

**Результат:** все проверки зелёные. Итоговый отчёт: [swarm-report/day3-site-tests-2026-10-25.md](swarm-report/day3-site-tests-2026-10-25.md).

## Отчёты

| Файл | Что внутри |
|---|---|
| [day3-site-tests-2026-10-25.md](swarm-report/day3-site-tests-2026-10-25.md) | Итоговый отчёт: задача, Research, план, реализация, оба уровня тестов, проблемы, статус |
| [day3-smoke-report-2026-10-25.md](swarm-report/day3-smoke-report-2026-10-25.md) | Smoke через Playwright MCP: шаги, результаты, скриншоты |
| [day3-site-tests-e2e-scenario.md](swarm-report/day3-site-tests-e2e-scenario.md) | Чеклист smoke-сценариев S1–S5, источник правды для агента |
| [day3-screenshots/](swarm-report/day3-screenshots/) | 39 скриншотов smoke, `<ширина>-s<сценарий>-<шаг>.png` |
| [day3-site-tests-research.md](swarm-report/day3-site-tests-research.md) | Сводка консилиума и решения пользователя |
| [day3-site-tests-plan.md](swarm-report/day3-site-tests-plan.md) | Утверждённый план |
| [day3-site-tests-executing.md](swarm-report/day3-site-tests-executing.md) | Отчёт исполнителя: сверка API, первый прогон тестов, миграция PrimeVue, правки |

## Требования

- Node.js 24 или новее (`frontend/.nvmrc` = `24`), npm.
- Для E2E: Chromium для Playwright, ставится один раз командой ниже.
- Для smoke через MCP: Claude Code и Playwright MCP.

## Запуск сайта

Все команды выполняются из `Day3/frontend/`.

```sh
cd Day3/frontend
npm ci                      # установка строго по package-lock.json
npm run dev                 # dev-сервер с HMR: http://localhost:5173
```

Собранная статика, как на хостинге:

```sh
npm run build               # vite-ssg пререндерит страницы в dist/
npm run preview             # http://localhost:4173
```

В `dist/` появятся `index.html`, `404.html` и `projects/<slug>.html` на каждый файл из `src/content/projects/`.

## Прогон всех тестов

```sh
cd Day3/frontend
npm ci
npx playwright install chromium   # один раз, браузер для E2E

npm run lint        # ESLint, включая правила безопасности (сеть, хранилища, console, v-html)
npm run typecheck   # vue-tsc --noEmit
npm test            # уровень 1: Vitest, 137 тестов
npm run build       # сборка SSG; должна пройти без предупреждений
npm run test:e2e    # Playwright E2E: сам собирает и поднимает preview на :4174, проекты desktop 1280×800 и mobile 360×740
```

Одной строкой, остановка на первой ошибке:

```sh
npm run lint && npm run typecheck && npm test && npm run build && npm run test:e2e
```

Ожидаемый результат: lint и typecheck без вывода, `137 passed`, сборка без warnings, E2E `57 passed, 5 skipped`.
Пропуски штатные: проверка мобильного меню идёт только в проекте mobile, проверка файлов `dist/` только в desktop.

Где смотреть результаты E2E:

- `frontend/test-results/`: скриншот каждого теста (`screenshot: 'on'`);
- `frontend/playwright-report/`: HTML-отчёт, открывается командой `npx playwright show-report`.

Обе папки в `.gitignore`.

Если E2E падает по таймаутам в случайных местах, проверьте, что ноутбук не уходит в сон. На батарее macOS
делает maintenance sleep, и тесты с таймаутом 30 с «длятся» минуты. Порт 4174 должен быть свободен.

### Unit-тесты (уровень 1)

| Файл | Модуль |
|---|---|
| `content-core.spec.ts` | `build/content-core.ts`: валидация frontmatter, рендер и очистка Markdown (XSS-кейсы) |
| `content-index.spec.ts` | `src/content/index.ts`: сортировка проектов и опыта, поиск по slug |
| `useContactForm.spec.ts` | `src/composables/useContactForm.ts`: граничные длины, email, `idle → sending → success/error` |
| `included-routes.spec.ts` | `src/router/includedRoutes.ts`: список страниц для пререндера |
| `router.spec.ts` | маршруты, неизвестный slug ведёт на 404 |
| `sections.spec.ts` | компоненты секций: рендер props, пустой список проектов |

## Smoke через Playwright MCP (уровень 2)

Сценарии описаны в [swarm-report/day3-site-tests-e2e-scenario.md](swarm-report/day3-site-tests-e2e-scenario.md):

1. Якорная навигация по 6 разделам.
2. Карточка проекта → `/projects/<slug>` → «Назад».
3. Неизвестный slug → 404.
4. Форма: ошибки на пустой отправке, затем успешная «отправка» без сетевых запросов.
5. 360px: нет горизонтального скролла, мобильное меню работает, тап-цели не меньше 44px.

Повторный прогон:

```sh
# один раз: подключить Playwright MCP для каталога Day3, затем перезапустить Claude Code из Day3
claude mcp add playwright -- npx @playwright/mcp@latest

# в отдельном терминале поднять сайт
cd Day3/frontend && npm run build && npm run preview
```

Затем попросите Claude Code: «прогони smoke по `swarm-report/day3-site-tests-e2e-scenario.md` через Playwright MCP».
Перед повтором сбросьте отметки `[x]` в файле на `[ ]`. Агент продолжает с первого невыполненного шага, а пройденные
пропускает. Playwright MCP пишет файлы только в `Day3/` и `Day3/.playwright-mcp/`. Временный `.playwright-mcp/`
после прогона можно удалить.

## Структура

```
Day3/
  AGENTS.md          правила проекта: стек, версии, структура, безопасность, критерии готовности
  CLAUDE.md          субагенты и порядок Validation для Claude Code
  README.md          этот файл
  frontend/          код сайта и тесты, описание каталогов в AGENTS.md
  swarm-report/      отчёты и скриншоты
```
