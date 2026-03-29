// Copyright 2026, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractProviderName(t *testing.T) {
	tests := []struct {
		name         string
		resourceType string
		expected     string
	}{
		{"aws provider", "aws:s3/bucket:Bucket", "aws"},
		{"random provider", "random:index/randomString:RandomString", "random"},
		{"tls provider", "tls:index/privateKey:PrivateKey", "tls"},
		{"command provider", "command:local:Command", "command"},
		{"azure-native provider", "azure-native:compute:VirtualMachine", "azure-native"},
		{"kubernetes provider", "kubernetes:core/v1:ConfigMap", "kubernetes"},
		{"no colon — empty", "noColon", ""},
		{"leading colon — empty", ":bad/type:Bad", ""},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractProviderName(tt.resourceType))
		})
	}
}

func TestBuildInputPropertySet(t *testing.T) {
	t.Run("converts to lookup map", func(t *testing.T) {
		input := map[string][]string{
			"aws:s3/bucket:Bucket":                {"bucket", "tags", "versioning"},
			"aws:ec2/securityGroup:SecurityGroup": {"ingress", "egress", "description"},
		}
		result := buildInputPropertySet(input)

		assert.True(t, result["aws:s3/bucket:Bucket"]["bucket"])
		assert.True(t, result["aws:s3/bucket:Bucket"]["tags"])
		assert.True(t, result["aws:s3/bucket:Bucket"]["versioning"])
		assert.False(t, result["aws:s3/bucket:Bucket"]["arn"]) // not in set

		assert.True(t, result["aws:ec2/securityGroup:SecurityGroup"]["ingress"])
		assert.False(t, result["aws:ec2/securityGroup:SecurityGroup"]["vpcId"]) // not in this set
	})

	t.Run("nil input returns empty map", func(t *testing.T) {
		result := buildInputPropertySet(nil)
		assert.Empty(t, result)
	})

	t.Run("empty input returns empty map", func(t *testing.T) {
		result := buildInputPropertySet(map[string][]string{})
		assert.Empty(t, result)
	})

	t.Run("unknown resource type returns nil set", func(t *testing.T) {
		result := buildInputPropertySet(map[string][]string{
			"aws:s3/bucket:Bucket": {"bucket"},
		})
		assert.Nil(t, result["aws:ec2/instance:Instance"])
	})
}

// TestBuildInputPropertySet_RealData loads the actual aws_input_properties.json
// testdata and verifies the set is built correctly for real resource types.
func TestBuildInputPropertySet_RealData(t *testing.T) {
	inputProps := loadInputProperties(t)
	result := buildInputPropertySet(inputProps)

	// S3 Bucket: tags should be an input, tagsAll should NOT be present
	bucketSet := result["aws:s3/bucket:Bucket"]
	assert.NotNil(t, bucketSet, "S3 Bucket should have input properties")
	assert.True(t, bucketSet["tags"], "tags should be an input property")
	assert.True(t, bucketSet["bucket"], "bucket should be an input property")
	assert.True(t, bucketSet["versioning"], "versioning should be an input property")
	assert.False(t, bucketSet["tagsAll"], "tagsAll should NOT be in schema inputProperties")
	assert.False(t, bucketSet["arn"], "arn should NOT be in schema inputProperties")
	assert.False(t, bucketSet["id"], "id should NOT be in schema inputProperties")

	// Lambda Function: runtime, handler, role are inputs; arn, lastModified are not
	lambdaSet := result["aws:lambda/function:Function"]
	assert.NotNil(t, lambdaSet, "Lambda Function should have input properties")
	assert.True(t, lambdaSet["runtime"])
	assert.True(t, lambdaSet["handler"])
	assert.True(t, lambdaSet["role"])
	assert.True(t, lambdaSet["environment"])
	assert.False(t, lambdaSet["arn"])
	assert.False(t, lambdaSet["lastModified"])

	// SSM Parameter: value is an input, version is not
	ssmSet := result["aws:ssm/parameter:Parameter"]
	assert.NotNil(t, ssmSet)
	assert.True(t, ssmSet["value"], "value should be an input property")
	assert.True(t, ssmSet["name"])
	assert.True(t, ssmSet["type"])
	assert.False(t, ssmSet["version"], "version should NOT be in schema inputProperties")
}
