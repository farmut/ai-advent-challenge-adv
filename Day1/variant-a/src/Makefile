.PHONY: build run test cover lint

build:
	go build -o bin/weatherbot ./cmd/weatherbot

run:
	set -a && . ./.env && set +a && go run ./cmd/weatherbot

test:
	go test -race -count=1 ./...

cover:
	go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out

lint:
	gofmt -l . && go vet ./...
