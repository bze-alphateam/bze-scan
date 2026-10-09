APP         := bze-scan
CMD         := ./cmd/bze-scan
BUILD_DIR   := build
COMPOSE     := docker compose -f ../docker/compose.yml
ARCHIVE_RPC ?= https://rpc.getbze.com
REST        ?= https://rest.getbze.com
# The PostgreSQL of docker/compose.yml unless DATABASE_URL is set.
DATABASE_URL ?= postgres://bze:bze@localhost:15432/bze_index?sslmode=disable

.PHONY: build
build: ## Build the binary into build/
	go build -o $(BUILD_DIR)/$(APP) $(CMD)

.PHONY: run
run: ## Run the backend (serve)
	go run $(CMD) serve

.PHONY: migrate
migrate: ## Run migrate up (compose database unless DATABASE_URL is set)
	DATABASE_URL="$(DATABASE_URL)" go run $(CMD) migrate up

.PHONY: test
test: ## Run unit tests (no network, no docker)
	go test ./... -race

.PHONY: vet
vet: ## Run go vet (acceptance tests included)
	go vet -tags=e2e ./...

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run

.PHONY: tidy-check
tidy-check: ## Fail when go.mod/go.sum are not tidy
	go mod tidy -diff

.PHONY: vulncheck
vulncheck: ## govulncheck; fails on reachable vulnerabilities that have a fix
	scripts/vulncheck.sh

.PHONY: check
check: vet lint tidy-check vulncheck test e2e ## Everything CI runs

.PHONY: e2e-up
e2e-up: ## Start the PostgreSQL of docker/compose.yml
	$(COMPOSE) up -d --wait postgres

.PHONY: e2e-down
e2e-down: ## Stop and remove the compose containers
	$(COMPOSE) down -v

.PHONY: e2e
e2e: e2e-up ## Run acceptance tests (starts PostgreSQL, runs tests, tears it down)
	go test -tags=e2e -race -v -count=1 ./e2e/ ; ret=$$? ; $(MAKE) e2e-down ; exit $$ret

.PHONY: fixtures
fixtures: ## Record node fixtures: make fixtures HEIGHTS="24998316 a..b" [ARCHIVE_RPC=...]
	HEIGHTS="$(HEIGHTS)" ARCHIVE_RPC="$(ARCHIVE_RPC)" scripts/record-fixtures.sh

.PHONY: grpc-fixtures
grpc-fixtures: ## Record fake gRPC fixtures: make grpc-fixtures [SETS="validators accounts"] VALIDATORS="bzevaloper1…" ACCOUNTS="bze1…" [DENOMS="ubze …"] [PROPOSALS="47 …"] [REST=...]
	SETS="$(SETS)" VALIDATORS="$(VALIDATORS)" ACCOUNTS="$(ACCOUNTS)" DENOMS="$(DENOMS)" PROPOSALS="$(PROPOSALS)" REST="$(REST)" scripts/record-grpc-fixtures.sh

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BUILD_DIR)

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
