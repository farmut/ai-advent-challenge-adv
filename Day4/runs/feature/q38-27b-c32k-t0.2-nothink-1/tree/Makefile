.PHONY: build run test lint

build:
	go build ./...

run:
	go run ./cmd/weatherbot

test:
	go test -race -cover ./...

lint:
	go vet ./...
	gofmt -l .
