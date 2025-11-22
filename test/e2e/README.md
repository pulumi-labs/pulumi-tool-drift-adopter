# E2E Test: Drift Adoption with Claude

This test validates the complete drift adoption workflow using:
- **Pulumi** to deploy infrastructure
- **AWS CLI** to create drift
- **Claude SDK** to automatically fix the drift using the `drift-adopt` skill

## Test Flow

1. **Setup**: Uses pulumitest's `CopyToTempDir()` to copy the `examples/simple-s3` Pulumi program to a temp directory
2. **Deploy Infrastructure**: Deploys the stack (S3 bucket without tags)
3. **Create Drift**: Uses AWS CLI to add tags to the bucket manually
4. **Invoke Claude**: Calls Claude SDK with the drift-adopt skill to fix the code
   - Claude runs `pulumi-drift-adopt next` which automatically refreshes state
5. **Verify**: Confirms the code was updated and preview shows no drift

## Prerequisites

### Required Tools
- Go 1.21+
- Pulumi CLI
- AWS CLI configured with credentials
- Node.js and npm (for the TypeScript Pulumi program)

### Required Environment Variables

```bash
export ANTHROPIC_API_KEY="your-api-key"
export AWS_REGION="us-west-2"  # Optional, defaults to us-west-2
export AWS_ACCESS_KEY_ID="your-access-key"
export AWS_SECRET_ACCESS_KEY="your-secret-key"
```

### AWS Permissions

The test needs permissions to:
- Create/delete S3 buckets
- Add/remove bucket tags
- Read bucket configuration

## Running the Test

```bash
# From the repository root
go test -v -tags=e2e -timeout=30m ./test/e2e/

# Or with specific test
go test -v -tags=e2e -timeout=30m -run TestDriftAdoptionWorkflow ./test/e2e/
```

**Note**: This test:
- Takes several minutes to complete (deploys real infrastructure)
- Incurs small AWS costs (S3 bucket is created and destroyed)
- Consumes Claude API tokens (typically ~10-20K tokens)

## What the Test Validates

✅ **Claude can read and understand the drift-adopt skill**
✅ **Claude can run the `pulumi-drift-adopt next` command**
✅ **Claude can parse the JSON output correctly**
✅ **Claude can identify which files to modify**
✅ **Claude can update TypeScript code with the correct property values**
✅ **Claude iterates until drift is fully resolved**
✅ **Final preview shows no remaining drift**

## Test Output Example

```
=== RUN   TestDriftAdoptionWorkflow
    drift_adoption_test.go:45: Deploying initial stack...
    drift_adoption_test.go:55: Deployed bucket: drift-test-bucket-dev-a1b2c3
    drift_adoption_test.go:58: Running pulumi refresh...
    drift_adoption_test.go:61: Creating drift by adding tags to bucket...
    drift_adoption_test.go:66: Running pulumi refresh to capture drift in state...
    drift_adoption_test.go:69: Verifying drift exists...
    drift_adoption_test.go:74: Invoking Claude to fix drift...
    drift_adoption_test.go:80: Verifying code was updated...
    drift_adoption_test.go:88: Running preview to verify drift is fixed...
    drift_adoption_test.go:97: ✅ Drift adoption complete!
--- PASS: TestDriftAdoptionWorkflow (245.32s)
PASS
```

## Troubleshooting

### Test hangs during Claude invocation

- Check that `ANTHROPIC_API_KEY` is set correctly
- Verify the drift-adopt skill file exists at `skills/drift-adopt.md`
- Check Claude API status at status.anthropic.com

### Preview shows unexpected changes

- Ensure `pulumi refresh` ran successfully after creating drift
- Check AWS CLI command output for errors
- Verify the bucket exists and tags were applied

### Claude makes incorrect changes

- Review the drift-adopt skill instructions
- Check the tool output formatting (JSON structure)
- Look at test logs to see what Claude attempted

### AWS permissions errors

- Verify AWS credentials are configured
- Check IAM permissions for S3 operations
- Ensure the AWS region is accessible

## Skill File

The test uses the drift-adopt skill located at:
```
skills/drift-adopt.md
```

This skill provides Claude with:
- Overview of the drift adoption workflow
- Step-by-step instructions for using `pulumi-drift-adopt next`
- How to interpret JSON output
- How to update code for each action type
- Troubleshooting tips

## Extending the Test

To test more complex scenarios:

1. **Add more resources**: Modify `createPulumiProgram()` to include VPCs, subnets, etc.
2. **Test different drift types**:
   - Resource deletion (remove resource from code)
   - Resource creation (add resource manually)
   - Multiple property changes
3. **Test error recovery**: Introduce syntax errors and verify Claude fixes them
4. **Test dependencies**: Create drift on resources with dependencies

## CI/CD Integration

To run this test in CI:

```yaml
- name: Run E2E Tests
  env:
    ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
    AWS_ACCESS_KEY_ID: ${{ secrets.AWS_ACCESS_KEY_ID }}
    AWS_SECRET_ACCESS_KEY: ${{ secrets.AWS_SECRET_ACCESS_KEY }}
  run: go test -v -tags=e2e -timeout=30m ./test/e2e/
```

**Important**: This test should run in a separate CI job from unit tests since it:
- Takes longer to run
- Requires cloud credentials
- Costs money (AWS + Claude API)
