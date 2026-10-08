APP         := bze-scan
CMD         := ./cmd/bze-scan
BUILD_DIR   := build
COMPOSE     := docker compose -f ../docker/compose.yml
ARCHIVE_RPC ?= https://rpc.getbze.com

.PHONY: build
build: ## Build the binary into build/
	go build -o $(BUILD_DIR)/$(APP) $(CMD)

.PHONY: run
run: ## Run the backend (serve)
	go run $(CMD) serve

.PHONY: test
test: ## Run unit tests (no network, no docker)
	go test ./... -race

.PHONY: vet
vet: ## Run go vet
	go vet ./...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run

.PHONY: e2e-up
e2e-up: ## Start the PostgreSQL of docker/compose.yml
	$(COMPOSE) up -d --wait postgres

.PHONY: e2e-down
e2e-down: ## Stop and remove the compose containers
	$(COMPOSE) down -v

.PHONY: e2e
e2e: e2e-up ## Run acceptance tests (starts PostgreSQL, runs tests, tears it down)
	go test -tags=e2e -v -count=1 ./e2e/ ; ret=$$? ; $(MAKE) e2e-down ; exit $$ret

.PHONY: fixtures
fixtures: ## Record node fixtures: make fixtures HEIGHTS="24998316 ..." [ARCHIVE_RPC=...]
	HEIGHTS="$(HEIGHTS)" ARCHIVE_RPC="$(ARCHIVE_RPC)" scripts/record-fixtures.sh

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BUILD_DIR)

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
