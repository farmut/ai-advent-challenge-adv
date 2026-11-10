.PHONY: build test vet clean help \
        check-tooling workspace clean-workspace run-tooling-demo run-tooling-perm-test \
        sandbox-image test-sandbox \
        proxy-up proxy-wait proxy-down run-loop e2e-loop-fake e2e-limits \
        stand-up stand-down stand-agent

# Day14: everything lives in agent/ (the agent) and git-mcp-server/ (its git
# MCP server). This Makefile only forwards to them. The LLM is reached through
# the Day13 guard proxy configured in agent/agent.config.yaml (llm.base_url);
# the key comes from env LLM_API_KEY only.

AGENT_MAKE_DIR := $(CURDIR)/agent
GIT_MCP_DIR    := $(CURDIR)/git-mcp-server

build: ## Build the agent and the git MCP server
	@$(MAKE) -C $(GIT_MCP_DIR) build
	@$(MAKE) -C $(AGENT_MAKE_DIR) build

test: ## Run unit tests (no network, no LLM)
	@cd $(AGENT_MAKE_DIR) && go test ./...
	@cd $(GIT_MCP_DIR) && go test ./...

vet: ## go vet + gofmt check for the agent
	@cd $(AGENT_MAKE_DIR) && go vet ./... && test -z "$$(gofmt -l .)"

clean: ## Remove build artefacts
	@$(MAKE) -C $(AGENT_MAKE_DIR) clean

# ---------------------------------------------------------------------------
# File tooling + permission model — proxied to agent/Makefile, which owns the
# targets. Read root is Day14/, write root is Day14/workspace/.
# ---------------------------------------------------------------------------
check-tooling: ## Verify npx and fff-mcp are installed
	@$(MAKE) -C $(AGENT_MAKE_DIR) check-tooling

workspace: ## Create the agent's write root (workspace/)
	@$(MAKE) -C $(AGENT_MAKE_DIR) workspace

clean-workspace: ## Clear workspace/ (not part of `clean`)
	@$(MAKE) -C $(AGENT_MAKE_DIR) clean-workspace

run-tooling-demo: ## Demo: read + search + analyse + write a report into workspace/ (TOOLING_TASK=)
	@$(MAKE) -C $(AGENT_MAKE_DIR) run-tooling-demo $(if $(TOOLING_TASK),TOOLING_TASK="$(TOOLING_TASK)",) DEBUG="$(DEBUG)"

run-tooling-perm-test: ## Negative test: writing outside workspace/ must fail with READ_ONLY_PATH
	@$(MAKE) -C $(AGENT_MAKE_DIR) run-tooling-perm-test DEBUG="$(DEBUG)"

sandbox-image: ## Build the docker sandbox image (generated code/tests run only there)
	@$(MAKE) -C $(AGENT_MAKE_DIR) sandbox-image

test-sandbox: ## Live sandbox isolation tests (needs docker + sandbox-image)
	@$(MAKE) -C $(AGENT_MAKE_DIR) test-sandbox

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*##' $(firstword $(MAKEFILE_LIST)) \
		| awk 'BEGIN{FS=":.*##"}{printf "  %-24s %s\n", $$1, $$2}'

# ---------------------------------------------------------------------------
# Execution loop e2e (plan PR9/section 4): fake upstream -> guard proxy ->
# agent --loop, with the real docker sandbox. The proxy keys come from env
# PROXY_API_KEYS only; the agent key is LLM_API_KEY (both are the long synthetic key below).
# ---------------------------------------------------------------------------
DAY13_DIR      := $(CURDIR)/../Day13
PROXY_BIN      := $(DAY13_DIR)/bin/llm-proxy
FAKE_BIN       := $(DAY13_DIR)/bin/fake-upstream
FAKE_LISTEN    := 127.0.0.1:18099
FAKE_SCRIPT    := $(CURDIR)/agent/testdata/loop/fake_script.json
LOOP_E2E_CFG   := testdata/loop/agent.e2e.yaml
AGENT_BIN      := ./ai-adv-agent-agent   # agent/Makefile BINARY_NAME = ai-adv-agent-<basename(agent)>
E2E_KEY        := e2e-key-0123456789abcdef0123456789abcd

