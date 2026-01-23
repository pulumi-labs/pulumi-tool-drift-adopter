# Parsing Logic and NDJSON Structure

## Overview

The drift adoption tool accepts input in **two formats**:

1. **Single JSON** - Standard Pulumi CLI output (`pulumi preview --json`)
2. **NDJSON** - Newline-Delimited JSON from MCP tools or engine events

## Parsing Flow

```
Input Bytes
    ↓
parsePreviewOutput()
    ↓
    ├─→ Try Single JSON Format
    │   Success? → Return steps
    │   Fail? ↓
    │
    └─→ parseNDJSON()
        ↓
        For each line:
        ├─→ Is it resourcePreEvent?
        │   No → Skip (policy events, etc.)
        │   Yes ↓
        │
        ├─→ Try Standard SDK Format
        │   Has oldState/newState? → Use it
        │   Nil? ↓
        │
        └─→ Try Pulumi-Service Format
            Parse old/new + diffKind
            Convert to standard format
            → Return steps
```

## Format 1: Single JSON (Standard Pulumi CLI)

### Source
`pulumi preview --json`

### Structure
```json
{
  "steps": [
    {
      "op": "update",
      "urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket",
      "oldState": {
        "type": "aws:s3/bucket:Bucket",
        "outputs": { "tags": { "Environment": "prod" } }
      },
      "newState": {
        "type": "aws:s3/bucket:Bucket",
        "outputs": { "tags": { "Environment": "dev" } }
      },
      "detailedDiff": {
        "tags.Environment": {
          "kind": "update",
          "inputDiff": false
        }
      }
    }
  ]
}
```

### Key Fields
- **steps[]** - Array of preview steps
- **oldState** - Resource state in Pulumi state (what we want)
- **newState** - Resource state from code (what currently exists)
- **kind** - Type of change: "add", "delete", "update"

### What We Extract
- `op` → Action to take (create/delete/update)
- `oldState.outputs` → Desired values (what infrastructure has)
- `newState.outputs` → Current values (what code has)
- `detailedDiff` → Specific property changes

---

## Format 2: NDJSON (Multiple Variations)

### NDJSON Basics
**NDJSON** = Newline-Delimited JSON
- One complete JSON object per line
- No outer array wrapper
- Can be streamed line-by-line

```
{"event1": "data"}\n
{"event2": "data"}\n
{"event3": "data"}\n
```

### Variation A: Pulumi-Service MCP Tool (Real-World)

**Source:** `pulumi_preview` MCP tool in pulumi-service
**Example File:** `testdata/simple-s3-drift.ndjson` (16 lines)

#### Line Structure
Each line has this structure:
```json
{
  "timestamp": 1769121796,
  "type": "resourcePreEvent",
  "resourcePreEvent": {
    "metadata": { /* see below */ },
    "planning": true
  }
}
```

#### Event Types in File
```
16 total lines:
├─ 1  policyLoadEvent          (skip)
├─ 1  preludeEvent              (skip)
├─ 3  policyRemediateSummaryEvent (skip)
├─ 3  policyAnalyzeSummaryEvent (skip)
├─ 1  policyEvent              (skip)
├─ 2  resourcePreEvent         ← WE USE THESE
├─ 3  resOutputsEvent           (skip)
├─ 1  policyAnalyzeStackSummaryEvent (skip)
└─ 1  summaryEvent             (skip)
```

**We only care about `resourcePreEvent` lines!**

#### resourcePreEvent.metadata Structure (Pulumi-Service Format)

