SHELL := /bin/sh

MODULES := talk-libs talk mcp-playground mcp-owm mcp-ign-nav mcp-searxng

.PHONY: all test cover vet lint clean help $(MODULES)

COLOR_RESET  = \033[0m
COLOR_BOLD   = \033[1m
COLOR_GREEN  = \033[32m
COLOR_YELLOW = \033[33m
COLOR_BLUE   = \033[34m
CHECK        = [ok]

ECHO = printf '%b\n'

.DEFAULT_GOAL := help

all: vet test ## Run vet and test for all modules

test: ## Run tests for all modules
	@for mod in $(MODULES); do \
		$(ECHO) "$(COLOR_YELLOW)Testing $$mod...$(COLOR_RESET)"; \
		(cd $$mod && go test ./...) || exit 1; \
		$(ECHO) "$(COLOR_GREEN)$(CHECK) $$mod tests passed$(COLOR_RESET)"; \
	done

cover: ## Run tests with coverage for all modules
	@for mod in $(MODULES); do \
		$(ECHO) "$(COLOR_YELLOW)Coverage for $$mod...$(COLOR_RESET)"; \
		(cd $$mod && go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out) || exit 1; \
		$(ECHO) "$(COLOR_GREEN)$(CHECK) $$mod coverage done$(COLOR_RESET)"; \
	done

cover-html: ## Generate and open HTML coverage reports
	@for mod in $(MODULES); do \
		$(ECHO) "$(COLOR_YELLOW)Coverage HTML for $$mod...$(COLOR_RESET)"; \
		(cd $$mod && go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out -o coverage.html) || exit 1; \
		$(ECHO) "$(COLOR_GREEN)$(CHECK) $$mod coverage.html generated$(COLOR_RESET)"; \
	done
	@$(ECHO) "$(COLOR_BOLD)Reports:$(COLOR_RESET)"
	@for mod in $(MODULES); do \
		$(ECHO) "  $(COLOR_BLUE)$$mod/coverage.html$(COLOR_RESET)"; \
	done

vet: ## Run go vet for all modules
	@for mod in $(MODULES); do \
		$(ECHO) "$(COLOR_YELLOW)Vetting $$mod...$(COLOR_RESET)"; \
		(cd $$mod && go vet ./...) || exit 1; \
		$(ECHO) "$(COLOR_GREEN)$(CHECK) $$mod vet passed$(COLOR_RESET)"; \
	done

lint: ## Run golangci-lint for all modules
	@for mod in $(MODULES); do \
		$(ECHO) "$(COLOR_YELLOW)Linting $$mod...$(COLOR_RESET)"; \
		(cd $$mod && golangci-lint run --timeout=5m) || exit 1; \
		$(ECHO) "$(COLOR_GREEN)$(CHECK) $$mod lint passed$(COLOR_RESET)"; \
	done

fmt: ## Format code (go fmt + goimports) for all modules
	@command -v goimports >/dev/null 2>&1 || { \
		$(ECHO) "$(COLOR_YELLOW)Installing goimports...$(COLOR_RESET)"; \
		go install golang.org/x/tools/cmd/goimports@latest; \
	}
	@for mod in $(MODULES); do \
		$(ECHO) "$(COLOR_YELLOW)Formatting $$mod...$(COLOR_RESET)"; \
		(cd $$mod && go fmt ./... && goimports -w .) || exit 1; \
		$(ECHO) "$(COLOR_GREEN)$(CHECK) $$mod formatted$(COLOR_RESET)"; \
	done

build: ## Build all binaries
	@$(MAKE) -C talk build
	@$(MAKE) -C mcp-owm build
	@$(MAKE) -C mcp-ign-nav build
	@$(MAKE) -C mcp-searxng build
	@$(MAKE) -C mcp-playground build

clean: ## Remove build artifacts from all modules
	@for mod in $(MODULES); do \
		rm -f $$mod/coverage.out $$mod/bin/*; \
	done
	@$(ECHO) "$(COLOR_GREEN)$(CHECK) Clean complete$(COLOR_RESET)"

help: ## Show help
	@$(ECHO) "$(COLOR_BOLD)Talks monorepo$(COLOR_RESET)"
	@$(ECHO) "$(COLOR_GREEN)Available commands:$(COLOR_RESET)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  $(COLOR_BLUE)%-20s$(COLOR_RESET) %s\n", $$1, $$2}'
