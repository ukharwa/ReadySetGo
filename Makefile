# Variables from .env (if present) are exported to every command below.
# Keep .env values unquoted: make reads quotes literally.
-include .env
export

BIN := bin/kickstart
SQLC := bin/sqlc
SQLC_VERSION := v1.31.1

.DEFAULT_GOAL := help

.PHONY: help
help: ## list targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-15s %s\n", $$1, $$2}'

.PHONY: run
run: ## run the HTTP server
	go run ./cmd/kickstart serve

.PHONY: worker
worker: ## run the job worker on its own (serve already runs one)
	go run ./cmd/kickstart worker

.PHONY: build
build: ## build a static binary into bin/
	CGO_ENABLED=0 go build -o $(BIN) ./cmd/kickstart

.PHONY: test
test: ## run all tests with the race detector
	go test -race ./...

.PHONY: fmt
fmt: ## format all Go code
	gofmt -w .

.PHONY: vet
vet: ## run go vet
	go vet ./...

.PHONY: tools
tools: ## install the pinned dev tools into bin/
	GOBIN=$(CURDIR)/bin go install github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)

.PHONY: generate
generate: ## regenerate internal/store from the SQL queries
	$(SQLC) generate

.PHONY: services
services: ## start the local dev services in compose.dev.yaml
	docker compose -f compose.dev.yaml up -d --wait

.PHONY: services-down
services-down: ## stop the local dev services (their data is kept)
	docker compose -f compose.dev.yaml down

.PHONY: migrate
migrate: ## apply pending migrations
	go run ./cmd/kickstart migrate up

.PHONY: migration
migration: ## create a new migration: make migration NAME=add_things
	go run ./cmd/kickstart migrate create $(NAME)

# kickstart:begin db=sqlite
.PHONY: backup
backup: ## write a database snapshot into backups/
	go run ./cmd/kickstart backup
# kickstart:end

# kickstart:begin template
.PHONY: init
init: ## turn this template into a new project: make init NAME=myapp [MODULE=…] [DB=sqlite|postgres]
	go run ./tools/kickstart init -name "$(NAME)" -module "$(MODULE)" -db "$(DB)"

.PHONY: template-check
template-check: ## init a temporary copy and run every check on it: make template-check [DB=…]
	go run ./tools/kickstart check -db "$(DB)"
# kickstart:end
