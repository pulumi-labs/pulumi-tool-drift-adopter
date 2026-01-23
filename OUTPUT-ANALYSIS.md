# Drift Adoption Tool Output Analysis

This document analyzes the tool's output format and how the adopt-drift skill interprets it.

## Test Scenario 1: Property Updates and Additions

**Command:**
```bash
pulumi plugin run drift-adopter -- next --events-file testdata/ndjson_update.ndjson
```

**Output:**
```json
{
  "status": "changes_needed",
  "resources": [
    {
      "action": "update_code",
      "urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket",
      "type": "aws:s3/bucket:Bucket",
      "name": "my-bucket",
      "properties": [
        {
          "path": "tags.ManagedBy",
          "currentValue": null,
          "desiredValue": "pulumi",
          "kind": "add"
        },
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

### Skill Interpretation

**Action: `update_code`**
- Find resource "my-bucket" of type `aws:s3/bucket:Bucket` in code
- Apply property changes:

**Property 1: `tags.ManagedBy`**
- **Current state:** Not in code (`currentValue: null`)
- **Desired state:** Should be `"pulumi"` (from infrastructure)
- **Kind:** `add`
- **Skill action:** ADD this tag to the code
  ```typescript
  // Before
  tags: { Environment: "dev" }

  // After
  tags: { Environment: "dev", ManagedBy: "pulumi" }
  ```

**Property 2: `tags.Environment`**
- **Current state:** Code has `"dev"`
- **Desired state:** Should be `"production"` (from infrastructure)
- **Kind:** `update`
- **Skill action:** UPDATE the value
  ```typescript
  // Before
  tags: { Environment: "dev" }

  // After
  tags: { Environment: "production" }
  ```

### Semantic Correctness

✅ **Kind inversion is correct:**
- Preview said "delete" for `tags.ManagedBy` (in state, not code)
- Tool outputs `kind: "add"` - correct! Need to ADD to code
- Preview said "update" for `tags.Environment`
- Tool outputs `kind: "update"` - correct! Need to UPDATE code

✅ **Values are correct:**
- `currentValue` shows what's in code (or null if missing)
- `desiredValue` shows what should be in code (from state/infrastructure)

---

## Test Scenario 2: Resource Creation and Deletion

**Command:**
```bash
pulumi plugin run drift-adopter -- next --events-file testdata/ndjson_create_delete.ndjson
```

**Output:**
```json
{
  "status": "changes_needed",
  "resources": [
    {
      "action": "delete_from_code",
      "urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::extra-bucket",
      "type": "aws:s3/bucket:Bucket",
      "name": "extra-bucket"
    },
    {
      "action": "add_to_code",
      "urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::missing-bucket",
      "type": "aws:s3/bucket:Bucket",
      "name": "missing-bucket"
    }
  ]
}
```

### Skill Interpretation

**Resource 1: `extra-bucket`**
- **Action:** `delete_from_code`
- **Reason:** Preview wants to CREATE = resource in code but NOT in state
- **Skill action:** Delete the entire resource definition
  ```typescript
  // Before
  const extraBucket = new aws.s3.Bucket("extra-bucket", { ... });

  // After - remove this entire declaration
  ```

**Resource 2: `missing-bucket`**
- **Action:** `add_to_code`
- **Reason:** Preview wants to DELETE = resource in state but NOT in code
- **Skill action:** Add the resource back to code
  ```typescript
  // After
  const missingBucket = new aws.s3.Bucket("missing-bucket", {
    // Need to query state for current configuration
    bucket: "missing-bucket-123",
    tags: { ... }
  });
  ```

### Semantic Correctness

✅ **Action inversion is correct:**
- Preview operation "create" → Tool action "delete_from_code" ✓
- Preview operation "delete" → Tool action "add_to_code" ✓

✅ **No properties listed:**
- For `delete_from_code`: Makes sense, deleting entire resource
- For `add_to_code`: Makes sense, resource doesn't exist yet in code

---

## Test Scenario 3: Real Pulumi Service Data

**Command:**
```bash
pulumi plugin run drift-adopter -- next --events-file testdata/simple-s3-drift.ndjson
```

**Output:**
```json
{
  "status": "changes_needed",
  "resources": [
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
          "kind": "add"
        },
        {
          "path": "tags.ManagedBy",
          "currentValue": null,
          "desiredValue": "manual",
          "kind": "add"
        },
        {
          "path": "tagsAll.Environment",
          "currentValue": null,
          "desiredValue": "production",
          "kind": "add"
        },
        {
          "path": "tagsAll.ManagedBy",
          "currentValue": null,
          "desiredValue": "manual",
          "kind": "add"
        }
      ]
    }
  ]
}
```

### Skill Interpretation

**Scenario:** Someone manually added tags to the S3 bucket in AWS console

**Skill action:** Find "test-bucket" and ADD all missing tags:

```typescript
// Before
const bucket = new aws.s3.Bucket("test-bucket", {
  bucket: "test-bucket-xyz",
  // No tags defined
});

