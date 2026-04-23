# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- **Unified property format**: `add_to_code` resources now use the same `properties` array as `update_code` instead of `inputProperties` map. Properties are flattened to leaf-level dot-paths with `dependsOn` as a structured field.
- **Dependency sorting expanded**: `update_code` resources with cross-resource `dependsOn` now participate in topological sorting alongside `add_to_code` resources.

### Removed
- `inputProperties` field from resource output (replaced by `properties` array)

## [1.1.0] - 2026-04-21

### Added
- **Two-phase output model**: compact summary JSON to stdout, full resource details written to output file
- **`--output-file` flag** to specify path for full output file (defaults to auto-generated temp file)
- **`--skip-refresh` flag** to omit `--refresh` from `pulumi preview`
- **`--dep-map-file` flag** to provide a pre-computed dependency map (skips stack export on subsequent calls)
- **`--exclude-urns` flag** to exclude specific resource URNs from results
- **Dependency map caching**: state export is processed in memory to build a dependency map (resource names, types, output property names only — no secret values), saved to a temp file for reuse. When `--dep-map-file` is provided, the file is reused as-is (not overwritten)
- **Schema-based output filtering**: uses `pulumi package get-schema` to identify and skip computed-only properties (e.g. `tagsAll`, `arn`, `version`) that should not appear in `add_to_code` output
- **Secret value supplementation**: `[secret]` placeholder values in preview output are supplemented with real plaintext from state export using Pulumi's secret envelope format
- **SDK PropertyPath parsing**: uses Pulumi SDK `resource.PropertyPath` for correct handling of bracket-quoted keys, consecutive indices, and dots in property keys
- **`parseErrors` field in summary output**: all three format paths (standard JSON, engine events, NDJSON) track and report corrupt entries via stderr warnings and a `parseErrors` count in the summary JSON
- **`--show-secrets` on `pulumi preview`**: ensures preview OldState values are plaintext, consistent with state export, for correct `DeepEquals` matching
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
- **Value resolution fix**: `currentValue` now always uses `NewState.Inputs` (matching engine's `TranslateDetailedDiff` semantics) instead of checking stale Outputs first
- **Unknown sentinel filtering**: all 7 Pulumi SDK unknown sentinel types are now filtered using `plugin` constants
- **ResourceMetadata struct**: replaces flat `DependencyMap` entries to hold dependencies, schema `inputProperties`, and state lookup data per resource
- Removed redundant property-level change `Kind` field from output

### Removed
- **`--max-resources` flag**: No longer needed — full output goes to a file, so there is no stdout size concern
- E2E tests (moved to agent-skills repo)
- Examples directory
- GitHub issue and PR templates

### Security
- **Dependency map contains no secrets**: state export (via `--show-secrets`) is processed in memory; the dependency map persisted to disk contains only resource names, types, and output property names
- **Note**: the output file (`--output-file`) may contain plaintext secret values where `[secret]` placeholders were supplemented from state export
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
[1.1.0]: https://github.com/pulumi-labs/pulumi-tool-drift-adopter/releases/tag/v1.1.0
[Unreleased]: https://github.com/pulumi-labs/pulumi-tool-drift-adopter/compare/v1.1.0...HEAD
