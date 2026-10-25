.PHONY: build run test lint

build:
	go build -o weatherbot cmd/weatherbot/main.go

run:
	go run cmd/weatherbot/main.go

test:
	go test -race -cover ./...

lint:
	go vet ./...
	gofmt -d .

.PHONY: help
help:
	@echo "Available commands:"
	@echo "  build - build the project"
	@echo "  run - run the project"
	@echo "  test - run tests"
	@echo "  lint - run linters"