// After
const bucket = new aws.s3.Bucket("test-bucket", {
  bucket: "test-bucket-xyz",
  tags: {
    Environment: "production",
    ManagedBy: "manual"
  }
});
```

### Semantic Correctness

✅ **All properties have `kind: "add"`:**
- Preview had `diffKind: "delete"` (tags in state, not code)
- Tool correctly inverted to `kind: "add"` - need to ADD to code

✅ **Includes both `tags` and `tagsAll`:**
- AWS automatically populates `tagsAll` with all tags (including provider default tags)
- Skill should focus on `tags.*` properties (not `tagsAll.*`)
- Skill documentation mentions this pattern

---

## Test Scenario 4: Multiple Resources

**Command:**
```bash
pulumi plugin run drift-adopter -- next --events-file testdata/ndjson_multiple_resources.ndjson
```

**Output:**
```json
{
  "status": "changes_needed",
  "resources": [
    {
      "action": "update_code",
      "urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket-1",
      "type": "aws:s3/bucket:Bucket",
      "name": "bucket-1",
      "properties": [
        {
          "path": "tags.Version",
          "currentValue": "v1",
          "desiredValue": "v2",
          "kind": "update"
        }
      ]
    },
    {
      "action": "update_code",
      "urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket-2",
      "type": "aws:s3/bucket:Bucket",
      "name": "bucket-2",
      "properties": [
        {
          "path": "tags.Owner",
          "currentValue": "team-b",
          "desiredValue": "team-a",
          "kind": "update"
        }
      ]
    },
    {
      "action": "delete_from_code",
      "urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket-3",
      "type": "aws:s3/bucket:Bucket",
      "name": "bucket-3"
    }
  ]
}
```

### Skill Interpretation

**Skill workflow:**
1. Process each resource in order
2. Make code changes for all resources
3. Commit and push changes
4. Run preview again to get next batch

**Resource 1:** Update `bucket-1` tags.Version from "v1" to "v2"
**Resource 2:** Update `bucket-2` tags.Owner from "team-b" to "team-a"
**Resource 3:** Delete entire `bucket-3` resource from code

### Semantic Correctness

✅ **Batching works correctly:**
- Default limit is 10 resources per batch
- Skill iterates until status becomes "clean"

✅ **Mixed actions handled:**
- Two updates + one deletion in same batch
- Skill processes each independently

---

## Property Kind Semantics

The `kind` field tells the skill what to do with the property:

| Kind | Meaning | Skill Action | Example |
|------|---------|--------------|---------|
| `add` | Property missing from code | ADD property to code | `tags: { Env: "prod" }` → add ManagedBy |
| `delete` | Property in code but not state | REMOVE property from code | `tags: { Env: "prod", Temp: "x" }` → remove Temp |
| `update` | Property differs | CHANGE value in code | `tags: { Env: "dev" }` → change to "prod" |

### Kind Inversion Examples

| Preview Says | Preview Meaning | Tool Outputs | Code Action |
|--------------|----------------|--------------|-------------|
| `diffKind: "add"` | Code will add to infra | `kind: "delete"` | DELETE from code |
| `diffKind: "delete"` | Code will delete from infra | `kind: "add"` | ADD to code |
| `diffKind: "update"` | Code will update infra | `kind: "update"` | UPDATE code |

---

## Skill Decision Logic

### For `action: "update_code"` with properties:

```
For each property:
  if kind == "add":
    → Add property to resource definition
    → Use desiredValue from infrastructure

  if kind == "delete":
    → Remove property from resource definition
    → Ignore currentValue (it's wrong)

  if kind == "update":
    → Change property value
    → From currentValue to desiredValue
```

### For `action: "delete_from_code"`:

```
→ Find entire resource definition in code
→ Delete it completely (including exports, references)
→ No properties to process
```

### For `action: "add_to_code"`:

```
→ Create new resource definition
→ May need to query state for full configuration
→ No properties listed (resource doesn't exist in code yet)
```

---

## Stack Config Consideration

The skill should evaluate whether properties should use stack config:

**Example from output:**
```json
{
  "path": "tags.Environment",
  "currentValue": "dev",
  "desiredValue": "production",
  "kind": "update"
}
```

**Skill considers:**
- Is this environment-specific? YES (dev vs production)
- Should use config instead of hardcoding

**Skill generates:**
```typescript
const config = new pulumi.Config();

// Instead of: tags: { Environment: "production" }
// Use config with old value as default:
tags: { Environment: config.get("environment") || "dev" }
```

Then sets config:
```bash
pulumi config set environment production
```

---

## Semantic Correctness Summary

### ✅ What's Correct:

1. **Kind inversion works:** `add` ↔ `delete` inverted, `update` stays same
2. **Values are correct:** `currentValue` = code, `desiredValue` = state/infrastructure
3. **Resource actions inverted:** create→delete, delete→add, update→update
4. **Batching works:** Default 10 resources, iterative workflow
5. **Mixed actions handled:** Different actions in same batch work correctly

### 🎯 How Skill Interprets:

1. **Reads status:** "changes_needed" → proceed with changes
2. **Processes resources:** For each resource, check action type
3. **Applies property changes:** Based on kind (add/delete/update)
4. **Uses correct values:** Always use `desiredValue` (from state)
5. **Commits and iterates:** Push changes, run preview again

### 💡 Key Insight:

The output is from the **code's perspective**, not the preview's perspective:
- `kind: "add"` means "ADD this to your code"
- `desiredValue` means "this is what your code should have"
- `currentValue` means "this is what your code currently has"

This inversion makes the tool output intuitive for the skill to consume and for developers to understand!
