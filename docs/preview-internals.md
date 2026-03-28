# Pulumi Preview Internals and Drift-Adopter Interpretation

## Context

The drift-adopter was built empirically — by observing preview output from a handful of providers (random, tls, command, aws) and writing code to handle what we saw. It was not built from a first-principles understanding of how the Pulumi engine produces previews or how arbitrary provider implementations affect that output. The `triggers[0]` bug (array index paths in `getNestedProperty`) is symptomatic of this gap: we didn't know that path could contain array indices because we never studied how `DetailedDiff` paths are constructed.

This document closes that gap — both as reference documentation and as an audit checklist for finding more bugs like the one we just fixed.

---

## Section 1: How the Engine Produces Preview Output

### 1.1 Preview Lifecycle

`pulumi preview` runs the following pipeline:

1. **Read program** — evaluate the user's Pulumi program to compute desired state
2. **Provider Check** — validate inputs for each resource via the provider's `Check` RPC
3. **Provider Diff** — compare old state against new inputs via the provider's `Diff` RPC
4. **Step plan** — engine produces a plan: create, update, replace, delete, same
5. **JSON serialization** — steps serialized to JSON for `--json` output

No mutations happen during preview. `Create` and `Update` are called with `preview=true` — providers should NOT actually create/modify resources but MAY return anticipated outputs.

### 1.2 The Provider Diff Contract

From the gRPC protocol (`DiffRequest`/`DiffResponse`):

**Inputs to Diff:**
- `urn` — resource identity
- `id` — resource ID from provider
- `olds` — old **output** properties (full state from last Create/Update)
- `news` — new **input** properties (already validated by Check)
- `old_inputs` — old input properties
- `ignoreChanges` — property paths to ignore

**Key asymmetry:** `olds` is outputs/state while `news` is inputs — providers must account for this when comparing.

**Return:**
- `changes` enum: `DIFF_UNKNOWN`, `DIFF_NONE`, `DIFF_SOME`
- `replaces` — property paths requiring replacement
- `stables` — output properties guaranteed unchanged
- `deleteBeforeReplace` — whether replacement requires delete-first
- `diffs` — top-level property names that differ (flat list)
- `detailedDiff` map + `hasDetailedDiff` flag

**Invariant:** every top-level key in `diffs` must have a matching path in `detailedDiff`, and vice versa.

### 1.3 PreviewStep Fields

Reference: `pulumi/pkg/display/json.go:58-76`

```go
type previewStep struct {
    Op             string                  `json:"op"`
    URN            string                  `json:"urn"`
    Provider       string                  `json:"provider,omitempty"`
    OldState       *apitype.ResourceV3     `json:"oldState,omitempty"`
    NewState       *apitype.ResourceV3     `json:"newState,omitempty"`
    DiffReasons    []resource.PropertyKey  `json:"diffReasons,omitempty"`
    ReplaceReasons []resource.PropertyKey  `json:"replaceReasons,omitempty"`
    DetailedDiff   map[string]PropertyDiff `json:"detailedDiff"`
}
```

- `Op`: operation type — `create`, `update`, `replace`, `delete`, `same`, `read`, `refresh`
- `URN`: resource identity
- `OldState`/`NewState`: `*apitype.ResourceV3` — each has Type, Inputs, Outputs, PropertyDependencies, etc.
- `DiffReasons`: top-level property keys that differ (flat list)
- `ReplaceReasons`: subset requiring replacement (flat list)
- `DetailedDiff`: `map[string]PropertyDiff` — per-property diff with kind and inputDiff flag

### 1.4 OldState vs NewState During Preview

This is the critical nuance for understanding preview output:

