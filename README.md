# pulumi-drift-adoption-tool

A guided state machine for LLMs to iteratively adopt infrastructure drift back into Pulumi IaC.

[![Test](https://github.com/pulumi/pulumi-drift-adoption-tool/actions/workflows/test.yml/badge.svg)](https://github.com/pulumi/pulumi-drift-adoption-tool/actions/workflows/test.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/pulumi/pulumi-drift-adoption-tool)](https://goreportcard.com/report/github.com/pulumi/pulumi-drift-adoption-tool)
[![codecov](https://codecov.io/gh/pulumi/pulumi-drift-adoption-tool/branch/main/graph/badge.svg)](https://codecov.io/gh/pulumi/pulumi-drift-adoption-tool)

## Overview

`pulumi-drift-adopt` helps you incorporate infrastructure drift (changes made outside of Pulumi IaC) back into your infrastructure as code. It's designed to work iteratively with LLMs (like Claude) to guide you through the adoption process step-by-step.

### What is Drift Adoption?

Drift occurs when infrastructure is modified directly in the cloud console or via other tools, causing your infrastructure's actual state to diverge from what's defined in your IaC. Drift adoption is the process of:

1. **Detecting drift** - Using Pulumi's drift detection to identify changes
2. **Analyzing dependencies** - Understanding which resources to update first
3. **Generating code** - Using LLMs to update your IaC to match reality
4. **Validating changes** - Ensuring the code compiles and produces the expected diff
5. **Iterating** - Processing drift chunk by chunk until complete

## Architecture

The tool follows a **guided state machine** pattern inspired by [`pulumi-terraform-migrate`](https://github.com/pulumi/pulumi-terraform-migrate):

```
┌─────────────────────────────────┐
│  LLM calls `next` repeatedly    │
│  Tool suggests next action      │
│  LLM executes suggestion        │
│  Repeat until "STOP"            │
└─────────────────────────────────┘
```

Key features:
- **Stateful workflow**: Progress tracked in `drift-plan.json`
- **Dependency-aware**: Processes resources in correct order (leaves first)
- **LLM-driven**: Uses Claude to generate code changes
- **Safe**: Validates compilation and preview before accepting changes
- **Resumable**: Can stop and restart at any point

## Installation

### Prerequisites

- Go 1.21 or later
- [Pulumi CLI](https://www.pulumi.com/docs/install/)
- Language-specific tooling for your Pulumi project:
  - **TypeScript**: Node.js and `tsc`
  - **Python**: Python 3.8+ and `mypy` (optional)
  - **Go**: Go 1.21+

### Install from Source

```bash
git clone https://github.com/pulumi/pulumi-drift-adoption-tool.git
cd pulumi-drift-adoption-tool
go install ./cmd/pulumi-drift-adopt
```

### Install from Release

```bash
# Coming soon
go install github.com/pulumi/pulumi-drift-adoption-tool/cmd/pulumi-drift-adopt@latest
```

## Quick Start

### 1. Configure Drift Detection

Enable drift detection on your Pulumi stack:

```bash
cd your-pulumi-project
pulumi config set deployment-settings.driftDetection enabled
```

### 2. Detect Drift

Run a refresh to update state with current cloud configuration:

```bash
pulumi refresh
```

Check for drift:

```bash
pulumi preview --diff
```

### 3. Generate Adoption Plan

Create an adoption plan that orders resources by dependencies:

```bash
pulumi-drift-adopt generate-plan --stack dev --output drift-plan.json
```

### 4. Iterative Adoption with LLM

The typical workflow with an LLM (like Claude):

```bash
# LLM repeatedly calls this command
pulumi-drift-adopt next

# Tool suggests next action, e.g.:
# "Next step: Adopt chunk-001"
# "Run: pulumi-drift-adopt adopt-chunk drift-plan.json chunk-001"

# LLM executes the suggestion
pulumi-drift-adopt adopt-chunk drift-plan.json chunk-001

# LLM calls `next` again
pulumi-drift-adopt next

# Eventually: "STOP - Drift adoption complete!"
```

### 5. Review and Apply

```bash
# Review changes
git diff

# Test
pulumi preview

# Create PR
gh pr create --fill
```

## Commands

### `next`

Suggests the next step in the adoption workflow. This is the main orchestrator command.

```bash
pulumi-drift-adopt next [--plan drift-plan.json]
```

### `generate-plan`

Creates an adoption plan by analyzing drift and dependencies.

```bash
pulumi-drift-adopt generate-plan --stack STACK --output FILE
```

### `adopt-chunk`

Processes a single drift chunk: generates code, validates, and updates plan.

```bash
pulumi-drift-adopt adopt-chunk PLAN_FILE CHUNK_ID
```

### `status`

Shows current adoption progress.

```bash
pulumi-drift-adopt status [--plan drift-plan.json]
```

### `skip`

Marks a chunk as skipped (for manual handling).

```bash
pulumi-drift-adopt skip PLAN_FILE CHUNK_ID [--reason "reason"]
```

### `reset-chunk`

Resets a failed chunk to retry.

```bash
pulumi-drift-adopt reset-chunk PLAN_FILE CHUNK_ID
```

## How It Works

### 1. Dependency Analysis

The tool analyzes your Pulumi state file to build a dependency graph:

```
Resource A (no dependencies) ← Resource B ← Resource C
Resource D (no dependencies) ← Resource E
```

Resources are processed in **topological order** (leaves first), ensuring dependencies are handled before dependents.

### 2. Chunking

Related drift changes are grouped into chunks for atomic processing. Each chunk contains one or more resources with their expected property changes.

### 3. LLM Code Generation

For each chunk, the tool:
1. Reads current source code
2. Generates a prompt with expected changes
3. Calls Claude API to generate updated code
4. Applies the changes

### 4. Validation

Before accepting changes:
1. **Compilation check**: Ensures code is syntactically valid
2. **Preview check**: Runs `pulumi preview` to get actual diff
3. **Diff matching**: Compares actual diff with expected changes

If validation fails, changes are rolled back and the chunk is marked as failed.

## Configuration

### Environment Variables

- `ANTHROPIC_API_KEY`: Claude API key (required for code generation)
- `PULUMI_ACCESS_TOKEN`: Pulumi access token (if using Pulumi Cloud)

### Plan File Format

The `drift-plan.json` file tracks adoption progress:

```json
{
  "stack": "dev",
  "generatedAt": "2024-01-15T10:30:00Z",
  "totalChunks": 5,
  "chunks": [
    {
      "id": "chunk-001",
      "order": 0,
      "status": "completed",
      "resources": [
        {
          "urn": "urn:pulumi:dev::proj::aws:s3/bucket:Bucket::my-bucket",
          "type": "aws:s3/bucket:Bucket",
          "diffType": "update",
          "propertyDiff": [
            {
              "path": "tags.Environment",
              "oldValue": "dev",
              "newValue": "production"
            }
          ]
        }
      ]
    }
  ]
}
```

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

### Running Tests

```bash
# Unit tests (fast, no external dependencies)
go test -tags=unit ./...

# Integration tests (uses filesystem, mock Pulumi)
go test -tags=integration ./...

# E2E tests (full workflow, requires setup)
go test -tags=e2e ./e2e/...

# All tests
go test -tags=unit,integration ./...
```

### Project Structure

```
.
├── cmd/
│   └── pulumi-drift-adopt/    # CLI commands
├── pkg/
│   └── driftadopt/            # Core logic
│       ├── validator/         # Language validators
│       ├── editor/            # Code editors
│       └── testutil/          # Test utilities
├── e2e/                       # E2E tests
├── testdata/                  # Test fixtures
└── DRIFT_ADOPTION_DESIGN.md   # Detailed design doc
```

## Design

For detailed architecture and design decisions, see [DRIFT_ADOPTION_DESIGN.md](DRIFT_ADOPTION_DESIGN.md).

Key patterns:
- **Sequential gate pattern**: Validates preconditions before proceeding
- **Test-Driven Development**: Tests written before implementation
- **Fail-fast validation**: Catches errors early
- **Stateful checkpointing**: Can resume after interruption

## Roadmap

See [GitHub Issues](https://github.com/pulumi/pulumi-drift-adoption-tool/issues) for planned features and current development.

### Phase 1 (Current)
- ✅ Project setup
- 🚧 Core data structures
- 🚧 Dependency graph analysis

### Phase 2
- LLM integration
- Compilation validators
- Diff matching

### Phase 3
- CLI commands
- E2E testing
- Documentation

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.

## Related Projects

- [pulumi-terraform-migrate](https://github.com/pulumi/pulumi-terraform-migrate) - Migrate from Terraform to Pulumi
- [Pulumi](https://github.com/pulumi/pulumi) - Infrastructure as Code

## Support

- 📖 [Documentation](DRIFT_ADOPTION_DESIGN.md)
- 🐛 [Issue Tracker](https://github.com/pulumi/pulumi-drift-adoption-tool/issues)
- 💬 [Pulumi Community Slack](https://slack.pulumi.com/)
