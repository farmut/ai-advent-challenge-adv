.PHONY: build run test vet bench bench-compare lint clean preflight

BINARY=bin/agile-consultant
CONFIG=configs/app.yaml

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
