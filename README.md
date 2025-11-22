# pulumi-drift-adoption-tool

A simple tool for adopting infrastructure drift back into Pulumi IaC, designed to work with AI agents like Claude.

[![Test](https://github.com/pulumi/pulumi-drift-adoption-tool/actions/workflows/test.yml/badge.svg)](https://github.com/pulumi/pulumi-drift-adoption-tool/actions/workflows/test.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/pulumi/pulumi-drift-adoption-tool)](https://goreportcard.com/report/github.com/pulumi/pulumi-drift-adoption-tool)

## Overview

`pulumi-drift-adopt` helps you incorporate infrastructure drift (changes made outside of Pulumi IaC) back into your infrastructure as code. It's designed to be called iteratively by AI agents (like Claude) to guide the drift adoption process.

### What is Drift Adoption?

Drift occurs when infrastructure is modified directly in the cloud console or via other tools, causing your infrastructure's actual state to diverge from what's defined in your IaC. Drift adoption is the process of:

1. **Run refresh** - Update Pulumi state to match actual infrastructure
2. **Detect drift** - Run preview to see differences between code and state
3. **Update code** - Agent updates code to match the actual state
4. **Iterate** - Repeat until preview is clean

## How It Works

The tool follows a simple stateless pattern:

```
┌──────────────────────────────────┐
│  Agent calls `next`              │
│  Tool runs pulumi preview        │
│  Tool returns needed changes     │
│  Agent updates code               │
│  Repeat until clean              │
└──────────────────────────────────┘
```

### Key Concept: Inverted Logic

After running `pulumi refresh`, the state represents the actual infrastructure (what you want), and your code represents what's currently written (what needs to be updated).

When `pulumi preview` shows:
- **Create operation** → Resource is in code but not in state → **DELETE from code**
- **Delete operation** → Resource is in state but not in code → **ADD to code**
- **Update operation** → Resource differs → **UPDATE code to match state**

For property changes:
- **Old value (LHS)** = What's in state = What you want (desired value)
- **New value (RHS)** = What's in code = What's currently there (current value)

The tool inverts this logic and tells you exactly what to change in your code.

## Installation

### Prerequisites

- Go 1.21 or later
- [Pulumi CLI](https://www.pulumi.com/docs/install/)

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

### 1. Ensure State is Up-to-Date

First, refresh your Pulumi state to match actual infrastructure:

```bash
cd your-pulumi-project
pulumi refresh
```

### 2. Run the Tool

The tool will run `pulumi preview` and tell you what code changes are needed:

```bash
pulumi-drift-adopt next
```

### 3. Example Output

```json
{
  "status": "changes_needed",
  "resources": [
    {
      "action": "update_code",
      "urn": "urn:pulumi:dev::app::aws:s3/bucket:Bucket::my-bucket",
      "type": "aws:s3/bucket:Bucket",
      "name": "my-bucket",
      "properties": [
        {
          "path": "tags.Environment",
          "currentValue": "dev",
          "desiredValue": "production",
          "kind": "update"
        }
      ]
    }
  ]
}
```

This tells you to update your code so that `my-bucket` has `tags.Environment = "production"`.

### 4. Update Code and Repeat

After updating your code, run `next` again:

```bash
pulumi-drift-adopt next
```

When all drift is resolved:

```json
{
  "status": "clean"
}
```

## Commands

### `next`

Runs `pulumi preview --json` and analyzes the output to determine what code changes are needed.

```bash
pulumi-drift-adopt next [--project .]
```

**Output Status:**
- `changes_needed` - Code changes required (includes list of resources and properties)
- `clean` - No drift detected, code matches state
- `error` - Preview failed (likely a code error to fix)

**Flags:**
- `--project string` - Project directory (default: ".")

## Workflow with AI Agent

The typical workflow with an AI agent (like Claude):

```bash
# Agent calls: What drift needs to be fixed?
pulumi-drift-adopt next

# Tool responds with JSON showing what needs to change
# Agent reads the output and updates code files

# Agent calls next again to verify
pulumi-drift-adopt next

# Tool responds with "status": "clean"
# Agent knows drift adoption is complete
```

## Agent-Oriented Design

- **Stateless**: No plan files or state management
- **Simple**: Single command that does one thing
- **Clear**: JSON output easy for agents to parse
- **Safe**: Only reads, never modifies files
- **Lightweight**: No LLM API dependencies

## Examples

### Example 1: Property Update

Code has:
```typescript
const bucket = new aws.s3.Bucket("my-bucket", {
    tags: { Environment: "dev" },
});
```

Actual infrastructure has `Environment: "production"`.

Tool output:
```json
{
  "action": "update_code",
  "name": "my-bucket",
  "properties": [{
    "path": "tags.Environment",
    "currentValue": "dev",
    "desiredValue": "production"
  }]
}
```

Update code to:
```typescript
const bucket = new aws.s3.Bucket("my-bucket", {
    tags: { Environment: "production" },
});
```

### Example 2: Resource in Code but Not State

Code has a bucket that doesn't exist in infrastructure.

Tool output:
```json
{
  "action": "delete_from_code",
  "name": "unused-bucket"
}
```

Remove the bucket from your code.

### Example 3: Resource in State but Not Code

Infrastructure has a bucket that's not in code.

Tool output:
```json
{
  "action": "add_to_code",
  "name": "missing-bucket",
  "type": "aws:s3/bucket:Bucket"
}
```

Add the bucket to your code.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

### Running Tests

```bash
# Unit tests
go test -tags=unit ./...

# Integration tests
go test -tags=integration ./...

# E2E tests
go test -tags=e2e ./e2e/

# All tests
go test -tags=unit,integration,e2e ./...
```

### Project Structure

```
.
├── cmd/
│   └── pulumi-drift-adopt/    # CLI
│       ├── main.go            # Entry point
│       ├── root.go            # Root command
│       └── next.go            # Main command
├── pkg/
│   └── driftadopt/            # Core logic
│       ├── types.go           # Data structures
│       ├── preview.go         # Preview parsing
│       └── validator/         # Language validators
│           ├── types.go
│           ├── typescript.go
│           ├── python.go
│           └── go.go
└── e2e/                       # E2E tests
```

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.

## Related Projects

- [pulumi-terraform-migrate](https://github.com/pulumi/pulumi-terraform-migrate) - Migrate from Terraform to Pulumi (inspiration for this tool)
- [Pulumi](https://github.com/pulumi/pulumi) - Infrastructure as Code platform

## Support

- 🐛 [Issue Tracker](https://github.com/pulumi/pulumi-drift-adoption-tool/issues)
- 💬 [Pulumi Community Slack](https://slack.pulumi.com/)
