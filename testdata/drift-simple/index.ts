import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

// Simple S3 bucket with tags
const bucket = new aws.s3.Bucket("my-bucket", {
    bucket: "my-example-bucket-12345",
    tags: {
        Environment: "dev",
        Team: "platform",
    },
});

export const bucketName = bucket.id;
