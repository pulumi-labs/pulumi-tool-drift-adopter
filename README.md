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

## Parsing Logic

The `next` command accepts two input formats and normalizes both into a unified pipeline.

### Input Formats

#### Standard JSON (`pulumi preview --json`)

A single JSON object with a `steps` array. Each step may include `detailedDiff`, `replaceReasons`, and `diffReasons`. For replace operations, `detailedDiff` is often `null` and diff information lives in `replaceReasons`/`diffReasons` instead.

```json
{
  "steps": [
    {
      "op": "replace",
      "urn": "urn:pulumi:dev::proj::tls:index/privateKey:PrivateKey::my-key",
      "provider": "urn:pulumi:dev::proj::pulumi:providers:tls::default::uuid",
      "oldState": {
        "type": "tls:index/privateKey:PrivateKey",
        "inputs": { "algorithm": "ECDSA", "ecdsaCurve": "P256" },
        "outputs": { "algorithm": "ECDSA", "ecdsaCurve": "P256", "id": "..." }
      },
      "newState": {
        "type": "tls:index/privateKey:PrivateKey",
        "inputs": { "algorithm": "RSA", "rsaBits": 2048 },
        "outputs": { "algorithm": "RSA", "ecdsaCurve": "P224", "rsaBits": 2048 }
      },
      "diffReasons": ["algorithm", "ecdsaCurve"],
      "replaceReasons": ["algorithm", "ecdsaCurve"],
      "detailedDiff": null
    },
    {
      "op": "update",
      "urn": "urn:pulumi:dev::proj::command:local:Command::cmd-4",
      "oldState": { "inputs": { "create": "echo modified" }, "outputs": { "create": "echo modified" } },
      "newState": { "inputs": { "create": "echo original" } },
      "diffReasons": ["create", "environment"],
      "detailedDiff": {
        "create": { "kind": "update", "inputDiff": false },
        "environment": { "kind": "delete", "inputDiff": false }
      }
    }
  ]
}
```

#### NDJSON (pulumi-service MCP tool)

Newline-delimited JSON where each line is an engine event. Only `resourcePreEvent` lines are processed; `preludeEvent`, `summaryEvent`, `diagnosticEvent`, and `cancelEvent` are skipped. Key field-name differences from standard JSON: `old`/`new` instead of `oldState`/`newState`, `diffKind` instead of `kind`, and `diffs` instead of `diffReasons`/`replaceReasons`.

```json
{"type":"preludeEvent","preludeEvent":{"config":{"aws:region":"us-west-2"}}}
{"type":"resourcePreEvent","resourcePreEvent":{"metadata":{
  "op": "update",
  "urn": "urn:pulumi:dev::proj::aws:s3/bucket:Bucket::my-bucket",
  "type": "aws:s3/bucket:Bucket",
  "old": {
    "type": "aws:s3/bucket:Bucket",
    "inputs": { "tags": { "Environment": "production", "ManagedBy": "pulumi" } },
    "outputs": { "tags": { "Environment": "production", "ManagedBy": "pulumi" } }
  },
  "new": {
    "type": "aws:s3/bucket:Bucket",
    "inputs": { "tags": { "Environment": "dev" } },
    "outputs": { "tags": { "Environment": "dev" } }
  },
  "diffs": ["tags"],
  "detailedDiff": {
    "tags.Environment": { "diffKind": "update", "inputDiff": true },
    "tags.ManagedBy": { "diffKind": "delete", "inputDiff": true }
  }
}}}
{"type":"summaryEvent","summaryEvent":{"resourceChanges":{"update":1}}}
```

### Field Mapping

| Concept | Standard JSON | NDJSON |
|---------|--------------|--------|
| Wrapper | `{"steps": [...]}` | One JSON object per line |
| Old state | `oldState` | `old` |
| New state | `newState` | `new` |
| Diff kind | `detailedDiff[key].kind` | `detailedDiff[key].diffKind` |
| Diff keys (fallback) | `replaceReasons`, `diffReasons` | `diffs` |

### Normalization Pipeline

Both formats are parsed into `auto.PreviewStep` structs, then processed through two normalization stages before property extraction:

1. **DetailedDiff normalization** (`normalizeDetailedDiff`) — For update/replace steps where `DetailedDiff` is empty (common in standard JSON where `detailedDiff` is `null`), entries are synthesized from `ReplaceReasons` (preferred) or `DiffReasons` with `Kind: "update"` and `InputDiff: true`. The NDJSON parser performs equivalent normalization from its `diffs` field during format conversion.

2. **Property extraction** (`extractPropertyChanges`) — With `DetailedDiff` guaranteed populated for all update/replace steps, a single code path handles property value lookup. The `InputDiff` flag controls lookup strategy: input-diff entries resolve from `Inputs` only, while other entries try `Outputs` first with an `Inputs` fallback (`resolvePropertyValue`). Delete operations (no `DetailedDiff`) extract all properties from `OldState.Outputs`.

### Inversion

Preview output describes what Pulumi *would do* to infrastructure. The tool inverts this to describe what the *code* needs:

| Preview Op | Code Action | Property Kind Inversion |
|-----------|-------------|------------------------|
| `create` | `delete_from_code` | — |
| `delete` | `add_to_code` | — |
| `update` | `update_code` | `add` → `delete`, `delete` → `add` |
| `replace` | `update_code` | `add` → `delete`, `delete` → `add` |

For synthesized input-diff entries (from `ReplaceReasons`/`DiffReasons`), property kind is refined from the default `"update"` based on nil values: nil current → `"delete"`, nil desired → `"add"`.

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
