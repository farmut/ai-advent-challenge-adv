.PHONY: build run test vet bench bench-compare lint clean preflight e2e-injection-off e2e-injection-on e2e-injection-summary \
	proxy-build proxy proxy-e2e proxy-mlx-upstream test-limits e2e-proxy e2e-proxy-mlx e2e-proxy-compat e2e-proxy-summary audit-report fmt-check arch-check \
	setup model fake-upstream

BINARY=bin/agile-consultant
CONFIG=configs/app.yaml
TS := $(shell date +%Y%m%d-%H%M%S)
E2E_INJECTION=go test -tags e2e -count=1 -timeout 30m -v ./e2e/injection/

build:
	go build -o $(BINARY) ./cmd/agile-consultant

run: build
	./$(BINARY) --config $(CONFIG)

test:
	go test ./...

vet:
	go vet ./...
	go vet -tags e2e ./...

bench: build
	./$(BINARY) bench --config $(CONFIG) --model tuned

bench-compare: build
	./scripts/benchmark.sh

lint: vet
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed, skipping"; \
	fi

clean:
	rm -rf bin/ logs/*.log

fmt-check:
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

# Limit checks only (fake clock, no sleeps): rate limit and auth bruteforce from
# configs/proxy.limits.yaml, limit config validation. Functional tests and e2e
# use configs/proxy.e2e.yaml (high rate limit).
test-limits:
	go test -count=1 -run 'Limits' ./internal/app/ ./internal/config/

# Clean Architecture guard: usecase/ports must not import Gin/YAML/adapters; Gin only in delivery/http.
arch-check:
	go test -count=1 -run TestArchitecture ./internal/app/

preflight: fmt-check test vet arch-check build proxy-build
	@echo "Preflight checks passed"

# Prompt-injection e2e against the local baseline model (starts mlx_lm.server on 127.0.0.1:18080).
# Both runs use prompt v2 with real system/user roles; only the guard in code differs.
E2E_PROMPT=$(CURDIR)/configs/prompts/consultant.v2.txt

# guard off: baseline, records how far the prompt alone holds (PASS = no call errors).
e2e-injection-off:
	PROMPT_FILE=$(E2E_PROMPT) E2E_GUARD=off E2E_OUT=$(CURDIR)/reports/injection/v2-off-$(TS) $(E2E_INJECTION)

# guard on: sanitizer, policy, nonce boundary, output validation; expect_on=blocked cases must not be delivered.
e2e-injection-on:
	PROMPT_FILE=$(E2E_PROMPT) E2E_GUARD=on E2E_OUT=$(CURDIR)/reports/injection/v2-on-$(TS) $(E2E_INJECTION)

# Report «v2 · guard off» vs «v2 · guard on» from the latest matching runs (no model needed).
e2e-injection-summary:
	go run ./cmd/injection-summary --runs $(CURDIR)/reports/injection --prompts $(CURDIR)/configs/prompts --cases $(CURDIR)/e2e/injection/testdata/cases.json --out $(CURDIR)/reports/injection/summary-$(TS) --copy-to $(CURDIR)/reports/summary

# ---------------------------------------------------------------------------
# Day13: guard-proxy (OpenAI-compatible) in front of LLM APIs.
PROXY_BINARY=bin/llm-proxy
PROXY_CONFIG=configs/proxy.yaml
PROXY_MLX_PORT=18081
MODEL_DIR ?= models/baseline-qwen2.5-7b-4bit
PROXY_MLX_MODEL=$(CURDIR)/$(MODEL_DIR)
# mlx_lm.server: .venv from "make setup", then PATH, then the shared ../Day6/.venv.
MLX_SERVER_BIN ?= $(shell if [ -x '$(CURDIR)/.venv/bin/mlx_lm.server' ]; then echo '$(CURDIR)/.venv/bin/mlx_lm.server'; \
	else command -v mlx_lm.server 2>/dev/null || echo '$(CURDIR)/../Day6/.venv/bin/mlx_lm.server'; fi)
E2E_PROXY=go test -tags e2e -count=1 -v ./e2e/proxy/
AUDIT ?= $(CURDIR)/logs/audit/proxy-audit.jsonl

proxy-build:
	go build -o $(PROXY_BINARY) ./cmd/llm-proxy

# Requires PROXY_API_KEYS (comma-separated Bearer keys, 32-256 chars) and listens
# on 127.0.0.1:8090; OPENAI_API_KEY enables the cloud route.
proxy: proxy-build
	./$(PROXY_BINARY) --config $(PROXY_CONFIG)

# Proxy with the fast test config (rate limit 100000/min, fake-coder route to the
# fake upstream on 127.0.0.1:18099); for Day14 e2e. Needs PROXY_API_KEYS.
proxy-e2e: proxy-build
	./$(PROXY_BINARY) --config configs/proxy.e2e.yaml

# Deterministic fake upstream binary for the Day14 loop e2e (make e2e-loop-fake):
# scripted OpenAI-compatible server, roles from a JSON script
# (see internal/testkit/fakeupstream). Requires -script on run.
FAKE_UPSTREAM_BINARY=bin/fake-upstream

fake-upstream:
	go build -o $(FAKE_UPSTREAM_BINARY) ./cmd/fake-upstream

# Local upstream for the qwen2.5-7b-local route (absolute model path, so no reload per request).
proxy-mlx-upstream:
	$(MLX_SERVER_BIN) --model $(PROXY_MLX_MODEL) --host 127.0.0.1 --port $(PROXY_MLX_PORT)

# Deterministic e2e: fake upstream, all cases in mask and block mode + extra scenarios
# (incl. AUTH-01..10). Writes report.md/html/png; copies proxy-fake.* to reports/summary.
e2e-proxy:
	E2E_UPSTREAM=fake E2E_OUT=$(CURDIR)/reports/proxy/fake-$(TS) E2E_COPY_TO=$(CURDIR)/reports/summary $(E2E_PROXY) -race -timeout 10m

# Real model: starts mlx_lm.server on 127.0.0.1:18081 (or uses E2E_BASE_URL); output cases are n/a.
e2e-proxy-mlx:
	E2E_UPSTREAM=mlx E2E_OUT=$(CURDIR)/reports/proxy/mlx-$(TS) E2E_COPY_TO=$(CURDIR)/reports/summary $(E2E_PROXY) -timeout 60m

# Real model behind any OpenAI-compatible API (Ollama, LM Studio, ...): no server is
# started, the qwen2.5-7b-local route is pointed at E2E_BASE_URL with E2E_MODEL;
# output cases are n/a. Example:
#   make e2e-proxy-compat E2E_BASE_URL=http://127.0.0.1:11434/v1 E2E_MODEL=qwen2.5:7b-instruct
e2e-proxy-compat:
	@if [ -z '$(E2E_BASE_URL)' ] || [ -z '$(E2E_MODEL)' ]; then \
		echo "usage: make e2e-proxy-compat E2E_BASE_URL=http://127.0.0.1:11434/v1 E2E_MODEL=qwen2.5:7b-instruct"; exit 2; fi
	E2E_UPSTREAM=openai-compat E2E_BASE_URL='$(E2E_BASE_URL)' E2E_MODEL='$(E2E_MODEL)' E2E_OUT=$(CURDIR)/reports/proxy/compat-$(TS) \
		E2E_COPY_TO=$(CURDIR)/reports/summary $(E2E_PROXY) -timeout 60m

# Combined HTML/PNG/JSON report from the latest complete run of every upstream
# (fake, mlx, openai-compat, ...; no model needed). Old-schema runs are marked STALE, failed runs FAILED. Copies proxy-summary.*
# to reports/summary file by file.
e2e-proxy-summary:
	go run ./cmd/proxy-summary --runs $(CURDIR)/reports/proxy --cases $(CURDIR)/e2e/proxy/testdata/cases.json --out $(CURDIR)/reports/proxy-summary-$(TS) --copy-to $(CURDIR)/reports/summary

# Aggregate the proxy audit log, e.g. make audit-report AUDIT='logs/audit/*.jsonl'
audit-report:
	go run ./cmd/audit-report --audit '$(AUDIT)' --out $(CURDIR)/reports/audit-report-$(TS).md --json $(CURDIR)/reports/audit-report-$(TS).json
	@echo "report: $(CURDIR)/reports/audit-report-$(TS).md"

# ---------------------------------------------------------------------------
# Stand deployment: make setup, then make model UPSTREAM=mlx|ollama|lmstudio.
PYTHON ?= python3
GO_MIN_MINOR = 26
UPSTREAM ?= mlx
MODEL_REPO ?= mlx-community/Qwen2.5-7B-Instruct-4bit
# Commit sha of $(MODEL_REPO) on huggingface.co (pinned for reproducibility).
MODEL_REV ?= c26a38f6a37d0a51b4e9a1eb3026530fa35d9fed
OLLAMA ?= ollama
OLLAMA_MODEL ?= qwen2.5:7b-instruct
LMS ?= $(shell command -v lms 2>/dev/null || { [ -x "$$HOME/.lmstudio/bin/lms" ] && echo "$$HOME/.lmstudio/bin/lms"; })
LMSTUDIO_MODEL ?= qwen2.5-7b-instruct
VENV = .venv
HF = $(VENV)/bin/hf
REQ_MLX = requirements-mlx.txt

# Idempotent: Go >= 1.26, Python >= 3.10, go mod download, proxy binary, logs/audit
# (0700) and, on Apple Silicon only, .venv with $(REQ_MLX). The venv is reinstalled
# when $(REQ_MLX) changes (stamp file .venv/.stamp-<sha of $(REQ_MLX)>).
setup:
	@v="$$(go env GOVERSION)" || { echo "setup: Go not found (need Go >= 1.$(GO_MIN_MINOR), https://go.dev/dl/)"; exit 1; }; \
	echo "$$v" | awk -F'[go.]+' -v min=$(GO_MIN_MINOR) '{ if ($$2+0 > 1 || ($$2+0 == 1 && $$3+0 >= min)) exit 0; exit 1 }' \
		|| { echo "setup: $$v found, need Go >= 1.$(GO_MIN_MINOR) (or GOTOOLCHAIN=auto)"; exit 1; }; \
	echo "setup: $$v ok"
	@$(PYTHON) -c 'import sys; sys.exit(0 if sys.version_info >= (3, 10) else 1)' 2>/dev/null \
		|| { echo "setup: $(PYTHON) >= 3.10 is required (e.g. make setup PYTHON=python3.13)"; exit 1; }; \
	echo "setup: $$($(PYTHON) --version) ok"
	go mod download
	@$(MAKE) --no-print-directory proxy-build
	@mkdir -p logs/audit && chmod 700 logs/audit
	@if [ "$$(uname -s)/$$(uname -m)" != "Darwin/arm64" ]; then \
		echo "skip MLX: not Apple Silicon — use Ollama/LM Studio"; \
	else \
		stamp="$(VENV)/.stamp-$$(shasum -a 256 $(REQ_MLX) | cut -c1-16)"; \
		if [ -f "$$stamp" ]; then \
			echo "setup: $(VENV) is up to date ($(REQ_MLX))"; \
		else \
			set -e; \
			[ -x $(VENV)/bin/python ] || $(PYTHON) -m venv $(VENV); \
			$(VENV)/bin/python -m pip install --upgrade --quiet pip; \
			$(VENV)/bin/python -m pip install --quiet -r $(REQ_MLX); \
			touch "$$stamp"; \
			echo "setup: installed $(REQ_MLX) into $(VENV) ($$($(VENV)/bin/python --version))"; \
		fi; \
	fi
	@echo ""
	@echo "next: cp .env.example .env && chmod 600 .env  (values: openssl rand -hex 32), then one of"
	@echo "      make model                    # MLX (Apple Silicon), ~4-5 GB"
	@echo "      make model UPSTREAM=ollama    # Ollama, ~4.7 GB"
	@echo "      make model UPSTREAM=lmstudio  # LM Studio, ~4-5 GB"
	@echo "      quick check without a model: make e2e-proxy && make e2e-proxy-summary"

# Downloads Qwen2.5-7B-Instruct (~4-5 GB) for UPSTREAM=mlx (default) | ollama | lmstudio.
# hf >= 1.x: one --include per pattern (extra positional patterns would be taken as
# file names and --include ignored, dropping model.safetensors).
model:
	@case '$(UPSTREAM)' in \
	mlx) \
		if [ -f '$(MODEL_DIR)/config.json' ] && ls '$(MODEL_DIR)'/model*.safetensors >/dev/null 2>&1; then \
			echo "model: $(MODEL_DIR) already present, skip download"; exit 0; fi; \
		if [ ! -x '$(HF)' ]; then echo "model: $(HF) not found — run: make setup (MLX needs Apple Silicon)"; exit 1; fi; \
		echo "model: downloading $(MODEL_REPO)@$(MODEL_REV) (~4-5 GB) to $(MODEL_DIR)"; \
		'$(HF)' download '$(MODEL_REPO)' --revision '$(MODEL_REV)' --local-dir '$(MODEL_DIR)' \
			--include "*.safetensors" --include "*.json" --include "tokenizer*" --include "*.txt" ;; \
	ollama) \
		if ! command -v '$(OLLAMA)' >/dev/null 2>&1; then \
			echo "model: '$(OLLAMA)' not found — install: brew install ollama (or https://ollama.com/download), then run: ollama serve"; exit 1; fi; \
		echo "model: $(OLLAMA) pull $(OLLAMA_MODEL) (~4.7 GB)"; \
		'$(OLLAMA)' pull '$(OLLAMA_MODEL)' ;; \
	lmstudio) \
		if [ -z '$(LMS)' ] || [ ! -x '$(LMS)' ]; then \
			echo "model: lms CLI not found. In the LM Studio app: Discover -> search \"$(LMSTUDIO_MODEL)\" -> Download (~4-5 GB),"; \
			echo "       then Developer -> Local Server -> Start (port 1234). CLI: https://lmstudio.ai/docs/cli"; exit 1; fi; \
		echo "model: $(LMS) get $(LMSTUDIO_MODEL) (~4-5 GB)"; \
		'$(LMS)' get '$(LMSTUDIO_MODEL)' ;; \
	*) echo "model: UPSTREAM must be mlx, ollama or lmstudio (got '$(UPSTREAM)')"; exit 2 ;; \
	esac
