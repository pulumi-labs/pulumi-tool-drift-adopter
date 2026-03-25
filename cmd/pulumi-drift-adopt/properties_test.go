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
	"github.com/stretchr/testify/require"
)

func TestNextCommandActionMapping(t *testing.T) {
	tests := []struct {
		name           string
		operation      string
		expectedAction string
	}{
		{
			name:           "create -> delete_from_code",
			operation:      "create",
			expectedAction: "delete_from_code",
		},
		{
			name:           "delete -> add_to_code",
			operation:      "delete",
			expectedAction: "add_to_code",
		},
		{
			name:           "update -> update_code",
			operation:      "update",
			expectedAction: "update_code",
		},
		{
			name:           "replace -> update_code",
			operation:      "replace",
			expectedAction: "update_code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eventsContent := `{
				"steps": [{
					"op": "` + tt.operation + `",
					"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::test-bucket",
					"oldState": {
						"type": "aws:s3/bucket:Bucket",
						"outputs": {"bucket": "test-bucket", "tags": {"Env": "prod"}}
					},
					"newState": {
						"type": "aws:s3/bucket:Bucket",
						"outputs": {"bucket": "test-bucket", "tags": {"Env": "dev"}}
					},
					"detailedDiff": {"tags.Env": {"kind": "update"}}
				}]
			}`

			summary, full := runProcessTest(t, []byte(eventsContent))

			assert.Equal(t, "changes_needed", summary.Status)
			require.NotEmpty(t, full.Resources)
			assert.Equal(t, tt.expectedAction, full.Resources[0].Action)
		})
	}
}

// TestNextCommandPropertyChanges tests that property changes are correctly extracted
func TestNextCommandPropertyChanges(t *testing.T) {
	eventsContent := `{
		"steps": [{
			"op": "update",
			"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket",
			"oldState": {
				"type": "aws:s3/bucket:Bucket",
				"outputs": {
					"tags": {
						"Environment": "production",
						"Owner": "team-a"
					},
					"versioning": {
						"enabled": true
					}
				}
			},
			"newState": {
				"type": "aws:s3/bucket:Bucket",
				"outputs": {
					"tags": {
						"Environment": "dev",
						"Owner": "team-a"
					},
					"versioning": {
						"enabled": false
					}
				}
			},
			"detailedDiff": {
				"tags.Environment": {
					"kind": "update"
				},
				"versioning.enabled": {
					"kind": "update"
				}
			}
		}]
	}`

	summary, full := runProcessTest(t, []byte(eventsContent))

	assert.Equal(t, "changes_needed", summary.Status)
	require.Len(t, full.Resources, 1)

	resource := full.Resources[0]
	assert.Equal(t, "update_code", resource.Action)
	assert.Equal(t, "aws:s3/bucket:Bucket", resource.Type)
	assert.Equal(t, "my-bucket", resource.Name)

	// Verify properties
	assert.Len(t, resource.Properties, 2)

	var envProp, versioningProp *PropertyChange
	for i := range resource.Properties {
		if resource.Properties[i].Path == "tags.Environment" {
			envProp = &resource.Properties[i]
		}
		if resource.Properties[i].Path == "versioning.enabled" {
			versioningProp = &resource.Properties[i]
		}
	}

	require.NotNil(t, envProp, "Environment property not found")
	assert.Equal(t, "dev", envProp.CurrentValue)
	assert.Equal(t, "production", envProp.DesiredValue)

	require.NotNil(t, versioningProp, "Versioning property not found")
	assert.Equal(t, false, versioningProp.CurrentValue)
	assert.Equal(t, true, versioningProp.DesiredValue)
}

// TestNextCommandReplaceWithStandardJSON tests replace operations in standard JSON format
func TestNextCommandReplaceWithStandardJSON(t *testing.T) {
	eventsContent := `{
		"steps": [{
			"op": "replace",
			"urn": "urn:pulumi:dev::test::random:index/randomString:RandomString::my-string",
			"oldState": {
				"type": "random:index/randomString:RandomString",
				"inputs": {"length": 16, "special": true},
				"outputs": {"length": 16, "result": "abc123", "special": true}
			},
			"newState": {
				"type": "random:index/randomString:RandomString",
				"inputs": {"length": 32, "special": true},
				"outputs": {"length": 32, "special": true}
			},
			"detailedDiff": {
				"length": {
					"kind": "update-replace"
				}
			}
		}]
	}`

	summary, full := runProcessTest(t, []byte(eventsContent))

	assert.Equal(t, "changes_needed", summary.Status)
	require.Len(t, full.Resources, 1)

	resource := full.Resources[0]
	assert.Equal(t, "update_code", resource.Action)
	require.NotEmpty(t, resource.Properties, "Replace resource should have properties")

	var lengthProp *PropertyChange
	for i := range resource.Properties {
		if resource.Properties[i].Path == "length" {
			lengthProp = &resource.Properties[i]
		}
	}
	require.NotNil(t, lengthProp, "length property not found")
	assert.Equal(t, float64(32), lengthProp.CurrentValue)
	assert.Equal(t, float64(16), lengthProp.DesiredValue)
}

// TestNextCommandReplaceWithNullDetailedDiff tests that standard JSON replace ops with null detailedDiff
// still produce property changes by falling back to replaceReasons/diffReasons.
func TestNextCommandReplaceWithNullDetailedDiff(t *testing.T) {
	summary, full := runProcessTestFile(t, "testdata/standard_json_replace.json")

	assert.Equal(t, "changes_needed", summary.Status)
	require.Len(t, full.Resources, 3)

	var replaceRes, updateRes, createRes *ResourceChange
	for i := range full.Resources {
		switch full.Resources[i].Name {
		case "tls-key-0":
			replaceRes = &full.Resources[i]
		case "cmd-4":
			updateRes = &full.Resources[i]
		case "cmd-38":
			createRes = &full.Resources[i]
		}
	}

	// Replace resource (tls-key-0) should have properties from replaceReasons
	require.NotNil(t, replaceRes, "tls-key-0 not found")
	assert.Equal(t, "update_code", replaceRes.Action)
	assert.Equal(t, "tls:index/privateKey:PrivateKey", replaceRes.Type)
	require.NotEmpty(t, replaceRes.Properties, "Replace resource should have properties synthesized from replaceReasons")

	// Find algorithm and ecdsaCurve properties
	var algoProp, ecdsaProp *PropertyChange
	for i := range replaceRes.Properties {
		switch replaceRes.Properties[i].Path {
		case "algorithm":
			algoProp = &replaceRes.Properties[i]
		case "ecdsaCurve":
			ecdsaProp = &replaceRes.Properties[i]
		}
	}

	require.NotNil(t, algoProp, "algorithm property not found")
	// NewState.inputs has the current code value, OldState.inputs has the desired state value
	assert.Equal(t, "RSA", algoProp.CurrentValue)
	assert.Equal(t, "ECDSA", algoProp.DesiredValue)

	require.NotNil(t, ecdsaProp, "ecdsaCurve property not found")
	// ecdsaCurve is in OldState.inputs (desired) but not NewState.inputs (current)
	assert.Nil(t, ecdsaProp.CurrentValue)
	assert.Equal(t, "P256", ecdsaProp.DesiredValue)

	// Should NOT have noisy output-only diffs like id, publicKeyPem
	for _, prop := range replaceRes.Properties {
		assert.NotEqual(t, "id", prop.Path, "Output-only property 'id' should not be in properties")
		assert.NotEqual(t, "publicKeyPem", prop.Path, "Output-only property 'publicKeyPem' should not be in properties")
	}

	// Update resource (cmd-4) should still work with populated detailedDiff
	require.NotNil(t, updateRes, "cmd-4 not found")
	assert.Equal(t, "update_code", updateRes.Action)
	require.NotEmpty(t, updateRes.Properties, "Update resource should have properties from detailedDiff")

	// Create resource maps to delete_from_code
	require.NotNil(t, createRes, "cmd-38 not found")
	assert.Equal(t, "delete_from_code", createRes.Action)
}

// TestNextCommandReplaceInputDiffOnly tests that replace ops with null detailedDiff produce
// properties from replaceReasons only, not noisy output diffs from diffReasons.
func TestNextCommandReplaceInputDiffOnly(t *testing.T) {
	eventsContent := `{
		"steps": [{
			"op": "replace",
			"urn": "urn:pulumi:dev::test::tls:index/privateKey:PrivateKey::my-key",
			"oldState": {
				"type": "tls:index/privateKey:PrivateKey",
				"inputs": {"algorithm": "ECDSA", "ecdsaCurve": "P256"},
				"outputs": {"algorithm": "ECDSA", "ecdsaCurve": "P256", "id": "abc", "publicKeyPem": "..."}
			},
			"newState": {
				"type": "tls:index/privateKey:PrivateKey",
				"inputs": {"algorithm": "RSA", "rsaBits": 2048},
				"outputs": {"algorithm": "RSA", "rsaBits": 2048, "id": "def", "publicKeyPem": "..."}
			},
			"diffReasons": ["algorithm", "ecdsaCurve", "id", "publicKeyPem"],
			"replaceReasons": ["algorithm", "ecdsaCurve"],
			"detailedDiff": null
		}]
	}`

	summary, full := runProcessTest(t, []byte(eventsContent))

	assert.Equal(t, "changes_needed", summary.Status)
	require.Len(t, full.Resources, 1)

	resource := full.Resources[0]
	assert.Equal(t, "update_code", resource.Action)

	propertyPaths := make(map[string]bool)
	for _, prop := range resource.Properties {
		propertyPaths[prop.Path] = true
	}

	assert.True(t, propertyPaths["algorithm"], "algorithm should be in properties")
	assert.True(t, propertyPaths["ecdsaCurve"], "ecdsaCurve should be in properties")
	assert.False(t, propertyPaths["id"], "id (output-only) should NOT be in properties")
	assert.False(t, propertyPaths["publicKeyPem"], "publicKeyPem (output-only) should NOT be in properties")

	for _, prop := range resource.Properties {
		if prop.Path == "algorithm" {
			assert.Equal(t, "RSA", prop.CurrentValue)
			assert.Equal(t, "ECDSA", prop.DesiredValue)
		}
		if prop.Path == "ecdsaCurve" {
			assert.Nil(t, prop.CurrentValue)
			assert.Equal(t, "P256", prop.DesiredValue)
		}
	}
}

// TestNextCommandDeletePrefersInputs verifies that add_to_code actions use Inputs (not Outputs)
func TestNextCommandDeletePrefersInputs(t *testing.T) {
	eventsContent := `{
		"steps": [{
			"op": "delete",
			"urn": "urn:pulumi:dev::test::tls:index/privateKey:PrivateKey::my-key",
			"oldState": {
				"type": "tls:index/privateKey:PrivateKey",
				"inputs": {
					"algorithm": "RSA",
					"rsaBits": 4096
				},
				"outputs": {
					"algorithm": "RSA",
					"rsaBits": 4096,
					"privateKeyPem": "-----BEGIN RSA PRIVATE KEY-----\nMIIE...",
					"publicKeyPem": "-----BEGIN PUBLIC KEY-----\nMIIB...",
					"publicKeyOpenssh": "ssh-rsa AAAA...",
					"id": "abc123"
				}
			},
			"detailedDiff": {}
		}]
	}`

	summary, full := runProcessTest(t, []byte(eventsContent))

	assert.Equal(t, "changes_needed", summary.Status)
	require.Len(t, full.Resources, 1)

	res := full.Resources[0]
	assert.Equal(t, "add_to_code", res.Action)

	assert.NotNil(t, res.InputProperties, "should have InputProperties map")
	assert.Nil(t, res.Properties, "should NOT have Properties array for add_to_code")
	assert.Equal(t, "RSA", res.InputProperties["algorithm"])
	assert.Equal(t, float64(4096), res.InputProperties["rsaBits"])
	assert.Nil(t, res.InputProperties["privateKeyPem"])
	assert.Nil(t, res.InputProperties["publicKeyPem"])
	assert.Nil(t, res.InputProperties["id"])
	assert.Len(t, res.InputProperties, 2)
}

// TestNextCommandDeleteFallsBackToOutputs verifies fallback when Inputs is empty
func TestNextCommandDeleteFallsBackToOutputs(t *testing.T) {
	eventsContent := `{
		"steps": [{
			"op": "delete",
			"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::legacy-bucket",
			"oldState": {
				"type": "aws:s3/bucket:Bucket",
				"outputs": {
					"bucket": "legacy-bucket",
					"arn": "arn:aws:s3:::legacy-bucket"
				}
			},
			"detailedDiff": {}
		}]
	}`

	summary, full := runProcessTest(t, []byte(eventsContent))

	assert.Equal(t, "changes_needed", summary.Status)
	require.Len(t, full.Resources, 1)

	res := full.Resources[0]
	assert.Equal(t, "add_to_code", res.Action)
	assert.NotNil(t, res.InputProperties, "should have InputProperties map from outputs fallback")
	assert.Nil(t, res.Properties, "should NOT have Properties array for add_to_code")
	assert.Len(t, res.InputProperties, 2, "should have 2 output properties as fallback")
}

// TestNextCommandInputPropertiesFormat verifies add_to_code uses InputProperties (flat map)
// while update_code uses Properties (array)
func TestNextCommandInputPropertiesFormat(t *testing.T) {
	eventsContent := `{
		"steps": [
			{
				"op": "delete",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::missing-bucket",
				"oldState": {
					"type": "aws:s3/bucket:Bucket",
					"inputs": {
						"bucket": "missing-bucket",
						"tags": {"Environment": "production"}
					}
				},
				"detailedDiff": {}
			},
			{
				"op": "update",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::existing-bucket",
				"oldState": {
					"type": "aws:s3/bucket:Bucket",
					"outputs": {"tags": {"Environment": "production"}}
				},
				"newState": {
					"type": "aws:s3/bucket:Bucket",
					"outputs": {"tags": {"Environment": "dev"}}
				},
				"detailedDiff": {"tags.Environment": {"kind": "update"}}
			}
		]
	}`

	summary, full := runProcessTest(t, []byte(eventsContent))

	assert.Equal(t, "changes_needed", summary.Status)
	require.Len(t, full.Resources, 2)

	var addResource, updateResource *ResourceChange
	for i := range full.Resources {
		switch full.Resources[i].Action {
		case "add_to_code":
			addResource = &full.Resources[i]
		case "update_code":
			updateResource = &full.Resources[i]
		}
	}

	require.NotNil(t, addResource)
	assert.NotNil(t, addResource.InputProperties, "add_to_code should have InputProperties")
	assert.Nil(t, addResource.Properties, "add_to_code should NOT have Properties")
	assert.Equal(t, "missing-bucket", addResource.InputProperties["bucket"])
	tags, ok := addResource.InputProperties["tags"].(map[string]interface{})
	require.True(t, ok, "tags should be a nested map")
	assert.Equal(t, "production", tags["Environment"])

	require.NotNil(t, updateResource)
	assert.NotNil(t, updateResource.Properties, "update_code should have Properties")
	assert.Nil(t, updateResource.InputProperties, "update_code should NOT have InputProperties")
	assert.Len(t, updateResource.Properties, 1)
	assert.Equal(t, "tags.Environment", updateResource.Properties[0].Path)
}

func TestExtractPropertyChangesReplaceKinds(t *testing.T) {
	t.Run("delete-replace produces desiredValue with nil currentValue", func(t *testing.T) {
		eventsContent := `{
			"steps": [{
				"op": "replace",
				"urn": "urn:pulumi:dev::test::command:local:Command::deploy-cmd",
				"oldState": {
					"type": "command:local:Command",
					"inputs": {"triggers": ["secret-val"]}
				},
				"newState": {
					"type": "command:local:Command",
					"inputs": {}
				},
				"detailedDiff": {
					"triggers": {"kind": "delete-replace", "inputDiff": true}
				}
			}]
		}`
		_, full := runProcessTest(t, []byte(eventsContent))
		require.Len(t, full.Resources, 1)
		require.Len(t, full.Resources[0].Properties, 1)
		prop := full.Resources[0].Properties[0]
		assert.Equal(t, "triggers", prop.Path)
		assert.Nil(t, prop.CurrentValue)
		assert.NotNil(t, prop.DesiredValue)
	})

	t.Run("add-replace produces currentValue with nil desiredValue", func(t *testing.T) {
		eventsContent := `{
			"steps": [{
				"op": "replace",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket",
				"oldState": {
					"type": "aws:s3/bucket:Bucket",
					"inputs": {}
				},
				"newState": {
					"type": "aws:s3/bucket:Bucket",
					"inputs": {"forceDestroy": true}
				},
				"detailedDiff": {
					"forceDestroy": {"kind": "add-replace", "inputDiff": true}
				}
			}]
		}`

		_, full := runProcessTest(t, []byte(eventsContent))
		require.Len(t, full.Resources, 1)
		require.Len(t, full.Resources[0].Properties, 1)
		prop := full.Resources[0].Properties[0]
		assert.Equal(t, "forceDestroy", prop.Path)
		assert.NotNil(t, prop.CurrentValue)
		assert.Nil(t, prop.DesiredValue)
	})

	t.Run("null/null properties are skipped", func(t *testing.T) {
		eventsContent := `{
			"steps": [{
				"op": "replace",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket",
				"oldState": {
					"type": "aws:s3/bucket:Bucket",
					"inputs": {"bucket": "old-name"}
				},
				"newState": {
					"type": "aws:s3/bucket:Bucket",
					"inputs": {"bucket": "new-name"}
				},
				"detailedDiff": {
					"computedField": {"kind": "update", "inputDiff": false},
					"bucket": {"kind": "update", "inputDiff": true}
				}
			}]
		}`

		_, full := runProcessTest(t, []byte(eventsContent))
		require.Len(t, full.Resources, 1)
		// computedField has nil/nil (not in inputs/outputs) and should be skipped
		require.Len(t, full.Resources[0].Properties, 1)
		assert.Equal(t, "bucket", full.Resources[0].Properties[0].Path)
	})
}