proxy-build: ## Build the Day13 proxy and fake-upstream binaries
	@$(MAKE) -C $(DAY13_DIR) proxy-build fake-upstream

proxy-up: proxy-build ## Start the guard proxy (PROXY_CONFIG default configs/proxy.e2e.yaml) in the background
	@if [ -f .proxy.pid ] && kill -0 $$(cat .proxy.pid) 2>/dev/null; then \
		echo "proxy already running (pid $$(cat .proxy.pid))"; exit 0; \
	fi
	@set -a; . $(DAY13_DIR)/.env 2>/dev/null || true; set +a; \
	PROXY_CONFIG=$(or $(PROXY_CONFIG),$(DAY13_DIR)/configs/proxy.e2e.yaml) \
	PROXY_API_KEYS=$(E2E_KEY) nohup $(PROXY_BIN) --config $(or $(PROXY_CONFIG),$(DAY13_DIR)/configs/proxy.e2e.yaml) \
		> proxy-e2e.log 2>&1 & echo $$! > .proxy.pid
	@echo "proxy pid $$(cat .proxy.pid), log: proxy-e2e.log"

proxy-wait: ## Wait until the proxy answers /readyz (up to 30s)
	@for i in $$(seq 1 30); do \
		if curl -fsS http://127.0.0.1:8090/readyz >/dev/null 2>&1; then echo "proxy is up"; exit 0; fi; \
		sleep 1; \
	done; echo "proxy did not start, see proxy-e2e.log"; tail -20 proxy-e2e.log; exit 1

proxy-down: ## Stop the background proxy
	@if [ -f .proxy.pid ]; then kill $$(cat .proxy.pid) 2>/dev/null || true; rm -f .proxy.pid; echo "proxy stopped"; else echo "no proxy pid file"; fi

run-loop: proxy-wait ## Run the agent loop against the running proxy (GOAL="..." or GOAL=@file.md, AGENT_CONFIG, extra args via ARGS)
	@if [ -z "$(GOAL)" ]; then echo "GOAL is required: make run-loop GOAL='@agent/testdata/loop/sqli_goal.md'"; exit 2; fi
	@cd $(AGENT_MAKE_DIR) && LLM_API_KEY=$(E2E_KEY) $(AGENT_BIN) --config $(or $(AGENT_CONFIG),$(LOOP_E2E_CFG)) --loop "$(GOAL)" $(ARGS)

e2e-loop-fake: sandbox-image ## Full deterministic loop e2e (plan section 4): fake upstream + proxy + agent + docker sandbox
	@set -e; \
	trap 'kill $$FAKE_PID $$PROXY_PID 2>/dev/null || true; rm -f .fake.pid .proxy.pid' EXIT; \
	$(MAKE) -C $(DAY13_DIR) proxy-build fake-upstream; \
	$(MAKE) -C $(AGENT_MAKE_DIR) build; \
	$(FAKE_BIN) -listen $(FAKE_LISTEN) -script $(FAKE_SCRIPT) > fake-upstream.log 2>&1 & FAKE_PID=$$!; \
	PROXY_API_KEYS=$(E2E_KEY) $(PROXY_BIN) --config $(DAY13_DIR)/configs/proxy.loop-e2e.yaml > proxy-e2e.log 2>&1 & PROXY_PID=$$!; \
	$(MAKE) proxy-wait; \
	cd $(AGENT_MAKE_DIR) && LLM_API_KEY=$(E2E_KEY) $(AGENT_BIN) --config $(LOOP_E2E_CFG) --loop "@testdata/loop/sqli_goal.md"; \
	echo "e2e-loop-fake: agent exit 0 (loop done)"

e2e-limits: ## Limit-only e2e (plan section 7.3): proxy.limits.yaml + agent.limits.yaml
	@cd $(AGENT_MAKE_DIR) && go test -tags limits -count=1 -v ./e2e/...

