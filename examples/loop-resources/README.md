# Loop-Resources Example

This example creates S3 buckets in a loop from a list to test resource deletion drift adoption.

## Test Scenario

1. Deploy stack with 3 S3 buckets created from array: ["logs-bucket", "data-bucket", "backup-bucket"]
2. Manually delete "data-bucket" via AWS CLI to create drift
3. Run `pulumi-drift-adopt` to detect the deletion
4. Claude should update the code to remove "data-bucket" from the `bucketNames` array

## Expected Behavior

After drift adoption:
- `bucketNames` array should be updated to: ["logs-bucket", "backup-bucket"]
- "data-bucket" should be removed from the array
- Loop logic should remain intact
- `bucketCount` export should be updated to 2
- No drift should remain in the stack

## Key Challenge

This tests Claude's ability to:
- Understand loop-based resource creation patterns
- Identify that a deleted resource was part of a loop
- Update the data structure (array) that drives the loop
- Maintain the loop structure while removing one element
