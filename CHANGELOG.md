# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Two-phase output model**: compact summary JSON to stdout, full resource details written to output file
- **`--output-file` flag** to specify path for full output file (defaults to auto-generated temp file)
- **`--skip-refresh` flag** to omit `--refresh` from `pulumi preview`
- **`--dep-map-file` flag** to provide a pre-computed dependency map (skips stack export on subsequent calls)
- **`--exclude-urns` flag** to exclude specific resource URNs from results
- **Dependency map caching**: state export is processed in memory to build a dependency map (resource names, types, output property names only — no secret values), saved to a file for reuse
- **Topological dependency sorting**: resources sorted by dependency level (leaf nodes first) using Kahn's algorithm
- **Element-level dependency resolution**: map and array properties (e.g. `dependsOn`) resolve individual elements instead of collapsing
- **`-replace` property kind handling**: `add-replace` and `delete-replace` diff kinds are now correctly inverted
- **`stop_with_skipped` status**: returned when all remaining resources were excluded or had missing properties
- Apache 2.0 LICENSE file
- Copyright headers to all Go source files
- Pre-push git hook for linting and testing
- CONTRIBUTING.md tailored to this repo (replaces copy from pulumi/pulumi)
- CODE-OF-CONDUCT.md

### Changed
- Moved skill to [pulumi/agent-skills](https://github.com/pulumi/agent-skills) repository
- Simplified CI to run unit tests only
- Switched task runner from Make to Just
- Updated Go version to 1.24
- Command constructors refactored from global vars to `newRootCmd()`/`newNextCmd()` functions for test isolation
- Properties where both current and desired values are nil (computed-only fields) are now filtered out

### Removed
- E2E tests (moved to agent-skills repo)
- Examples directory
- GitHub issue and PR templates

### Security
- **Eliminated plaintext secrets on disk**: state export (containing decrypted secrets) is now processed in memory only and never written to disk; only a dependency map (containing resource names, types, and output property names) is cached
- Updated golang.org/x/crypto to v0.47.0

## [1.0.1] - 2026-01-22

### Fixed
- NDJSON parsing now handles pulumi-service field name differences (`resource` vs `metadata`)
- Property kind inversion corrected (was showing opposite of intended)
- Property extraction for `add_to_code` actions now works correctly

### Changed
- Simplified parsing logic to handle only the two real formats (JSON and NDJSON)

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

[1.0.0]: https://github.com/pulumi-labs/pulumi-tool-drift-adopter/releases/tag/v1.0.0
[1.0.1]: https://github.com/pulumi-labs/pulumi-tool-drift-adopter/releases/tag/v1.0.1
[Unreleased]: https://github.com/pulumi-labs/pulumi-tool-drift-adopter/compare/v1.0.1...HEAD
