# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- Claude skill (`skills/drift-adopt.md`) that encapsulates the complete drift adoption workflow
- E2E test using pulumitest and Claude SDK to validate the full workflow
- Test demonstrates: deploy stack → create drift → invoke Claude → verify fixes
- `--stack` flag to `next` command to specify Pulumi stack name

### Changed
- `next` command now automatically runs `pulumi preview --refresh`
- No need to manually run `pulumi refresh` before using the tool
- Updated all documentation to reflect automatic refresh behavior

## [1.0.0] - 2025-11-21

### Added
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

[1.0.0]: https://github.com/pulumi/pulumi-drift-adoption-tool/releases/tag/v1.0.0
[Unreleased]: https://github.com/pulumi/pulumi-drift-adoption-tool/compare/v1.0.0...HEAD
