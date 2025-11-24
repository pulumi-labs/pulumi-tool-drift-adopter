# Multi-Resource Example

This example creates multiple S3 buckets to test resource deletion drift adoption.

## Test Scenario

1. Deploy stack with 3 S3 buckets (bucket-a, bucket-b, bucket-c)
2. Manually delete bucket-b via AWS CLI to create drift
3. Run `pulumi-drift-adopt` to detect the deletion
4. Claude should update the code to remove the bucket-b resource and its export

## Expected Behavior

After drift adoption:
- `bucketB` resource declaration should be removed
- `bucketNameB` export should be removed
- `bucketA` and `bucketC` should remain unchanged
- No drift should remain in the stack
