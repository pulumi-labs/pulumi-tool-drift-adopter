import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

// Create an S3 bucket without tags
// This example is used for drift adoption testing:
// 1. Deploy this stack
// 2. Manually add tags via AWS CLI (creates drift)
// 3. Run pulumi-drift-adopt to update this code with the tags
const bucket = new aws.s3.Bucket("test-bucket", {
    forceDestroy: true, // Allow cleanup in tests
});

// Export the bucket name for testing
export const bucketName = bucket.id;
