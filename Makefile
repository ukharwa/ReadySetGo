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

.PHONY: image
image: ## build the app's Docker image
	docker compose build app

.PHONY: up
up: ## run the production stack locally at https://$$APP_DOMAIN (localhost)
	docker compose up -d --build --wait

.PHONY: down
down: ## stop the local production stack (its data is kept)
	docker compose down

DEPLOY_DIR ?= /srv/kickstart

.PHONY: deploy
deploy: ## update the server: make deploy DEPLOY_HOST=user@server [DEPLOY_DIR=/srv/kickstart]
	@test -n "$(DEPLOY_HOST)" || { echo "set DEPLOY_HOST (user@server)"; exit 1; }
	rsync -a compose.yaml deploy $(DEPLOY_HOST):$(DEPLOY_DIR)/
	ssh $(DEPLOY_HOST) 'cd $(DEPLOY_DIR) && docker compose pull app && docker compose up -d --remove-orphans'

.PHONY: migrate
migrate: ## apply pending migrations
	go run ./cmd/kickstart migrate up

.PHONY: seed
seed: ## add sample users for development (password: dev-password-123)
	go run ./cmd/kickstart seed

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
init: ## turn this template into a new project: make init NAME=myapp [MODULE=…] [DB=sqlite|postgres] [UI=html|api]
	go run ./tools/kickstart init -name "$(NAME)" -module "$(MODULE)" -db "$(DB)" -ui "$(UI)"

.PHONY: template-check
template-check: ## init a temporary copy and run every check on it: make template-check [DB=…] [UI=…]
	go run ./tools/kickstart check -db "$(DB)" -ui "$(UI)"
# kickstart:end