| Operation | OldState | NewState |
|-----------|----------|----------|
| **create** | nil (resource doesn't exist in state) | Inputs from program; outputs empty/placeholder (Create() not called in dry-run) |
| **update** | Full resource from state (inputs + outputs) | Inputs from program; outputs **retain OLD values** (Update() not called) |
| **replace** | Full resource from state | Create-replacement step has **placeholder outputs** (unknown sentinels) |
| **delete** | Full resource from state | nil (resource doesn't exist in program) |
| **read** | Previous state if any | Outputs ARE populated (Read runs during preview) |

After `--refresh`, OldState reflects actual infrastructure (the refreshed state).

### 1.5 Secrets in Preview JSON

`showSecrets` is **hardcoded to `false`** in the preview JSON serialization.

Reference: `pulumi/pkg/backend/display/json.go:282,291`
```go
stack.SerializeResource(ctx, oldState, config.NewPanicCrypter(), false /* showSecrets */)
stack.SerializeResource(ctx, newState, config.NewPanicCrypter(), false /* showSecrets */)
```

All secret values appear as `"[secret]"` in preview JSON output. The `--show-secrets` CLI flag does NOT affect JSON serialization.

Secrets are identified in the protobuf wire format using a special signature property:
- Key: `"4dabf18193072939515e22adb298388d"` (defined in `pulumi/sdk/go/common/resource/sig/sig.go:21`)
- Value: `"1b47061264138c4ac30d75fd1eb44270"`

### 1.6 Unknowns in Preview Output

Unknown values are represented by a special sentinel string:

```go
// pulumi/sdk/go/common/resource/plugin/rpc.go:61
UnknownStringValue = "04da6b54-80e4-46f7-96ec-b56ff0331ba9"
```

During preview, computed output properties appear as this UUID. Unknown status cascades through all dependent computations. When delete-before-replace is happening, the engine substitutes unknowns for the replaced resource's outputs before calling Diff on dependents.

**Concrete example** from `testdata/small_scale_10_replace.json` — a RandomString replace operation:
```json
"newState": {
    "outputs": {
        "id": "04da6b54-80e4-46f7-96ec-b56ff0331ba9",
        "result": "04da6b54-80e4-46f7-96ec-b56ff0331ba9",
        "length": 16,
        "special": false
    }
}
```

Here `id` and `result` are computed outputs that can't be known until the actual Create runs, so the engine fills them with the unknown sentinel. Note that input-derived outputs like `length` and `special` retain their input values.

---

## Section 2: DetailedDiff — The Core Diff Mechanism

### 2.1 Two Sources of DetailedDiff

1. **Provider's `Diff()` method** (primary) — `step_generator.go:2725`. Provider compares old state against new inputs, returns `DiffResult` with `DetailedDiff` and `HasDetailedDiff=true`.

2. **Engine-synthesized fallback** when provider returns `DIFF_UNKNOWN` (legacy, generally avoided) — engine computes `oldInputs.Diff(newInputs)` with `inputDiff=true`:

   Reference: `pulumi/pkg/resource/deploy/step_generator.go:2739-2748`
   ```go
   diff.DetailedDiff = plugin.NewDetailedDiffFromObjectDiff(tmp, true /* inputDiff */)
   ```

### 2.2 The `inputDiff` Flag

The most misunderstood field in the diff system:

- `true` → diff between **old inputs** and **new inputs** — "the user changed this"
- `false` → diff between **old provider state/outputs** and **new inputs** — may include provider-side drift or transformations

**This is NOT "is this an input property"** — it describes the comparison methodology used to detect the change.

Reference: `pulumi/sdk/go/common/resource/plugin/provider.go:616-620`
```go
type PropertyDiff struct {
    Kind      DiffKind // The kind of diff assoiated with this property.
    InputDiff bool     // The difference is between old and new inputs, not old state and new inputs.
}
```

### 2.3 DiffKind Values

Reference: `pulumi/sdk/go/common/resource/plugin/provider.go:601-614`

| Constant | Value | Meaning |
|----------|-------|---------|
| `DiffAdd` | 0 | Property was added |
| `DiffAddReplace` | 1 | Property was added, requires replacement |
| `DiffDelete` | 2 | Property was deleted |
| `DiffDeleteReplace` | 3 | Property was deleted, requires replacement |
| `DiffUpdate` | 4 | Property was updated |
| `DiffUpdateReplace` | 5 | Property was updated, requires replacement |

In JSON serialization these become lowercase strings: `"add"`, `"add-replace"`, `"delete"`, `"delete-replace"`, `"update"`, `"update-replace"`.

### 2.4 DiffReasons vs ReplaceReasons vs DetailedDiff

These three fields serve different purposes and have different population guarantees:

- **DiffReasons**: flat list of top-level keys that changed. Always populated when there's a diff.
- **ReplaceReasons**: subset where change requires replacement. Only for replace ops.
- **DetailedDiff**: nested map with per-property detail. **May be null** even when DiffReasons is populated (depends on provider's `hasDetailedDiff`).

**Concrete example** — TLS PrivateKey replace from `testdata/standard_json_replace.json`:
```json
{
    "op": "replace",
    "urn": "...tls:index/privateKey:PrivateKey::tls-key-0",
    "diffReasons": [
        "algorithm", "ecdsaCurve", "id",
        "privateKeyOpenssh", "privateKeyPem", "privateKeyPemPkcs8",
        "publicKeyFingerprintMd5", "publicKeyFingerprintSha256",
        "publicKeyOpenssh", "publicKeyPem"
    ],
    "replaceReasons": ["algorithm", "ecdsaCurve"],
    "detailedDiff": null
}
```

Key observations:
- `detailedDiff` is null — TLS provider doesn't set `hasDetailedDiff`
- `diffReasons` includes output-only properties (`id`, `publicKeyPem`, etc.)
- `replaceReasons` includes only the input properties that triggered replacement
- The drift-adopter must synthesize DetailedDiff from these lists (see Section 4.3)

### 2.5 PropertyPath Format in DetailedDiff Keys

From Pulumi's `resource.PropertyPath.String()`:

| Pattern | Example | Description |
|---------|---------|-------------|
| Dot-separated keys | `"tags.Environment"` | Object/map property access |
| Bracket notation | `"ingress[0]"` | Array/list element access |
| Mixed nested | `"ingress[0].fromPort"` | Combined object + array |
| Bracket-quoted keys | `'["key.with.dots"]'` | Keys containing dots or special characters |

This is the canonical format used by all providers. The bridge delegates to `resource.PropertyPath.String()` via `property_path.go:24-26`:
```go
func (k propertyPath) String() string {
    return resource.PropertyPath(k).String()
}
```

---

## Section 3: How Provider Implementations Affect Previews

### 3.1 Three Categories of Providers

| Category | DetailedDiff | inputDiff | Examples |
|----------|-------------|-----------|---------|
| **Terraform-bridged** | Always populated (`hasDetailedDiff=true`) | Set by bridge (typically `true` for input comparisons) | AWS, GCP, Azure, Docker, Cloudflare |
| **Native Pulumi providers** | Implementation-dependent | Implementation-dependent | Command, Random, TLS |
| **DIFF_UNKNOWN providers** | Engine-synthesized | Always `true` | Legacy/minimal providers |

### 3.2 Terraform-Bridged Providers

The majority of the Pulumi ecosystem. Bridge lives at `pulumi-terraform-bridge/pkg/tfbridge/`.

**Always returns `HasDetailedDiff=true`** — confirmed at `provider.go:1292`:
```go
HasDetailedDiff: true,
```
This is set unconditionally in all code paths.

**Two diff algorithms:**
- `MakeDetailedDiffV2` (accurate, compares proposed vs prior state)
- `makeDetailedDiffExtra` (legacy, uses Terraform's InstanceDiff)
- Chosen by `enableAccurateBridgePreview` flag

**ForceNew → Replace mapping** — `detailed_diff.go:75-91`:
```go
func promoteToReplace(kind pulumirpc.PropertyDiff_Kind) pulumirpc.PropertyDiff_Kind {
    switch kind {
    case pulumirpc.PropertyDiff_ADD:
        return pulumirpc.PropertyDiff_ADD_REPLACE
    case pulumirpc.PropertyDiff_DELETE:
        return pulumirpc.PropertyDiff_DELETE_REPLACE
    case pulumirpc.PropertyDiff_UPDATE:
        return pulumirpc.PropertyDiff_UPDATE_REPLACE
    }
}
```
Applied when `propertyPathTriggersReplacement()` returns true (Terraform's `RequiresNew`). Two-level checking: property itself and parent collection.

**Computed values included in diff** — bridge does NOT filter computed property changes. They appear as UPDATE entries with `inputDiff` potentially false.

**Set handling** (`detailed_diff.go:305-617`) — uses `SetHash()` for element identity matching. When hash-based matching fails, reports whole set as UPDATE.

**MaxItemsOne flattening** (`detailed_diff.go:271-288`) — TypeList/TypeSet with MaxItems:1 flattened to single object. Path skips array index: `"prop.nested"` not `"prop[0].nested"`. The bridge's `getEffectiveType()` recursively unwraps when `IsMaxItemsOne(tfs, ps)` is true.

**Path format** — uses Pulumi's `PropertyPath.String()`: `"tags.Name"`, `"ingress[0].fromPort"`, `"egressRules[0].cidrBlocks[0]"`.

### 3.3 Command Provider (`command:local:Command`)

- Reports ALL diffs with `inputDiff=false` — provider's Diff compares state vs inputs
- Copies input values to outputs, so the fallback chain (Outputs→Inputs) in `resolvePropertyValue` works
- Does not always populate DetailedDiff for all change types

**Example** from `testdata/standard_json_replace.json` — update with environment deletion:
```json
"detailedDiff": {
    "create": {"kind": "update", "inputDiff": false},
    "environment": {"kind": "delete", "inputDiff": false}
}
```

### 3.4 TLS Provider

- May return **null `DetailedDiff`** for replace operations
- Provides ReplaceReasons and DiffReasons but no detailed diff
- DiffReasons includes output-only properties (`id`, `publicKeyPem`, etc.)

### 3.5 Random Provider

- Similar to TLS: null DetailedDiff for replace operations
- Unknown sentinels appear in NewState outputs for computed fields during replace

**Example** from `testdata/small_scale_10_replace.json`:
```json
"replaceReasons": ["length", "special"],
"detailedDiff": null
```

### 3.6 Key Observation

Bridged providers (AWS, GCP, Azure) are the most predictable because the bridge enforces consistent behavior — `HasDetailedDiff` is always true, paths follow `PropertyPath.String()`, and the ForceNew mapping is mechanical. Native providers are the wild cards.

---

## Section 4: How the Drift-Adopter Interprets Preview Data

### 4.1 Semantic Inversion

The drift-adopter inverts preview semantics — preview says "what infrastructure changes are needed to match code", but we want "what code changes are needed to match infrastructure":

Reference: `next.go:232-255`

| Preview Op | Drift-Adopter Action | Interpretation |
|-----------|---------------------|----------------|
| `delete` | `add_to_code` | Resource in infrastructure, not in code |
| `create` | `delete_from_code` | Resource in code, not in infrastructure |
| `update` | `update_code` | Code differs from infrastructure |
| `replace` | `update_code` | Code change requires replacement |
| `same`/`read`/`refresh` | (skipped) | No code changes needed |

**State value inversion:**
- NewState = currentValue (what code says now — what's wrong)
- OldState = desiredValue (what infrastructure is — what we want the code to match)

### 4.2 Three-Format Normalization

Reference: `parse.go:60-101`

The drift-adopter accepts three input formats, all normalized to `[]auto.PreviewStep`:

1. **Standard JSON** (`{"steps":[...]}`) — from `pulumi preview --json`
2. **Engine Events** (`{"events":[...]}`) — from Pulumi Cloud API. Uses `"old"`/`"new"` (not `"oldState"`/`"newState"`) and `"diffKind"` (not `"kind"`). Engine events path synthesizes DetailedDiff from Diffs when DetailedDiff is empty.
3. **NDJSON** — from `pulumi_preview` MCP tool. Each line is an engine event.

### 4.3 DetailedDiff Normalization

Reference: `properties.go:159-177`

For replace/update ops with empty DetailedDiff, the drift-adopter synthesizes entries:

```go
func normalizeDetailedDiff(step *auto.PreviewStep) {
    if len(step.DetailedDiff) > 0 || (step.Op != "replace" && step.Op != "update") {
        return
    }
    diffKeys := step.ReplaceReasons    // prefer ReplaceReasons
    if len(diffKeys) == 0 {
        diffKeys = step.DiffReasons    // fall back to DiffReasons
    }
    // ...
    for _, key := range diffKeys {
        step.DetailedDiff[string(key)] = auto.PropertyDiff{Kind: "update", InputDiff: true}
    }
}
```

**Design choice:** ReplaceReasons preferred over DiffReasons because DiffReasons includes output-only properties (like `id`, `publicKeyPem` in TLS) that produce noisy, unactionable output.

### 4.4 Value Resolution

Reference: `properties.go:179-193`

```go
func resolvePropertyValue(state *apitype.ResourceV3, path string, inputsOnly bool) interface{} {
    if !inputsOnly && state.Outputs != nil {
        if v := getNestedProperty(state.Outputs, path); v != nil {
            return v
        }
    }
    if state.Inputs != nil {
        return getNestedProperty(state.Inputs, path)
    }
    return nil
}
```

Resolution strategy:
- `inputDiff=false` → try **Outputs first**, fall back to **Inputs**
- `inputDiff=true` → **Inputs only**
- For `update_code`: currentValue from **NewState**, desiredValue from **OldState**
- Both nil → property skipped (assumed computed-only)

### 4.5 Path Resolution

Reference: `properties.go:244-291`

`getNestedProperty()` splits on `.` for nested map access, and handles `key[N]` array index syntax:

```go
func getNestedProperty(props map[string]interface{}, path string) interface{} {
    parts := strings.Split(path, ".")
    for _, part := range parts {
        if idx := strings.IndexByte(part, '['); idx >= 0 {
            key := part[:idx]
            indexStr := strings.TrimRight(part[idx+1:], "]")
            // Navigate to array, then index into it
        }
        // Otherwise: plain map key access
    }
}
```

### 4.6 Data Flow Diagram

```
pulumi preview --json --refresh
       |
       v
  [Raw JSON: 3 formats]
       |
  parsePreviewOutput()              (parse.go:60-101)
       |
       v
  []auto.PreviewStep  (normalized)
       |
  +--> normalizeDetailedDiff()      (properties.go:159-177)
  |         per step
  |
  +--> getActionForOperation()      (next.go:232-255)
  |         maps op -> action
  |
  +--> extractPropertyChanges()     (properties.go:98-136)
  |    or extractInputProperties()  (properties.go:26-96)
  |         |
  |         +--> resolvePropertyValue()   (properties.go:179-193)
  |                   per DetailedDiff entry
  |                   |
  |                   +--> getNestedProperty()  (properties.go:244-291)
  |                             on Outputs/Inputs
  |
  +--> dependency enrichment from state export
       |
       v
  []ResourceChange
       |
  sortResourcesByDependencies()
       |
  outputResult()                    (output.go:25-109)
       |
       v
  JSON file + stdout summary
```

---

## Section 5: Annotated Examples

### Example 1: Simple Update — AWS S3 Bucket (Engine Events Format)

From `testdata/engine_events_update.json`:

```json
{
    "op": "update",
    "urn": "...aws:s3/bucket:Bucket::my-bucket",
    "old": {
        "inputs": {
            "bucket": "my-bucket-123",
            "tags": {"Environment": "production", "ManagedBy": "pulumi"}
        },
        "outputs": {
            "bucket": "my-bucket-123",
            "tags": {"Environment": "production", "ManagedBy": "pulumi"}
        }
    },
    "new": {
        "inputs": {
            "bucket": "my-bucket-123",
            "tags": {"Environment": "dev"}
        },
        "outputs": {
            "bucket": "my-bucket-123",
            "tags": {"Environment": "dev"}
        }
    },
    "detailedDiff": {
        "tags.Environment": {"diffKind": "update", "inputDiff": true},
        "tags.ManagedBy":   {"diffKind": "delete", "inputDiff": true}
    }
}
```

**Drift-adopter interpretation:**
- Action: `update_code` (preview op = "update")
- `tags.Environment`: currentValue=`"dev"` (NewState.Inputs), desiredValue=`"production"` (OldState.Inputs)
- `tags.ManagedBy`: currentValue=nil (not in NewState), desiredValue=`"pulumi"` (OldState.Inputs)
- `inputDiff=true` → values resolved from Inputs only (correct — bridged AWS provider)

Note: engine events format uses `"diffKind"` instead of `"kind"` and `"old"`/`"new"` instead of `"oldState"`/`"newState"`. The parser normalizes these at `parse.go:161-174`.

### Example 2: Replace with Null DetailedDiff — TLS PrivateKey

From `testdata/standard_json_replace.json`:

```json
{
    "op": "replace",
    "urn": "...tls:index/privateKey:PrivateKey::tls-key-0",
    "oldState": {
        "inputs": {"algorithm": "ECDSA", "ecdsaCurve": "P256"},
        "outputs": {
            "algorithm": "ECDSA", "ecdsaCurve": "P256",
            "id": "4e643a5dcb5637f8bdf30444f377798c49ebcae0",
            "publicKeyPem": "-----BEGIN PUBLIC KEY-----\n...",
            "privateKeyPem": "[secret]"
        }
    },
    "newState": {
        "inputs": {"algorithm": "RSA", "rsaBits": 2048},
        "outputs": {
            "algorithm": "RSA", "ecdsaCurve": "P224",
            "id": "04da6b54-80e4-46f7-96ec-b56ff0331ba9",
            "publicKeyPem": "04da6b54-80e4-46f7-96ec-b56ff0331ba9",
            "privateKeyPem": "[secret]"
        }
    },
    "diffReasons": [
        "algorithm", "ecdsaCurve", "id",
        "privateKeyOpenssh", "privateKeyPem", "privateKeyPemPkcs8",
        "publicKeyFingerprintMd5", "publicKeyFingerprintSha256",
        "publicKeyOpenssh", "publicKeyPem"
    ],
    "replaceReasons": ["algorithm", "ecdsaCurve"],
    "detailedDiff": null
}
```

**Drift-adopter interpretation:**
1. `normalizeDetailedDiff()` fires because `detailedDiff` is null
2. Prefers `replaceReasons` over `diffReasons` → synthesizes entries for `"algorithm"` and `"ecdsaCurve"` only
3. This avoids 8 noisy output-only properties in `diffReasons` (`id`, `publicKeyPem`, etc.)
4. Both synthesized entries get `inputDiff: true` → resolves from Inputs only
5. `algorithm`: currentValue=`"RSA"` (NewState.Inputs), desiredValue=`"ECDSA"` (OldState.Inputs)
6. `ecdsaCurve`: currentValue=nil (not in NewState.Inputs), desiredValue=`"P256"` (OldState.Inputs)

**Unknown sentinels visible:** NewState outputs contain `"04da6b54-80e4-46f7-96ec-b56ff0331ba9"` for computed fields (`id`, `publicKeyPem`, etc.). These are NOT the real values — they're placeholders because Create() hasn't run.

### Example 3: Command Provider — inputDiff=false and Array Paths

From `testdata/command_with_triggers.json`:

```json
{
    "op": "replace",
    "urn": "...command:local:Command::cache-cmd",
    "oldState": {
        "inputs": {
            "create": "echo \"Initializing cache\"",
            "environment": {"APP_NAME": "cache", "DRIFT": "true"},
            "triggers": ["old-secret-val"]
        },
        "outputs": {
            "create": "echo \"Initializing cache\"",
            "environment": {"APP_NAME": "cache", "DRIFT": "true"},
            "stderr": "", "stdout": "Initializing cache",
            "triggers": ["old-secret-val"]
        }
    },
    "newState": {
        "inputs": {
            "create": "echo \"Initializing cache\"",
            "environment": {"APP_NAME": "cache"},
            "triggers": ["new-computed-val"]
        },
        "outputs": {
            "create": "echo \"Initializing cache\"",
            "environment": {"APP_NAME": "cache"},
            "stderr": "placeholder", "stdout": "placeholder",
            "triggers": ["new-computed-val"]
        }
    },
    "detailedDiff": {
        "environment.DRIFT": {"kind": "delete", "inputDiff": false},
        "triggers[0]":       {"kind": "update-replace", "inputDiff": false}
    }
}
```

**Key observations:**
- `inputDiff=false` on both entries — Command provider compares state vs inputs, not old-inputs vs new-inputs
- `triggers[0]` uses array index syntax — this was the bug that motivated this document
- Since `inputDiff=false`, `resolvePropertyValue` tries Outputs first. For the Command provider, outputs mirror inputs, so this works correctly.
- `environment.DRIFT`: currentValue=nil (not in NewState), desiredValue=`"true"` (from OldState.Outputs→Inputs chain)

### Example 4: Delete → add_to_code

From `testdata/small_scale_10_replace.json`:

```json
{
    "op": "delete",
    "urn": "...random:index/randomString:RandomString::random-str-extra-0",
    "oldState": {
        "inputs": {"length": 16, "special": false},
        "outputs": {
            "id": "mooRWQ7ZnUwjiiOf",
            "length": 16, "special": false,
            "lower": true, "minLower": 0, "minNumeric": 0,
            "minSpecial": 0, "minUpper": 0,
            "number": true, "numeric": true,
            "result": "mooRWQ7ZnUwjiiOf",
            "upper": true
        }
    }
}
```

**Drift-adopter interpretation:**
- Action: `add_to_code` (preview op = "delete" — resource exists in state but not in code)
- `extractInputProperties()` runs (not `extractPropertyChanges`)
- Prefers `OldState.Inputs` (`{"length": 16, "special": false}`) over `OldState.Outputs`
- This avoids exposing 10+ computed output properties (`id`, `result`, `lower`, `upper`, etc.)
- If Inputs were empty, would fall back to Outputs

### Example 5: Unknown Sentinels in Replace NewState

From `testdata/small_scale_10_replace.json`:

```json
{
    "op": "replace",
    "urn": "...random:index/randomString:RandomString::random-str-0",
    "oldState": {
        "inputs": {"length": 32, "special": true},
        "outputs": {
            "id": "NmO$?6)J-wwX2DM1Os&ZKN!173xdU4EZ",
            "result": "NmO$?6)J-wwX2DM1Os&ZKN!173xdU4EZ",
            "length": 32, "special": true
        }
    },
    "newState": {
        "inputs": {"length": 16, "special": false},
        "outputs": {
            "id": "04da6b54-80e4-46f7-96ec-b56ff0331ba9",
            "result": "04da6b54-80e4-46f7-96ec-b56ff0331ba9",
            "length": 16, "special": false
        }
    },
    "replaceReasons": ["length", "special"],
    "detailedDiff": null
}
```

**The unknown sentinel `"04da6b54-80e4-46f7-96ec-b56ff0331ba9"` appears in:**
- `newState.outputs.id` — computed, can't be known until Create runs
- `newState.outputs.result` — computed, can't be known until Create runs

**Why this matters:** If the drift-adopter resolves `currentValue` from NewState.Outputs for these paths, it would get the sentinel UUID as a literal string. The agent would then try to set `id = "04da6b54-..."` in code — which is nonsensical.

**Current protection:** For this specific case, `normalizeDetailedDiff` uses `replaceReasons` (only `"length"` and `"special"`), so the computed paths are never queried. But this protection is indirect and brittle.

---

## Section 6: Risks, Gaps, and Audit Checklist

### 5.1 Unknown Sentinel Values Treated as Literal Strings

**Risk:** The unknown sentinel `"04da6b54-80e4-46f7-96ec-b56ff0331ba9"` appears in NewState.Outputs for create/replace steps. If any code path resolves a value from these outputs, the agent gets a UUID it would try to set in code.

**Why it exists:** `resolvePropertyValue` with `inputDiff=false` checks Outputs first. For replace operations, NewState.Outputs contain sentinel values for computed fields.

**Symptoms:** Agent instruction to set a property to a UUID-like string, or property changes with the sentinel as currentValue.

- [ ] **Investigate:** Filter unknown sentinels in `resolvePropertyValue` or `extractPropertyChanges`. Check if the SDK's deserialization preserves the sentinel or if it's already converted. The sentinel is defined at `rpc.go:61`.

### 5.2 Secrets Always Masked in Preview JSON

**Risk:** `showSecrets` is hardcoded to `false` in preview JSON serialization (`json.go:282,291`). Agent gets `"[secret]"` for both currentValue and desiredValue.

**Symptoms:** For `update_code`, both values may be `"[secret]"` — agent can't determine the actual change. For `add_to_code`, InputProperties contain `"[secret]"` for secret fields.

**Example from testdata:** TLS PrivateKey `privateKeyPem: "[secret]"` in both OldState and NewState outputs.

- [ ] **Investigate:** Supplement preview values from state export (which DOES honor `--show-secrets` — see `parse.go:248`). Or patch the engine to honor the flag in JSON serialization.

### 5.3 inputDiff=false with Stale NewState Outputs

**Risk:** For updates, NewState.Outputs retains OLD values (Update() not called in preview). When `inputDiff=false`, `resolvePropertyValue` checks NewState.Outputs first — gets stale old outputs, not the current-code values.

**Why it works in practice:** For input properties, the fallback to NewState.Inputs finds the correct value. Fails only for output-only properties reported with `inputDiff=false`.

**Symptoms:** `currentValue` shows the old output value instead of what the code currently specifies.

- [ ] **Investigate:** Audit whether any real-world DetailedDiff entries have output-only paths with `inputDiff=false` where the stale value causes incorrect currentValue.

### 5.4 Computed Values in Bridged Provider Diffs

**Risk:** Terraform-bridged providers include computed property changes in DetailedDiff. The bridge does NOT filter them. These appear as UPDATE entries with `inputDiff` potentially false.

**Current protection:** The nil/nil skip in `extractPropertyChanges` (`properties.go:124-126`) handles cases where both old and new values are nil. But some computed properties may resolve to stale-value/stale-value (non-nil but wrong) rather than nil/nil.

- [ ] **Investigate:** Check whether computed properties can produce non-nil but incorrect values that bypass the nil/nil filter.

### 5.5 Set Element Reordering in Bridged Providers

**Risk:** Terraform sets use hash-based identity. When bridge set matching fails (`detailed_diff.go:580-617`), it reports the entire set as UPDATE. Individual set elements may show as changed even though only ordering changed.

**Symptoms:** Agent makes unnecessary code changes for set property reordering that doesn't affect infrastructure behavior.

- [ ] **Investigate:** Check if this produces false-positive property changes in the drift-adopter.

### 5.6 MaxItemsOne Path Flattening

**Risk:** Bridged providers flatten TypeList/TypeSet with MaxItems:1 — path is `"prop.nested"` not `"prop[0].nested"`. If state has the un-flattened representation (array), the path won't match.

**Current protection:** `getNestedProperty` handles dot-paths fine. The bridge's `getEffectiveType()` (`detailed_diff.go:271-288`) ensures the path is already flattened.

- [ ] **Investigate:** Check whether state export and preview JSON are consistent about MaxItemsOne flattening.

### 5.7 normalizeDetailedDiff Always Uses "update" Kind

**Risk:** Synthesized entries (`properties.go:175`) always use `Kind: "update"` regardless of whether the property was added, deleted, or updated. Loses add/delete semantics.

**Current impact:** Low — the drift-adopter resolves actual values (nil for missing properties) so the correct semantics are recovered downstream.

- [ ] **Low priority:** Could parse old/new inputs to determine accurate kinds if downstream consumers ever need it.

### 5.8 Consecutive Array Indices in Paths

**Risk:** `items[0].nested[1]` works (each dot-segment has at most one `[`). But `items[0][1]` (consecutive indices without a dot) would fail — `getNestedProperty` splits on `.` first, leaving `items[0][1]` as a single segment where the parser only handles one `[`.

- [ ] **Investigate:** Check whether any provider generates paths with consecutive array indices. Pulumi's `PropertyPath.String()` likely doesn't produce this for map-then-array patterns, but verify for nested arrays (list of lists).

### 5.9 Keys Containing Dots or Special Characters

**Risk:** Pulumi's canonical path format uses `["key.with.dots"]` for keys containing special characters. `getNestedProperty` splits on `.` unconditionally — would break on these paths.

**Example:** A tag key like `"kubernetes.io/cluster-name"` might produce a DetailedDiff path `tags["kubernetes.io/cluster-name"]`.

- [ ] **Investigate:** Check whether any real-world provider produces DetailedDiff paths with bracket-quoted keys. If so, need a proper PropertyPath parser instead of naive `strings.Split(path, ".")`.

---

## Section 7: Quick Reference Table

| Preview Op | Drift-Adopter Action | Property Source | Value Resolution | Key Pitfalls |
|---|---|---|---|---|
| `delete` | `add_to_code` | `OldState.Inputs` (fallback: Outputs) | All properties as flat InputProperties map | Outputs fallback includes computed fields |
| `create` | `delete_from_code` | None | No properties needed | — |
| `update` | `update_code` | DetailedDiff entries | NewState=current, OldState=desired | Stale NewState outputs; secrets masked; unknowns |
| `replace` | `update_code` | DetailedDiff (or synthesized from ReplaceReasons) | NewState=current, OldState=desired | May need synthesis; placeholder outputs with unknowns |

---

## Source References

### Engine source
- `pulumi/pkg/display/json.go:58-76` — PreviewStep struct definition
- `pulumi/pkg/resource/deploy/step_generator.go:2739-2748` — DetailedDiff population, DiffUnknown fallback
- `pulumi/sdk/go/common/resource/plugin/provider.go:601-620` — DiffKind enum, PropertyDiff struct
- `pulumi/pkg/backend/display/json.go:282,291` — Preview JSON serialization, showSecrets hardcoded to false
- `pulumi/sdk/go/common/resource/plugin/rpc.go:61` — Unknown sentinel UUID
- `pulumi/sdk/go/common/resource/sig/sig.go:21` — Secret signature key

### Bridge source
- `pulumi-terraform-bridge/pkg/tfbridge/provider.go:1292` — HasDetailedDiff unconditionally true
- `pulumi-terraform-bridge/pkg/tfbridge/detailed_diff.go:75-91` — promoteToReplace() ForceNew mapping
- `pulumi-terraform-bridge/pkg/tfbridge/detailed_diff.go:271-288` — MaxItemsOne getEffectiveType()
- `pulumi-terraform-bridge/pkg/tfbridge/detailed_diff.go:305-617` — Set handling with SetHash
- `pulumi-terraform-bridge/pkg/tfbridge/property_path.go:24-26` — Path delegation to PropertyPath.String()

### Drift-adopter source
- `cmd/pulumi-drift-adopt/properties.go:98-136` — extractPropertyChanges
- `cmd/pulumi-drift-adopt/properties.go:159-177` — normalizeDetailedDiff
- `cmd/pulumi-drift-adopt/properties.go:179-193` — resolvePropertyValue
- `cmd/pulumi-drift-adopt/properties.go:244-291` — getNestedProperty
- `cmd/pulumi-drift-adopt/parse.go:60-101` — Three-format parsing
- `cmd/pulumi-drift-adopt/next.go:232-255` — getActionForOperation
- `cmd/pulumi-drift-adopt/output.go:25-109` — outputResult skip/filter logic

### Testdata
- `testdata/standard_json_replace.json` — TLS null DetailedDiff, unknown sentinels in outputs
- `testdata/command_with_triggers.json` — Command inputDiff=false, array index paths
- `testdata/engine_events_update.json` — AWS S3 bucket update with nested tags path
- `testdata/small_scale_10_replace.json` — Unknown sentinels, delete operation, Random provider behavior
