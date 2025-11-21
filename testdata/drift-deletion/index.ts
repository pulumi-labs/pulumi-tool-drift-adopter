import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

// S3 bucket that was manually deleted in AWS
const bucket = new aws.s3.Bucket("deleted-bucket", {
    bucket: "my-deleted-bucket-12345",
    tags: {
        Name: "deleted-bucket",
        Environment: "dev",
    },
});

// Another bucket that still exists (for context)
const activeBucket = new aws.s3.Bucket("active-bucket", {
    bucket: "my-active-bucket-12345",
    tags: {
        Name: "active-bucket",
        Environment: "dev",
    },
});

export const deletedBucketName = bucket.id;
export const activeBucketName = activeBucket.id;
