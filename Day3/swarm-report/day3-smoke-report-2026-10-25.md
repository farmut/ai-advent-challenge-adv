# Day3 smoke-отчёт (UI, Playwright MCP) — 2026-10-25

Инструмент: Playwright MCP (`mcp__playwright__*`), Chromium, сайт собран `vite-ssg`, `npm run preview` на http://localhost:4173. Ширины 1280x800 и 360x740. Источник правды: `day3-site-tests-e2e-scenario.md` (все шаги `[x]`, 0 невыполненных).
Предыдущая стадия: unit 137/137, build без warnings, E2E 57 passed / 5 skipped.

Метод: клики и проверки через Playwright (`getByTestId`/`getByRole`), замеры через evaluate, часть шагов объединена в один вызов `browser_run_code_unsafe` со скриншотом на каждом шаге. Сеть S4 проверена и `browser_network_requests` (до/после), и слушателем `page.on('request')`.

## Итог
| Сценарий | 1280 | 360 | Примечание |
|---|---|---|---|
| S1 Якорная навигация | OK | OK | hero/contact на 1280 не ниже шапки из-за границ страницы |
| S2 Карточка -> страница -> назад | OK | OK | |
| S3 404 | OK | OK | |
| S4 Форма | OK | OK | состояние sending поймано |
| S5 Мобильная вёрстка | - | OK | минор по aria-label |

## S1. Якорная навигация
- 1.1 [1280] `/`: nav и h1 «Алексей Вымыслов» на месте.
- 1.2 [1280] 6 кликов по `nav-<id>`: URL `#<id>` верный. Высота шапки 52px. Заголовок раздела top=124 (about, skills, projects, experience), ниже шапки с зазором 72px, в допуске (<132). hero: top=181 (scrollY=0, верх страницы), contact: top=166 (конец страницы, прокрутить дальше нельзя). Формально вне окна «<h+80», но это границы документа, не дефект.
- 1.3 [360] бургер «Открыть меню» виден, пункты скрыты.
- 1.4 [360] все 6: меню открывается, пункт кликается, URL `#<id>`, заголовок top≈108 (hero 150 — верх страницы), меню закрывается (aria-expanded=false).
![](day3-screenshots/1280-s1-1.1-home.png)
![](day3-screenshots/1280-s1-1.2-about.png)
![](day3-screenshots/1280-s1-1.2-projects.png)
![](day3-screenshots/1280-s1-1.2-contact.png)
![](day3-screenshots/360-s1-1.3-home.png)
![](day3-screenshots/360-s1-1.4-menu-open.png)
![](day3-screenshots/360-s1-1.4-experience.png)
Остальные: `day3-screenshots/1280-s1-1.2-hero|skills|experience.png`, `day3-screenshots/360-s1-1.4-hero|about|skills|projects|contact.png`.

## S2. Карточка проекта -> страница -> «Назад»
- 2.1 порядок карточек task-tracker, weather-dashboard, markdown-notes.
- 2.2 клик -> `/projects/task-tracker`, `project-title` «Трекер задач для небольших команд», описание из Markdown видно (текст 641 символ).
- 2.3 `back-link` -> `/#projects`, заголовок «Проекты» под шапкой (top=124).
- 2.4 то же на 360 (top=108).
![](day3-screenshots/1280-s2-2.1-cards.png)
![](day3-screenshots/1280-s2-2.2-project.png)
![](day3-screenshots/1280-s2-2.3-back.png)
![](day3-screenshots/360-s2-2.4-cards.png)
![](day3-screenshots/360-s2-2.4-project.png)
![](day3-screenshots/360-s2-2.4-back.png)

## S3. 404
- 3.1 `/projects/unknown-slug`: `not-found`, h1 «Страница не найдена», кнопка «На главную».
- 3.2 клик -> `/`.
- 3.3 [360] `/foo/bar`: `not-found`, scrollWidth 360 = innerWidth.
![](day3-screenshots/1280-s3-3.1-404.png)
![](day3-screenshots/1280-s3-3.2-home.png)
![](day3-screenshots/360-s3-3.3-404.png)

## S4. Форма
- 4.1 `demo-note` «Это демо: сообщение никуда не отправляется» виден.
- 4.2 пустой submit: «Укажите имя», «Укажите email», «Напишите сообщение», status idle.
- 4.3 после ввода валидных данных ошибки исчезли.
- 4.4 клик submit: `data-status=sending` пойман сразу после клика, затем `success` («Спасибо! Сообщение «отправлено».»).
- 4.5 новых запросов нет (listener: 0; `browser_network_requests` до и после идентичны — 4 запроса загрузки страницы: doc, js, css, svg), localStorage=0, sessionStorage=0, cookie пуст, URL `/#contact` не менялся.
- 4.6 повтор на 360: идентичный результат.
- Перепроверка оркестратором: исходные кадры `*-success.png` сняты во время анимации появления Message — сообщение на них не видно. Через Playwright MCP проверено: после `success` у `.p-message` opacity 1, высота 41px, текст виден. Кадры 4.4/4.6-success переснятые (через 1 с после `success`). Дефекта кода нет; урок для smoke — ждать окончания анимации перед скриншотом.
![](day3-screenshots/1280-s4-4.1-contact.png)
![](day3-screenshots/1280-s4-4.2-errors.png)
![](day3-screenshots/1280-s4-4.3-filled.png)
![](day3-screenshots/1280-s4-4.4-sending.png)
![](day3-screenshots/1280-s4-4.4-success.png)
![](day3-screenshots/360-s4-4.6-errors.png)
![](day3-screenshots/360-s4-4.6-sending.png)
![](day3-screenshots/360-s4-4.6-success.png)

## S5. Мобильная вёрстка 360
- 5.1 `/`: scrollWidth 345 <= 360. 5.2 `/projects/task-tracker`: 345 <= 360. 5.3 `/404`: 360 <= 360. (345 = ширина без классической полосы прокрутки 15px.)
- 5.4 бургер 44x44, пункты меню 303x44 (>=44), закрытие работает.
- 5.5 подвал «Демонстрационный сайт, персона вымышлена» виден.
![](day3-screenshots/360-s5-5.1.png)
![](day3-screenshots/360-s5-5.2.png)
![](day3-screenshots/360-s5-5.3.png)
![](day3-screenshots/360-s5-5.4-open.png)
![](day3-screenshots/360-s5-5.4-closed.png)
![](day3-screenshots/360-s5-5.5-footer.png)

## Консоль
`browser_console_messages` (уровень warning, за всю сессию): 0 сообщений, 0 ошибок, 0 предупреждений.

## Найденные проблемы
Критичных нет, ни один шаг не упал.
1. Минор (a11y): у бургера aria-label статичный «Открыть меню» и остаётся таким при открытом меню. `Day3/frontend/src/components/ui/AppHeader.vue:54` (`:button-props="{ 'aria-label': 'Открыть меню' }"`). Гипотеза: label не зависит от состояния меню; aria-expanded при этом ведёт PrimeVue.
2. Наблюдение: на 1280 заголовки hero и contact не оказываются «сразу под шапкой» (181/166px) — следствие верха/низа страницы, а не scroll-margin. Проверка в сценарии для них неприменима строго.

## Вывод
Все 5 сценариев пройдены на обеих ширинах (S5 только 360): навигация, карточки/назад, 404, форма без сетевой активности и хранения данных, нет горизонтального скролла, тап-цели >=44px. Консоль чистая. Скриншотов: 39.
