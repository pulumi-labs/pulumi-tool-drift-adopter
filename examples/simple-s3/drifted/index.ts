import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

// Drifted version: This has tags added to simulate external changes to infrastructure
const bucket = new aws.s3.Bucket("test-bucket", {
    forceDestroy: true,
    tags: {
        Environment: "production",
        ManagedBy: "manual",
    },
});

export const bucketName = bucket.id;
