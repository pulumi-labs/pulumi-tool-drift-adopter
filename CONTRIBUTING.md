# Contributing to pulumi-tool-drift-adopter

Thank you for your interest in contributing! This document provides guidelines and instructions for contributing to the project.

## Code of Conduct

This project adheres to the Pulumi Community Code of Conduct. By participating, you are expected to uphold this code.

## Getting Started

### Prerequisites

- Go 1.24 or later
- Git
- [just](https://github.com/casey/just) (task runner)
- [Pulumi CLI](https://www.pulumi.com/docs/install/)

### Development Setup

1. **Fork and clone the repository**

```bash
git clone https://github.com/YOUR_USERNAME/pulumi-tool-drift-adopter.git
cd pulumi-tool-drift-adopter
```

2. **Install dependencies and tools**

```bash
go mod download
just install-tools
just install-hooks
```

3. **Build the tool**

```bash
just build
```

## Project Structure

```
.
├── cmd/
│   └── pulumi-drift-adopt/    # CLI
│       ├── main.go            # Entry point
│       ├── root.go            # Root command
│       └── next.go            # Main command (~180 lines)
├── bin/                       # Built binary
└── .github/                   # GitHub workflows and templates
```

The entire tool logic is in `cmd/pulumi-drift-adopt/next.go` - a single, self-contained file (~180 lines).

## Making Changes

### 1. Create a Feature Branch

```bash
git checkout -b feature/your-feature-name
```

### 2. Follow Go Best Practices

- Use `gofmt` and `goimports` for formatting
- Run `golangci-lint` for linting
- Follow [Effective Go](https://golang.org/doc/effective_go)
- Add comments for non-obvious logic

### 3. Keep Commits Atomic

- One logical change per commit
- Write clear, descriptive commit messages
- Reference issue numbers when applicable

```bash
git commit -m "feat: add source location to output (#42)

Extracts file and line information from preview output
and includes it in the resource change JSON.

Fixes #42"
```

### 4. Run Quality Checks

Before pushing (the pre-push hook runs these automatically):

```bash
# Run linter
just lint-go

# Run linter with auto-fix
just lint-fix

# Run unit tests
just test-unit

# Build
just build
```

## Pull Request Process

### 1. Update Documentation

- Update README.md if adding user-facing features
- Update CHANGELOG.md with your changes
- Add comments to code for complex logic

### 2. Create Pull Request

- Use the PR template
- Link related issues
- Provide clear description of changes
- Show example output if applicable

### 3. PR Checklist

- [ ] Linter passing (`just lint-go`)
- [ ] Unit tests passing (`just test-unit`)
- [ ] Binary builds successfully (`just build`)
- [ ] Documentation updated
- [ ] Commits are atomic and well-described
- [ ] PR description is clear

### 4. Code Review

- Address reviewer feedback promptly
- Discuss design decisions if needed
- Keep PR scope focused

## Common Tasks

### Modifying the `next` Command

Since all logic is in one file (`cmd/pulumi-drift-adopt/next.go`):

1. Read the existing code to understand the flow
2. Make your changes
3. Test manually with a Pulumi project
4. Verify the JSON output is correct
5. Update documentation

## Debugging

### Running Locally

```bash
# Build
just build

# Run in a Pulumi project
cd /path/to/pulumi/project
/path/to/pulumi-tool-drift-adopter/bin/pulumi-drift-adopt next
```

### Debug Output

Add debug prints to `next.go`:

```go
fmt.Fprintf(os.Stderr, "DEBUG: found %d resources\n", len(resources))
```

### Common Issues

**Build fails:**
```bash
go mod tidy
```

**Import errors:**
```bash
goimports -w .
```

**Linter errors:**
```bash
just lint-fix
```

## Getting Help

- 📖 Read [DRIFT_ADOPTION_DESIGN.md](DRIFT_ADOPTION_DESIGN.md) for design details
- 📖 Read [README.md](README.md) for usage examples
- 🐛 Search [existing issues](https://github.com/pulumi/pulumi-tool-drift-adopter/issues)
- 💬 Join [Pulumi Community Slack](https://slack.pulumi.com/)
- ❓ Ask questions in issue comments

## Reporting Issues

### Bug Reports

Use the bug report template and include:
- Clear description of the issue
- Steps to reproduce
- Expected vs actual behavior
- Environment details (OS, Go version, Pulumi version)
- Relevant command output or error messages

### Feature Requests

Use the feature request template and include:
- Problem you're trying to solve
- Proposed solution
- Alternative approaches considered
- Example use case
- Willingness to implement

## Design Philosophy

The tool follows a **simple, stateless** design:
- Single command that does one thing well
- No external state or plan files
- Straightforward preview parsing with inverted logic
- JSON output for easy agent consumption
- Minimal dependencies

When proposing changes, keep this philosophy in mind.

## License

By contributing, you agree that your contributions will be licensed under the Apache 2.0 License.

## Recognition

Contributors will be recognized in:
- GitHub contributors page
- Release notes (for significant contributions)

Thank you for contributing! 🎉
