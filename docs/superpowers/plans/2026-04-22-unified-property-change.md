# Unified PropertyChange Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Unify `add_to_code` and `update_code` property representations into a single `[]PropertyChange` format with flattened dot-paths and structured `DependsOn` fields.

**Architecture:** Remove `InputProperties map[string]interface{}` from `ResourceChange`. Rewrite `extractInputProperties` to return `[]PropertyChange` with recursive flattening. Simplify dependency name extraction to read `DependsOn` directly from `Properties`. Update the agent skill to consume the new format.

**Tech Stack:** Go, Cobra CLI, Pulumi SDK

**Spec:** `docs/superpowers/specs/2026-04-22-unified-property-change-design.md`

**Branch:** `feat/enrich-update-code-deps` (PR #48 — already has `DependsOn` field on `PropertyChange`)

---

## Chunk 1: Core refactor

### Task 1: Rewrite `extractInputProperties` to return `[]PropertyChange`

**Files:**
- Modify: `cmd/pulumi-drift-adopt/properties.go:36-99` (rewrite `extractInputProperties`)
- Modify: `cmd/pulumi-drift-adopt/properties.go:274-292` (delete `extractAllProperties`)
- Test: `cmd/pulumi-drift-adopt/dependencies_test.go`

- [ ] **Step 1: Write failing test for recursive flattening**

Add test in `dependencies_test.go` (where existing `extractInputProperties` tests live):

```go
func TestExtractInputPropertiesUnified(t *testing.T) {
	step := auto.PreviewStep{
		Op:  "delete",
		URN: resource.URN("urn:pulumi:dev::proj::aws:s3/bucket:Bucket::my-bucket"),
		OldState: &apitype.ResourceV3{
			Type: "aws:s3/bucket:Bucket",
			Inputs: map[string]interface{}{
				"bucket": "my-bucket",
				"tags":   map[string]interface{}{"Environment": "prod", "Team": "platform"},
				"corsRules": []interface{}{
					map[string]interface{}{"allowedMethods": []interface{}{"GET"}},
				},
			},
		},
	}

	result := extractInputProperties(step, nil)

	// Should return []PropertyChange with flattened paths
	pathMap := make(map[string]interface{})
	for _, pc := range result {
		assert.Nil(t, pc.CurrentValue, "add_to_code properties should have nil CurrentValue")
		pathMap[pc.Path] = pc.DesiredValue
	}

	assert.Equal(t, "my-bucket", pathMap["bucket"])
	assert.Equal(t, "prod", pathMap["tags.Environment"])
	assert.Equal(t, "platform", pathMap["tags.Team"])
	assert.Equal(t, []interface{}{"GET"}, pathMap["corsRules[0].allowedMethods"])
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -tags=unit -run TestExtractInputPropertiesUnified -v ./cmd/pulumi-drift-adopt/`
Expected: FAIL — return type mismatch

- [ ] **Step 3: Write failing test for deep nesting**

```go
func TestExtractInputPropertiesDeepNesting(t *testing.T) {
	step := auto.PreviewStep{
		Op:  "delete",
		URN: resource.URN("urn:pulumi:dev::proj::pkg:mod:Res::deep"),
		OldState: &apitype.ResourceV3{
			Type:   "pkg:mod:Res",
			Inputs: map[string]interface{}{
				"metadata": map[string]interface{}{
					"labels": map[string]interface{}{"app": "nginx"},
					"ports":  []interface{}{80, 443},
				},
			},
		},
	}

	result := extractInputProperties(step, nil)

	pathMap := make(map[string]interface{})
	for _, pc := range result {
		pathMap[pc.Path] = pc.DesiredValue
	}

	assert.Equal(t, "nginx", pathMap["metadata.labels.app"])
	assert.Equal(t, float64(80), pathMap["metadata.ports[0]"])
	assert.Equal(t, float64(443), pathMap["metadata.ports[1]"])
}
```

- [ ] **Step 4: Write failing test for depMap enrichment**

```go
func TestExtractInputPropertiesWithDeps(t *testing.T) {
	urn := "urn:pulumi:dev::proj::tls:index/selfSignedCert:SelfSignedCert::my-cert"
	step := auto.PreviewStep{
		Op:  "delete",
		URN: resource.URN(urn),
		OldState: &apitype.ResourceV3{
			Type: "tls:index/selfSignedCert:SelfSignedCert",
			Inputs: map[string]interface{}{
				"privateKeyPem": "some-pem-value",
				"subject":       "CN=test",
			},
		},
	}
	depMap := DependencyMap{
		urn: {
			"privateKeyPem": DependencyRef{
				ResourceName:   "ca-key",
				ResourceType:   "tls:index/privateKey:PrivateKey",
				OutputProperty: "privateKeyPem",
			},
		},
	}

	result := extractInputProperties(step, depMap)

	var pkPem, subject *PropertyChange
	for i := range result {
		switch result[i].Path {
		case "privateKeyPem":
			pkPem = &result[i]
		case "subject":
			subject = &result[i]
		}
	}

	require.NotNil(t, pkPem)
	require.NotNil(t, pkPem.DependsOn)
	assert.Equal(t, "ca-key", pkPem.DependsOn.ResourceName)
	assert.Equal(t, "privateKeyPem", pkPem.DependsOn.OutputProperty)
	assert.Nil(t, pkPem.DesiredValue, "dependsOn properties should not have DesiredValue")

	require.NotNil(t, subject)
	assert.Nil(t, subject.DependsOn)
	assert.Equal(t, "CN=test", subject.DesiredValue)
}
```

- [ ] **Step 5: Rewrite `extractInputProperties` and delete `extractAllProperties`**

Replace `extractInputProperties` in `properties.go` with:

```go
// extractInputProperties returns a flattened []PropertyChange for add_to_code resources.
// Nested maps and arrays are recursively flattened to leaf-level dot-path/bracket-index entries.
// When depMap has a matching entry for a leaf path, DependsOn is set and DesiredValue is nil.
func extractInputProperties(step auto.PreviewStep, depMap DependencyMap) []PropertyChange {
	if step.OldState == nil {
		return nil
	}
	source := step.OldState.Inputs
	if len(source) == 0 {
		source = step.OldState.Outputs
	}
	if len(source) == 0 {
		return nil
	}

	urn := string(step.URN)
	urnDeps := depMap[urn]

	var properties []PropertyChange
	flattenProperties(source, "", urnDeps, &properties)
	return properties
}

// flattenProperties recursively flattens a property map into leaf-level PropertyChange entries.
func flattenProperties(props map[string]interface{}, prefix string, urnDeps map[string]DependencyRef, out *[]PropertyChange) {
	for key, value := range props {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		flattenValue(value, path, urnDeps, out)
	}
}

// flattenValue handles a single value during recursive flattening.
func flattenValue(value interface{}, path string, urnDeps map[string]DependencyRef, out *[]PropertyChange) {
	// Check depMap first — if matched, emit DependsOn with no value
	if ref, ok := urnDeps[path]; ok {
		*out = append(*out, PropertyChange{
			Path:      path,
			DependsOn: &DependencyRef{
				ResourceName:   ref.ResourceName,
				ResourceType:   ref.ResourceType,
				OutputProperty: ref.OutputProperty,
			},
		})
		return
	}

	switch v := value.(type) {
	case map[string]interface{}:
		flattenProperties(v, path, urnDeps, out)
	case []interface{}:
		for i, elem := range v {
			elemPath := fmt.Sprintf("%s[%d]", path, i)
			flattenValue(elem, elemPath, urnDeps, out)
		}
	default:
		*out = append(*out, PropertyChange{
			Path:         path,
			DesiredValue: truncateValue(value),
		})
	}
}
```

Delete `extractAllProperties` (lines 274-292).

- [ ] **Step 6: Run all three new tests**

Run: `go test -tags=unit -run "TestExtractInputProperties(Unified|DeepNesting|WithDeps)" -v ./cmd/pulumi-drift-adopt/`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add cmd/pulumi-drift-adopt/properties.go cmd/pulumi-drift-adopt/dependencies_test.go
git commit -m "feat: rewrite extractInputProperties to return []PropertyChange with recursive flattening"
```

### Task 2: Remove `InputProperties`, delete dead code, simplify dependency functions, update all tests

All structural changes and test updates are done in a single task to avoid intermediate non-compiling states. Tasks 2-4 from the original plan are merged.

**Files:**
- Modify: `cmd/pulumi-drift-adopt/next.go` (remove `InputProperties` field, update `convertStepsToResources`, inline depMap enrichment)
- Modify: `cmd/pulumi-drift-adopt/output.go:38` (skip-logic check)
- Modify: `cmd/pulumi-drift-adopt/properties.go` (delete dead branch, delete `extractAllProperties`, delete `truncateStringValues`, delete `enrichPropertyDependencies`)
- Modify: `cmd/pulumi-drift-adopt/dependencies.go` (rewrite `extractDependencyNames`, delete `collectDependencyNames`, delete `depRefToDependsOn`)
- Modify: `cmd/pulumi-drift-adopt/dependencies_test.go` (rewrite all `InputProperties` references)
- Modify: `cmd/pulumi-drift-adopt/properties_test.go` (rewrite all `InputProperties` references)

- [ ] **Step 1: Remove `InputProperties` field from `ResourceChange`**

In `next.go`, remove the `InputProperties` line from `ResourceChange`:

```go
type ResourceChange struct {
	URN             string           `json:"urn"`
	Name            string           `json:"name"`
	Type            string           `json:"type"`
	Action          string           `json:"action"`
	Properties      []PropertyChange `json:"properties,omitempty"`
	DependencyLevel int              `json:"dependencyLevel,omitempty"`
	Reason          string           `json:"reason,omitempty"`
}
```

- [ ] **Step 2: Update `convertStepsToResources`**

Change the `ActionAddToCode` case to assign to `res.Properties`:

```go
case ActionAddToCode:
	// For resources that need to be added, extract all input properties from state
	res.Properties = extractInputProperties(*step, depMap)
```

Replace the `enrichPropertyDependencies` call in the `default` case with inline logic:

```go
default:
	// For update/replace, extract changed properties with schema-based filtering
	res.Properties = extractPropertyChanges(*step, inputPropSet)
	// Enrich properties with dependency metadata from depMap
	if depMap != nil {
		urnDeps := depMap[string(step.URN)]
		if len(urnDeps) > 0 {
			for i := range res.Properties {
				if ref, ok := urnDeps[res.Properties[i].Path]; ok {
					res.Properties[i].DependsOn = &ref
				}
			}
		}
	}
	// Supplement "[secret]" values with real values from state export
	if stateLookup != nil {
		supplementSecretValues(res.Properties, string(step.URN), stateLookup)
	}
```

- [ ] **Step 3: Update `output.go` skip-logic**

Change line 38 from:
```go
} else if res.Action == ActionAddToCode && len(res.InputProperties) == 0 {
```
to:
```go
} else if res.Action == ActionAddToCode && len(res.Properties) == 0 {
```

- [ ] **Step 4: Delete dead code in `properties.go`**

Delete ALL of the following:
- The `step.Op == "delete"` early-return branch in `extractPropertyChanges` (lines 109-120)
- `extractAllProperties` function (lines 274-292) — subsumed by `flattenProperties`
- `truncateStringValues` function (lines 340-357) — no callers remain after `extractInputProperties` rewrite
- `enrichPropertyDependencies` function (added by PR #48) — replaced by inline logic in `convertStepsToResources`

- [ ] **Step 5: Delete obsolete functions in `dependencies.go`**

Delete:
- `depRefToDependsOn` — `DependencyRef` goes directly on the struct
- `collectDependencyNames` — recursive wrapper-map scanning is obsolete

Rewrite `extractDependencyNames`:

```go
// extractDependencyNames returns the names of resources (within nameSet) that res depends on,
// by reading DependsOn fields from Properties.
func extractDependencyNames(res ResourceChange, nameSet map[string]bool) []string {
	var deps []string
	seen := make(map[string]bool)
	for _, prop := range res.Properties {
		if prop.DependsOn == nil {
			continue
		}
		name := prop.DependsOn.ResourceName
		if nameSet[name] && !seen[name] {
			seen[name] = true
			deps = append(deps, name)
		}
	}
	return deps
}
```

- [ ] **Step 6: Fix all `InputProperties` references in `dependencies_test.go`**

Rewrite the `dependsOnProp()` and `makeRes()` test helpers:

```go
// makeRes builds a ResourceChange for sorting tests with add_to_code action.
func makeRes(name string, properties []PropertyChange) ResourceChange {
	return ResourceChange{
		URN:    "urn:pulumi:dev::proj::pkg:mod:Res::" + name,
		Name:   name,
		Type:   "pkg:mod:Res",
		Action: ActionAddToCode,
		Properties: properties,
	}
}

// depProp creates a PropertyChange with DependsOn set.
func depProp(path, depName string) PropertyChange {
	return PropertyChange{
		Path: path,
		DependsOn: &DependencyRef{
			ResourceName: depName,
			ResourceType: "pkg:mod:Res",
		},
	}
}
```

Update each sorting test case:
```go
// Before:
makeRes("a", map[string]interface{}{"ref": dependsOnProp("b")})
// After:
makeRes("a", []PropertyChange{depProp("ref", "b")})
```

Rewrite all dependency resolution tests that assert `{"dependsOn": {...}}` wrapper maps. There are ~20 references across these test functions — search for `InputProperties` and update each one:
- `TestDependencyResolution` — assertions at lines 58, 70, 73, 82, 92
- `TestDependencyResolutionEdgeCases` — all subtests (lines 112-149, 174-229, 277-395)
- `TestNestedDependsOnMapProperty` — all subtests (lines 564-691)
- `TestDepMapReusedOnSubsequentCalls` — line 431, 488

Note: `TestAWSCascadingDeps_DependencyMapFromState` does NOT reference `ResourceChange.InputProperties` — it tests `buildDepMapFromState` and asserts on `DependencyRef` struct fields. No changes needed.

For each, replace assertions like:
```go
pkMap := resources[0].InputProperties["privateKeyPem"].(map[string]interface{})
depInfo := pkMap["dependsOn"].(map[string]interface{})
assert.Equal(t, "ca-key", depInfo["resourceName"])
```

With a helper pattern:
```go
// findProp is a test helper to find a PropertyChange by path.
func findProp(t *testing.T, props []PropertyChange, path string) *PropertyChange {
	t.Helper()
	for i := range props {
		if props[i].Path == path {
			return &props[i]
		}
	}
	t.Fatalf("property %q not found", path)
	return nil
}

// Usage:
pkPem := findProp(t, resources[0].Properties, "privateKeyPem")
require.NotNil(t, pkPem.DependsOn)
assert.Equal(t, "ca-key", pkPem.DependsOn.ResourceName)
```

For plain value assertions, replace:
```go
assert.Equal(t, float64(87600), caCert.InputProperties["validityPeriodHours"])
```
With:
```go
vph := findProp(t, caCert.Properties, "validityPeriodHours")
assert.Equal(t, float64(87600), vph.DesiredValue)
```

- [ ] **Step 7: Fix all `InputProperties` references in `properties_test.go`**

Update these specific tests:
- `TestNextCommandDeletePrefersInputs` (line 332) — change `res.InputProperties` assertions to `res.Properties` with `findProp` pattern. The test verifies that Inputs are preferred over Outputs — assert that `algorithm` and `rsaBits` are present as flattened paths, and that Outputs-only fields (`privateKeyPem`, `publicKeyPem`, `id`) are NOT present. Use `assert.Len(t, res.Properties, 2)` to verify count.
- `TestNextCommandDeleteFallsBackToOutputs` (line 375) — assert `res.Properties` contains flattened entries from Outputs (since Inputs is empty). Use `assert.Len(t, res.Properties, 2)` and verify paths exist via `findProp`.
- `TestNextCommandInputPropertiesFormat` (line 404) — rewrite completely: `add_to_code` now uses `Properties` (not `InputProperties`). Nested maps are flattened, so `tags.Environment` becomes a dot-path. Replace `addResource.InputProperties["tags"].(map[string]interface{})` with `findProp(t, addResource.Properties, "tags.Environment")` and assert `DesiredValue == "production"`. Remove all `assert.Nil(t, res.InputProperties)` and `assert.Nil(t, res.Properties)` assertions that test field exclusivity.

Delete `TestEnrichPropertyDependencies` and `TestEnrichPropertyDependencies_NoDeps` (added by PR #48).

Note: `TestMetadataRoundTrip_WithInputProperties` (line 1307) and `TestAWSCascadingDeps_LambdaInputProperties` (line 825) reference `InputProperties` in the schema sense (provider schema input properties), NOT `ResourceChange.InputProperties` — these do NOT need changes.

- [ ] **Step 8: Add test for `update_code` resources in dependency sorting**

```go
func TestSortResourcesByDependencies_UpdateCodeIncluded(t *testing.T) {
	resources := []ResourceChange{
		{
			Name:   "consumer",
			Action: ActionUpdateCode,
			Properties: []PropertyChange{
				{Path: "roleArn", DependsOn: &DependencyRef{ResourceName: "producer", ResourceType: "aws:iam/role:Role", OutputProperty: "arn"}},
			},
		},
		{
			Name:   "producer",
			Action: ActionUpdateCode,
			Properties: []PropertyChange{
				{Path: "name", CurrentValue: "old", DesiredValue: "new"},
			},
		},
	}

	sorted := sortResourcesByDependencies(resources)

	assert.Equal(t, "producer", sorted[0].Name, "producer should come first")
	assert.Equal(t, 0, sorted[0].DependencyLevel)
	assert.Equal(t, "consumer", sorted[1].Name, "consumer depends on producer")
	assert.Equal(t, 1, sorted[1].DependencyLevel)
}
```

- [ ] **Step 9: Run full test suite**

Run: `go test -tags=unit -v -race ./cmd/pulumi-drift-adopt/`
Expected: ALL PASS

- [ ] **Step 10: Run linter**

Run: `golangci-lint run --timeout=5m`
Expected: 0 issues

- [ ] **Step 11: Commit**

```bash
git add cmd/pulumi-drift-adopt/next.go cmd/pulumi-drift-adopt/output.go cmd/pulumi-drift-adopt/properties.go cmd/pulumi-drift-adopt/dependencies.go cmd/pulumi-drift-adopt/dependencies_test.go cmd/pulumi-drift-adopt/properties_test.go
git commit -m "refactor: remove InputProperties, unify on Properties for all action types"
```

## Chunk 2: Documentation and skill updates

### Task 5: Update README output examples

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update the full output JSON example**

The example at lines 71-93 only shows `update_code`. Add an `add_to_code` resource to the `resources` array that uses the new `properties` format (not `inputProperties`):

```json
{
  "action": "add_to_code",
  "urn": "urn:pulumi:dev::app::aws:s3/bucket:Bucket::missing-bucket",
  "type": "aws:s3/bucket:Bucket",
  "name": "missing-bucket",
  "properties": [
    {"path": "bucket", "desiredValue": "missing-bucket"},
    {"path": "tags.Environment", "desiredValue": "production"}
  ]
}
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: update README output examples for unified PropertyChange format"
```

### Task 6: Update agent skill in `pulumi/agent-skills`

**Files:**
- Modify: `/Users/jdavenport/pulumi-repos/agent-skills/authoring/skills/pulumi-adopt-drift/SKILL.md`

- [ ] **Step 1: Update `add_to_code` response example (lines 177-189)**

Replace:
```json
{
  "action": "add_to_code",
  "urn": "urn:pulumi:dev::app::aws:s3/bucket:Bucket::missing-bucket",
  "type": "aws:s3/bucket:Bucket",
  "name": "missing-bucket",
  "inputProperties": {
    "bucket": "missing-bucket",
    "tags": {"Environment": "production"}
  }
}
```

With:
```json
{
  "action": "add_to_code",
  "urn": "urn:pulumi:dev::app::aws:s3/bucket:Bucket::missing-bucket",
  "type": "aws:s3/bucket:Bucket",
  "name": "missing-bucket",
  "properties": [
    {"path": "bucket", "desiredValue": "missing-bucket"},
    {"path": "tags.Environment", "desiredValue": "production"}
  ]
}
```

- [ ] **Step 2: Update `inputProperties` description (line 192)**

Replace:
```
`inputProperties` is a flat map of property names to values — use these directly when writing the resource declaration.
```

With:
```
`add_to_code` resources use the same `properties` array format as `update_code`. Each entry has a `path` (dot-separated property path) and a `desiredValue` (the value from infrastructure). `currentValue` is always absent since the property doesn't exist in code yet.
```

- [ ] **Step 3: Update Runtime Values section (lines 197-202)**

Replace references to `inputProperties` values with `desiredValue`:
```
`currentValue` and `desiredValue` are **runtime values** — the actual string/number/object
that exists in infrastructure or that code evaluates to. Your code must be an expression
that evaluates to this exact value at runtime.
```

- [ ] **Step 4: Update Truncated Values section (lines 214-218)**

Replace:
```
- For **`desiredValue`** or **`inputProperties`**: retrieve the full value via `pulumi stack export --show-secrets` and look up the resource by URN
```

With:
```
- For **`desiredValue`**: retrieve the full value via `pulumi stack export --show-secrets` and look up the resource by URN
```

- [ ] **Step 5: Update Cross-Resource References section (lines 226-268)**

Replace the entire section. The `dependsOn` metadata is now on the `PropertyChange` struct, not inlined as the property value:

````markdown
### Cross-Resource References

When a property depends on another resource's output, the property has a `dependsOn` field
instead of a `desiredValue`:

```json
{
  "path": "privateKeyPem",
  "dependsOn": {
    "resourceName": "ca-key",
    "resourceType": "tls:index/privateKey:PrivateKey",
    "outputProperty": "privateKeyPem"
  }
}
```

**When you see `dependsOn`:** ALWAYS use a resource reference — there is no literal value provided.
Write `caKey.privateKeyPem` (not a literal value). The `resourceName` tells you which
resource variable to reference, and `outputProperty` tells you which output.

This applies to both `add_to_code` and `update_code` resources.

#### Bare dependsOn (no outputProperty)

When the tool cannot determine the exact output property, `outputProperty` is omitted:

```json
{
  "path": "triggers",
  "dependsOn": {
    "resourceName": "api-pass-5",
    "resourceType": "random:index/randomPassword:RandomPassword"
  }
}
```

**When you see bare `dependsOn`:** The tool knows the dependency but could not match the
value to a specific output — commonly because the value is encrypted or the property is
an array or map whose values are resource outputs. The referenced resource's type is in
your inventory. Use it to infer the correct output for the property you are setting.

For example, `RandomPassword` → `result`, so write `apiPass5.result`.

Properties without `dependsOn` are plain values — use `desiredValue` as-is.
````

- [ ] **Step 6: Update Scale Strategy section (lines 44-46)**

Replace:
```
4. If any `inputProperties` values contain bare `dependsOn` (no `outputProperty`), see
   the "Bare dependsOn" section below for how to resolve them.
```

With:
```
4. If any properties have bare `dependsOn` (no `outputProperty`), see
   the "Bare dependsOn" section below for how to resolve them.
```

- [ ] **Step 7: Update Key fields section (lines 163-170)**

Replace the key fields list to note that `properties` is used for all action types:

```markdown
**Key fields:**
- `action`: What to do (update_code, delete_from_code, add_to_code)
- `name`: Resource name to find in code
- `type`: Resource type (e.g., "aws:s3/bucket:Bucket")
- `properties`: Array of property changes (used by both `update_code` and `add_to_code`)
  - `path`: Property path (e.g., "tags.Environment")
  - `currentValue`: What's in code now (absent for `add_to_code`)
  - `desiredValue`: What it should be (from infrastructure)
  - `dependsOn`: Cross-resource dependency reference (replaces `desiredValue` when present)
```

- [ ] **Step 8: Update Important Notes (line 306)**

Replace:
```
- **Reading output**: Always read `outputFile` from the summary to get full resource details — stdout only contains the summary
```

With:
```
- **Reading output**: Always read `outputFile` from the summary to get full resource details — stdout only contains the summary. Both `update_code` and `add_to_code` resources use the same `properties` array format.
```

- [ ] **Step 9: Commit skill changes**

```bash
cd /Users/jdavenport/pulumi-repos/agent-skills
git add authoring/skills/pulumi-adopt-drift/SKILL.md
git commit -m "feat: update drift-adopt skill for unified PropertyChange format"
```

### Task 7: Update CHANGELOG

**Files:**
- Modify: `CHANGELOG.md` (in drift-adopter repo)

- [ ] **Step 1: Add entry under `[Unreleased]`**

```markdown
## [Unreleased]

### Changed
- **Unified property format**: `add_to_code` resources now use the same `properties` array as `update_code` instead of `inputProperties` map. Properties are flattened to leaf-level dot-paths with `dependsOn` as a structured field.
- **Dependency sorting expanded**: `update_code` resources with cross-resource `dependsOn` now participate in topological sorting alongside `add_to_code` resources.

### Removed
- `inputProperties` field from resource output (replaced by `properties` array)
```

- [ ] **Step 2: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: add changelog entry for unified PropertyChange format"
```
