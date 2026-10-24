BIN := bin/weatherbot

.PHONY: build run test lint

build:
	go build -o $(BIN) ./cmd/weatherbot

# Подхватывает .env, если он есть (значения из .env перекрывают окружение).
run:
	@set -a; [ -f .env ] && . ./.env; set +a; go run ./cmd/weatherbot

test:
	go test -race -cover ./...

lint:
	go vet ./...
	@out="$$(gofmt -l .)"; if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
