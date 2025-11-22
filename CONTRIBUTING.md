# Contributing to pulumi-drift-adoption-tool

Thank you for your interest in contributing! This document provides guidelines and instructions for contributing to the project.

## Code of Conduct

This project adheres to the Pulumi Community Code of Conduct. By participating, you are expected to uphold this code.

## Getting Started

### Prerequisites

- Go 1.21 or later
- Git
- [Pulumi CLI](https://www.pulumi.com/docs/install/)
- Node.js (for TypeScript validation testing)
- Python 3.8+ (for Python validation testing)

### Development Setup

1. **Fork and clone the repository**

```bash
git clone https://github.com/YOUR_USERNAME/pulumi-drift-adoption-tool.git
cd pulumi-drift-adoption-tool
```

2. **Install dependencies**

```bash
go mod download
```

3. **Run tests to verify setup**

```bash
# Run unit tests
go test -tags=unit ./...

# Run all tests
go test -tags=unit,integration ./...
```

## Development Workflow

This project follows **Test-Driven Development (TDD)** principles:

### 1. Red-Green-Refactor Cycle

```
1. RED: Write a failing test
2. GREEN: Write minimal code to pass the test
3. REFACTOR: Clean up and optimize
4. REPEAT: Move to the next feature
```

### 2. Writing Tests

**Always write tests before implementation:**

```go
// Example: Test first
func TestDriftPlan_Serialization(t *testing.T) {
    // Arrange
    plan := &DriftPlan{
        Stack: "dev",
        TotalSteps: 1,
    }

    // Act
    data, err := json.Marshal(plan)
    require.NoError(t, err)

    // Assert
    var unmarshaled DriftPlan
    err = json.Unmarshal(data, &unmarshaled)
    require.NoError(t, err)
    assert.Equal(t, plan.Stack, unmarshaled.Stack)
}
```

### 3. Test Categories

Use build tags to categorize tests:

```go
//go:build unit
// Fast tests, no external dependencies

//go:build integration
// Tests with filesystem, mock services

//go:build e2e
// Full workflow tests
```

### 4. Running Tests

```bash
# Unit tests only (fast)
go test -tags=unit ./...

# Integration tests
go test -tags=integration ./...

# E2E tests
go test -tags=e2e ./e2e/...

# All tests with coverage
go test -tags=unit,integration -coverprofile=coverage.out ./...

# View coverage
go tool cover -html=coverage.out
```

## Project Structure

```
.
├── cmd/
│   └── pulumi-drift-adopt/    # CLI entry point and commands
├── pkg/
│   └── driftadopt/            # Core logic
│       ├── validator/         # Language-specific validators
│       ├── editor/            # Code manipulation
│       └── testutil/          # Test utilities
├── e2e/                       # End-to-end tests
├── testdata/                  # Test fixtures
├── .github/                   # GitHub workflows and templates
└── DRIFT_ADOPTION_DESIGN.md   # Detailed design document
```

## Making Changes

### 1. Create a Feature Branch

```bash
git checkout -b feature/your-feature-name
```

### 2. Follow Go Best Practices

- Use `gofmt` and `goimports` for formatting
- Run `golangci-lint` for linting
- Follow [Effective Go](https://golang.org/doc/effective_go)
- Add godoc comments for exported functions

### 3. Write Tests First (TDD)

For every new feature:
1. Write test(s) that fail
2. Implement the feature
3. Ensure tests pass
4. Refactor if needed

### 4. Keep Commits Atomic

- One logical change per commit
- Write clear, descriptive commit messages
- Reference issue numbers when applicable

```bash
git commit -m "feat: add DriftPlan serialization (#2)

Implements JSON marshaling/unmarshaling for DriftPlan type.
Includes comprehensive tests for edge cases.

Fixes #2"
```

### 5. Run Quality Checks

Before pushing:

```bash
# Format code
gofmt -w .
goimports -w .

# Run linter
golangci-lint run

# Run tests
go test -tags=unit,integration ./...

# Check coverage
go test -tags=unit -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | grep total
# Target: >80% coverage
```

## Pull Request Process

### 1. Update Documentation

- Update README.md if adding user-facing features
- Add/update godoc comments
- Update CHANGELOG.md (if exists)

### 2. Create Pull Request

- Use the PR template
- Link related issues
- Provide clear description of changes
- Add screenshots if applicable

### 3. PR Checklist

- [ ] Tests written and passing
- [ ] Code formatted (`gofmt`, `goimports`)
- [ ] Linter passing (`golangci-lint`)
- [ ] Documentation updated
- [ ] Commits are atomic and well-described
- [ ] PR description is clear

### 4. Code Review

- Address reviewer feedback promptly
- Discuss design decisions if needed
- Keep PR scope focused

## Development Phases

The project is being developed in phases (see [issues](https://github.com/pulumi/pulumi-drift-adoption-tool/issues)):

1. **Phase 1**: Core data structures
2. **Phase 2**: Dependency graph analysis
3. **Phase 3**: Code generation with LLM
4. **Phase 4**: Compilation validation
5. **Phase 5**: Diff matching
6. **Phase 6**: Step adopter orchestration
7. **Phase 7**: CLI commands
8. **Phase 8**: E2E testing
9. **Phase 9**: Polish & documentation

Check current phase in issues before starting work.

## Common Tasks

### Adding a New Command

1. Create test file: `cmd/pulumi-drift-adopt/mycommand_test.go`
2. Write failing tests
3. Create command file: `cmd/pulumi-drift-adopt/mycommand.go`
4. Implement command using Cobra
5. Register in `root.go`
6. Update README.md

### Adding a New Validator

1. Create test file: `pkg/driftadopt/validator/mylang_test.go`
2. Write tests for valid/invalid code
3. Implement validator: `pkg/driftadopt/validator/mylang.go`
4. Update factory to detect the language
5. Add integration tests

### Adding Test Fixtures

```bash
# Create fixture directory
mkdir -p testdata/my-scenario

# Add files
testdata/my-scenario/
  ├── input.json          # Input data
  ├── expected.json       # Expected output
  └── README.md           # Scenario description
```

## Debugging

### Running with Debug Output

```bash
# Set verbose mode (when implemented)
pulumi-drift-adopt --verbose next

# Run with debugger
dlv debug ./cmd/pulumi-drift-adopt -- next
```

### Common Issues

**Tests fail with missing modules:**
```bash
go mod tidy
```

**Linter errors:**
```bash
golangci-lint run --fix
```

**Coverage too low:**
```bash
# Find untested code
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Getting Help

- 📖 Read [DRIFT_ADOPTION_DESIGN.md](DRIFT_ADOPTION_DESIGN.md) for architecture details
- 🐛 Search [existing issues](https://github.com/pulumi/pulumi-drift-adoption-tool/issues)
- 💬 Join [Pulumi Community Slack](https://slack.pulumi.com/)
- ❓ Ask questions in issue comments

## Reporting Issues

### Bug Reports

Use the bug report template and include:
- Clear description of the issue
- Steps to reproduce
- Expected vs actual behavior
- Environment details (OS, Go version, etc.)
- Relevant logs or error messages

### Feature Requests

Use the feature request template and include:
- Problem you're trying to solve
- Proposed solution
- Alternative approaches considered
- Willingness to implement

## License

By contributing, you agree that your contributions will be licensed under the Apache 2.0 License.

## Recognition

Contributors will be recognized in:
- GitHub contributors page
- Release notes (for significant contributions)
- Project documentation

Thank you for contributing! 🎉
