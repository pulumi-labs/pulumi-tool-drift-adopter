# Drift Deletion Test Fixture

## Scenario
Resource deleted in cloud but still defined in IaC code.

## Drift Description
- Resource: `aws:s3/bucket:Bucket` named "deleted-bucket"
- Status: Deleted in AWS (manually or by another process)
- Code: Still defined in `index.ts`

## Expected Behavior
1. Tool detects deletion via preview (diffType: "delete")
2. Creates single chunk for the deleted resource
3. Agent generates code to remove the resource definition
4. Tool validates TypeScript compilation
5. Tool verifies preview shows resource will be removed
6. Chunk marked as completed

## Notes
- Deletion drift typically requires removing code rather than updating it
- Agent must also remove related exports and dependencies
- This tests a different type of code change than property updates

## Files
- `index.ts` - Pulumi program with deleted resource still defined
- `Pulumi.yaml` - Project configuration
- `preview.json` - Simulated preview showing deletion
- `expected-plan.json` - Expected plan with delete diffType
