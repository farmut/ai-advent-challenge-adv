# Plan: Day3 — сайт-визитка + тесты (2026-10-25)
Источник: агент Plan. Полный текст ниже сжат до ключевых решений.

- Шестой раздел для smoke S1: Hero `id="hero"`, пункт меню «Главная» → `/#hero` (AGENTS.md перечисляет 5 якорей).
- Шаг 0: сверка через context7 — vite-ssg 28 (includedRoutes/dirStyle/ClientOnly/htmlAttrs), Vite 8 rolldown (moduleType, glob, preview flat-html), PrimeVue 5 (Menubar+хэш, SSR-стили, Button as, Message, definePreset), vitest 5 projects, TS 6 types, ESLint 10 flat, Playwright 1.64. После install: npm ls (одна версия unhead 2.x/vue/router/vite), peer-проверка, npm audit --omit=dev.
- Э1 каркас: package.json, .npmrc engine-strict, .nvmrc, .gitignore, index.html lang=ru, 4 tsconfig, eslint.config.ts (security-правила), vite/vitest/playwright config, env.d.ts, md.d.ts. Критерий: npm ci && lint && typecheck.
- Э2 контент-ядро: types.ts; build/content-core.ts (parseContentFile, validate*, renderMarkdown, sanitizeContentHtml, assertSafeUrl, ContentError); build/content-plugin.ts (load-хук + generateBundle-проверка moduleIds).
- Э3 контент: профиль «Алексей Вымыслов»; проекты task-tracker (cover), weather-dashboard, markdown-notes; опыт studio-pixel, fintech-lab, web-agency. content/index.ts: sortProjects, sortExperience, findProjectBySlug, getProjectSlugs + glob.
- Э4 роутинг/SSG: routes.ts (beforeEnter → not-found), scrollBehavior.ts, includedRoutes.ts (buildIncludedRoutes), main.ts, theme/preset.ts (indigo, 44px), CSP meta (build only). Критерий: build + dist файлы + нет build-библиотек в dist/assets.
- Э5 форма: useContactForm({delayMs, simulate}).
- Э6 UI: tokens.css, ui/(MarkdownContent, SectionHeading, ProjectCard, AppHeader, AppFooter), 6 секций, 3 страницы, App.vue; testid по списку.
- Э7 E2E: home, project, prerender, not-found, contact, layout.
- Unit (уровень 1): content-core, content-index, useContactForm, included-routes, sections, router — 6 spec-файлов. Первый прогон фиксируется как есть.
- Validation: swarm-report/day3-site-tests-e2e-scenario.md, S1–S5 × {360,1280}, скриншоты swarm-report/day3-screenshots/<w>-s<N>-<step>.png.
- Fallback по рискам: includedRoutes → ssgOptions; preview → запрос .html; moduleType опционально; Menubar → слот #item с RouterLink, SVG-иконка; FOUC → cssLayer/зафиксировать; vitest projects → комментарий окружения; dedupe.
