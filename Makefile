# Variables from .env (if present) are exported to every command below.
# Keep .env values unquoted: make reads quotes literally.
-include .env
export

BIN := bin/kickstart

.DEFAULT_GOAL := help

.PHONY: help run build test fmt vet

help: ## list targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-10s %s\n", $$1, $$2}'

run: ## run the HTTP server
	go run ./cmd/kickstart serve

build: ## build a static binary into bin/
	CGO_ENABLED=0 go build -o $(BIN) ./cmd/kickstart

test: ## run all tests with the race detector
	go test -race ./...

fmt: ## format all Go code
	gofmt -w .

vet: ## run go vet
	go vet ./...
