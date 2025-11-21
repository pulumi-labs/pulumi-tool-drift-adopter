# Drift Simple Test Fixture

## Scenario
Single AWS S3 bucket with property drift in tags.

## Drift Description
- Resource: `aws:s3/bucket:Bucket` named "my-bucket"
- Changes:
  - `tags.Environment` changed from "dev" to "production" (manual change in AWS)
  - `tags.Owner` added with value "john@example.com" (manual addition in AWS)

## Expected Behavior
1. Tool detects drift via preview
2. Creates single chunk (no dependencies)
3. Agent generates code to update tags
4. Tool validates TypeScript compilation
5. Tool verifies preview matches expected diff
6. Chunk marked as completed

## Files
- `index.ts` - Pulumi program (current state in code)
- `Pulumi.yaml` - Project configuration
- `preview.json` - Simulated preview output showing drift
- `expected-plan.json` - Expected drift adoption plan
