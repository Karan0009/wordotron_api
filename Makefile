# ---------------------------------------------------------------------------
# Backend API - developer task runner
# ---------------------------------------------------------------------------
SHELL          := /bin/bash
APP_NAME       ?= backend-api
BIN_DIR        ?= bin
MAIN_PKG       ?= ./cmd/api
MIGRATIONS_DIR ?= migrations
DOCKER_COMPOSE ?= podman compose

# Read DATABASE_URL from .env when present, otherwise fall back to the local default.
ifneq (,$(wildcard ./.env))
include .env
export
endif
DATABASE_URL ?= postgres://postgres:postgres@localhost:5432/appdb?sslmode=disable

GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -s -w -X main.version=$(GIT_COMMIT) -X main.buildTime=$(BUILD_TIME)

.DEFAULT_GOAL := help
.PHONY: help run build test test-integration test-cover lint fmt vet tidy sqlc \
        migrate migrate-down migrate-force migrate-create docker docker-up docker-down \
        docker-logs seed hooks clean

help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | \
	    awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## --------------------------------------------------------------------------
## Development
## --------------------------------------------------------------------------
run: ## Run the API locally (requires postgres + redis reachable)
	go run $(MAIN_PKG)

build: ## Compile a static binary into ./bin
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(APP_NAME) $(MAIN_PKG)

tidy: ## Sync go.mod / go.sum
	go mod tidy

fmt: ## Format the codebase
	go fmt ./...

vet: ## Run go vet
	go vet ./...

lint: ## Run golangci-lint
	golangci-lint run ./...

## --------------------------------------------------------------------------
## Testing
## --------------------------------------------------------------------------
test: ## Unit tests (fast, no external dependencies)
	go test -race -count=1 ./...

test-integration: ## Integration tests (spins up containers via testcontainers)
	go test -race -count=1 -tags=integration -timeout=10m ./tests/...

test-cover: ## Unit tests with an HTML coverage report
	go test -race -count=1 -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "report written to coverage.html"

## --------------------------------------------------------------------------
## Database
## --------------------------------------------------------------------------
sqlc: ## Regenerate type-safe query code from sql/queries
	sqlc generate

migrate: ## Apply all pending migrations
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" up

migrate-down: ## Roll back the most recent migration
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" down 1

migrate-force: ## Clear a dirty migration state: make migrate-force VERSION=1
	migrate -path $(MIGRATIONS_DIR) -database "$(DATABASE_URL)" force $(VERSION)

migrate-create: ## Scaffold a migration pair: make migrate-create NAME=add_widgets
	migrate create -ext sql -dir $(MIGRATIONS_DIR) -seq $(NAME)

seed: ## Insert the development seed data (admin + demo user)
	psql "$(DATABASE_URL)" -f scripts/seed.sql

## --------------------------------------------------------------------------
## Docker
## --------------------------------------------------------------------------
docker: ## Build the production image
	docker build -t $(APP_NAME):$(GIT_COMMIT) -t $(APP_NAME):latest .

docker-up: ## Start the full stack (api + postgres + redis + migrations)
	$(DOCKER_COMPOSE) up --build -d

docker-down: ## Stop the stack and remove volumes
	$(DOCKER_COMPOSE) down -v

docker-logs: ## Tail the API logs
	$(DOCKER_COMPOSE) logs -f api

## --------------------------------------------------------------------------
## Misc
## --------------------------------------------------------------------------
hooks: ## Install the pre-commit hooks
	pre-commit install

clean: ## Remove build and test artifacts
	rm -rf $(BIN_DIR) coverage.out coverage.html