```json
{
  "op": "update",
  "urn": "urn:pulumi:test::simple-s3::aws:s3/bucket:Bucket::test-bucket",
  "type": "aws:s3/bucket:Bucket",
  "old": {                           // ← Note: "old" not "oldState"
    "type": "aws:s3/bucket:Bucket",
    "urn": "...",
    "custom": true,
    "id": "test-bucket-dbf2a83",
    "inputs": {
      "tags": {
        "Environment": "production",
        "ManagedBy": "manual"
      }
    },
    "outputs": {
      "tags": {
        "Environment": "production",
        "ManagedBy": "manual"
      }
    },
    "provider": "..."
  },
  "new": {                           // ← Note: "new" not "newState"
    "inputs": {
      "tags": null                   // Tags removed in code
    },
    "outputs": {}
  },
  "diffs": ["tags", "tagsAll"],
  "detailedDiff": {
    "tags.Environment": {
      "diffKind": "delete",          // ← Note: "diffKind" not "kind"
      "inputDiff": false
    },
    "tags.ManagedBy": {
      "diffKind": "delete",
      "inputDiff": false
    }
  },
  "logical": true,
  "provider": "..."
}
```

#### Key Differences from Standard Format

| Field | Standard SDK | Pulumi-Service |
|-------|-------------|----------------|
| State field | `oldState` / `newState` | `old` / `new` |
| Diff kind | `kind` | `diffKind` |
| Top-level type | No | Yes (`"type": "resourcePreEvent"`) |

### Variation B: Unit Test Fixtures (Synthetic)

**Source:** Hand-crafted for unit tests
**Example Files:** `ndjson_update.ndjson`, `ndjson_create_delete.ndjson`, etc.

#### Line Structure
Each line has multiple event types as keys:
```json
{
  "sequence": 1,
  "timestamp": 1234567890,
  "resourcePreEvent": {
    "metadata": { /* standard SDK format */ }
  }
}
```

Or:
```json
{
  "sequence": 2,
  "timestamp": 1234567890,
  "preludeEvent": {
    "config": { ... }
  }
}
```

#### resourcePreEvent.metadata Structure (Unit Test Format)

```json
{
  "op": "update",
  "urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket",
  "oldState": {                      // ← Standard "oldState"
    "type": "aws:s3/bucket:Bucket",
    "outputs": {
      "tags": {
        "Environment": "production",
        "Owner": "team-a"
      }
    }
  },
  "newState": {                      // ← Standard "newState"
    "type": "aws:s3/bucket:Bucket",
    "outputs": {
      "tags": {
        "Environment": "dev",
        "Owner": "team-a"
      }
    }
  },
  "detailedDiff": {
    "tags.Environment": {
      "kind": "update",              // ← Standard "kind"
      "inputDiff": false
    }
  }
}
```

#### Uses Standard SDK Field Names
- `oldState` / `newState` ✅
- `kind` ✅
- No top-level `type` field

---

## What Parts We Extract and Use

### From resourcePreEvent.metadata

```
┌─────────────────────────────────────────┐
│ resourcePreEvent.metadata               │
├─────────────────────────────────────────┤
│ ✅ op           → "update", "create"    │
│ ✅ urn          → Resource identifier    │
│ ✅ type         → "aws:s3/bucket:Bucket"│
│ ✅ old/oldState → What infrastructure has│
│    ├─ inputs   → Input properties       │
│    └─ outputs  → Output properties      │
│ ✅ new/newState → What code defines      │
│    ├─ inputs                            │
│    └─ outputs                           │
│ ✅ detailedDiff → Property-level changes│
│    └─ {path}: {kind/diffKind, inputDiff}│
│                                         │
│ ❌ diffs        → High-level list (ignored)│
│ ❌ provider     → Not used directly      │
│ ❌ logical      → Internal flag (ignored)│
└─────────────────────────────────────────┘
```

### What We Skip

**Entire Event Types:**
- `policyLoadEvent` - Policy initialization
- `preludeEvent` - Preview setup/config
- `policyRemediateSummaryEvent` - Policy results
- `policyAnalyzeSummaryEvent` - Policy analysis
- `policyEvent` - Individual policy messages
- `resOutputsEvent` - Resource output events (duplicate info)
- `summaryEvent` - Preview summary
- `cancelEvent` - Preview cancellation
- `diagnosticEvent` - Warnings/errors

**We ONLY process `resourcePreEvent` lines.**

### Operations We Filter

After extracting `resourcePreEvent`, we filter by `op`:

