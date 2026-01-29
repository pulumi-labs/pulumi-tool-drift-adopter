# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.1] - 2026-01-29

### Changed
- Moved skill to [pulumi/agent-skills](https://github.com/pulumi/agent-skills) repository
- Simplified CI to run unit tests only
- Switched task runner from Make to Just
- Updated Go version to 1.24

### Added
- Apache 2.0 LICENSE file
- Copyright headers to all Go source files
- Pre-push git hook for linting and testing

### Removed
- E2E tests (moved to agent-skills repo)
- Examples directory
- CONTRIBUTING.md
- GitHub issue and PR templates

### Security
- Updated golang.org/x/crypto to v0.47.0

## [1.0.0] - 2026-01-22

### Added
- **`--events-file` flag** to accept pre-generated engine events from deployment systems
- Comprehensive unit tests for events file functionality
- Claude skill (`skills/drift-adopt.md`) that encapsulates the complete drift adoption workflow
- E2E test using pulumitest and Claude SDK to validate the full workflow
- Test demonstrates: deploy stack → create drift → invoke Claude → verify fixes
- `--stack` flag to `next` command to specify Pulumi stack name
- Simple stateless CLI tool for drift adoption
- `next` command runs `pulumi preview --json` and returns structured output
- Inverted logic: interprets state (old values) as desired, code (new values) as current
- Three action types:
  - `delete_from_code` - Resource in code but not in state
  - `add_to_code` - Resource in state but not in code
  - `update_code` - Resource exists in both but differs
- JSON output with status: `changes_needed`, `clean`, or `error`
- Property-level change details with `currentValue` and `desiredValue`
- Agent-oriented design: tool called by AI agents iteratively
- Minimal implementation: single command, ~180 lines of code
- Built with Go 1.21+ and Cobra CLI framework
- Simple enough to verify manually with real Pulumi projects

### Changed
- Skip preview call when `--events-file` is provided (enables deployment system integration)
- `next` command now automatically runs `pulumi preview --refresh`
- No need to manually run `pulumi refresh` before using the tool
- Updated all documentation to reflect automatic refresh behavior
- Maintains backward compatibility (existing usage unchanged)

### Fixed
- golangci-lint configuration updated for newer linter versions

[1.0.0]: https://github.com/pulumi/pulumi-tool-drift-adopter/releases/tag/v1.0.0
[1.0.1]: https://github.com/pulumi/pulumi-tool-drift-adopter/releases/tag/v1.0.1
[Unreleased]: https://github.com/pulumi/pulumi-tool-drift-adopter/compare/v1.0.1...HEAD
