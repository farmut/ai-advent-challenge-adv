# Day1: CLAUDE.md A против B

Эксперимент: один промпт ([prompt.txt](prompt.txt), Telegram-бот погоды на Go с OpenWeatherMap) и два `CLAUDE.md`.
A содержит 16 строк общих пожеланий, B — 594 строки конкретики (стек, слои, папки, контракты, примеры, антипаттерны, шаблон).

**Итоги:** [REPORT.md](REPORT.md) и [report.html](report.html). Review 22/32 против 30/32, black-box 13/14 против 14/14.

## Структура

```
prompt.txt, env.txt        промпт, версии, флаги запуска, sha256
variant-{a,b}/CLAUDE.md    правила варианта
variant-{a,b}/src          сгенерированный код (не правился)
variant-{a,b}/run.json     итог `claude -p`: ходы, время, стоимость
compare/metrics.sh         сбор метрик → metrics-{a,b}.txt
compare/review.md          ручное review, баги
compare/diff.patch         diff -ruN variant-a/src variant-b/src
compare/harness/           black-box стенд (фейковые Telegram и OWM)
compare/transcripts/       результаты стенда, логи ботов
compare/screenshots/       скриншоты чатов и отчёта
```

## Как воспроизвести

1. Генерация: в пустом каталоге (`git init`) положить `CLAUDE.md` варианта и выполнить
   `claude -p "$(cat prompt.txt)" --setting-sources project --model claude-opus-5-5 --permission-mode bypassPermissions --output-format json --max-budget-usd 6 > run.json`.
2. Метрики: `compare/metrics.sh variant-a/src compare/metrics-a.txt` (для B аналогично).
   Нужен golangci-lint, собранный под go1.26, путь к нему передаётся в `GOLANGCI_LINT`.
3. Black-box из корня репозитория: `go -C Day1/compare/harness run . -variant all`.
   Для A нужен Docker с `alpine:latest`.
4. Рендер чатов и скриншоты: `go -C Day1/compare/harness run ./cmd/render -screenshots` (нужен Google Chrome).

Ограничение: по одному прогону на вариант (n = 1).