```go
func getActionForOperation(op string) string {
    switch op {
    case "create":
        return "delete_from_code"  // Resource in code but not in state
    case "delete":
        return "add_to_code"       // Resource in state but not in code
    case "update", "replace":
        return "update_code"       // Resource differs between code and state
    default:
        return ""                  // Skip: "same", "read", etc.
    }
}
```

**Filtered Out:**
- `"same"` - No drift, skip
- `"read"` - External resource, skip
- `"refresh"` - State refresh, skip

### From detailedDiff

For each property path in `detailedDiff`:

```json
"tags.Environment": {
  "diffKind": "delete",     // We extract this as "kind"
  "inputDiff": false        // We include this
}
```

We extract:
- **Path** - Property path (e.g., `"tags.Environment"`)
- **Kind** - Change type (`"add"`, `"delete"`, `"update"`)
- **InputDiff** - Whether it's input vs output diff

Then we look up actual values:
- **desiredValue** - From `oldState.outputs[path]` (what infrastructure has)
- **currentValue** - From `newState.outputs[path]` (what code has)

### Example Extraction

**Input (NDJSON line):**
```json
{
  "type": "resourcePreEvent",
  "resourcePreEvent": {
    "metadata": {
      "op": "update",
      "urn": "urn:pulumi:test::simple-s3::aws:s3/bucket:Bucket::test-bucket",
      "old": {
        "outputs": {
          "tags": {"Environment": "production"}
        }
      },
      "new": {
        "outputs": {
          "tags": null
        }
      },
      "detailedDiff": {
        "tags.Environment": {"diffKind": "delete"}
      }
    }
  }
}
```

**Output (ResourceChange):**
```json
{
  "action": "update_code",
  "urn": "urn:pulumi:test::simple-s3::aws:s3/bucket:Bucket::test-bucket",
  "type": "aws:s3/bucket:Bucket",
  "name": "test-bucket",
  "properties": [
    {
      "path": "tags.Environment",
      "currentValue": null,
      "desiredValue": "production",
      "kind": "delete"
    }
  ]
}
```

---

## Testdata File Comparison

| File | Format | Field Names | Lines | Resource Events | Use Case |
|------|--------|-------------|-------|-----------------|----------|
| `simple-s3-drift.ndjson` | Pulumi-Service | old/new, diffKind | 16 | 2 (1 useful) | Real-world MCP tool output |
| `ndjson_update.ndjson` | Unit Test | oldState/newState, kind | 4 | 1 | Standard update operation |
| `ndjson_create_delete.ndjson` | Unit Test | oldState/newState, kind | 5 | 2 | Create + delete operations |
| `ndjson_multiple_resources.ndjson` | Unit Test | oldState/newState, kind | 6 | 3 | Batch processing |
| `ndjson_with_diagnostics.ndjson` | Unit Test | oldState/newState, kind | 7 | 1 + diagnostics | Mixed event types |
| `ndjson_empty.ndjson` | Unit Test | N/A | 4 | 0 | Clean state (no drift) |

### Consistency Analysis

**Inconsistent:**
- ❌ Top-level structure differs (type field vs event keys)
- ❌ Field names differ (old/new vs oldState/newState)
- ❌ DetailedDiff field names differ (diffKind vs kind)

**Consistent:**
- ✅ All have `resourcePreEvent` wrapper
- ✅ All have `metadata` containing preview step
- ✅ All have `op`, `urn`, state objects
- ✅ All have `detailedDiff` with property changes

**Why Two Formats?**
1. **simple-s3-drift.ndjson** - Real output from pulumi-service MCP tool
2. **Other files** - Synthetic fixtures created for unit tests before real format was documented

Our parsing logic **handles both formats** transparently by:
1. Trying standard format first
2. Falling back to pulumi-service format if needed
3. Converting to standard format internally

---

## Parsing Logic Step-by-Step

### Step 1: Try Single JSON
```go
var previewResult struct {
    Steps []auto.PreviewStep `json:"steps"`
}
if err := json.Unmarshal(output, &previewResult); err == nil {
    return previewResult.Steps, nil
}
```

**Success:** Has `steps` array → Return immediately
**Failure:** NDJSON or malformed → Continue to Step 2

