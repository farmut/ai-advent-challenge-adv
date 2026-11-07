.PHONY: build run test vet bench bench-compare lint clean preflight e2e-injection-v1 e2e-injection-v2 e2e-injection-summary

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

preflight: test vet build
	@echo "Preflight checks passed"

# Prompt-injection e2e against the local baseline model (starts mlx_lm.server on 127.0.0.1:18080).
# v1: current prompt, attacks are expected to SUCCEED (documents the vulnerability).
e2e-injection-v1:
	PROMPT_FILE=$(CURDIR)/configs/prompts/consultant.v1.txt E2E_EXPECT=vulnerable E2E_OUT=$(CURDIR)/reports/injection/v1-$(TS) $(E2E_INJECTION)

# v2: hardened prompt, attacks are expected to be BLOCKED.
e2e-injection-v2:
	PROMPT_FILE=$(CURDIR)/configs/prompts/consultant.v2.txt E2E_EXPECT=robust E2E_OUT=$(CURDIR)/reports/injection/v2-$(TS) $(E2E_INJECTION)

# Cross-version summary of the latest run per prompt version (no model needed).
e2e-injection-summary:
	go run ./cmd/injection-summary --runs $(CURDIR)/reports/injection --prompts $(CURDIR)/configs/prompts --cases $(CURDIR)/e2e/injection/testdata/cases.json --out $(CURDIR)/reports/injection/summary-$(TS) --copy-to $(CURDIR)/reports/summary
