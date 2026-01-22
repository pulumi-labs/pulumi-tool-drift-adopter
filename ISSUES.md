# Known Issues in v1.0.0

## Summary

| Issue | Status | Fix Commit |
|-------|--------|------------|
| Property Values Are Nil | ✅ Fixed | 0dcb4a0 |
| "Invalid Character" Parsing Error | ✅ Fixed | 6babcb7 |

Both issues are **FIXED** in the current main branch. A new release with these fixes should be created.

---

## Issue #1: Property Values Are Nil - ✅ FIXED

**Status:** Fixed in commit 0dcb4a0
**Date:** Jan 22, 2026

### Symptom (Before Fix)
When parsing NDJSON from pulumi-service integration tests, the tool correctly identified resources with drift but returned `null` for all property values:

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

**NDJSON from pulumi-service MCP tool** uses different field names:
- `"old"` and `"new"` instead of `"oldState"` and `"newState"`
- `"diffKind"` instead of `"kind"`

```json
{
  "resourcePreEvent": {
    "metadata": {
      "old": { "inputs": {...}, "outputs": {...} },
      "new": { "inputs": {...}, "outputs": {...} },
      "detailedDiff": {
        "tags.Environment": {
          "diffKind": "delete",
          "inputDiff": false
        }
      }
    }
  }
}
```

**Go SDK `auto.PreviewStep` struct** expects standard names:
```go
type PreviewStep struct {
    OldState *apitype.ResourceV3 `json:"oldState,omitempty"`
    NewState *apitype.ResourceV3 `json:"newState,omitempty"`
}

type PropertyDiff struct {
    Kind string `json:"kind"`  // Not "diffKind"
}
```

When `json.Unmarshal` parsed the NDJSON, it looked for `"oldState"` and `"newState"` but found `"old"` and `"new"`, so `OldState` and `NewState` remained nil.

### The Fix
Added fallback parsing logic in `cmd/pulumi-drift-adopt/next.go:parseNDJSON()`:

1. Try parsing with standard SDK format first (`oldState`, `newState`, `kind`)
2. If fields are nil, parse with custom format (`old`, `new`, `diffKind`)
3. Convert custom format to standard `PreviewStep` format

### After Fix
```json
{
  "path": "tags.Environment",
  "currentValue": null,
  "desiredValue": "production",
  "kind": "delete"
}
```

Now:
- ✅ `desiredValue` populated from old state
- ✅ `currentValue` correctly null (tags don't exist in code)
- ✅ `kind` populated with "delete"
- ✅ Resource `type` field populated
- ✅ All tests pass
- ✅ Backward compatible with standard format

### Impact
- **Before Fix:** Tool detected drift but couldn't tell agents what to change
- **After Fix:** Tool provides complete information for drift adoption

---

## Issue #2: "Invalid Character" Parsing Error - ✅ FIXED

**Status:** Fixed in commit 6babcb7
**Date:** Jan 22, 2026 at 13:15 PM

### Symptom (Before Fix)
In earlier versions (before commit 6babcb7), running with NDJSON would fail immediately:

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
var previewResult struct {
    Steps []auto.PreviewStep `json:"steps"`
}

if err := json.Unmarshal(output, &previewResult); err != nil {
    return outputError(fmt.Sprintf("failed to parse preview output: %v", err))
}
```

When `json.Unmarshal` tried to parse NDJSON:
```
{"line1": "data"}
{"line2": "data"}
```

It would parse the first line successfully, then encounter `{` on line 2 and error with "invalid character '{' after top-level value".

### The Fix (Commit 6babcb7)
Made parsing attempt single JSON first, then gracefully fall back to NDJSON:

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

### Why We Couldn't Initially Reproduce

The integration test in pulumi-service ran with a **cached v1.0.0 plugin** that was downloaded BEFORE we re-released v1.0.0.

**Timeline:**
- Original v1.0.0 release: Had the parsing bug (before commit 6babcb7)
- Plugin cached locally with bug
- Commit 6babcb7: Fixed NDJSON parsing (Jan 22 at 13:15 PM)
- Integration test ran: Jan 22 at 17:35 PM (5:35 PM)
  - Used **cached OLD v1.0.0** with bug → Got error
- We re-released v1.0.0: Moved tag to include fixes (Jan 22 at 22:28 PM)
- We tried to reproduce: Used **NEW v1.0.0** with fix → No error

**Verification:**
```bash
# Checkout old code before fix
git checkout fdddd81

# Test with NDJSON
go run ./cmd/pulumi-drift-adopt next --events-file events.ndjson
# Result: "invalid character '{' after top-level value"

# Checkout fixed code
git checkout main

# Test with NDJSON
go run ./cmd/pulumi-drift-adopt next --events-file events.ndjson
# Result: Works correctly
```

### After Fix
- ✅ NDJSON parsing works
- ✅ Single JSON backward compatibility maintained
- ✅ Graceful fallback between formats

---

## Test Coverage

### Comprehensive Test Added
`TestNextCommandRealPulumiServiceNDJSON` in `next_test.go`:
- Uses real NDJSON from pulumi-service integration test
- File: `testdata/simple-s3-drift.ndjson`
- Verifies resource detection, property values, and drift identification
- Serves as regression test for both issues

### Test Results (After Fixes)
```bash
$ go test ./cmd/pulumi-drift-adopt
PASS
ok      github.com/pulumi/pulumi-drift-adoption-tool/cmd/pulumi-drift-adopt    0.261s
```

All tests pass including:
- ✅ NDJSON parsing (no errors)
- ✅ Resource detection
- ✅ Property value extraction
- ✅ Backward compatibility with old JSON format
- ✅ Multiple resources handling
- ✅ Max resources limiting

---

## Recommendations

### 1. Create New Release
Create v1.0.1 (or re-release v1.0.0 again) with these fixes:
- Both issues are fixed in current main branch
- All tests pass
- Backward compatible

### 2. Clear Plugin Cache in CI/CD
When testing new releases, always clear plugin cache:
```bash
echo "yes" | pulumi plugin rm tool drift-adopter 1.0.0
pulumi plugin install tool drift-adopter v1.0.0
```

### 3. Update Integration Tests
Add cache clearing step before running pulumi-service integration tests to ensure latest plugin is used.

### 4. Future-Proof Parsing
Consider adding a version field or capability detection to handle format differences more robustly.
