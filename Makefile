.DEFAULT_GOAL := help

COMPOSE := docker compose

.PHONY: help up down test test-integration migrate-up demo

help: ## Show available commands
	@awk 'BEGIN {FS = ":.*## "; printf "Usage: make <target>\n\n"} /^[a-zA-Z0-9_-]+:.*## / {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

up: ## Start PostgreSQL, apply migrations and start the service
	$(COMPOSE) up -d --build

down: ## Stop the containers; database data is kept
	$(COMPOSE) down

test: ## Run unit tests
	go test -race ./...

test-integration: ## Run unit and integration tests; needs Docker
	go test -race -tags=integration ./...

migrate-up: ## Apply pending migrations to the Compose database
	$(COMPOSE) run --rm migrate

demo: ## Run every workflow against the running service; start it with make up first
	./scripts/demo.sh
