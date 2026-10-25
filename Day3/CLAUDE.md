@AGENTS.md

# Claude Code: субагенты Day3

Глобальный `~/.claude/CLAUDE.md` ссылается на эти назначения. Backend в Day3 нет, поэтому Go-агенты не назначаются.

| Назначение                       | Frontend (Vue/TS)                |
|----------------------------------|----------------------------------|
| Executing (фича) / Fix (баг)     | `voltagent-lang:vue-expert`      |
| Архитектор (Research, Diagnose)  | `voltagent-lang:vue-expert`      |
| Фронтенд-эксперт (Research)      | `voltagent-lang:vue-expert`      |
| UI-дизайнер (Research)           | `voltagent-core-dev:ui-designer` |

Консилиум Research для Day3: Архитектор, Фронтенд-эксперт, UI-дизайнер, Безопасность.
DevOps, API-дизайнер и DBA не запускать: деплоя, API и БД нет.

## Документация

API Vue, Vite, vite-ssg, PrimeVue, vue-router, Vitest и Playwright сверять через context7
(`resolve-library-id` → `query-docs`). Код по памяти не писать: у PrimeVue 4 и vite-ssg 28 API отличается от старых версий. PrimeVue 5 не ставить: лицензия PrimeUI требует ключ.

## Validation

- Платформа: Web. Backend-проверок нет.
- Сборка и тесты: команды из раздела «Команды» в `AGENTS.md`, через Bash.
- UI-сценарии: Playwright MCP (`mcp__playwright__*`) по `npm run preview`, ширины 360 и 1280px. Подключение: `claude mcp add playwright -- npx @playwright/mcp@latest`.
  Если Playwright MCP не подключён, сказать об этом пользователю и опираться на Playwright E2E. Молча шаг не пропускать.
