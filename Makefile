.PHONY: check test help

help:
	@echo "Available targets:"
	@echo "  make check - Run linting and code quality checks (vet, golangci-lint)"
	@echo "  make test  - Run Go tests (unit, service, database, handler, integration, E2E)"

check: vet lint

test:
	@echo "Running unit tests..."
	go test ./internal/config/... ./internal/middleware/... ./internal/models/... -v
	@echo "Running service tests..."
	go test ./internal/services/... -v
	@echo "Note: database, handler, integration, and E2E tests require TEST_DB_* environment variables"
	@echo "Set TEST_DB_HOST, TEST_DB_PORT, TEST_DB_USER, TEST_DB_PASSWORD, TEST_DB_NAME to run those tests"

vet:
	@echo "Running go vet..."
	go vet ./...

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		echo "Running golangci-lint..."; \
		golangci-lint run ./... --timeout=5m; \
	else \
		echo "golangci-lint not installed, skipping lint check"; \
		echo "Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@v2.10.1"; \
	fi
