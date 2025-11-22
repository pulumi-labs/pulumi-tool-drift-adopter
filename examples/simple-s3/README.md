# Simple S3 Example

A minimal Pulumi program that creates an S3 bucket without tags. This example is used for drift adoption testing and demonstration.

## Purpose

This example demonstrates the drift adoption workflow:
1. Deploy a clean stack with a bucket (no tags)
2. Create drift by manually adding tags via AWS CLI
3. Use `pulumi-drift-adopt` to update the code with the tags
4. Verify the drift is resolved

## Files

- `Pulumi.yaml` - Project configuration
- `package.json` - Node.js dependencies
- `tsconfig.json` - TypeScript configuration
- `index.ts` - Main program (creates S3 bucket)

## Usage

### Manual Testing

```bash
cd examples/simple-s3

# Install dependencies
npm install

# Create a new stack
pulumi stack init dev

# Deploy
pulumi up

# Get the bucket name
export BUCKET_NAME=$(pulumi stack output bucketName)

# Create drift by adding tags manually
aws s3api put-bucket-tagging \
  --bucket $BUCKET_NAME \
  --tagging 'TagSet=[{Key=Environment,Value=production},{Key=ManagedBy,Value=manual}]'

# Run drift adoption tool
pulumi-drift-adopt next

# The tool will tell you what to change in index.ts
# Update index.ts to add the tags shown in the output

# Run again to verify
pulumi-drift-adopt next
# Should return: {"status": "clean"}

# Cleanup
pulumi destroy
pulumi stack rm dev
```

### Automated Testing

This example is used by the E2E test in `test/e2e/drift_adoption_test.go`. The test:
1. Copies this example to a temp directory
2. Deploys the stack
3. Creates drift via AWS CLI
4. Invokes Claude to fix the drift
5. Verifies the code was updated correctly

## Expected Drift

When you manually add tags to the bucket, the tool should output:

```json
{
  "status": "changes_needed",
  "resources": [
    {
      "action": "update_code",
      "urn": "urn:pulumi:dev::simple-s3::aws:s3/bucket:Bucket::test-bucket",
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
        }
      ]
    }
  ]
}
```

## Fix

Update `index.ts` to add the tags:

```typescript
const bucket = new aws.s3.Bucket("test-bucket", {
    forceDestroy: true,
    tags: {
        Environment: "production",
        ManagedBy: "manual",
    },
});
```

Then run `pulumi-drift-adopt next` again to verify the drift is fixed.
