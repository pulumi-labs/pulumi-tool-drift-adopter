# pulumi-tool-drift-adopter

CLI tool for AI agents to adopt infrastructure drift back into Pulumi IaC.

[![Test](https://github.com/pulumi-labs/pulumi-tool-drift-adopter/actions/workflows/test.yml/badge.svg)](https://github.com/pulumi-labs/pulumi-tool-drift-adopter/actions/workflows/test.yml)

## Overview

Drift occurs when infrastructure is modified outside of Pulumi. This tool detects drift and outputs JSON describing what code changes are needed to match the actual infrastructure state.

Designed for AI agents to call iteratively: run `next` → get changes → update code → repeat until clean.

## Installation

```bash
pulumi plugin install tool drift-adopter --server github://api.github.com/pulumi-labs
```

## Usage

### Mode 1: Standalone (runs preview internally)

```bash
cd your-pulumi-project
pulumi plugin run drift-adopter -- next [--stack <name>]
```

Runs `pulumi preview --json --refresh` internally and parses the output.

### Mode 2: With pre-generated events file

```bash
# First, run refresh and preview externally
pulumi refresh
pulumi preview --json > events.json

# Then pass the events file
pulumi plugin run drift-adopter -- next --events-file events.json
```

Use this mode when integrating with deployment systems that run preview separately.

## Output

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

**Status values:**
- `changes_needed` - Code changes required
- `clean` - No drift, code matches state
- `error` - Preview failed

**Actions:**
- `update_code` - Update properties in code to match `desiredValue`
- `delete_from_code` - Remove resource from code (exists in code but not infrastructure)
- `add_to_code` - Add resource to code (exists in infrastructure but not code)

## Flags

| Flag | Description |
|------|-------------|
| `--stack` | Pulumi stack name (default: current stack) |
| `--events-file` | Path to engine events file (skips running preview) |
| `--max-resources` | Max resources per batch (default: 10, 0 = unlimited) |
| `--project` | Project directory (default: ".") |

## Limitations

This tool relies on `pulumi refresh` which only tracks resources already in state. Resources created outside Pulumi must first be imported with [`pulumi import`](https://www.pulumi.com/docs/cli/commands/pulumi_import/).

## Development

```bash
git clone https://github.com/pulumi-labs/pulumi-tool-drift-adopter.git
cd pulumi-tool-drift-adopter
just install-tools
just install-hooks
just build
just test-unit
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

Apache 2.0 - See [LICENSE](LICENSE)
