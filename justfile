# Default recipe - show help
default:
    @just --list

# Build the pulumi-drift-adopt binary
build:
    @echo "Building pulumi-drift-adopt..."
    go build -o ./bin/pulumi-drift-adopt ./cmd/pulumi-drift-adopt
    @echo "✅ Binary built at ./bin/pulumi-drift-adopt"

# Run all tests (unit + integration)
test: test-unit test-integration

# Run unit tests
test-unit:
    @echo "Running unit tests..."
    go test -tags=unit -v -race -coverprofile=coverage-unit.out ./...

# Run integration tests
test-integration:
    @echo "Running integration tests..."
    go test -tags=integration -v -race -coverprofile=coverage-integration.out ./...

# Run all linters (Go + workflows)
lint: lint-go lint-workflows

# Run golangci-lint
lint-go:
    @echo "Running golangci-lint..."
    @which golangci-lint > /dev/null || (echo "❌ golangci-lint not found. Run 'just install-tools' to install it." && exit 1)
    golangci-lint run --timeout=5m

# Run golangci-lint with auto-fix
lint-fix:
    @echo "Running golangci-lint with auto-fix..."
    @which golangci-lint > /dev/null || (echo "❌ golangci-lint not found. Run 'just install-tools' to install it." && exit 1)
    golangci-lint run --fix --timeout=5m
    @echo "✅ Auto-fixable issues have been corrected"

# Lint GitHub Actions workflows
lint-workflows:
    @echo "Linting GitHub Actions workflows..."
    @which actionlint > /dev/null || (echo "❌ actionlint not found. Run 'just install-tools' to install it." && exit 1)
    actionlint

# Install development tools (golangci-lint, actionlint)
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

# Install git hooks (pre-push)
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
