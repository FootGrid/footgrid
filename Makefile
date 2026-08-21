GO ?= go
MIGRATE ?= migrate
DATABASE_URL ?= postgres://footgrid:footgrid@localhost:5432/footgrid?sslmode=disable

.PHONY: fmt lint test test-integration build run-match-api migrate-up migrate-down openapi-check

fmt:
	$(GO) fmt ./...

lint:
	$(GO) vet ./...

test:
	$(GO) test ./...

test-integration:
	RUN_INTEGRATION_TESTS=1 $(GO) test ./...

build:
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -o bin/identity-api ./cmd/identity-api
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -o bin/match-api ./cmd/match-api
	GOOS=linux GOARCH=arm64 CGO_ENABLED=0 $(GO) build -o bin/projection-worker ./cmd/projection-worker

run-match-api:
	$(GO) run ./cmd/match-api

migrate-up:
	$(MIGRATE) -path migrations -database "$(DATABASE_URL)" up

migrate-down:
	$(MIGRATE) -path migrations -database "$(DATABASE_URL)" down 1

openapi-check:
	ruby -e "require 'yaml'; d=YAML.load_file('api/openapi.yaml'); abort('expected OpenAPI 3.1') unless d['openapi'] == '3.1.0'; puts 'OpenAPI YAML OK'"
