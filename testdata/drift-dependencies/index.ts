import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

// VPC (leaf node - no dependencies)
const vpc = new aws.ec2.Vpc("main-vpc", {
    cidrBlock: "10.0.0.0/16",
    tags: {
        Name: "main-vpc",
        Environment: "dev",
    },
});

// Subnet (depends on VPC)
const subnet = new aws.ec2.Subnet("main-subnet", {
    vpcId: vpc.id,
    cidrBlock: "10.0.1.0/24",
    availabilityZone: "us-west-2a",
    tags: {
        Name: "main-subnet",
        Environment: "dev",
    },
});

// Security Group (depends on VPC)
const sg = new aws.ec2.SecurityGroup("main-sg", {
    vpcId: vpc.id,
    description: "Main security group",
    ingress: [{
        protocol: "tcp",
        fromPort: 80,
        toPort: 80,
        cidrBlocks: ["0.0.0.0/0"],
    }],
    tags: {
        Name: "main-sg",
        Environment: "dev",
    },
});

export const vpcId = vpc.id;
export const subnetId = subnet.id;
export const securityGroupId = sg.id;
