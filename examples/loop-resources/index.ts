import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

// Create S3 buckets in a loop for testing resource deletion drift
// This example is used for drift adoption testing:
// 1. Deploy this stack with buckets created from a list
// 2. Manually delete one bucket (e.g., "data-bucket") via AWS CLI
// 3. Run pulumi-drift-adopt to update code and remove the deleted bucket from the list

const bucketNames = ["logs-bucket", "data-bucket", "backup-bucket"];

const buckets: { [key: string]: aws.s3.Bucket } = {};
const bucketIds: { [key: string]: pulumi.Output<string> } = {};

for (const name of bucketNames) {
    buckets[name] = new aws.s3.Bucket(name, {
        forceDestroy: true, // Allow cleanup in tests
    });
    bucketIds[name] = buckets[name].id;
}

// Export individual bucket IDs for testing
export const logsBucketId = buckets["logs-bucket"].id;
export const dataBucketId = buckets["data-bucket"].id;
export const backupBucketId = buckets["backup-bucket"].id;
export const bucketCount = bucketNames.length;
