# Day15: обёртки над стендом Day14.
#
# Вся логика — в ../Day14/Makefile (stand-up/stand-agent/…) и ../Day13/Makefile
# (proxy, fake-upstream, e2e). Здесь — точки входа «из Day15» + preflight/verify,
# чтобы не запоминать относительные пути. Подробности — в README.md этого каталога.

.PHONY: help stand-up stand-down stand-agent doctor verify sandbox-image \
        run-loop e2e-loop-fake proxy-up proxy-down

DAY14 := $(abspath ../Day14)
DAY13 := $(abspath ../Day13)

help: ## Список целей
	@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*##"}{printf "  %-18s %s\n", $$1, $$2}'

# ---------------------------------------------------------------- стенд ----

stand-up: ## Поднять стенд: backend (UPSTREAM=ollama|lmstudio|mlx|cloud) + прокси + сборка агента
	@$(MAKE) -C $(DAY14) stand-up UPSTREAM=$(or $(UPSTREAM),ollama)

stand-down: ## Остановить поднятое стендом
	@$(MAKE) -C $(DAY14) stand-down

stand-agent: ## Агент через прокси (ARGS="--interactive"|--query "…", STAND_MODEL, AGENT_CONFIG)
	@$(MAKE) -C $(DAY14) stand-agent $(if $(ARGS),ARGS="$(ARGS)",) \
		$(if $(STAND_MODEL),STAND_MODEL="$(STAND_MODEL)",) \
		$(if $(AGENT_CONFIG),AGENT_CONFIG="$(AGENT_CONFIG)",)

# ------------------------------------------------------- loop и sandbox ----

sandbox-image: ## Собрать docker-образ песочницы (нужен docker, один раз)
	@$(MAKE) -C $(DAY14) sandbox-image

run-loop: ## Запустить execution loop (GOAL="…" или GOAL=@file), лимит $1 из конфига
	@$(MAKE) -C $(DAY14) run-loop $(if $(GOAL),GOAL="$(GOAL)",)

e2e-loop-fake: ## Детерминированный loop-e2е без модели (fake + прокси + агент + sandbox)
	@$(MAKE) -C $(DAY14) e2e-loop-fake

# ------------------------------------------------------- ручной прокси ----

proxy-up: ## Поднять прокси Day13 в фоне (конфиг configs/proxy.yaml)
	@$(MAKE) -C $(DAY14) proxy-up

proxy-down: ## Остановить фоновый прокси
	@$(MAKE) -C $(DAY14) proxy-down

# -------------------------------------------------------- проверки ---------

doctor: ## Preflight: go, docker, .env, порты 8090/11434/1234/18081
	@fail=""; \
	command -v go >/dev/null || { echo "  [fail] go не найден (нужен 1.26)"; fail=1; }; \
	command -v docker >/dev/null || { echo "  [warn] docker не найден — loop не запустится (exit 4)"; }; \
	[ -f $(DAY13)/.env ] || { echo "  [fail] нет $(DAY13)/.env — см. README §3"; fail=1; }; \
	grep -q '^PROXY_API_KEYS=.\{32,\}' $(DAY13)/.env 2>/dev/null \
		|| { echo "  [fail] PROXY_API_KEYS в .env пустой или короткий"; fail=1; }; \
	for p in 8090 11434 1234 18081; do \
		lsof -nP -iTCP:$$p -sTCP:LISTEN >/dev/null 2>&1 && echo "  [info] :$$p занят"; \
	done; \
	[ -z "$$fail" ] && echo "doctor: OK" || { echo "doctor: есть проблемы"; exit 1; }

verify: ## Проверка звеньев: бэкенд, /readyz прокси, chat через прокси с X-Guard-*
	@set -a; . $(DAY13)/.env; set +a; \
	key="$${PROXY_API_KEYS%%,*}"; \
	if curl -fsS -m 2 http://127.0.0.1:11434/v1/models >/dev/null 2>&1; then be=ollama; \
	elif curl -fsS -m 2 http://127.0.0.1:1234/v1/models >/dev/null 2>&1; then be=lmstudio; \
	elif curl -fsS -m 2 http://127.0.0.1:18081/v1/models >/dev/null 2>&1; then be=mlx; \
	else be=cloud/none; fi; echo "backend: $$be"; \
	curl -fsS -m 5 http://127.0.0.1:8090/readyz >/dev/null \
		&& echo "proxy /readyz: OK" || { echo "proxy /readyz: FAIL"; exit 1; }; \
	ids=$$(curl -fsS -m 5 http://127.0.0.1:8090/v1/models -H "Authorization: Bearer $$key" 2>/dev/null | \
		grep -o '"id": *"[^"]*"' | sed 's/.*: *"//; s/"$$//'); \
	[ -n "$$ids" ] || { echo "chat via proxy: FAIL (список моделей пуст)"; exit 1; }; \
	ok=""; for m in $$ids; do \
		code=$$(curl -s -m 120 http://127.0.0.1:8090/v1/chat/completions \
			-H "Authorization: Bearer $$key" -H 'Content-Type: application/json' \
			-d "{\"model\":\"$$m\",\"messages\":[{\"role\":\"user\",\"content\":\"Say OK\"}],\"max_tokens\":16}" \
			-D /tmp/day15-verify-headers -o /dev/null -w '%{http_code}'); \
		if [ "$$code" = "200" ]; then ok=$$m; echo "chat via proxy: 200 (model=$$m)"; break; fi; \
		echo "  model $$m -> $$code"; \
	done; \
	[ -n "$$ok" ] || { echo "chat via proxy: FAIL (ни одна модель не ответила 200)"; exit 1; }; \
	grep -i '^X-Guard-Input:\|^X-Upstream-Model:' /tmp/day15-verify-headers | sed 's/^/  /'
