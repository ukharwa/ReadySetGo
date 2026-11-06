# Variables from .env (if present) are exported to every command below.
# Keep .env values unquoted: make reads quotes literally.
-include .env
export

BIN := bin/readysetgo
SQLC := bin/sqlc
LINT := bin/golangci-lint

# Pinned dev tool versions. Dependabot doesn't update these: bump them by
# hand now and then, and run `make tools`.
SQLC_VERSION := v1.31.1
AIR_VERSION := v1.67.4
GOLANGCI_LINT_VERSION := v2.15.0
GOVULNCHECK_VERSION := v1.9.0

.DEFAULT_GOAL := help

.PHONY: help
help: ## list targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-15s %s\n", $$1, $$2}'

.PHONY: run
run: ## run the HTTP server
	go run ./cmd/readysetgo serve

.PHONY: dev
dev: ## run the server, rebuilding and restarting it when code changes
	bin/air

.PHONY: worker
worker: ## run the job worker on its own (serve already runs one)
	go run ./cmd/readysetgo worker

.PHONY: build
build: ## build a static binary into bin/
	CGO_ENABLED=0 go build -o $(BIN) ./cmd/readysetgo

.PHONY: test
test: ## run all tests with the race detector
	go test -race ./...

.PHONY: fmt
fmt: ## format all Go code with the formatters lint checks (gofmt, goimports)
	$(LINT) fmt

.PHONY: vet
vet: ## run go vet
	go vet ./...

.PHONY: lint
lint: ## run the linters and formatting checks
	$(LINT) run ./...

.PHONY: vuln
vuln: ## look for known vulnerabilities in the code's dependencies and Go itself
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

.PHONY: check
check: ## run everything CI checks
	$(LINT) run ./...
	go mod tidy -diff
	$(SQLC) diff
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...
	go test -race ./...

.PHONY: tools
tools: ## install the pinned dev tools into bin/ (sqlc needs a C compiler)
	GOBIN=$(CURDIR)/bin go install github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)
	GOBIN=$(CURDIR)/bin go install github.com/air-verse/air@$(AIR_VERSION)
	GOBIN=$(CURDIR)/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: generate
generate: ## regenerate each feature's store package from its SQL queries
	$(SQLC) generate

.PHONY: services
services: ## start the local dev services in compose.dev.yaml
	docker compose -f compose.dev.yaml up -d --wait

.PHONY: services-down
services-down: ## stop the local dev services (their data is kept)
	docker compose -f compose.dev.yaml down

.PHONY: image
image: ## build the app's Docker image
	docker compose build app

.PHONY: up
up: ## run the production stack locally at https://$$APP_DOMAIN (localhost)
	docker compose up -d --build --wait

.PHONY: down
down: ## stop the local production stack (its data is kept)
	docker compose down

DEPLOY_DIR ?= /srv/readysetgo

.PHONY: deploy
deploy: ## update the server: make deploy DEPLOY_HOST=user@server [DEPLOY_DIR=/srv/readysetgo]
	@test -n "$(DEPLOY_HOST)" || { echo "set DEPLOY_HOST (user@server)"; exit 1; }
	rsync -a compose.yaml deploy $(DEPLOY_HOST):$(DEPLOY_DIR)/
	ssh $(DEPLOY_HOST) 'cd $(DEPLOY_DIR) && docker compose pull app && docker compose up -d --remove-orphans'

.PHONY: migrate
migrate: ## apply pending migrations
	go run ./cmd/readysetgo migrate up

.PHONY: seed
seed: ## add sample users for development (password: dev-password-123)
	go run ./cmd/readysetgo seed

.PHONY: migration
migration: ## create a new migration: make migration NAME=add_things
	go run ./cmd/readysetgo migrate create $(NAME)

# readysetgo:begin db=sqlite
.PHONY: backup
backup: ## write a database snapshot into backups/
	go run ./cmd/readysetgo backup
# readysetgo:end

# example:begin
.PHONY: remove-example
remove-example: ## delete the notes example (before the first deploy)
	go run ./tools/removeexample
# example:end

# readysetgo:begin template
.PHONY: init
init: ## turn this template into a new project: make init NAME=myapp MODULE=github.com/you/myapp [DB=sqlite|postgres] [UI=html|api] [MODULES=a,b|all]
	go run ./tools/readysetgo init -name "$(NAME)" -module "$(MODULE)" -db "$(DB)" -ui "$(UI)" -modules "$(MODULES)"

.PHONY: template-check
template-check: ## init a temporary copy and run every check on it: make template-check [DB=…] [UI=…] [MODULES=…] [REMOVE_EXAMPLE=1]
	go run ./tools/readysetgo check -db "$(DB)" -ui "$(UI)" -modules "$(MODULES)" $(if $(REMOVE_EXAMPLE),-remove-example)
# readysetgo:end
