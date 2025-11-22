# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.0] - 2025-11-21

### Changed - Major Simplification
- **BREAKING**: Complete architectural simplification - tool is now stateless
- Removed all plan/step management functionality
- Removed commands: `generate-plan`, `show-step`, `apply-diff`, `status`, `skip`, `rollback`
- Simplified `next` command to just run `pulumi preview --json` and return results
- Tool now uses inverted logic: state values (old) = desired, code values (new) = current
- Operations inverted: create → delete_from_code, delete → add_to_code, update → update_code
- Single command workflow: agent calls `next`, updates code, calls `next` again
- Removed plan files, dependency graphs, step orchestration, diff recording
- Greatly simplified codebase: removed ~50% of code
- Test suite reduced to essential tests (24 tests total)

### Rationale
The previous architecture was over-engineered for the use case. The simplified version:
- No state management needed - `pulumi preview` already has all the information
- Agent can directly interpret preview output without intermediate plan files
- Stateless = simpler, more reliable, easier to understand
- Single responsibility: parse preview and format for agent consumption

## [0.1.0] - 2025-11-21

### Added
- Initial release of pulumi-drift-adoption-tool
- Agent-oriented architecture for drift adoption workflow
- Sequential gate pattern orchestration via `next` command
- CLI commands:
  - `next` - Shows next step in workflow
  - `show-step` - Displays step details for agents
  - `apply-diff` - Applies and validates code changes
  - `status` - Shows adoption progress
  - `skip` - Skips a step
  - `rollback` - Rolls back changes
  - `generate-plan` - Plan generation (placeholder)
- Dependency graph analysis with topological sorting (Kahn's algorithm)
- Language validators:
  - TypeScript (via `tsc --noEmit`)
  - Python (via `py_compile` + `mypy`)
  - Go (via `go build`)
- Diff matching with fuzzy value comparison
- Automatic rollback on validation failures
- Diff recording for audit trail
- Comprehensive test suite:
  - 97 unit tests (79% coverage for pkg/driftadopt)
  - 6 integration tests
  - 8 E2E tests
- Test fixtures for common scenarios:
  - Simple property updates
  - Multi-resource with dependencies
  - Resource deletion
  - Resource replacement
- Complete documentation:
  - README with quick start and examples
  - Inline code documentation
  - Test fixture READMEs

### Known Limitations
- `generate-plan` command is not fully implemented
- Preview runner uses mocks in tests
- Limited to TypeScript, Python, and Go languages

### Technical Details
- Built with Go 1.21+
- CLI framework: Cobra
- Dependency management: Go modules
- Test organization: Build tags (unit, integration, e2e)
- Code coverage: 71% overall

[0.1.0]: https://github.com/pulumi/pulumi-drift-adoption-tool/releases/tag/v0.1.0
[Unreleased]: https://github.com/pulumi/pulumi-drift-adoption-tool/compare/v0.1.0...HEAD