# ---------------------------------------------------------------------------
# Полный стенд для ручной проверки: модель -> прокси -> агент.
#   make stand-up [UPSTREAM=ollama|mlx|lmstudio|cloud] [STAND_MODEL=<alias>]
#   make stand-agent [ARGS="--query ..."]   (или --interactive)
#   make stand-down
# Бэкенды и прокси стартуют в фоне (PID-файлы и логи в .stand/), повторный
# stand-up идемпотентен: уже живые компоненты не перезапускает.
# ---------------------------------------------------------------------------
# Бэкенд, выбранный при stand-up, запоминается в .stand/upstream, чтобы
# stand-agent не приходилось дублировать UPSTREAM=; переопределение —
# аргументом: make stand-agent UPSTREAM=lmstudio
UPSTREAM = $(shell cat $(CURDIR)/.stand/upstream 2>/dev/null || echo ollama)
# Алиас агента следует за бэкендом автоматически (cloud → deepseek-flash/OpenRouter);
# переопределение — только аргументом: make stand-agent STAND_MODEL=<alias>
STAND_MODEL    = $(if $(filter cloud,$(UPSTREAM)),deepseek-flash,qwen2.5-7b-$(UPSTREAM))
STAND_DIR      := $(CURDIR)/.stand
PROXY_KEY      := $(shell sed -n 's/^PROXY_API_KEYS=//p' $(DAY13_DIR)/.env 2>/dev/null | cut -d, -f1)

$(STAND_DIR):
	@mkdir -p "$(STAND_DIR)"

