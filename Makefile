.PHONY: run test test-int lint migrate gen fmt deps

run:
	docker-compose up --build

test:
	go test ./... -race -count=1

test-int:
	go test ./tests/integration/... -tags integration -race -timeout 120s

lint:
	golangci-lint run ./...

migrate:
	goose -dir internal/db/migrations postgres "$(DATABASE_URL)" up

migrate-down:
	goose -dir internal/db/migrations postgres "$(DATABASE_URL)" down

gen:
	sqlc generate

fmt:
	gofmt -w .
	goimports -w .

deps:
	go mod tidy
	go mod verify

build:
	go build -o bin/nexus ./cmd/server

.env:
	cp .env.example .env
	@echo "Created .env — fill in the values before running."
