# pulumi-drift-adoption-tool

A guided workflow tool for adopting infrastructure drift back into Pulumi IaC, designed to work with AI agents like Claude.

[![Test](https://github.com/pulumi/pulumi-drift-adoption-tool/actions/workflows/test.yml/badge.svg)](https://github.com/pulumi/pulumi-drift-adoption-tool/actions/workflows/test.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/pulumi/pulumi-drift-adoption-tool)](https://goreportcard.com/report/github.com/pulumi/pulumi-drift-adoption-tool)

## Overview

`pulumi-drift-adopt` helps you incorporate infrastructure drift (changes made outside of Pulumi IaC) back into your infrastructure as code. It follows an **agent-oriented** pattern where AI agents (like Claude) call the tool to perform drift adoption step-by-step.

### What is Drift Adoption?

Drift occurs when infrastructure is modified directly in the cloud console or via other tools, causing your infrastructure's actual state to diverge from what's defined in your IaC. Drift adoption is the process of:

1. **Detecting drift** - Using Pulumi preview to identify changes
2. **Analyzing dependencies** - Understanding which resources to update first
3. **Generating code** - Agent generates code to match actual state
4. **Validating changes** - Tool validates compilation and preview
5. **Iterating** - Processing drift chunk by chunk until complete

## Architecture

The tool follows a **sequential gate pattern** inspired by [`pulumi-terraform-migrate`](https://github.com/pulumi/pulumi-terraform-migrate):

```
┌──────────────────────────────────┐
│  Agent calls `next` command      │
│  Tool suggests next action       │
│  Agent executes suggestion       │
│  Repeat until "STOP"             │
└──────────────────────────────────┘
```

**Agent-Oriented Design:**
- Agents call the tool (not the other way around)
- Tool validates and provides feedback
- No LLM API dependencies
- Works with any AI agent (Claude, ChatGPT, etc.)

Key features:
- **Stateful workflow**: Progress tracked in `drift-plan.json`
- **Dependency-aware**: Processes resources in topological order (leaves first)
- **Safe validation**: Compilation and preview checks before accepting changes
- **Automatic rollback**: Failed changes are reverted
- **Resumable**: Can stop and restart at any point

## Installation

### Prerequisites

- Go 1.21 or later
- [Pulumi CLI](https://www.pulumi.com/docs/install/)
- Language-specific tooling for your Pulumi project:
  - **TypeScript**: Node.js and `tsc`
  - **Python**: Python 3.8+ (with `mypy` recommended)
  - **Go**: Go 1.21+

### Install from Source

```bash
git clone https://github.com/pulumi/pulumi-drift-adoption-tool.git
cd pulumi-drift-adoption-tool
go install ./cmd/pulumi-drift-adopt
```

Or build locally:

```bash
cd pulumi-drift-adoption-tool
go build -o ./bin/pulumi-drift-adopt ./cmd/pulumi-drift-adopt
```

## Quick Start

### 1. Detect Drift

Run a preview to detect drift in your stack:

```bash
cd your-pulumi-project
pulumi preview --diff > preview-output.txt
```

### 2. Generate Adoption Plan

**Note**: `generate-plan` is currently a placeholder. For now, you can manually create a drift-plan.json file based on your preview output.

```bash
pulumi-drift-adopt generate-plan --stack dev
```

Expected plan structure:

```json
{
  "stack": "dev",
  "generatedAt": "2024-01-15T10:00:00Z",
  "totalChunks": 2,
  "chunks": [
    {
      "id": "chunk-001",
      "order": 0,
      "resources": [{
        "urn": "urn:pulumi:dev::proj::aws:s3/bucket:Bucket::my-bucket",
        "type": "aws:s3/bucket:Bucket",
        "name": "my-bucket",
        "diffType": "update",
        "propertyDiff": [{
          "path": "tags.Environment",
          "oldValue": "dev",
          "newValue": "production",
          "diffKind": "update"
        }],
        "sourceFile": "index.ts",
        "sourceLine": 10
      }],
      "status": "pending",
      "dependencies": [],
      "attempt": 0
    }
  ]
}
```

### 3. Iterative Adoption with AI Agent

The typical workflow with an AI agent (like Claude):

```bash
# Agent calls: What should I do next?
pulumi-drift-adopt next

# Tool responds: "Next chunk ready: chunk-001 (Order: 0)"
# "View chunk details:"
# "  pulumi-drift-adopt show-chunk --chunk chunk-001"

# Agent views chunk details
pulumi-drift-adopt show-chunk --chunk chunk-001

# Agent generates code changes and applies them
pulumi-drift-adopt apply-diff --chunk chunk-001 --files file1.ts file2.ts

# Agent calls next again
pulumi-drift-adopt next

# Eventually: "STOP - Drift adoption complete!"
```

### 4. Check Status

```bash
pulumi-drift-adopt status
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

Shows the next step in the adoption workflow. This is the main orchestrator command that guides the entire process.

```bash
pulumi-drift-adopt next [--plan drift-plan.json] [--project .]
```

**Gates:**
1. Check if plan exists
2. Load plan
3. Check for pending chunks
4. Check for failed chunks
5. All complete - output "STOP"

### `show-chunk`

Displays detailed information about a chunk for agent consumption.

```bash
pulumi-drift-adopt show-chunk --chunk CHUNK_ID [--plan drift-plan.json] [--project .]
```

Shows:
- Resources in chunk
- Current source code
- Expected property changes
- Dependencies on other chunks

### `apply-diff`

Applies agent-generated code changes and validates them.

```bash
pulumi-drift-adopt apply-diff --chunk CHUNK_ID --files file1:content file2:content [--plan drift-plan.json] [--project .]
```

Process:
1. Applies code changes
2. Validates compilation
3. Runs pulumi preview (optional)
4. Matches diff with expected changes
5. Updates chunk status
6. Rolls back on failure

### `status`

Shows current drift adoption progress.

```bash
pulumi-drift-adopt status [--plan drift-plan.json] [--project .]
```

Displays:
- Overall progress percentage
- Status breakdown (completed/pending/failed/skipped)
- Next pending chunk
- Failed chunks with error messages
- Recent changes

### `skip`

Marks a chunk as skipped for manual handling later.

```bash
pulumi-drift-adopt skip --chunk CHUNK_ID [--reason "reason"] [--plan drift-plan.json]
```

### `rollback`

Rolls back code changes from a previously applied diff.

```bash
pulumi-drift-adopt rollback --diff DIFF_ID [--project .]
```

### `generate-plan`

**Note**: Currently a placeholder. Planned to analyze drift and create an adoption plan.

```bash
pulumi-drift-adopt generate-plan --stack STACK [--plan drift-plan.json]
```

## How It Works

### 1. Dependency Analysis

The tool analyzes your Pulumi state file to build a dependency graph using topological sorting (Kahn's algorithm):

```
VPC (order 0, no dependencies)
├── Subnet (order 1, depends on VPC)
└── SecurityGroup (order 1, depends on VPC)
```

Resources are processed in dependency order, ensuring dependencies are adopted before dependents.

### 2. Chunking

Resources at the same dependency level are grouped into chunks. Each chunk can be processed independently from chunks at other levels.

**Example:**
- Chunk 1 (order 0): VPC
- Chunk 2 (order 1): Subnet + SecurityGroup

### 3. Agent Code Generation

For each chunk, the agent:
1. Calls `show-chunk` to see current code and expected changes
2. Generates updated code
3. Calls `apply-diff` to submit changes

### 4. Validation

The tool validates all changes before accepting them:

1. **Compilation check**:
   - TypeScript: Runs `tsc --noEmit`
   - Python: Runs `py_compile` + `mypy`
   - Go: Runs `go build`

2. **Preview check** (optional):
   - Runs `pulumi preview --json`
   - Parses resource diffs

3. **Diff matching**:
   - Compares actual preview with expected changes
   - Supports fuzzy value matching (string ↔ bool, string ↔ number)

If validation fails, changes are automatically rolled back.

### 5. Diff Recording

All applied changes are recorded with:
- Unique diff ID
- Original file contents
- Timestamp
- Associated chunk ID

This enables rollback and audit trail.

## Configuration

### Global Flags

All commands support these flags:

- `--plan string`: Path to drift plan file (default: "drift-plan.json")
- `--project string`: Project directory (default: ".")

### Project Structure

```
your-pulumi-project/
├── index.ts                 # Your Pulumi program
├── Pulumi.yaml              # Project config
├── drift-plan.json          # Generated plan (gitignored)
└── .pulumi-drift/
    └── diffs/               # Recorded diffs for rollback
        ├── diff-001.json
        └── diff-002.json
```

## Examples

See the [`testdata/`](testdata/) directory for complete examples:

- [`drift-simple/`](testdata/drift-simple/) - Single resource with property updates
- [`drift-dependencies/`](testdata/drift-dependencies/) - Multi-resource with dependencies
- [`drift-deletion/`](testdata/drift-deletion/) - Resource deleted in cloud
- [`drift-replacement/`](testdata/drift-replacement/) - Resource requiring replacement

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

### Running Tests

```bash
# Unit tests (fast, no external dependencies)
go test -tags=unit ./...

# Integration tests (uses filesystem)
go test -tags=integration ./...

# E2E tests (full workflow scenarios)
go test -tags=e2e ./e2e/

# All tests with coverage
go test -tags=unit -coverprofile=coverage.out ./pkg/...
go tool cover -html=coverage.out
```

### Project Structure

```
.
├── cmd/
│   └── pulumi-drift-adopt/    # CLI commands (Cobra)
│       ├── main.go            # Entry point
│       ├── root.go            # Root command
│       ├── next.go            # Sequential gate pattern
│       ├── show_chunk.go      # Display chunk for agents
│       ├── apply_diff.go      # Apply and validate changes
│       ├── status.go          # Show progress
│       ├── skip.go            # Skip chunk
│       └── rollback.go        # Rollback changes
├── pkg/
│   └── driftadopt/            # Core logic
│       ├── types.go           # Data structures
│       ├── planfile.go        # Plan persistence
│       ├── plan_methods.go    # Plan operations
│       ├── graph.go           # Dependency graph
│       ├── diff_recorder.go   # Diff tracking
│       ├── diff_applier.go    # Apply changes
│       ├── chunk_guide.go     # Agent guidance
│       ├── diffmatch.go       # Diff comparison
│       ├── apply_orchestrator.go  # Coordination
│       ├── preview.go         # Pulumi preview
│       └── validator/         # Language validators
│           ├── types.go
│           ├── typescript.go
│           ├── python.go
│           └── go.go
├── e2e/
│   └── drift_test.go          # E2E workflow tests
├── testdata/                  # Test fixtures
│   ├── drift-simple/
│   ├── drift-dependencies/
│   ├── drift-deletion/
│   └── drift-replacement/
└── DRIFT_ADOPTION_DESIGN.md   # Detailed design doc
```

## Design

For detailed architecture and design decisions, see [DRIFT_ADOPTION_DESIGN.md](DRIFT_ADOPTION_DESIGN.md).

Key patterns:
- **Sequential gate pattern**: Validates preconditions at each step
- **Agent-oriented**: Tool called BY agents, not calling out
- **Test-Driven Development**: Comprehensive unit, integration, and E2E tests
- **Fail-fast validation**: Catches errors early with automatic rollback
- **Stateful checkpointing**: Resume after interruption

## Project Status

✅ **Phases Complete:**
1. Core Data Structures
2. Dependency Graph Analysis
3. Diff Management
4. Compilation Validation
5. Diff Matching
6. Apply-Diff Orchestration
7. CLI Commands
8. End-to-End Testing
9. Documentation & Polish

**Current Status**: Ready for initial testing

**Known Limitations**:
- `generate-plan` command is a placeholder (manual plan creation required)
- Preview runner not fully implemented (validation tests use mocks)
- Limited to TypeScript, Python, and Go (other languages need validators)

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

**Development Priorities:**
1. Complete `generate-plan` implementation
2. Add more language validators (C#, Java, etc.)
3. Improve error messages and agent guidance
4. Add more test coverage
5. Performance optimizations

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.

## Related Projects

- [pulumi-terraform-migrate](https://github.com/pulumi/pulumi-terraform-migrate) - Migrate from Terraform to Pulumi (inspiration for this tool)
- [Pulumi](https://github.com/pulumi/pulumi) - Infrastructure as Code platform

## Support

- 📖 [Design Documentation](DRIFT_ADOPTION_DESIGN.md)
- 🐛 [Issue Tracker](https://github.com/pulumi/pulumi-drift-adoption-tool/issues)
- 💬 [Pulumi Community Slack](https://slack.pulumi.com/)
