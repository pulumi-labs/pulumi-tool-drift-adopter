.PHONY: help build test test-unit test-integration test-e2e lint lint-go lint-fix lint-workflows clean install-tools install-hooks

# Default target
help:
	@echo "Available targets:"
	@echo "  make build              - Build the pulumi-drift-adopt binary"
	@echo "  make test               - Run all tests (unit + integration)"
	@echo "  make test-unit          - Run unit tests"
	@echo "  make test-integration   - Run integration tests"
	@echo "  make test-e2e           - Run E2E tests (requires AWS + Pulumi + Anthropic setup)"
	@echo "  make lint               - Run all linters (Go + workflows)"
	@echo "  make lint-go            - Run golangci-lint"
	@echo "  make lint-fix           - Run golangci-lint with auto-fix"
	@echo "  make lint-workflows     - Lint GitHub Actions workflows"
	@echo "  make install-tools      - Install development tools (golangci-lint, actionlint)"
	@echo "  make install-hooks      - Install git hooks (pre-push)"
	@echo "  make clean              - Clean build artifacts"

# Build the binary
build:
	@echo "Building pulumi-drift-adopt..."
	go build -o ./bin/pulumi-drift-adopt ./cmd/pulumi-drift-adopt
	@echo "✅ Binary built at ./bin/pulumi-drift-adopt"

# Run all tests
test: test-unit test-integration

# Run unit tests
test-unit:
	@echo "Running unit tests..."
	go test -tags=unit -v -race -coverprofile=coverage-unit.out ./...

# Run integration tests
test-integration:
	@echo "Running integration tests..."
	go test -tags=integration -v -race -coverprofile=coverage-integration.out ./...

# Run E2E tests
test-e2e: build
	@echo "Running E2E tests..."
	@echo "⚠️  This requires:"
	@echo "   - ANTHROPIC_API_KEY environment variable"
	@echo "   - AWS credentials configured"
	@echo "   - Pulumi access (via PULUMI_ACCESS_TOKEN or logged in)"
	@echo ""
	go test -tags=e2e -v -timeout=30m -run TestDriftAdoptionWorkflow ./test/e2e/

# Run all linters
lint: lint-go lint-workflows

# Run golangci-lint
lint-go:
	@echo "Running golangci-lint..."
	@which golangci-lint > /dev/null || (echo "❌ golangci-lint not found. Run 'make install-tools' to install it." && exit 1)
	golangci-lint run --timeout=5m

# Run golangci-lint with auto-fix
lint-fix:
	@echo "Running golangci-lint with auto-fix..."
	@which golangci-lint > /dev/null || (echo "❌ golangci-lint not found. Run 'make install-tools' to install it." && exit 1)
	golangci-lint run --fix --timeout=5m
	@echo "✅ Auto-fixable issues have been corrected"

# Lint GitHub Actions workflows
lint-workflows:
	@echo "Linting GitHub Actions workflows..."
	@which actionlint > /dev/null || (echo "❌ actionlint not found. Run 'make install-tools' to install it." && exit 1)
	actionlint

# Install development tools
install-tools:
	@echo "Installing development tools..."
	@echo "Installing golangci-lint..."
	@which golangci-lint > /dev/null || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Installing actionlint..."
	@which actionlint > /dev/null || go install github.com/rhysd/actionlint/cmd/actionlint@latest
	@echo "✅ Tools installed"
	@echo ""
	@echo "Installed tools:"
	@which golangci-lint && golangci-lint --version
	@which actionlint && actionlint -version

# Install git hooks
install-hooks:
	@echo "Installing git hooks..."
	cp scripts/hooks/pre-push .git/hooks/pre-push
	chmod +x .git/hooks/pre-push
	@echo "✅ Git hooks installed"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf ./bin
	rm -f coverage-*.out
	@echo "✅ Clean complete"
