# Known Issues in v1.0.0

## Issue #1: Property Values Are Nil

### Symptom
When parsing NDJSON from pulumi-service integration tests, the tool correctly identifies resources with drift but returns `null` for all property values:

```json
{
  "path": "tags.Environment",
  "currentValue": null,
  "desiredValue": null,
  "kind": ""
}
```

### Root Cause
**Field Name Mismatch** between pulumi-service NDJSON and Go SDK struct tags:

- **NDJSON from pulumi-service MCP tool** uses lowercase field names:
  ```json
  {
    "resourcePreEvent": {
      "metadata": {
        "old": { "inputs": {...}, "outputs": {...} },
        "new": { "inputs": {...}, "outputs": {...} }
      }
    }
  }
  ```

- **Go SDK `auto.PreviewStep` struct** expects camelCase:
  ```go
  type PreviewStep struct {
      OldState *apitype.ResourceV3 `json:"oldState,omitempty"`
      NewState *apitype.ResourceV3 `json:"newState,omitempty"`
  }
  ```

When `json.Unmarshal` parses the NDJSON, it looks for `"oldState"` and `"newState"` fields but finds `"old"` and `"new"`, so `OldState` and `NewState` remain nil.

### Verification
```bash
# The data IS in the NDJSON:
$ jq '.resourcePreEvent.metadata.old.outputs.tags' testdata/simple-s3-drift.ndjson
{
  "Environment": "production",
  "ManagedBy": "manual"
}

# But Go can't access it due to field name mismatch
```

### Impact
- Resource detection: ✅ Works
- Drift detection: ✅ Works
- Property extraction: ❌ Broken (values are nil)
- Agent can't see what values to change

### Fix Required
The parsing code in `cmd/pulumi-drift-adopt/next.go` needs to handle both field name formats:

1. Try parsing with standard SDK structs (camelCase: `oldState`, `newState`)
2. If fields are nil, try custom struct with lowercase (`old`, `new`)
3. Convert custom struct to SDK structs

## Issue #2: "Invalid Character" Error - Already Fixed

### Original Error (Before Fix)
In earlier versions of the tool (before commit `6babcb7`), running with NDJSON would fail:

```json
{
  "status": "error",
  "error": "failed to parse preview output: invalid character '{' after top-level value"
}
```

### Root Cause (Historical)
The old code tried to parse NDJSON as a single JSON object and immediately returned an error:

```go
// OLD CODE (before 6babcb7)
if err := json.Unmarshal(output, &previewResult); err != nil {
    return outputError(fmt.Sprintf("failed to parse preview output: %v", err))
}
```

When `json.Unmarshal` tried to parse NDJSON like:
```
{"line1": "data"}
{"line2": "data"}
```

It would parse the first line, then encounter `{` on line 2 and error with "invalid character '{' after top-level value".

### Fix (Commit 6babcb7)
The fix made the code try single JSON first, then fall back to NDJSON:

```go
// NEW CODE (in v1.0.0)
if err := json.Unmarshal(output, &previewResult); err == nil {
    // Single JSON object - use it
    steps = previewResult.Steps
} else {
    // NDJSON format - parse line by line
    steps = parseNDJSON(output)
}
```

### Why We Couldn't Reproduce
The integration test ran AFTER the fix was committed:
- Fix committed: Jan 22, 2026 at 13:15 (1:15 PM)
- Integration test ran: Jan 22, 2026 at 17:35 (5:35 PM)
- v1.0.0 release includes the fix

The parsing error no longer occurs in v1.0.0.

## Test Coverage

### Passing Tests
- ✅ NDJSON parsing (no errors)
- ✅ Resource detection
- ✅ Drift identification
- ✅ Backward compatibility with old JSON format

### Failing Functionality (Not Caught by Tests)
- ❌ Property value extraction from NDJSON
- ❌ Type field population from NDJSON

### Test Added
`TestNextCommandRealPulumiServiceNDJSON` in `next_test.go`:
- Uses real NDJSON from pulumi-service integration test
- Documents the field name mismatch issue
- Serves as regression test for parsing

## Recommendations

### Priority 1: Fix Field Name Mismatch
Create adapter logic to handle both `old`/`new` and `oldState`/`newState` field names.

### Priority 2: Improve Test Assertions
Current test only checks that resources are found, not that property values are correct. Add assertions for:
- `currentValue` != nil
- `desiredValue` != nil
- `kind` is populated

### Priority 3: Integration Test with Real Tool
Run actual `pulumi plugin run drift-adopter` command in CI with NDJSON fixtures to catch these issues.
