.PHONY: help test test-unit test-integration test-integration-local lint fmt build

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

test: test-unit ## Run all tests

test-unit: ## Run unit tests
	go test -v -race -coverprofile=coverage.out ./...

test-integration: ## Run integration tests (requires databases)
	go test -p 1 -v -tags=integration ./...

test-integration-local: ## Start databases and run integration tests locally
	@echo "Starting databases with docker compose..."
	docker compose up -d
	@echo "Waiting for databases to be ready..."
	@sleep 5
	@echo "Running integration tests..."
	POSTGRES_HOST=localhost POSTGRES_PORT=5432 POSTGRES_USER=postgres POSTGRES_PASSWORD=postgres POSTGRES_DB=pupsourcing_test \
	go test -p 1 -v -tags=integration ./... || true
	@echo "Stopping databases..."
	docker compose down

lint: ## Run linter
	golangci-lint run --timeout=5m

fmt: ## Format code
	gofmt -w -s .
	goimports -w -local github.com/getpup/pupsourcing .

build: ## Build all packages
	go build -v ./...