stand-up: $(STAND_DIR) ## Поднять стенд: backend (UPSTREAM) + прокси + сборка агента
	@if [ -z "$(PROXY_KEY)" ]; then \
		echo "Нет ключей: создайте $(DAY13_DIR)/.env (см. README Day13, раздел «Ключи»)"; exit 1; \
	fi
	@echo "$(UPSTREAM)" > "$(STAND_DIR)/upstream"
	@case "$(UPSTREAM)" in \
		ollama) \
			if curl -fsS -m 2 http://127.0.0.1:11434/v1/models >/dev/null 2>&1; then \
				echo "[stand] ollama уже запущен"; \
			else \
				echo "[stand] стартую ollama serve..."; \
				nohup ollama serve > "$(STAND_DIR)/ollama.log" 2>&1 & echo $$! > "$(STAND_DIR)/ollama.pid"; \
				for i in $$(seq 1 30); do curl -fsS -m 2 http://127.0.0.1:11434/v1/models >/dev/null 2>&1 && break; sleep 1; done; \
			fi; \
			if ollama list 2>/dev/null | awk 'NR>1 {print $$1}' | grep -qx "$(or $(OLLAMA_MODEL),qwen2.5:7b-instruct)"; then \
				echo "[stand] модель $(or $(OLLAMA_MODEL),qwen2.5:7b-instruct) уже установлена"; \
			else \
				$(MAKE) -C $(DAY13_DIR) model UPSTREAM=ollama $(if $(OLLAMA_MODEL),OLLAMA_MODEL="$(OLLAMA_MODEL)",) || exit 1; \
			fi ;; \
		mlx) \
			if curl -fsS -m 2 http://127.0.0.1:18081/v1/models >/dev/null 2>&1; then \
				echo "[stand] mlx_lm.server уже запущен"; \
			else \
				$(MAKE) -C $(DAY13_DIR) setup model; \
				echo "[stand] стартую mlx_lm.server..."; \
				nohup $(MAKE) -C $(DAY13_DIR) proxy-mlx-upstream > "$(STAND_DIR)/mlx.log" 2>&1 & echo $$! > "$(STAND_DIR)/mlx.pid"; \
				for i in $$(seq 1 60); do curl -fsS -m 2 http://127.0.0.1:18081/v1/models >/dev/null 2>&1 && break; sleep 2; done; \
			fi ;; \
		lmstudio) \
			if curl -fsS -m 2 http://127.0.0.1:1234/v1/models >/dev/null 2>&1; then \
				echo "[stand] LM Studio server уже запущен"; \
			else \
				$(MAKE) -C $(DAY13_DIR) model UPSTREAM=lmstudio || exit 1; \
				lms server start --port 1234 || exit 1; \
			fi ;; \
		cloud) \
			if [ -z "$$OPENAI_API_KEY" ] && [ -z "$$OPENROUTER_API_KEY" ] && [ -z "$$LLM_API_KEY" ]; then \
				echo "[stand] UPSTREAM=cloud требует env: LLM_API_KEY (OpenRouter, алиас deepseek-flash) или OPENAI_API_KEY (gpt-4.1-mini)"; exit 1; \
			fi; \
			if [ -z "$$OPENROUTER_API_KEY" ] && [ -n "$$LLM_API_KEY" ]; then \
				export OPENROUTER_API_KEY="$$LLM_API_KEY"; \
				echo "[stand] OPENROUTER_API_KEY взят из LLM_API_KEY"; \
			fi; \
			echo "[stand] облачный маршрут, бэкенд не нужен" ;; \
		*) echo "неизвестный UPSTREAM=$(UPSTREAM) (ollama|mlx|lmstudio|cloud)"; exit 2 ;; \
	esac
	@$(MAKE) -C $(DAY13_DIR) proxy-build
	@if [ -f "$(STAND_DIR)/proxy.pid" ] && kill -0 $$(cat "$(STAND_DIR)/proxy.pid") 2>/dev/null; then \
		echo "[stand] прокси уже запущен (pid $$(cat "$(STAND_DIR)/proxy.pid"))"; \
	else \
		echo "[stand] стартую llm-proxy (configs/proxy.yaml)..."; \
		set -a; . $(DAY13_DIR)/.env; set +a; \
		nohup $(PROXY_BIN) --config $(DAY13_DIR)/configs/proxy.yaml > "$(STAND_DIR)/proxy.log" 2>&1 & echo $$! > "$(STAND_DIR)/proxy.pid"; \
	fi
	@$(MAKE) proxy-wait
	@$(MAKE) -C $(AGENT_MAKE_DIR) build
	@if ! docker image inspect ai-adv-sandbox:1 >/dev/null 2>&1; then \
		echo "[stand] ВНИМАНИЕ: образ ai-adv-sandbox:1 не найден — execution loop не стартует (make sandbox-image)"; \
	fi
	@echo ""
	@echo "Стенд поднят: модель ($(UPSTREAM)) -> прокси (:8090) -> агент собран."
	@echo "  Агент:   make stand-agent ARGS=\"--interactive\""
	@echo "           make stand-agent ARGS=\"--query \\\"твоя задача\\\"\""
	@echo "  Loop:    make run-loop GOAL='@agent/testdata/loop/sqli_goal.md'"
	@echo "  Стоп:    make stand-down"

stand-agent: ## Запустить агента против поднятого стенда (ARGS="--interactive" или --query "…", AGENT_CONFIG=path.yaml)
	@if [ -z "$(PROXY_KEY)" ]; then echo "сначала make stand-up"; exit 1; fi
	@cd $(AGENT_MAKE_DIR) && LLM_API_KEY="$(PROXY_KEY)" ./ai-adv-agent-agent \
		--config $(or $(AGENT_CONFIG),agent.config.yaml) --model "$(STAND_MODEL)" $(ARGS)

stand-down: ## Остановить стенд (proxy и бэкенд, запущенные stand-up)
	@for name in proxy mlx ollama; do \
		if [ -f "$(STAND_DIR)/$$name.pid" ]; then \
			kill $$(cat "$(STAND_DIR)/$$name.pid") 2>/dev/null && echo "[stand] $$name остановлен"; \
			rm -f "$(STAND_DIR)/$$name.pid"; \
		fi; \
	done
	@echo "[stand] стенд остановлен (LM Studio останавливается вручную: lms server stop)"

.DEFAULT_GOAL := help