### Step 2: Split into Lines
```go
lines := strings.Split(string(output), "\n")
for _, line := range lines {
    line = strings.TrimSpace(line)
    if line == "" {
        continue  // Skip empty lines
    }
    // Process each line...
}
```

### Step 3: Extract resourcePreEvent
```go
var event struct {
    ResourcePreEvent *struct {
        Metadata json.RawMessage `json:"metadata"`
    } `json:"resourcePreEvent"`
}
if err := json.Unmarshal([]byte(line), &event); err != nil {
    continue  // Not valid JSON, skip
}
if event.ResourcePreEvent == nil {
    continue  // Not a resourcePreEvent, skip
}
```

**Why `json.RawMessage`?**
We don't know if metadata uses standard or pulumi-service format yet, so we keep it as raw bytes for flexible parsing.

### Step 4: Try Standard Format
```go
var step auto.PreviewStep
if err := json.Unmarshal(event.ResourcePreEvent.Metadata, &step); err == nil {
    if step.OldState != nil || step.NewState != nil {
        steps = append(steps, step)
        continue  // Success! Use standard format
    }
}
```

**Check:** If `OldState`/`NewState` are populated → Standard format works!

### Step 5: Try Pulumi-Service Format
```go
var customStep struct {
    Op       string              `json:"op"`
    URN      string              `json:"urn"`
    Old      *apitype.ResourceV3 `json:"old,omitempty"`      // ← Custom
    New      *apitype.ResourceV3 `json:"new,omitempty"`      // ← Custom
    DetailedDiff map[string]json.RawMessage `json:"detailedDiff"`
}
if err := json.Unmarshal(event.ResourcePreEvent.Metadata, &customStep); err != nil {
    continue  // Can't parse, skip
}
```

**Note:** Uses `old`/`new` instead of `oldState`/`newState`

### Step 6: Convert DetailedDiff
```go
standardDetailedDiff := make(map[string]auto.PropertyDiff)
for path, rawDiff := range customStep.DetailedDiff {
    // Try pulumi-service format first
    var customDiff struct {
        DiffKind  string `json:"diffKind"`  // ← Custom field name
        InputDiff bool   `json:"inputDiff"`
    }
    if err := json.Unmarshal(rawDiff, &customDiff); err == nil && customDiff.DiffKind != "" {
        standardDetailedDiff[path] = auto.PropertyDiff{
            Kind:      customDiff.DiffKind,  // Rename to "kind"
            InputDiff: customDiff.InputDiff,
        }
        continue
    }

    // Fall back to standard format
    var standardDiff auto.PropertyDiff
    if err := json.Unmarshal(rawDiff, &standardDiff); err == nil {
        standardDetailedDiff[path] = standardDiff
    }
}
```

### Step 7: Create Standard PreviewStep
```go
standardStep := auto.PreviewStep{
    Op:           customStep.Op,
    URN:          resource.URN(customStep.URN),
    Provider:     customStep.Provider,
    OldState:     customStep.Old,        // Map old → OldState
    NewState:     customStep.New,        // Map new → NewState
    DetailedDiff: standardDetailedDiff,  // Already converted
}
steps = append(steps, standardStep)
```

**Result:** All formats converge to standard `auto.PreviewStep` structure!

---

## Summary

### Formats Supported
1. ✅ Single JSON (`pulumi preview --json`)
2. ✅ NDJSON with standard SDK fields
3. ✅ NDJSON with pulumi-service custom fields

### Parts Used
- **resourcePreEvent** lines only (others skipped)
- **op** field for operation type
- **old/oldState** for desired values (infrastructure state)
- **new/newState** for current values (code state)
- **detailedDiff** for property-level changes
- **type** and **urn** for resource identification

### Parts Ignored
- Policy events (all types)
- Summary events
- Diagnostic events
- Output events (duplicate info)
- Resources with `op: "same"` (no drift)

### Key Innovation
**Transparent Format Handling** - The tool accepts both standard SDK format and pulumi-service custom format, automatically detecting and converting to a unified internal representation.
