-include .env
export

MIGRATE ?= migrate
MIGRATE_VERSION ?= v4.20.1
SQLC ?= sqlc
SQLC_VERSION ?= v1.31.1
GOBIN ?= $(HOME)/.local/bin
MIGRATIONS_DIR ?= migrations
DATABASE_URL ?= postgres://relayforge:relayforge@localhost:5435/relayforge?sslmode=disable
MIGRATE_CMD = $(MIGRATE) -path "$(MIGRATIONS_DIR)" -database "$(DATABASE_URL)"

.PHONY: run-api run-worker build test fmt tidy \
	migrate-install migrate-up migrate-down migrate-status migrate-create \
	sqlc-install sqlc-generate \
	web-install web-dev web-build web-lint

run-api:
	go run ./cmd/api

run-worker:
	go run ./cmd/worker

build:
	go build ./...

test:
	go test ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

migrate-install:
	GOBIN="$(GOBIN)" go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@$(MIGRATE_VERSION)

migrate-up:
	$(MIGRATE_CMD) up

migrate-down:
	$(MIGRATE_CMD) down 1

migrate-status:
	$(MIGRATE_CMD) version

migrate-create:
	@test -n "$(name)" || (echo "usage: make migrate-create name=<migration_name>"; exit 1)
	$(MIGRATE) create -ext sql -dir "$(MIGRATIONS_DIR)" -seq "$(name)"

sqlc-install:
	GOBIN="$(GOBIN)" go install github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)

sqlc-generate:
	$(SQLC) generate

web-install:
	pnpm --dir web install

web-dev:
	pnpm --dir web dev

web-build:
	pnpm --dir web build

web-lint:
	pnpm --dir web lint
