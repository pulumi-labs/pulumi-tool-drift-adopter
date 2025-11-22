# Drift Replacement Test Fixture

## Scenario
Resource with immutable property changed, requiring replacement.

## Drift Description
- Resource: `aws:s3/bucket:Bucket` named "my-bucket"
- Issue: Bucket name changed from "my-original-bucket-12345" to "my-renamed-bucket-12345"
- Impact: Bucket name is immutable, requires destroy + recreate (replacement)

## Expected Behavior
1. Tool detects replacement drift via preview (diffType: "replace")
2. Creates single step for the resource
3. Agent generates code to update the bucket name
4. Tool validates TypeScript compilation
5. Tool verifies preview matches expected diff
6. Step marked as completed

## Notes
- Replacement drift is more complex than simple updates
- May have downstream impacts on dependent resources
- Agent needs to understand the implications of replacement
- In production, this might require manual review before applying

## Files
- `index.ts` - Pulumi program with bucket
- `Pulumi.yaml` - Project configuration
- `preview.json` - Simulated preview showing replacement
- `expected-plan.json` - Expected plan with replace diffType
