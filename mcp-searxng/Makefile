SHELL := /bin/sh

APP_NAME     := mcp-searxng
BINARY       := bin/$(APP_NAME)
CMD          := ./cmd
VERSION      := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS      := -ldflags "-X github.com/pixime-net/talk-libs/version.Version=$(VERSION)"
DOCKER_IMAGE := $(APP_NAME)
DEV_SEARXNG_IMAGE     := $(APP_NAME)-dev-searxng
DEV_SEARXNG_CONTAINER := $(APP_NAME)-dev-searxng
DEV_SEARXNG_PORT      ?= 8888
DEV_COMPOSE_FILE      := docker-compose.dev.yml

.PHONY: all build run dev test test-integration cover vet lint clean dockerize docker-run searxng-dev searxng-dev-stop dev-infra dev-infra-stop help

COLOR_RESET  = \033[0m
COLOR_BOLD   = \033[1m
COLOR_GREEN  = \033[32m
COLOR_YELLOW = \033[33m
COLOR_BLUE   = \033[34m
CHECK        = [ok]

ECHO = printf '%b\n'

.DEFAULT_GOAL := help

all: vet test build ## Run vet, test and build

build: ## Build the binary
	@$(ECHO) "$(COLOR_YELLOW)Building $(APP_NAME)...$(COLOR_RESET)"
	go build $(LDFLAGS) -o $(BINARY) $(CMD)
	@$(ECHO) "$(COLOR_GREEN)$(CHECK) Build successful: $(BINARY)$(COLOR_RESET)"

run: ## Run the server (TRANSPORT=http; address from HTTP_HOST/HTTP_PORT)
	go run $(LDFLAGS) $(CMD) --transport $(or $(TRANSPORT),http)

dev: ## Run the server with hot reload (air) in http mode
	air

test: ## Run tests
	@$(ECHO) "$(COLOR_YELLOW)Running tests...$(COLOR_RESET)"
	go test ./...
	@$(ECHO) "$(COLOR_GREEN)$(CHECK) Tests passed$(COLOR_RESET)"

test-integration: ## Run integration tests against a SearXNG container (requires Docker)
	@$(ECHO) "$(COLOR_YELLOW)Running integration tests...$(COLOR_RESET)"
	go test -tags integration -run Integration -count=1 ./...
	@$(ECHO) "$(COLOR_GREEN)$(CHECK) Integration tests passed$(COLOR_RESET)"

cover: ## Run tests with coverage
	@$(ECHO) "$(COLOR_YELLOW)Running tests with coverage...$(COLOR_RESET)"
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	@$(ECHO) "$(COLOR_GREEN)$(CHECK) Coverage done$(COLOR_RESET)"

vet: ## Run go vet
	go vet ./...

lint: ## Run golangci-lint
	@$(ECHO) "$(COLOR_YELLOW)Running linter...$(COLOR_RESET)"
	golangci-lint run ./...
	@$(ECHO) "$(COLOR_GREEN)$(CHECK) Lint passed$(COLOR_RESET)"

dockerize: ## Build the Docker image (mcp-searxng + embedded SearXNG)
	@$(ECHO) "$(COLOR_YELLOW)Building Docker image $(DOCKER_IMAGE)...$(COLOR_RESET)"
	docker build -t $(DOCKER_IMAGE) -f Dockerfile ..
	@$(ECHO) "$(COLOR_GREEN)$(CHECK) Docker image built: $(DOCKER_IMAGE)$(COLOR_RESET)"

docker-run: ## Run the image locally with SearXNG also reachable on :8888 (debug only)
	docker run --rm -p 8080:8080 -p 8888:8888 -e SEARXNG_BIND_ADDRESS=0.0.0.0 $(DOCKER_IMAGE)

searxng-dev: ## Start a local SearXNG for development on DEV_SEARXNG_PORT (default 8888; not for production)
	docker build -t $(DEV_SEARXNG_IMAGE) -f Dockerfile.dev .
	docker run -d --rm --name $(DEV_SEARXNG_CONTAINER) -p 127.0.0.1:$(DEV_SEARXNG_PORT):8888 $(DEV_SEARXNG_IMAGE)
	@$(ECHO) "$(COLOR_GREEN)$(CHECK) SearXNG running on http://localhost:$(DEV_SEARXNG_PORT)$(COLOR_RESET)"

searxng-dev-stop: ## Stop the local development SearXNG
	docker stop $(DEV_SEARXNG_CONTAINER)

dev-infra: ## Start the development stack (SearXNG + headless browser)
	docker compose -f $(DEV_COMPOSE_FILE) up -d --build
	@$(ECHO) "$(COLOR_GREEN)$(CHECK) SearXNG on http://localhost:8888, browser on ws://localhost:3000 (token: BROWSERLESS_TOKEN, default dev-token)$(COLOR_RESET)"

dev-infra-stop: ## Stop the development stack
	docker compose -f $(DEV_COMPOSE_FILE) down

clean: ## Remove build artifacts
	rm -rf bin coverage.out

help: ## Show help
	@$(ECHO) "$(COLOR_BOLD)mcp-searxng$(COLOR_RESET)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(COLOR_BLUE)%-20s$(COLOR_RESET) %s\n", $$1, $$2}'
