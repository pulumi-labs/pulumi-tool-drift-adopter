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
	"encoding/json"
	"os"
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
			expectedAction: ActionDeleteFromCode,
		},
		{
			name:           "delete -> add_to_code",
			operation:      "delete",
			expectedAction: ActionAddToCode,
		},
		{
			name:           "update -> update_code",
			operation:      "update",
			expectedAction: ActionUpdateCode,
		},
		{
			name:           "replace -> update_code",
			operation:      "replace",
			expectedAction: ActionUpdateCode,
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

			assert.Equal(t, StatusChangesNeeded, summary.Status)
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
				"inputs": {
					"tags": {
						"Environment": "production",
						"Owner": "team-a"
					},
					"versioning": {
						"enabled": true
					}
				},
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
				"inputs": {
					"tags": {
						"Environment": "dev",
						"Owner": "team-a"
					},
					"versioning": {
						"enabled": false
					}
				},
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

	assert.Equal(t, StatusChangesNeeded, summary.Status)
	require.Len(t, full.Resources, 1)

	resource := full.Resources[0]
	assert.Equal(t, ActionUpdateCode, resource.Action)
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

	assert.Equal(t, StatusChangesNeeded, summary.Status)
	require.Len(t, full.Resources, 1)

	resource := full.Resources[0]
	assert.Equal(t, ActionUpdateCode, resource.Action)
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

	assert.Equal(t, StatusChangesNeeded, summary.Status)
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
	assert.Equal(t, ActionUpdateCode, replaceRes.Action)
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
	assert.Equal(t, ActionUpdateCode, updateRes.Action)
	require.NotEmpty(t, updateRes.Properties, "Update resource should have properties from detailedDiff")

	// Create resource maps to delete_from_code
	require.NotNil(t, createRes, "cmd-38 not found")
	assert.Equal(t, ActionDeleteFromCode, createRes.Action)
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

	assert.Equal(t, StatusChangesNeeded, summary.Status)
	require.Len(t, full.Resources, 1)

	resource := full.Resources[0]
	assert.Equal(t, ActionUpdateCode, resource.Action)

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

	assert.Equal(t, StatusChangesNeeded, summary.Status)
	require.Len(t, full.Resources, 1)

	res := full.Resources[0]
	assert.Equal(t, ActionAddToCode, res.Action)

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

	assert.Equal(t, StatusChangesNeeded, summary.Status)
	require.Len(t, full.Resources, 1)

	res := full.Resources[0]
	assert.Equal(t, ActionAddToCode, res.Action)
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

	assert.Equal(t, StatusChangesNeeded, summary.Status)
	require.Len(t, full.Resources, 2)

	var addResource, updateResource *ResourceChange
	for i := range full.Resources {
		switch full.Resources[i].Action {
		case ActionAddToCode:
			addResource = &full.Resources[i]
		case ActionUpdateCode:
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

func TestGetNestedPropertyArrayIndex(t *testing.T) {
	props := map[string]interface{}{
		"triggers": []interface{}{"val-0", "val-1", "val-2"},
		"environment": map[string]interface{}{
			"APP_NAME": "cache",
		},
		"nested": map[string]interface{}{
			"items": []interface{}{
				map[string]interface{}{"name": "first"},
				map[string]interface{}{"name": "second"},
			},
		},
	}

	t.Run("array index at top level", func(t *testing.T) {
		assert.Equal(t, "val-0", getNestedProperty(props, "triggers[0]"))
		assert.Equal(t, "val-1", getNestedProperty(props, "triggers[1]"))
		assert.Equal(t, "val-2", getNestedProperty(props, "triggers[2]"))
	})

	t.Run("array index out of bounds", func(t *testing.T) {
		assert.Nil(t, getNestedProperty(props, "triggers[3]"))
		assert.Nil(t, getNestedProperty(props, "triggers[-1]"))
	})

	t.Run("array index on non-array", func(t *testing.T) {
		assert.Nil(t, getNestedProperty(props, "environment[0]"))
	})

	t.Run("array index on missing key", func(t *testing.T) {
		assert.Nil(t, getNestedProperty(props, "missing[0]"))
	})

	t.Run("nested array with dot path", func(t *testing.T) {
		result := getNestedProperty(props, "nested.items[0]")
		m, ok := result.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "first", m["name"])
	})

	t.Run("dot path still works", func(t *testing.T) {
		assert.Equal(t, "cache", getNestedProperty(props, "environment.APP_NAME"))
	})
}

func TestCommandWithTriggersNotSkipped(t *testing.T) {
	// Regression test: Command resources with array-indexed detailedDiff paths
	// (e.g., triggers[0]) were being skipped because getNestedProperty couldn't
	// resolve array indices, causing both currentValue and desiredValue to be nil.
	summary, full := runProcessTestFile(t, "testdata/command_with_triggers.json")

	assert.Equal(t, StatusChangesNeeded, summary.Status)

	// All 3 resources should be actionable (not skipped)
	require.Len(t, full.Resources, 3)
	assert.Empty(t, full.Skipped, "no resources should be skipped")

	// Find cache-cmd (the replace with triggers[0] diff)
	var cacheCmd *ResourceChange
	for i := range full.Resources {
		if full.Resources[i].Name == "cache-cmd" {
			cacheCmd = &full.Resources[i]
		}
	}
	require.NotNil(t, cacheCmd, "cache-cmd should be in resources, not skipped")
	assert.Equal(t, ActionUpdateCode, cacheCmd.Action)

	// Should have properties for both environment.DRIFT and triggers[0]
	paths := make(map[string]bool)
	for _, p := range cacheCmd.Properties {
		paths[p.Path] = true
	}
	assert.True(t, paths["environment.DRIFT"], "should have environment.DRIFT property")
	assert.True(t, paths["triggers[0]"], "should have triggers[0] property")

	// Verify triggers[0] values
	for _, p := range cacheCmd.Properties {
		if p.Path == "triggers[0]" {
			assert.Equal(t, "new-computed-val", p.CurrentValue)
			assert.Equal(t, "old-secret-val", p.DesiredValue)
		}
	}
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

// =============================================================================
// Bridged-provider tests using real AWS preview data + state exports
// =============================================================================

// runAWSProcessTest loads a preview JSON and state export, builds a dependency map
// from the state, and processes the preview through the full pipeline — matching
// the real tool behavior (parsePreviewOutput → parseStateExport → mergeStateLookupAndBuildDepMap → processNext).
func runAWSProcessTest(t *testing.T, previewFile, stateFile string) (NextSummaryOutput, NextOutput) {
	t.Helper()
	previewData, err := os.ReadFile(previewFile)
	require.NoError(t, err)

	stateData, err := os.ReadFile(stateFile)
	require.NoError(t, err)

	steps, parseErrors, err := parsePreviewOutput(previewData)
	require.NoError(t, err)
	assert.Equal(t, 0, parseErrors, "real AWS preview JSON should parse without errors")

	stateLookup, err := parseStateExport(stateData)
	require.NoError(t, err)

	depMap := mergeStateLookupAndBuildDepMap(steps, stateLookup)

	meta := &ResourceMetadata{
		Dependencies: depMap,
		StateLookup:  stateLookup,
	}
	return runProcessTestWithOptions(t, previewData, meta, nil, "")
}

// runAWSProcessTestWithSchema is like runAWSProcessTest but also loads schema-derived
// input properties from aws_input_properties.json and includes them in the metadata.
// Use this for tests that verify computed-property filtering.
func runAWSProcessTestWithSchema(t *testing.T, previewFile, stateFile string) (NextSummaryOutput, NextOutput) {
	t.Helper()
	previewData, err := os.ReadFile(previewFile)
	require.NoError(t, err)

	stateData, err := os.ReadFile(stateFile)
	require.NoError(t, err)

	steps, parseErrors, err := parsePreviewOutput(previewData)
	require.NoError(t, err)
	assert.Equal(t, 0, parseErrors, "real AWS preview JSON should parse without errors")

	stateLookup, err := parseStateExport(stateData)
	require.NoError(t, err)

	depMap := mergeStateLookupAndBuildDepMap(steps, stateLookup)

	inputPropsData, err := os.ReadFile("testdata/aws_input_properties.json")
	require.NoError(t, err)
	var inputProps map[string][]string
	require.NoError(t, json.Unmarshal(inputPropsData, &inputProps))

	meta := &ResourceMetadata{
		Dependencies:    depMap,
		InputProperties: inputProps,
		StateLookup:     stateLookup,
	}
	return runProcessTestWithOptions(t, previewData, meta, nil, "")
}

// TestAWSTagsAndSets_S3BucketProperties verifies that property changes for S3 bucket
// tag updates are correctly extracted from real AWS bridged-provider preview data.
// All DetailedDiff entries have inputDiff=false (bridged-provider behavior).
func TestAWSTagsAndSets_S3BucketProperties(t *testing.T) {
	_, full := runAWSProcessTest(t,
		"testdata/aws_tags_and_sets.json",
		"testdata/aws_tags_and_sets_state.json")

	assert.Equal(t, StatusChangesNeeded, full.Status)

	var bucket *ResourceChange
	for i := range full.Resources {
		if full.Resources[i].Name == "test-bucket" {
			bucket = &full.Resources[i]
		}
	}
	require.NotNil(t, bucket, "test-bucket not found in resources")
	assert.Equal(t, ActionUpdateCode, bucket.Action)
	assert.Equal(t, "aws:s3/bucket:Bucket", bucket.Type)

	props := make(map[string]PropertyChange)
	for _, p := range bucket.Properties {
		props[p.Path] = p
	}

	// tags.Environment: update — currentValue (code) = "production", desiredValue (infra) = "staging"
	require.Contains(t, props, "tags.Environment")
	assert.Equal(t, "production", props["tags.Environment"].CurrentValue)
	assert.Equal(t, "staging", props["tags.Environment"].DesiredValue)

	// tags.Owner: update — currentValue = "team-a", desiredValue = "team-b"
	require.Contains(t, props, "tags.Owner")
	assert.Equal(t, "team-a", props["tags.Owner"].CurrentValue)
	assert.Equal(t, "team-b", props["tags.Owner"].DesiredValue)

	// tags.CostCenter: add — exists in code (new) but not in infra (old)
	require.Contains(t, props, "tags.CostCenter")
	assert.Equal(t, "cc-100", props["tags.CostCenter"].CurrentValue)

	// tags.Team: delete — exists in infra (old) but not in code (new)
	require.Contains(t, props, "tags.Team")
	assert.Equal(t, "platform", props["tags.Team"].DesiredValue)
}

// TestAWSTagsAndSets_TagsAllFiltering verifies that tagsAll entries are filtered when
// schema data is available. tagsAll is a bridge-computed mirror of tags — it appears in
// the bridge's inputs but is NOT in the provider schema's inputProperties.
func TestAWSTagsAndSets_TagsAllFiltering(t *testing.T) {
	_, full := runAWSProcessTestWithSchema(t,
		"testdata/aws_tags_and_sets.json",
		"testdata/aws_tags_and_sets_state.json")

	var bucket *ResourceChange
	for i := range full.Resources {
		if full.Resources[i].Name == "test-bucket" {
			bucket = &full.Resources[i]
		}
	}
	require.NotNil(t, bucket)

	for _, p := range bucket.Properties {
		assert.NotContains(t, p.Path, "tagsAll",
			"tagsAll entries should be filtered — they are not in the schema's inputProperties")
	}
}

// TestAWSCascadingDeps_ComputedPropertyFiltering verifies that output-only properties
// like `version`, `insecureValue`, and `lastModified` are filtered from the output.
// These properties exist only in Outputs, not in any Inputs map, and the user cannot
// set them in code — they are computed by the provider.
func TestAWSCascadingDeps_ComputedPropertyFiltering(t *testing.T) {
	_, full := runAWSProcessTestWithSchema(t,
		"testdata/aws_cascading_deps.json",
		"testdata/aws_cascading_deps_state.json")

	for _, res := range full.Resources {
		if res.Type == "aws:ssm/parameter:Parameter" {
			for _, p := range res.Properties {
				assert.NotEqual(t, "version", p.Path,
					"%s: 'version' is output-only and should be filtered", res.Name)
				assert.NotEqual(t, "insecureValue", p.Path,
					"%s: 'insecureValue' is output-only and should be filtered", res.Name)
			}
		}
		if res.Type == "aws:lambda/function:Function" {
			for _, p := range res.Properties {
				assert.NotEqual(t, "lastModified", p.Path,
					"lastModified is output-only and should be filtered")
			}
		}
	}
}

// TestAWSCascadingDeps_KMSKeyProperties verifies correct value extraction for KMS key
// properties where all DetailedDiff entries have inputDiff=false.
func TestAWSCascadingDeps_KMSKeyProperties(t *testing.T) {
	_, full := runAWSProcessTest(t,
		"testdata/aws_cascading_deps.json",
		"testdata/aws_cascading_deps_state.json")

	var kmsKey *ResourceChange
	for i := range full.Resources {
		if full.Resources[i].Name == "app-key" {
			kmsKey = &full.Resources[i]
		}
	}
	require.NotNil(t, kmsKey, "app-key not found")
	assert.Equal(t, ActionUpdateCode, kmsKey.Action)

	props := make(map[string]PropertyChange)
	for _, p := range kmsKey.Properties {
		props[p.Path] = p
	}

	// description: currentValue (code) = "KMS key for app secrets",
	// desiredValue (infra) = "KMS key for app secrets - rotated"
	require.Contains(t, props, "description")
	assert.Equal(t, "KMS key for app secrets", props["description"].CurrentValue)
	assert.Equal(t, "KMS key for app secrets - rotated", props["description"].DesiredValue)

	// enableKeyRotation: currentValue = true, desiredValue = false
	require.Contains(t, props, "enableKeyRotation")
	assert.Equal(t, true, props["enableKeyRotation"].CurrentValue)
	assert.Equal(t, false, props["enableKeyRotation"].DesiredValue)
}

// TestAWSCascadingDeps_LambdaInputProperties verifies Lambda function property changes
// correctly resolve from Inputs, including deeply nested paths and delete ops.
func TestAWSCascadingDeps_LambdaInputProperties(t *testing.T) {
	_, full := runAWSProcessTest(t,
		"testdata/aws_cascading_deps.json",
		"testdata/aws_cascading_deps_state.json")

	var lambda *ResourceChange
	for i := range full.Resources {
		if full.Resources[i].Name == "app-handler" {
			lambda = &full.Resources[i]
		}
	}
	require.NotNil(t, lambda, "app-handler not found")

	props := make(map[string]PropertyChange)
	for _, p := range lambda.Properties {
		props[p.Path] = p
	}

	// runtime: currentValue = "python3.11" (code), desiredValue = "python3.12" (infra)
	require.Contains(t, props, "runtime")
	assert.Equal(t, "python3.11", props["runtime"].CurrentValue)
	assert.Equal(t, "python3.12", props["runtime"].DesiredValue)

	// memorySize: currentValue = 128 (code), desiredValue = 256 (infra)
	require.Contains(t, props, "memorySize")
	assert.Equal(t, float64(128), props["memorySize"].CurrentValue)
	assert.Equal(t, float64(256), props["memorySize"].DesiredValue)

	// timeout: currentValue = 30 (code), desiredValue = 60 (infra)
	require.Contains(t, props, "timeout")
	assert.Equal(t, float64(30), props["timeout"].CurrentValue)
	assert.Equal(t, float64(60), props["timeout"].DesiredValue)

	// environment.variables.APP_ENV: currentValue = "production", desiredValue = "staging"
	require.Contains(t, props, "environment.variables.APP_ENV")
	assert.Equal(t, "production", props["environment.variables.APP_ENV"].CurrentValue)
	assert.Equal(t, "staging", props["environment.variables.APP_ENV"].DesiredValue)

	// environment.variables.LOG_LEVEL: delete — desiredValue = "debug", currentValue = nil
	require.Contains(t, props, "environment.variables.LOG_LEVEL")
	assert.Equal(t, "debug", props["environment.variables.LOG_LEVEL"].DesiredValue)
	assert.Nil(t, props["environment.variables.LOG_LEVEL"].CurrentValue)
}

// TestAWSTagsAndSets_SecurityGroupReplace verifies that replace operations with
// set elements (ingress rules) are correctly handled.
func TestAWSTagsAndSets_SecurityGroupReplace(t *testing.T) {
	_, full := runAWSProcessTest(t,
		"testdata/aws_tags_and_sets.json",
		"testdata/aws_tags_and_sets_state.json")

	var sg *ResourceChange
	for i := range full.Resources {
		if full.Resources[i].Name == "test-sg" {
			sg = &full.Resources[i]
		}
	}
	require.NotNil(t, sg, "test-sg not found")
	assert.Equal(t, ActionUpdateCode, sg.Action)

	props := make(map[string]PropertyChange)
	for _, p := range sg.Properties {
		props[p.Path] = p
	}

	// description: update-replace trigger
	require.Contains(t, props, "description")
	assert.Equal(t, "Security group for drift-adopter testing", props["description"].CurrentValue)
	assert.Equal(t, "Updated security group for testing", props["description"].DesiredValue)
}

// TestAWSTagsAndSets_BucketVersioning verifies nested property path resolution.
func TestAWSTagsAndSets_BucketVersioning(t *testing.T) {
	_, full := runAWSProcessTest(t,
		"testdata/aws_tags_and_sets.json",
		"testdata/aws_tags_and_sets_state.json")

	var versioning *ResourceChange
	for i := range full.Resources {
		if full.Resources[i].Name == "test-bucket-versioning" {
			versioning = &full.Resources[i]
		}
	}
	require.NotNil(t, versioning, "test-bucket-versioning not found")

	require.Len(t, versioning.Properties, 1)
	p := versioning.Properties[0]
	assert.Equal(t, "versioningConfiguration.status", p.Path)
	assert.Equal(t, "Enabled", p.CurrentValue)
	assert.Equal(t, "Suspended", p.DesiredValue)
}

// TestAWSSecretInput_OutputOnlyFiltering verifies output-only property filtering
// and secret value handling for SSM SecureString parameters.
func TestAWSSecretInput_OutputOnlyFiltering(t *testing.T) {
	_, full := runAWSProcessTestWithSchema(t,
		"testdata/aws_secret_input.json",
		"testdata/aws_secret_input_state.json")

	for _, res := range full.Resources {
		if res.Type != "aws:ssm/parameter:Parameter" {
			continue
		}
		for _, p := range res.Properties {
			assert.NotEqual(t, "version", p.Path,
				"%s: version is output-only", res.Name)
			assert.NotEqual(t, "insecureValue", p.Path,
				"%s: insecureValue is output-only", res.Name)
		}
	}
}

// TestAWSSecretInput_SecretValueSupplementation verifies that "[secret]" property values
// are supplemented with real values from the state export. The agent needs actual secret
// values to write working code — there's no `pulumi config get` equivalent for state-sourced drift.
func TestAWSSecretInput_SecretValueSupplementation(t *testing.T) {
	_, full := runAWSProcessTest(t,
		"testdata/aws_secret_input.json",
		"testdata/aws_secret_input_state.json")

	var dbPassword *ResourceChange
	for i := range full.Resources {
		if full.Resources[i].Name == "db-password" {
			dbPassword = &full.Resources[i]
		}
	}
	require.NotNil(t, dbPassword, "db-password not found")

	props := make(map[string]PropertyChange)
	for _, p := range dbPassword.Properties {
		props[p.Path] = p
	}

	require.Contains(t, props, "value")
	valProp := props["value"]

	// desiredValue (what infra has) should be the REAL secret, not "[secret]".
	// The state export contains the actual plaintext via --show-secrets.
	assert.NotEqual(t, "[secret]", valProp.DesiredValue,
		"desiredValue should be supplemented with real secret from state export")
	assert.Equal(t, "new-db-password-def456", valProp.DesiredValue,
		"desiredValue should be the actual secret value from state export")
}

// TestValueResolution_CurrentValueFromInputsNotOutputs verifies that currentValue
// (from NewState) always uses Inputs, not stale Outputs. This matches the engine's
// TranslateDetailedDiff semantics: NEW values always come from Inputs.
//
// This matters during provider version upgrades where NewState.Outputs may carry
// forward stale values from the old provider version while NewState.Inputs reflects
// the new provider's Check output. The Terraform bridge is particularly susceptible
// because it carries forward old state outputs during preview planning.
func TestValueResolution_CurrentValueFromInputsNotOutputs(t *testing.T) {
	eventsContent := `{
		"steps": [{
			"op": "update",
			"urn": "urn:pulumi:dev::test::some:provider:Resource::my-resource",
			"oldState": {
				"type": "some:provider:Resource",
				"inputs": {"setting": "old-value"},
				"outputs": {"setting": "old-value", "computed": "old-computed"}
			},
			"newState": {
				"type": "some:provider:Resource",
				"inputs": {"setting": "new-from-code"},
				"outputs": {"setting": "stale-output-value", "computed": "stale-computed"}
			},
			"detailedDiff": {
				"setting": {"kind": "update", "inputDiff": false}
			}
		}]
	}`

	_, full := runProcessTest(t, []byte(eventsContent))
	require.Len(t, full.Resources, 1)
	require.Len(t, full.Resources[0].Properties, 1)

	prop := full.Resources[0].Properties[0]
	assert.Equal(t, "setting", prop.Path)
	// currentValue must come from NewState.Inputs, not stale Outputs from old provider version
	assert.Equal(t, "new-from-code", prop.CurrentValue,
		"currentValue should be from NewState.Inputs, not stale Outputs")
	assert.Equal(t, "old-value", prop.DesiredValue)
}

// TestGetNestedProperty_BracketQuotedKeys verifies that property paths with
// bracket-quoted keys (e.g., tags["kubernetes.io/name"]) are correctly parsed.
// The current getNestedProperty uses naive dot-splitting which fails on dots inside keys.
// PropertyPath from the Pulumi SDK handles this correctly.
func TestGetNestedProperty_BracketQuotedKeys(t *testing.T) {
	props := map[string]interface{}{
		"tags": map[string]interface{}{
			"kubernetes.io/name":    "my-cluster",
			"app.kubernetes.io/env": "prod",
		},
		"metadata": map[string]interface{}{
			"annotations": map[string]interface{}{
				"key.with.dots": "value",
			},
		},
	}

	// Bracket-quoted key with dots — standard PropertyPath syntax
	assert.Equal(t, "my-cluster", getNestedProperty(props, `tags["kubernetes.io/name"]`))
	assert.Equal(t, "prod", getNestedProperty(props, `tags["app.kubernetes.io/env"]`))

	// Nested bracket-quoted key
	assert.Equal(t, "value", getNestedProperty(props, `metadata.annotations["key.with.dots"]`))
}

// TestGetNestedProperty_ConsecutiveIndices verifies paths with consecutive array indices
// like "matrix[0][1]" which the current implementation doesn't handle.
func TestGetNestedProperty_ConsecutiveIndices(t *testing.T) {
	props := map[string]interface{}{
		"matrix": []interface{}{
			[]interface{}{"a", "b", "c"},
			[]interface{}{"d", "e", "f"},
		},
	}

	assert.Equal(t, "b", getNestedProperty(props, "matrix[0][1]"))
	assert.Equal(t, "f", getNestedProperty(props, "matrix[1][2]"))
	assert.Nil(t, getNestedProperty(props, "matrix[2][0]")) // out of bounds
}

// TestUnknownSentinelFiltering verifies that the Pulumi engine's unknown sentinel
// UUID ("04da6b54-80e4-46f7-96ec-b56ff0331ba9") is filtered from property values.
// These appear in Outputs during cascading replaces when a dependent resource
// hasn't been created yet.
func TestUnknownSentinelFiltering(t *testing.T) {
	const sentinel = "04da6b54-80e4-46f7-96ec-b56ff0331ba9"

	eventsContent := `{
		"steps": [{
			"op": "update",
			"urn": "urn:pulumi:dev::test::aws:ssm/parameter:Parameter::my-param",
			"oldState": {
				"type": "aws:ssm/parameter:Parameter",
				"inputs": {"keyId": "` + sentinel + `"},
				"outputs": {"keyId": "` + sentinel + `", "arn": "arn:aws:ssm:us-west-2:123:parameter/my-param"}
			},
			"newState": {
				"type": "aws:ssm/parameter:Parameter",
				"inputs": {"keyId": "real-key-id"},
				"outputs": {}
			},
			"detailedDiff": {
				"keyId": {"kind": "update", "inputDiff": true}
			}
		}]
	}`

	_, full := runProcessTest(t, []byte(eventsContent))
	require.Len(t, full.Resources, 1)
	require.Len(t, full.Resources[0].Properties, 1)

	prop := full.Resources[0].Properties[0]
	assert.Equal(t, "keyId", prop.Path)
	assert.Equal(t, "real-key-id", prop.CurrentValue)
	// desiredValue should be nil (sentinel filtered), not the literal UUID string
	assert.Nil(t, prop.DesiredValue,
		"unknown sentinel should be filtered to nil, not passed as literal string")
}

// TestAWSTagsAndSets_SecurityGroupSetElements verifies that array-indexed set elements
// (ingress rules) in security group replace operations resolve values correctly.
// AWS security groups use TypeSet for ingress/egress rules, which produces array-indexed
// paths like "ingress[0]" in DetailedDiff even though the underlying type is a set.
func TestAWSTagsAndSets_SecurityGroupSetElements(t *testing.T) {
	_, full := runAWSProcessTest(t,
		"testdata/aws_tags_and_sets.json",
		"testdata/aws_tags_and_sets_state.json")

	var sg *ResourceChange
	for i := range full.Resources {
		if full.Resources[i].Name == "test-sg" {
			sg = &full.Resources[i]
		}
	}
	require.NotNil(t, sg, "test-sg not found")

	// Collect all property paths
	paths := make(map[string]bool)
	for _, p := range sg.Properties {
		paths[p.Path] = true
	}

	// Security group should have ingress array-indexed paths (set elements)
	hasIngressPath := false
	for path := range paths {
		if len(path) >= 7 && path[:7] == "ingress" {
			hasIngressPath = true
			break
		}
	}
	assert.True(t, hasIngressPath, "security group should have ingress paths from TypeSet")

	// description change should be present (replace trigger)
	assert.True(t, paths["description"], "description should be a changed property")
}

// TestAWSTagsAndSets_SchemaFiltersCombined verifies that when schema data is available,
// all three scenarios work correctly together: tag filtering, set elements, and nested objects.
func TestAWSTagsAndSets_SchemaFiltersCombined(t *testing.T) {
	_, full := runAWSProcessTestWithSchema(t,
		"testdata/aws_tags_and_sets.json",
		"testdata/aws_tags_and_sets_state.json")

	for _, res := range full.Resources {
		for _, p := range res.Properties {
			// No resource should have tagsAll paths when schema filtering is active
			assert.NotContains(t, p.Path, "tagsAll",
				"%s/%s: tagsAll should be filtered by schema", res.Name, p.Path)
		}
	}

	// Verify each resource type is present
	types := make(map[string]bool)
	for _, res := range full.Resources {
		types[res.Type] = true
	}
	assert.True(t, types["aws:s3/bucket:Bucket"], "S3 bucket should be present")
	assert.True(t, types["aws:ec2/securityGroup:SecurityGroup"], "security group should be present")
}

// TestAWSTagsAndSets_IAMInlinePolicyArrayPath verifies that array-indexed nested paths
// like "inlinePolicies[0].policy" resolve correctly from Inputs.
func TestAWSTagsAndSets_IAMInlinePolicyArrayPath(t *testing.T) {
	_, full := runAWSProcessTest(t,
		"testdata/aws_tags_and_sets.json",
		"testdata/aws_tags_and_sets_state.json")

	var role *ResourceChange
	for i := range full.Resources {
		if full.Resources[i].Name == "test-role" {
			role = &full.Resources[i]
		}
	}
	require.NotNil(t, role, "test-role not found")
	assert.Equal(t, ActionUpdateCode, role.Action)

	// Should have inlinePolicies[0].policy path
	props := make(map[string]PropertyChange)
	for _, p := range role.Properties {
		props[p.Path] = p
	}

	require.Contains(t, props, "inlinePolicies[0].policy",
		"IAM role should have inlinePolicies[0].policy from array-indexed DetailedDiff")

	// Both values should be non-nil (update, not add/delete)
	p := props["inlinePolicies[0].policy"]
	assert.NotNil(t, p.CurrentValue, "currentValue should be non-nil (from NewState.Inputs)")
	assert.NotNil(t, p.DesiredValue, "desiredValue should be non-nil (from OldState)")
}

// TestAWSCascadingDeps_SchemaFiltersOutputProperties verifies the full pipeline
// with schema filtering on the cascading-deps scenario: KMS → SSM → Lambda.
func TestAWSCascadingDeps_SchemaFiltersOutputProperties(t *testing.T) {
	_, full := runAWSProcessTestWithSchema(t,
		"testdata/aws_cascading_deps.json",
		"testdata/aws_cascading_deps_state.json")

	// Collect all property paths by resource type
	propsByType := make(map[string][]string)
	for _, res := range full.Resources {
		for _, p := range res.Properties {
			propsByType[res.Type] = append(propsByType[res.Type], p.Path)
		}
	}

	// Lambda should NOT have lastModified (output-only)
	for _, path := range propsByType["aws:lambda/function:Function"] {
		assert.NotEqual(t, "lastModified", path, "lastModified is output-only")
	}

	// SSM should NOT have version or insecureValue (output-only per schema)
	for _, path := range propsByType["aws:ssm/parameter:Parameter"] {
		assert.NotEqual(t, "version", path, "version is output-only")
	}

	// KMS key should still have description and enableKeyRotation (real inputs)
	kmsProps := propsByType["aws:kms/key:Key"]
	assert.Contains(t, kmsProps, "description", "description is an input property")
	assert.Contains(t, kmsProps, "enableKeyRotation", "enableKeyRotation is an input property")
}

// TestAWSCascadingDeps_LambdaNestedMapDelete verifies that environment.variables.LOG_LEVEL
// delete is correctly handled with schema filtering — the nested path should survive
// filtering because "environment" IS a known input property.
func TestAWSCascadingDeps_LambdaNestedMapDelete(t *testing.T) {
	_, full := runAWSProcessTestWithSchema(t,
		"testdata/aws_cascading_deps.json",
		"testdata/aws_cascading_deps_state.json")

	var lambda *ResourceChange
	for i := range full.Resources {
		if full.Resources[i].Name == "app-handler" {
			lambda = &full.Resources[i]
		}
	}
	require.NotNil(t, lambda)

	props := make(map[string]PropertyChange)
	for _, p := range lambda.Properties {
		props[p.Path] = p
	}

	// environment.variables.LOG_LEVEL should be present even with schema filtering
	// because "environment" is a known input property — the top-level key passes the filter
	require.Contains(t, props, "environment.variables.LOG_LEVEL",
		"nested map delete should survive schema filtering via top-level key 'environment'")
	assert.Nil(t, props["environment.variables.LOG_LEVEL"].CurrentValue,
		"LOG_LEVEL was deleted from code, currentValue should be nil")
	assert.Equal(t, "debug", props["environment.variables.LOG_LEVEL"].DesiredValue,
		"LOG_LEVEL desiredValue should be the old infrastructure value")
}

// TestAWSSecretInput_MixedSecretAndPlaintext verifies that resources with both secret
// and non-secret properties are handled correctly — secrets are supplemented while
// plain values pass through unchanged.
func TestAWSSecretInput_MixedSecretAndPlaintext(t *testing.T) {
	_, full := runAWSProcessTestWithSchema(t,
		"testdata/aws_secret_input.json",
		"testdata/aws_secret_input_state.json")

	var appConfig *ResourceChange
	for i := range full.Resources {
		if full.Resources[i].Name == "app-config" {
			appConfig = &full.Resources[i]
		}
	}
	require.NotNil(t, appConfig, "app-config not found")

	props := make(map[string]PropertyChange)
	for _, p := range appConfig.Properties {
		props[p.Path] = p
	}

	// description is a plain (non-secret) property — should have real values
	if descProp, ok := props["description"]; ok {
		assert.NotEqual(t, "[secret]", descProp.CurrentValue,
			"description is not secret, should have real value")
		assert.NotEqual(t, "[secret]", descProp.DesiredValue,
			"description is not secret, should have real value")
	}

	// tags.Environment is a plain property — should have real values
	if tagProp, ok := props["tags.Environment"]; ok {
		assert.IsType(t, "", tagProp.CurrentValue, "tag value should be a string")
		assert.IsType(t, "", tagProp.DesiredValue, "tag value should be a string")
	}
}

// TestAWSSecretInput_AllSSMSecretsSupplemented verifies that ALL SSM SecureString
// parameters in the secret-input scenario have their "[secret]" values supplemented.
func TestAWSSecretInput_AllSSMSecretsSupplemented(t *testing.T) {
	_, full := runAWSProcessTest(t,
		"testdata/aws_secret_input.json",
		"testdata/aws_secret_input_state.json")

	for _, res := range full.Resources {
		if res.Type != "aws:ssm/parameter:Parameter" {
			continue
		}

		for _, p := range res.Properties {
			if p.Path == "value" {
				// desiredValue (from infra state) should be supplemented — not "[secret]"
				assert.NotEqual(t, "[secret]", p.DesiredValue,
					"%s: value desiredValue should be supplemented from state export", res.Name)
				assert.NotNil(t, p.DesiredValue,
					"%s: value desiredValue should not be nil", res.Name)
			}
		}
	}
}

// TestMetadataRoundTrip_WithInputProperties verifies that ResourceMetadata with
// InputProperties survives a save → load round-trip correctly.
func TestMetadataRoundTrip_WithInputProperties(t *testing.T) {
	inputProps := loadInputProperties(t)

	meta := &ResourceMetadata{
		Dependencies: DependencyMap{
			"urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket": {
				"tags.Name": {
					ResourceName:   "some-resource",
					ResourceType:   "aws:something:Something",
					OutputProperty: "name",
				},
			},
		},
		InputProperties: inputProps,
	}

	path, err := saveMetadata(meta)
	require.NoError(t, err)
	defer os.Remove(path)

	loaded, err := loadMetadata(path)
	require.NoError(t, err)

	// Dependencies should match
	assert.Equal(t, meta.Dependencies, loaded.Dependencies)

	// InputProperties should match
	assert.Equal(t, len(meta.InputProperties), len(loaded.InputProperties),
		"number of resource types should match")

	for resType, props := range meta.InputProperties {
		assert.Equal(t, props, loaded.InputProperties[resType],
			"input properties for %s should match", resType)
	}

	// StateLookup should be nil (not serialized)
	assert.Nil(t, loaded.StateLookup, "StateLookup should not survive round-trip")
}

// TestAWSFullPipelineWithSchema runs the complete pipeline (parse → extract → filter → sort → output)
// on all three AWS scenarios with schema filtering, verifying the end-to-end result.
func TestAWSFullPipelineWithSchema(t *testing.T) {
	scenarios := []struct {
		name        string
		previewFile string
		stateFile   string
	}{
		{"tags-and-sets", "testdata/aws_tags_and_sets.json", "testdata/aws_tags_and_sets_state.json"},
		{"cascading-deps", "testdata/aws_cascading_deps.json", "testdata/aws_cascading_deps_state.json"},
		{"secret-input", "testdata/aws_secret_input.json", "testdata/aws_secret_input_state.json"},
	}

	for _, sc := range scenarios {
		t.Run(sc.name, func(t *testing.T) {
			_, full := runAWSProcessTestWithSchema(t, sc.previewFile, sc.stateFile)

			assert.Equal(t, StatusChangesNeeded, full.Status, "all scenarios should have changes")
			assert.NotEmpty(t, full.Resources, "should have at least one resource")

			for _, res := range full.Resources {
				// Every resource should have a valid action
				assert.Contains(t, []string{ActionUpdateCode, ActionAddToCode, ActionDeleteFromCode}, res.Action,
					"invalid action for %s", res.Name)

				// update_code resources should have properties
				if res.Action == ActionUpdateCode {
					assert.NotEmpty(t, res.Properties,
						"%s: update_code should have properties", res.Name)
				}

				// No property should have tagsAll (schema-filtered)
				for _, p := range res.Properties {
					assert.NotContains(t, p.Path, "tagsAll",
						"%s/%s: tagsAll should be filtered", res.Name, p.Path)
				}
			}
		})
	}
}

// TestEnrichPropertyDependencies verifies that update_code properties get DependsOn
// populated when the depMap has a matching entry, and left nil otherwise.
func TestEnrichPropertyDependencies(t *testing.T) {
	urn := "urn:pulumi:dev::proj::tls:index/selfSignedCert:SelfSignedCert::my-cert"
	depMap := DependencyMap{
		urn: {
			"privateKeyPem": DependencyRef{
				ResourceName:   "ca-key",
				ResourceType:   "tls:index/privateKey:PrivateKey",
				OutputProperty: "privateKeyPem",
			},
		},
	}

	properties := []PropertyChange{
		{Path: "privateKeyPem", CurrentValue: "old-literal", DesiredValue: "matched-value"},
		{Path: "subject", CurrentValue: "CN=old", DesiredValue: "CN=new"},
	}

	enrichPropertyDependencies(properties, urn, depMap)

	// Property with matching dep should have DependsOn set
	require.NotNil(t, properties[0].DependsOn, "privateKeyPem should have DependsOn")
	assert.Equal(t, "ca-key", properties[0].DependsOn.ResourceName)
	assert.Equal(t, "tls:index/privateKey:PrivateKey", properties[0].DependsOn.ResourceType)
	assert.Equal(t, "privateKeyPem", properties[0].DependsOn.OutputProperty)

	// Property without dep should have nil DependsOn
	assert.Nil(t, properties[1].DependsOn, "subject should not have DependsOn")
}

// TestEnrichPropertyDependencies_NoDeps verifies enrichment is a no-op when the URN
// has no entries in depMap.
func TestEnrichPropertyDependencies_NoDeps(t *testing.T) {
	properties := []PropertyChange{
		{Path: "name", CurrentValue: "old", DesiredValue: "new"},
	}

	enrichPropertyDependencies(properties, "urn:pulumi:dev::proj::pkg:mod:Res::my-res", DependencyMap{})

	assert.Nil(t, properties[0].DependsOn)
}

// TestAWSCascadingDeps_DependencyMapFromState verifies that the dep map built from
// the real state export correctly resolves cross-resource dependencies.
func TestAWSCascadingDeps_DependencyMapFromState(t *testing.T) {
	stateData, err := os.ReadFile("testdata/aws_cascading_deps_state.json")
	require.NoError(t, err)

	stateLookup, err := parseStateExport(stateData)
	require.NoError(t, err)

	depMap := buildDepMapFromState(stateLookup)

	// Find Lambda URN — its role should reference lambda-role
	var lambdaURN string
	for urn, res := range stateLookup {
		if string(res.Type) == "aws:lambda/function:Function" {
			lambdaURN = urn
		}
	}
	require.NotEmpty(t, lambdaURN, "Lambda URN not found in state")

	lambdaDeps := depMap[lambdaURN]
	require.NotNil(t, lambdaDeps, "Lambda should have dependency entries")

	if roleRef, ok := lambdaDeps["role"]; ok {
		assert.Equal(t, "lambda-role", roleRef.ResourceName)
		assert.Equal(t, "aws:iam/role:Role", roleRef.ResourceType)
	}

	// SSM parameter keyId should depend on KMS key
	for urn, res := range stateLookup {
		if string(res.Type) != "aws:ssm/parameter:Parameter" {
			continue
		}
		ssmDeps := depMap[urn]
		if ssmDeps == nil {
			continue
		}
		if keyRef, ok := ssmDeps["keyId"]; ok {
			assert.Equal(t, "app-key", keyRef.ResourceName)
			assert.Equal(t, "aws:kms/key:Key", keyRef.ResourceType)
		}
	}
}
