# Parsing Logic and NDJSON Structure

## Overview

The drift adoption tool accepts input in **exactly two formats**:

1. **Single JSON** - Standard Pulumi CLI output (`pulumi preview --json`)
   - Uses standard SDK field names: `oldState`, `newState`, `kind`
2. **NDJSON** - Real pulumi-service MCP tool output (engine events)
   - Uses pulumi-service field names: `old`, `new`, `diffKind`, `type`

## Parsing Flow

```
Input Bytes
    ↓
parsePreviewOutput()
    ↓
    ├─→ Try Single JSON Format
    │   {"steps": [...]}
    │   Success? → Return steps (standard SDK format)
    │   Fail? ↓
    │
    └─→ parseNDJSON()
        ↓
        For each line:
        ├─→ Parse as JSON
        │   Invalid? → Skip
        │   Valid? ↓
        │
        ├─→ Check type field
        │   type == "resourcePreEvent"? → Process
        │   Other type? → Skip (policy, summary, etc.)
        │
        └─→ Parse metadata (pulumi-service format)
            - Extract old/new fields
            - Extract diffKind fields
            - Convert to standard PreviewStep
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

### Variation B: Unit Test Fixtures (Now Matches Real Format!)

**Source:** Hand-crafted for unit tests, **updated to match real pulumi-service format**
**Example Files:** `ndjson_update.ndjson`, `ndjson_create_delete.ndjson`, etc.

#### Line Structure
**Now uses same structure as real pulumi-service output:**
```json
{
  "timestamp": 1234567890,
  "type": "resourcePreEvent",
  "resourcePreEvent": {
    "metadata": { /* pulumi-service format */ }
  }
}
```

Or:
```json
{
  "timestamp": 1234567890,
  "type": "preludeEvent",
  "preludeEvent": {
    "config": { ... }
  }
}
```

#### resourcePreEvent.metadata Structure (Now Consistent!)

```json
{
  "op": "update",
  "urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket",
  "type": "aws:s3/bucket:Bucket",    // ← Has type field
  "old": {                           // ← Uses "old" not "oldState"
    "type": "aws:s3/bucket:Bucket",
    "outputs": {
      "tags": {
        "Environment": "production",
        "Owner": "team-a"
      }
    }
  },
  "new": {                           // ← Uses "new" not "newState"
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
      "diffKind": "update",          // ← Uses "diffKind" not "kind"
      "inputDiff": false
    }
  }
}
```

#### Now Uses Pulumi-Service Field Names
- `old` / `new` ✅ (not oldState/newState)
- `diffKind` ✅ (not kind)
- Top-level `type` field ✅

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
| `simple-s3-drift.ndjson` | Real NDJSON | old/new, diffKind | 16 | 2 (1 useful) | Real-world MCP tool output |
| `ndjson_update.ndjson` | Real NDJSON | old/new, diffKind | 4 | 1 | Standard update operation |
| `ndjson_create_delete.ndjson` | Real NDJSON | old/new, diffKind | 5 | 2 | Create + delete operations |
| `ndjson_multiple_resources.ndjson` | Real NDJSON | old/new, diffKind | 6 | 3 | Batch processing |
| `ndjson_with_diagnostics.ndjson` | Real NDJSON | old/new, diffKind | 7 | 1 + diagnostics | Mixed event types |
| `ndjson_empty.ndjson` | Real NDJSON | N/A | 4 | 0 | Clean state (no drift) |

### Consistency Analysis

**✅ ALL FILES NOW CONSISTENT!**

All NDJSON test files now use the **real pulumi-service format**:
- ✅ Top-level `type` field
- ✅ Event data under key matching type (e.g., `resourcePreEvent`)
- ✅ Uses `old`/`new` not `oldState`/`newState`
- ✅ Uses `diffKind` not `kind`
- ✅ Includes `type` field in metadata
- ✅ Has `timestamp` field

**All Consistent:**
- ✅ All have `type` field at top level
- ✅ All have `resourcePreEvent` wrapper for resource events
- ✅ All have `metadata` containing preview step
- ✅ All use `old`/`new` field names
- ✅ All use `diffKind` in detailedDiff
- ✅ All have `op`, `urn`, state objects

**Why This Is Better:**
1. **No ambiguity** - Only one NDJSON format to support
2. **Matches reality** - All fixtures match real pulumi-service output
3. **Simpler parsing** - No fallback logic needed
4. **Easy testing** - Unit tests validate real format

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
