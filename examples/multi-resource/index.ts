import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

// Create multiple S3 buckets for testing resource deletion drift
// This example is used for drift adoption testing:
// 1. Deploy this stack with multiple buckets
// 2. Manually delete one bucket via AWS CLI (creates drift)
// 3. Run pulumi-drift-adopt to update code and remove the deleted bucket

const bucketA = new aws.s3.Bucket("bucket-a", {
    forceDestroy: true, // Allow cleanup in tests
});

const bucketB = new aws.s3.Bucket("bucket-b", {
    forceDestroy: true,
});

const bucketC = new aws.s3.Bucket("bucket-c", {
    forceDestroy: true,
});

// Export all bucket names for testing
export const bucketNameA = bucketA.id;
export const bucketNameB = bucketB.id;
export const bucketNameC = bucketC.id;
