.PHONY: build run test vet bench bench-compare lint clean preflight e2e-injection-off e2e-injection-on e2e-injection-summary

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
