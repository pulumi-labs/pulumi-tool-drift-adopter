import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

// S3 bucket with immutable properties
// In this scenario, the bucket name was somehow changed (requires replacement)
const bucket = new aws.s3.Bucket("my-bucket", {
    bucket: "my-original-bucket-12345",
    acl: "private",
    tags: {
        Name: "my-bucket",
        Environment: "dev",
    },
});

export const bucketName = bucket.id;
export const bucketArn = bucket.arn;
