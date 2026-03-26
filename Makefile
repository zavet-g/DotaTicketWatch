.PHONY: up down restart logs build test

up:
	docker compose up -d --build

down:
	docker compose down

restart:
	docker compose restart bot

logs:
	docker compose logs -f bot

build:
	CGO_ENABLED=0 go build -o bin/bot ./cmd/bot

test:
	go test ./...
