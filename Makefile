# ──────────────────────────────────────────────────────────────────────────
# Balvia Backend — Makefile
# ──────────────────────────────────────────────────────────────────────────

# Load .env if present so DATABASE_URL is available to migrate targets.
-include .env
export

BINARY      := bin/api
MAIN        := ./cmd/api
MIGRATIONS  := migrations

.DEFAULT_GOAL := help

## help: list available targets
.PHONY: help
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed -e 's/## //' | awk -F': ' '{printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## run: run the API locally
.PHONY: run
run:
	go run $(MAIN)

## build: compile the API binary into bin/
.PHONY: build
build:
	go build -o $(BINARY) $(MAIN)

## test: run all tests
.PHONY: test
test:
	go test ./... -count=1

## fmt: format all Go code
.PHONY: fmt
fmt:
	go fmt ./...

## vet: run go vet static analysis
.PHONY: vet
vet:
	go vet ./...

## tidy: tidy go.mod / go.sum
.PHONY: tidy
tidy:
	go mod tidy

## sqlc: generate Go code from SQL queries
.PHONY: sqlc
sqlc:
	sqlc generate

## migrate-up: apply all pending migrations
.PHONY: migrate-up
migrate-up:
	migrate -path $(MIGRATIONS) -database "$(MIGRATE_DATABASE_URL)" up

## migrate-down: roll back the last migration
.PHONY: migrate-down
migrate-down:
	migrate -path $(MIGRATIONS) -database "$(MIGRATE_DATABASE_URL)" down 1

## migrate-force: set migration version without running it (recover dirty state) — usage: make migrate-force V=1
.PHONY: migrate-force
migrate-force:
	migrate -path $(MIGRATIONS) -database "$(MIGRATE_DATABASE_URL)" force $(V)

## migrate-version: print current migration version
.PHONY: migrate-version
migrate-version:
	migrate -path $(MIGRATIONS) -database "$(MIGRATE_DATABASE_URL)" version

## migrate-create: scaffold a new migration pair — usage: make migrate-create NAME=add_foo
.PHONY: migrate-create
migrate-create:
	migrate create -ext sql -dir $(MIGRATIONS) -seq $(NAME)
