# Provider-Agnostic Drift Testing

This document explains the provider-agnostic approach to drift testing implemented in the V2 tests.

## Motivation

The original tests (V1) used AWS CLI to create drift:
- `aws s3api put-bucket-tagging` to add tags
- `aws s3api delete-bucket` to delete buckets

This approach had limitations:
- **Provider-specific**: Only works with AWS
- **Requires CLI tools**: Needs AWS CLI installed and configured
- **Complex setup**: Different commands for different cloud providers

The V2 approach is **provider-agnostic** and uses only Pulumi itself.

## How It Works

### The Core Technique

1. **Deploy original program** → Infrastructure matches code matches state
   ```
   Code: no tags
   State: no tags
   Infrastructure: no tags
   ```

2. **Export state** → Save current state to file
   ```bash
   pulumi stack export --file state.json
   ```

3. **Deploy drifted program** → Infrastructure changes
   ```
   Code (drifted): with tags
   State: with tags
   Infrastructure: with tags ✅
   ```

4. **Import original state** → Reset state to pre-drift
   ```bash
   pulumi stack import --file state.json
   ```

5. **Result: DRIFT!**
   ```
   Code (original): no tags
   State (original): no tags
   Infrastructure: with tags ⚠️  DRIFT!
   ```

6. **Refresh** → Detect drift
   ```bash
   pulumi refresh
   pulumi preview  # Shows updates needed
   ```

### Directory Structure

Each example has `original/` and `drifted/` subdirectories with project files in the root:

```
examples/
  simple-s3/
    Pulumi.yaml        # Project configuration (no program files in root)
    package.json       # Dependencies
    original/
      index.ts         # Original: no tags
    drifted/
      index.ts         # Modified: with tags

  multi-resource/
    Pulumi.yaml
    package.json
    original/
      index.ts         # Original: 3 buckets (a, b, c)
    drifted/
      index.ts         # Modified: 2 buckets (a, c) - b removed

  loop-resources/
    Pulumi.yaml
    package.json
    original/
      index.ts         # Original: array with 3 items
    drifted/
      index.ts         # Modified: array with 2 items - data-bucket removed
```

## Implementation

### Helper Functions

The implementation uses `UpdateSource()` from pulumitest to swap between program versions:

#### CreateTestStack

```go
func CreateTestStack(t *testing.T, exampleDir string) *TestStack {
    // Convert to absolute path to avoid issues after CopyToTempDir
    absExampleDir, err := filepath.Abs(exampleDir)
    require.NoError(t, err)

    // Create test in parent directory (contains project files but no program)
    test := pulumitest.NewPulumiTest(t, absExampleDir).CopyToTempDir(t)

    // Copy original program files into working directory
    originalDir := filepath.Join(absExampleDir, "original")
    test.UpdateSource(t, originalDir)

    // Deploy initial stack
    test.Up(t)

    return &TestStack{...}
}
```

#### CreateDriftWithProgram

```go
func (ts *TestStack) CreateDriftWithProgram(t *testing.T, exampleDir string) error {
    absExampleDir, err := filepath.Abs(exampleDir)
    if err != nil {
        return err
    }

    // 1. Export state from original deployment
    stateFile := filepath.Join(os.TempDir(), "drift-test-state.json")
    exec.Command("pulumi", "stack", "export", "--file", stateFile).Run()

    // 2. UpdateSource to drifted program
    driftedDir := filepath.Join(absExampleDir, "drifted")
    ts.Test.UpdateSource(t, driftedDir)

    // 3. Deploy drifted program (modifies infrastructure)
    ts.Test.Up(t)

    // 4. Import original state back (resets state, creates drift)
    exec.Command("pulumi", "stack", "import", "--file", stateFile).Run()

    // 5. UpdateSource back to original program (for Claude to fix)
    originalDir := filepath.Join(absExampleDir, "original")
    ts.Test.UpdateSource(t, originalDir)

    return nil
}
```

**Key points:**
- Uses `UpdateSource()` to swap program files without creating new pulumitest instances
- Converts paths to absolute paths to avoid relative path issues after `CopyToTempDir()`
- All operations use the same stack, ensuring proper cleanup
- Restores original program files after creating drift so Claude sees the code that needs fixing

### Test Pattern

```go
func TestDriftAdoptionWorkflowV2(t *testing.T) {
    ctx := context.Background()
    config := DriftTestConfig{
        ExampleDir:    filepath.Join("..", "..", "examples", "simple-s3"),
        MaxIterations: 10,
    }

    // 1. Deploy original (from original/ subdirectory)
    testStack := CreateTestStack(t, config.ExampleDir)
    defer testStack.Destroy(t)

    // 2. Create drift using drifted program
    testStack.CreateDriftWithProgram(t, config.ExampleDir)

    // 3. Verify drift exists
    testStack.VerifyDriftExists(t)

    // 4. Run Claude to adopt drift
    RunDriftAdoptionWithClaude(ctx, testStack, config.MaxIterations, t)

    // 5. Verify code updated and no drift remains
    testStack.VerifyNoDrift(t)
}
```

Note: `CreateDriftWithProgram()` now takes the example directory (parent) rather than the drifted subdirectory.

## Benefits

✅ **Provider-agnostic** - Works with AWS, Azure, GCP, Kubernetes, etc.
✅ **No external tools** - Only requires Pulumi CLI
✅ **Consistent approach** - Same technique for all drift types
✅ **Easier to understand** - Uses familiar Pulumi commands
✅ **More maintainable** - No provider-specific API knowledge needed

## Running the Tests

```bash
# Run all V2 tests
go test -tags=e2e -v -timeout=30m ./test/e2e/ -run "V2$"

# Run specific V2 test
go test -tags=e2e -v -timeout=30m ./test/e2e/ -run TestDriftAdoptionWorkflowV2

# Run with test script
./scripts/test-with-env.sh -tags=e2e -v -timeout=30m -run TestDriftAdoptionWorkflowV2 ./test/e2e/
```

## Comparison: V1 vs V2

| Aspect | V1 (AWS CLI) | V2 (Provider-Agnostic) |
|--------|-------------|------------------------|
| Drift Creation | AWS CLI commands | Deploy drifted program |
| Provider Support | AWS only | Any provider |
| Setup Required | AWS CLI + credentials | Just Pulumi CLI |
| Complexity | High (provider APIs) | Low (Pulumi only) |
| Maintainability | Medium | High |
| Test Speed | Fast (direct API) | Moderate (full deploy) |

## Limitations

- **Slightly slower**: Full deployment vs direct API call
- **Requires valid provider**: Still need cloud credentials (but only for Pulumi)
- **State manipulation**: Relies on state export/import (generally safe)

## Future Extensions

This approach makes it easy to test drift with:
- Azure resources
- GCP resources
- Kubernetes manifests
- Multiple providers simultaneously
- Complex drift scenarios (property changes + deletions)
