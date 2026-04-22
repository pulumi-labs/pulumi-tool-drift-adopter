# Unified PropertyChange Design

## Problem

`add_to_code` and `update_code` resources represent property data in two different shapes:

- `add_to_code` uses `inputProperties map[string]interface{}` — a flat map where values can be nested maps/arrays, and dependency info is inlined as `{"dependsOn": {...}}` wrapper maps that replace the actual value.
- `update_code` uses `properties []PropertyChange` — structured entries with `path`, `currentValue`, `desiredValue`, and (via PR #48) `dependsOn *DependencyRef`.

This forces consumers to handle two formats for the same concept. It also requires recursive scanning (`collectDependencyNames`) to extract dependencies from `inputProperties` for sorting.

## Prerequisites

PR #48 must be merged first — it adds the `DependsOn *DependencyRef` field to `PropertyChange` and the `enrichPropertyDependencies` function. This spec builds on that field and replaces that function.

## Design

### Data Model

Remove `InputProperties map[string]interface{}` from `ResourceChange`. Both action types use `Properties []PropertyChange`.

`PropertyChange` struct (added by PR #48):

```go
type PropertyChange struct {
    Path         string         `json:"path"`
    CurrentValue interface{}    `json:"currentValue,omitempty"`
    DesiredValue interface{}    `json:"desiredValue,omitempty"`
    DependsOn    *DependencyRef `json:"dependsOn,omitempty"`
}
```

For `add_to_code`: `CurrentValue` is always nil, `DesiredValue` holds the value from state, `DependsOn` is set when a dependency exists in the depMap.

### Flattening

`add_to_code` properties are flattened to leaf-level dot-paths, matching `update_code` granularity. The new `extractInputProperties` must recursively flatten to arbitrary depth (maps within maps, arrays within maps, etc.):

- Nested maps: `{"tags": {"env": "prod"}}` becomes `{path: "tags.env", desiredValue: "prod"}`
- Deep maps: `{"metadata": {"labels": {"app": "nginx"}}}` becomes `{path: "metadata.labels.app", desiredValue: "nginx"}`
- Arrays: `{"triggers": ["a", "b"]}` becomes `{path: "triggers[0]", desiredValue: "a"}`, `{path: "triggers[1]", desiredValue: "b"}`
- Mixed: `{"metadata": {"ports": [80, 443]}}` becomes `{path: "metadata.ports[0]", desiredValue: 80}`, `{path: "metadata.ports[1]", desiredValue: 443}`
- Scalars: `{"name": "my-bucket"}` becomes `{path: "name", desiredValue: "my-bucket"}`

Note: array flattening is new behavior — the existing `extractAllProperties` only recurses into maps, treating arrays as leaf values. The rewrite adds proper array traversal.

### Function Changes

**`extractInputProperties`** — Rewrite signature to return `[]PropertyChange`. Recursively flatten nested values (maps and arrays) into dot-path/bracket-index entries to arbitrary depth. Check depMap for each leaf path; set `DependsOn` if matched, otherwise set `DesiredValue` (with truncation).

**`convertStepsToResources`** — `add_to_code` case calls the new `extractInputProperties` and assigns to `res.Properties` instead of `res.InputProperties`. The `update_code` case continues calling `extractPropertyChanges` followed by depMap enrichment for `DependsOn`.

**`extractDependencyNames`** — Rewrite to iterate `res.Properties` and collect `DependsOn.ResourceName` where non-nil. Works for all action types. This eliminates the need for `collectDependencyNames`, which is the recursive helper it currently delegates to.

**`sortResourcesByDependencies`** — No code change needed, but behavioral expansion: since `extractDependencyNames` now reads `DependsOn` from all resources' `Properties`, `update_code` resources with cross-resource dependencies will participate in dependency ordering (previously only `add_to_code` resources were considered). This is desired.

**`output.go`** — Update the skip-logic check for empty `add_to_code` resources: change `len(res.InputProperties) == 0` to `len(res.Properties) == 0`.

**Delete:**
- `depRefToDependsOn` — `DependencyRef` goes directly on the struct
- `collectDependencyNames` — recursive wrapper-map scanning is obsolete (replaced by direct `DependsOn` field reads in `extractDependencyNames`)
- `enrichPropertyDependencies` (PR #48) — dependency enrichment moves into `extractInputProperties` for `add_to_code`, and into `convertStepsToResources` for `update_code`
- `extractAllProperties` — flattening logic is subsumed by the new `extractInputProperties`
- The `step.Op == "delete"` branch in `extractPropertyChanges` (lines 111-119) — `add_to_code` properties now go through `extractInputProperties`, making this dead code

### Output Shape Change

Before:
```json
{
  "action": "add_to_code",
  "inputProperties": {
    "tags": {"env": "prod"},
    "name": "my-bucket",
    "privateKeyPem": {"dependsOn": {"resourceName": "ca-key", "resourceType": "tls:index/privateKey:PrivateKey", "outputProperty": "privateKeyPem"}}
  }
}
```

After:
```json
{
  "action": "add_to_code",
  "properties": [
    {"path": "tags.env", "desiredValue": "prod"},
    {"path": "name", "desiredValue": "my-bucket"},
    {"path": "privateKeyPem", "dependsOn": {"resourceName": "ca-key", "resourceType": "tls:index/privateKey:PrivateKey", "outputProperty": "privateKeyPem"}}
  ]
}
```

### Test Changes

- Dependency tests asserting `{"dependsOn": ...}` wrapper maps in `inputProperties` rewritten to assert `PropertyChange` entries with `DependsOn` fields.
- Sorting tests rewritten to build `[]PropertyChange` with `DependsOn` set instead of `inputProperties` with `dependsOnProp()` / `makeRes()` helpers.
- `TestEnrichPropertyDependencies` (PR #48) removed — function deleted.
- Integration tests checking `inputProperties` on `add_to_code` resources rewritten to check `Properties`.
- Add test case for `update_code` resources participating in dependency sorting via `DependsOn`.
- Add test cases for deep nesting and array flattening (new behavior not covered by existing tests).
