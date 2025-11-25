import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

// Drifted version: bucket-b has been removed to simulate resource deletion
const bucketA = new aws.s3.Bucket("bucket-a", {
    forceDestroy: true,
});

const bucketC = new aws.s3.Bucket("bucket-c", {
    forceDestroy: true,
});

export const bucketNameA = bucketA.id;
export const bucketNameC = bucketC.id;
