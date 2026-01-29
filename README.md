# pulumi-tool-drift-adopter

A CLI tool for adopting infrastructure drift back into Pulumi IaC, designed to work with AI agents like Claude.

[![Test](https://github.com/pulumi/pulumi-tool-drift-adopter/actions/workflows/test.yml/badge.svg)](https://github.com/pulumi/pulumi-tool-drift-adopter/actions/workflows/test.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/pulumi/pulumi-tool-drift-adopter)](https://goreportcard.com/report/github.com/pulumi/pulumi-tool-drift-adopter)

## Overview

`pulumi-drift-adopt` helps you incorporate infrastructure drift (changes made outside of Pulumi IaC) back into your infrastructure as code. It's designed to be called iteratively by AI agents (like Claude) to guide the drift adoption process.

### What is Drift Adoption?

Drift occurs when infrastructure is modified directly in the cloud console or via other tools, causing your infrastructure's actual state to diverge from what's defined in your IaC. Drift adoption is the process of:

1. **Detect drift** - Run preview with refresh to see actual infrastructure vs code
2. **Update code** - Agent updates code to match the actual state
3. **Iterate** - Repeat until preview is clean

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

The tool automatically runs `pulumi preview --refresh`, which updates state to match actual infrastructure, then compares it to your code.

After refresh, the state represents the actual infrastructure (what you want), and your code represents what's currently written (what needs to be updated).

When `pulumi preview` shows:
- **Create operation** → Resource is in code but not in state → **DELETE from code**
- **Delete operation** → Resource is in state but not in code → **ADD to code**
- **Update operation** → Resource differs → **UPDATE code to match state**

For property changes:
- **Old value (LHS)** = What's in state = What you want (desired value)
- **New value (RHS)** = What's in code = What's currently there (current value)

The tool inverts this logic and tells you exactly what to change in your code.

### Limitations

**Important:** This tool relies on `pulumi refresh` to detect drift, which only tracks resources that are already in the Pulumi state file. If a resource was created outside of Pulumi (e.g., manually in the cloud console), it will not be tracked by refresh and therefore cannot be adopted by this tool.

To adopt resources created outside of Pulumi, you need to first import them into your Pulumi state using [`pulumi import`](https://www.pulumi.com/docs/cli/commands/pulumi_import/).

## Installation

### Prerequisites

- Go 1.24 or later
- [Pulumi CLI](https://www.pulumi.com/docs/install/)

### Install with Go

```bash
go install github.com/pulumi/pulumi-tool-drift-adopter/cmd/pulumi-drift-adopt@latest
```

### Install from Source

```bash
git clone https://github.com/pulumi/pulumi-tool-drift-adopter.git
cd pulumi-tool-drift-adopter
just build
```

## Quick Start

### 1. Run the Tool

The tool automatically refreshes state and detects drift:

```bash
cd your-pulumi-project
pulumi-drift-adopt next

# Or specify a stack
pulumi-drift-adopt next --stack dev
```

### 2. Example Output

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

Runs `pulumi preview --json --refresh` to automatically detect drift and analyzes the output.

```bash
pulumi-drift-adopt next [--project .] [--stack <stack-name>] [--events-file <path>]
```

**Output Status:**
- `changes_needed` - Code changes required (includes list of resources and properties)
- `clean` - No drift detected, code matches state
- `error` - Preview failed (likely a code error to fix)

**Flags:**
- `--project string` - Project directory (default: ".")
- `--stack string` - Pulumi stack name (optional, uses current stack if not specified)
- `--events-file string` - Path to pre-generated engine events file (skips calling preview, for deployment system integration)
- `--max-resources int` - Maximum number of resources to return per batch (default: 10, use 0 for unlimited)

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

## Claude Skill

A Claude skill is provided in `skills/drift-adopt/SKILL.md` that encapsulates the complete workflow for using this tool. The skill provides Claude with:

- Step-by-step instructions for running the tool
- How to interpret each JSON output status
- What actions to take for each resource change type
- How to iterate until drift is resolved
- Troubleshooting guidance

### Using the Skill

If you're using Claude (via API or CLI), you can reference the skill:

```
I need you to adopt infrastructure drift using the drift-adopt skill.
Please read skills/drift-adopt/SKILL.md and follow the workflow to fix all drift.
```

Claude will:
1. Run `pulumi-drift-adopt next`
2. Parse the JSON output
3. Make necessary code changes
4. Repeat until status is "clean"

See `test/e2e/README.md` for an automated test that demonstrates this workflow.

### Note on Skill Versions

**Important:** The skill and E2E tests in this repository (`skills/drift-adopt.md` and `test/e2e/`) are designed for the **non-Neo version** of the drift adoption workflow. This version:

- Runs `pulumi preview --json` directly via CLI (no engine events files needed)
- Works with AI agents that have shell access to run Pulumi commands
- Does not require the `pulumi_preview` MCP tool or `--events-file` flag
- Is simpler and suitable for local development and basic Claude integration

For the production Neo agent version that uses:
- The `pulumi_preview` MCP tool with engine events files
- The `--events-file` flag for passing pre-generated events
- Deployment system integration
- Todo list management and git workflow

Refer to the skill in the `pulumi-service` repository at `cmd/agents/src/agents_py/skills/adopt-drift/SKILL.md`.

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

### Example 2: Resource Deleted from Infrastructure

Code has a bucket, but it was deleted directly in the cloud console.

After refresh, the state no longer includes this bucket, but code still references it.

Tool output:
```json
{
  "action": "delete_from_code",
  "name": "deleted-bucket"
}
```

Remove the bucket from your code to match the actual infrastructure state.

### Example 3: Resource Removed from Code

Your state has a bucket (deployed infrastructure), but your current code doesn't define it.

This happens when the code has changed since the last stack update - for example, if the resource was removed from the code but the stack hasn't been updated yet. After refresh, the infrastructure still exists and is tracked in state, but it's missing from code.

Tool output:
```json
{
  "action": "add_to_code",
  "name": "missing-bucket",
  "type": "aws:s3/bucket:Bucket"
}
```

Add the bucket back to your code so it matches the deployed state.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

### Project Structure

```
.
├── cmd/
│   └── pulumi-drift-adopt/    # CLI
│       ├── main.go            # Entry point
│       ├── root.go            # Root command
│       └── next.go            # Main command (all logic)
├── skills/
│   └── drift-adopt.md         # Claude skill for drift adoption
├── test/
│   └── e2e/                   # E2E tests with Claude SDK
│       ├── drift_adoption_test.go
│       └── README.md
└── bin/                       # Built binary
```

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.

## Related Projects

- [pulumi-terraform-migrate](https://github.com/pulumi/pulumi-terraform-migrate) - Migrate from Terraform to Pulumi (inspiration for this tool)
- [Pulumi](https://github.com/pulumi/pulumi) - Infrastructure as Code platform

## Support

- 🐛 [Issue Tracker](https://github.com/pulumi/pulumi-tool-drift-adopter/issues)
- 💬 [Pulumi Community Slack](https://slack.pulumi.com/)
