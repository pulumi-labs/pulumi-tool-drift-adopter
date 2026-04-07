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
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNextCommandWithEventsFile tests the next command using --events-file flag
func TestNextCommandWithEventsFile(t *testing.T) {
	tests := []struct {
		name            string
		eventsContent   string
		expectedStatus  string
		expectedError   string
		expectResources bool
	}{
		{
			name:          "empty steps array",
			eventsContent: `{"steps": []}`,
			expectedError: "preview output contains no steps",
		},
		{
			name:          "empty events array",
			eventsContent: `{"events": []}`,
			expectedError: "preview output contains no events",
		},
		{
			name: "update operation - drift detected",
			eventsContent: `{
				"steps": [{
					"op": "update",
					"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket",
					"oldState": {
						"type": "aws:s3/bucket:Bucket",
						"outputs": {
							"tags": {
								"Environment": "production"
							}
						}
					},
					"newState": {
						"type": "aws:s3/bucket:Bucket",
						"outputs": {
							"tags": {
								"Environment": "dev"
							}
						}
					},
					"detailedDiff": {
						"tags.Environment": {
							"kind": "update"
						}
					}
				}]
			}`,
			expectedStatus:  "changes_needed",
			expectResources: true,
		},
		{
			name: "create operation - resource should be deleted from code",
			eventsContent: `{
				"steps": [{
					"op": "create",
					"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::extra-bucket",
					"newState": {
						"type": "aws:s3/bucket:Bucket",
						"outputs": {
							"bucket": "extra-bucket"
						}
					},
					"detailedDiff": {}
				}]
			}`,
			expectedStatus:  "changes_needed",
			expectResources: true,
		},
		{
			name: "delete operation - resource should be added to code (prefers inputs over outputs)",
			eventsContent: `{
				"steps": [{
					"op": "delete",
					"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::missing-bucket",
					"oldState": {
						"type": "aws:s3/bucket:Bucket",
						"inputs": {
							"bucket": "missing-bucket"
						},
						"outputs": {
							"bucket": "missing-bucket",
							"arn": "arn:aws:s3:::missing-bucket",
							"id": "missing-bucket"
						}
					},
					"detailedDiff": {}
				}]
			}`,
			expectedStatus:  "changes_needed",
			expectResources: true,
		},
		{
			name: "delete operation - falls back to outputs when inputs empty",
			eventsContent: `{
				"steps": [{
					"op": "delete",
					"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::legacy-bucket",
					"oldState": {
						"type": "aws:s3/bucket:Bucket",
						"outputs": {
							"bucket": "legacy-bucket"
						}
					},
					"detailedDiff": {}
				}]
			}`,
			expectedStatus:  "changes_needed",
			expectResources: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Cases that should fail at parse time (before processNext)
			if tt.expectedStatus == "" && tt.expectedError != "" {
				_, _, err := parsePreviewOutput([]byte(tt.eventsContent))
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				return
			}

			summary, full := runProcessTest(t, []byte(tt.eventsContent))

			// Verify status
			assert.Equal(t, tt.expectedStatus, summary.Status, "Status mismatch")

			// Verify error message if expected
			if tt.expectedError != "" {
				assert.Contains(t, summary.Error, tt.expectedError, "Error message mismatch")
			}

			// Verify resources presence (from output file)
			if tt.expectResources {
				assert.NotEmpty(t, full.Resources, "Expected resources but got none")
			} else if tt.expectedError == "" {
				assert.Empty(t, full.Resources, "Expected no resources but got some")
			}
		})
	}
}

// TestNextCommandInvalidJSON tests that invalid JSON input produces a parse error
func TestNextCommandInvalidJSON(t *testing.T) {
	_, _, err := parsePreviewOutput([]byte(`{invalid json`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse preview output")
}

// TestNextCommandNDJSONRealFormat tests parsing actual NDJSON with full engine event structure
func TestNextCommandNDJSONRealFormat(t *testing.T) {
	summary, full := runProcessTestFile(t, "testdata/ndjson_update.ndjson")

	assert.Equal(t, "changes_needed", summary.Status)
	require.Len(t, full.Resources, 1)

	resource := full.Resources[0]
	assert.Equal(t, "update_code", resource.Action)
	assert.Equal(t, "my-bucket", resource.Name)
	assert.Equal(t, "aws:s3/bucket:Bucket", resource.Type)
	assert.Contains(t, resource.URN, "my-bucket")

	require.Len(t, resource.Properties, 2)

	var envProp, managedByProp *PropertyChange
	for i := range resource.Properties {
		if resource.Properties[i].Path == "tags.Environment" {
			envProp = &resource.Properties[i]
		}
		if resource.Properties[i].Path == "tags.ManagedBy" {
			managedByProp = &resource.Properties[i]
		}
	}

	require.NotNil(t, envProp, "tags.Environment property not found")
	assert.Equal(t, "dev", envProp.CurrentValue)
	assert.Equal(t, "production", envProp.DesiredValue)

	require.NotNil(t, managedByProp, "tags.ManagedBy property not found")
	assert.Equal(t, nil, managedByProp.CurrentValue)
	assert.Equal(t, "pulumi", managedByProp.DesiredValue)
}

// TestNextCommandNDJSONMixedEvents tests that non-resourcePreEvent lines are properly skipped
func TestNextCommandNDJSONMixedEvents(t *testing.T) {
	summary, full := runProcessTestFile(t, "testdata/ndjson_with_diagnostics.ndjson")

	assert.Equal(t, "changes_needed", summary.Status)
	require.Len(t, full.Resources, 1)

	resource := full.Resources[0]
	assert.Equal(t, "update_code", resource.Action)
	assert.Equal(t, "test-bucket", resource.Name)
	assert.Equal(t, "aws:s3/bucket:Bucket", resource.Type)

	require.Len(t, resource.Properties, 1)
	prop := resource.Properties[0]
	assert.Equal(t, "versioning.enabled", prop.Path)
	assert.Equal(t, false, prop.CurrentValue)
	assert.Equal(t, true, prop.DesiredValue)
}

// TestNextCommandNDJSONEmptyFile tests NDJSON with no resource events returns clean
func TestNextCommandNDJSONEmptyFile(t *testing.T) {
	summary, full := runProcessTestFile(t, "testdata/ndjson_empty.ndjson")

	assert.Equal(t, "clean", summary.Status)
	assert.Empty(t, full.Resources)
}

// TestNextCommandNDJSONMultipleResources tests parsing multiple resources from NDJSON
func TestNextCommandNDJSONMultipleResources(t *testing.T) {
	summary, full := runProcessTestFile(t, "testdata/ndjson_multiple_resources.ndjson")

	assert.Equal(t, "changes_needed", summary.Status)
	assert.Len(t, full.Resources, 3)

	if len(full.Resources) > 0 {
		resource := full.Resources[0]
		assert.Equal(t, "update_code", resource.Action)
		assert.Equal(t, "bucket-1", resource.Name)
		assert.Equal(t, "aws:s3/bucket:Bucket", resource.Type)
	}
}

// TestNextCommandNDJSONCreateDelete tests create and delete operations from NDJSON
func TestNextCommandNDJSONCreateDelete(t *testing.T) {
	summary, full := runProcessTestFile(t, "testdata/ndjson_create_delete.ndjson")

	assert.Equal(t, "changes_needed", summary.Status)
	require.Len(t, full.Resources, 2)

	var createResource, deleteResource *ResourceChange
	for i := range full.Resources {
		if full.Resources[i].Name == "extra-bucket" {
			createResource = &full.Resources[i]
		}
		if full.Resources[i].Name == "missing-bucket" {
			deleteResource = &full.Resources[i]
		}
	}

	require.NotNil(t, createResource, "extra-bucket not found")
	assert.Equal(t, "delete_from_code", createResource.Action)
	assert.Equal(t, "aws:s3/bucket:Bucket", createResource.Type)

	require.NotNil(t, deleteResource, "missing-bucket not found")
	assert.Equal(t, "add_to_code", deleteResource.Action)
	assert.Equal(t, "aws:s3/bucket:Bucket", deleteResource.Type)
}

// TestNextCommandNDJSONReplace tests that replace operations extract properties from Diffs field
func TestNextCommandNDJSONReplace(t *testing.T) {
	summary, full := runProcessTestFile(t, "testdata/ndjson_replace.ndjson")

	assert.Equal(t, "changes_needed", summary.Status)
	require.Len(t, full.Resources, 2)

	// Find resources by name
	var randomString, privateKey *ResourceChange
	for i := range full.Resources {
		if full.Resources[i].Name == "my-random-string" {
			randomString = &full.Resources[i]
		}
		if full.Resources[i].Name == "my-private-key" {
			privateKey = &full.Resources[i]
		}
	}

	// Verify RandomString replace has properties from Diffs
	require.NotNil(t, randomString, "RandomString resource not found")
	assert.Equal(t, "update_code", randomString.Action)
	assert.Equal(t, "random:index/randomString:RandomString", randomString.Type)
	require.NotEmpty(t, randomString.Properties, "RandomString should have properties extracted from Diffs")

	// Find length property
	var lengthProp *PropertyChange
	for i := range randomString.Properties {
		if randomString.Properties[i].Path == "length" {
			lengthProp = &randomString.Properties[i]
		}
	}
	require.NotNil(t, lengthProp, "length property not found")
	// Old inputs has length=16 (desired), new inputs has length=32 (current)
	assert.Equal(t, float64(32), lengthProp.CurrentValue)
	assert.Equal(t, float64(16), lengthProp.DesiredValue)

	// Verify PrivateKey replace has properties from Diffs
	require.NotNil(t, privateKey, "PrivateKey resource not found")
	assert.Equal(t, "update_code", privateKey.Action)
	assert.Equal(t, "tls:index/privateKey:PrivateKey", privateKey.Type)
	require.NotEmpty(t, privateKey.Properties, "PrivateKey should have properties extracted from Diffs")

	// Find algorithm property
	var algoProp *PropertyChange
	for i := range privateKey.Properties {
		if privateKey.Properties[i].Path == "algorithm" {
			algoProp = &privateKey.Properties[i]
		}
	}
	require.NotNil(t, algoProp, "algorithm property not found")
	// Old inputs has algorithm="RSA" (desired), new inputs has algorithm="ECDSA" (current)
	assert.Equal(t, "ECDSA", algoProp.CurrentValue)
	assert.Equal(t, "RSA", algoProp.DesiredValue)
}

// TestNextCommandEngineEventsJSON tests parsing the Pulumi Cloud GetEngineEvents API format
func TestNextCommandEngineEventsJSON(t *testing.T) {
	summary, full := runProcessTestFile(t, "testdata/engine_events_update.json")

	assert.Equal(t, "changes_needed", summary.Status)
	require.Len(t, full.Resources, 1)

	resource := full.Resources[0]
	assert.Equal(t, "update_code", resource.Action)
	assert.Equal(t, "my-bucket", resource.Name)
	assert.Equal(t, "aws:s3/bucket:Bucket", resource.Type)
	assert.Contains(t, resource.URN, "my-bucket")

	require.Len(t, resource.Properties, 2)

	var envProp, managedByProp *PropertyChange
	for i := range resource.Properties {
		if resource.Properties[i].Path == "tags.Environment" {
			envProp = &resource.Properties[i]
		}
		if resource.Properties[i].Path == "tags.ManagedBy" {
			managedByProp = &resource.Properties[i]
		}
	}

	require.NotNil(t, envProp, "tags.Environment property not found")
	assert.Equal(t, "dev", envProp.CurrentValue)
	assert.Equal(t, "production", envProp.DesiredValue)

	require.NotNil(t, managedByProp, "tags.ManagedBy property not found")
	assert.Equal(t, nil, managedByProp.CurrentValue)
	assert.Equal(t, "pulumi", managedByProp.DesiredValue)
}

// TestNextCommandRealPulumiServiceNDJSON tests parsing of real NDJSON from pulumi-service integration test
// This reproduces the parsing bug found during integration testing where the tool reported:
// "failed to parse preview output: invalid character '{' after top-level value"
func TestNextCommandRealPulumiServiceNDJSON(t *testing.T) {
	summary, full := runProcessTestFile(t, "testdata/simple-s3-drift.ndjson")

	assert.Equal(t, "changes_needed", summary.Status, "Expected drift to be detected")
	require.NotEmpty(t, full.Resources, "Expected at least one resource with drift")

	var bucketResource *ResourceChange
	for i := range full.Resources {
		if full.Resources[i].Name == "test-bucket" {
			bucketResource = &full.Resources[i]
			break
		}
	}

	require.NotNil(t, bucketResource, "Expected to find S3 bucket resource")
	assert.Equal(t, "update_code", bucketResource.Action)
	assert.Equal(t, "test-bucket", bucketResource.Name)
	assert.Contains(t, bucketResource.URN, "aws:s3/bucket:Bucket")

	var tagChanges []*PropertyChange
	for i := range bucketResource.Properties {
		if len(bucketResource.Properties[i].Path) >= 4 && bucketResource.Properties[i].Path[:4] == "tags" {
			tagChanges = append(tagChanges, &bucketResource.Properties[i])
		}
	}
	require.NotEmpty(t, tagChanges, "Expected tag changes to be detected")
}

// TestNextCommandSmallScaleRealPreview tests parsing of real `pulumi preview --json` output from the
// small-scale-10 fixture. This fixture has 5 drifted resources: 2 replaces (random-str-0, tls-key-0),
// 1 update (cmd-0), 1 create (cmd-3), 1 delete (random-str-extra-0).
// The key assertion is that every update_code resource has non-empty Properties.
func TestNextCommandSmallScaleRealPreview(t *testing.T) {
	summary, full := runProcessTestFile(t, "testdata/small_scale_10_replace.json")

	assert.Equal(t, "changes_needed", summary.Status)
	require.Len(t, full.Resources, 5, "Expected 5 drifted resources")

	resourceMap := make(map[string]*ResourceChange)
	for i := range full.Resources {
		resourceMap[full.Resources[i].Name] = &full.Resources[i]
	}

	assert.Equal(t, "delete_from_code", resourceMap["cmd-3"].Action, "create -> delete_from_code")
	assert.Equal(t, "add_to_code", resourceMap["random-str-extra-0"].Action, "delete -> add_to_code")
	assert.Equal(t, "update_code", resourceMap["cmd-0"].Action, "update -> update_code")
	assert.Equal(t, "update_code", resourceMap["random-str-0"].Action, "replace -> update_code")
	assert.Equal(t, "update_code", resourceMap["tls-key-0"].Action, "replace -> update_code")

	for _, res := range full.Resources {
		if res.Action == "update_code" {
			require.NotEmpty(t, res.Properties,
				"Resource %s (%s) with action update_code must have non-empty Properties", res.Name, res.Type)
		}
	}

	randomStr := resourceMap["random-str-0"]
	require.NotNil(t, randomStr)
	propMap := make(map[string]*PropertyChange)
	for i := range randomStr.Properties {
		propMap[randomStr.Properties[i].Path] = &randomStr.Properties[i]
	}

	lengthProp := propMap["length"]
	require.NotNil(t, lengthProp, "length property not found on random-str-0")
	assert.Equal(t, float64(16), lengthProp.CurrentValue)
	assert.Equal(t, float64(32), lengthProp.DesiredValue)

	specialProp := propMap["special"]
	require.NotNil(t, specialProp, "special property not found on random-str-0")
	assert.Equal(t, false, specialProp.CurrentValue)
	assert.Equal(t, true, specialProp.DesiredValue)

	tlsKey := resourceMap["tls-key-0"]
	require.NotNil(t, tlsKey)
	tlsPropMap := make(map[string]*PropertyChange)
	for i := range tlsKey.Properties {
		tlsPropMap[tlsKey.Properties[i].Path] = &tlsKey.Properties[i]
	}

	algoProp := tlsPropMap["algorithm"]
	require.NotNil(t, algoProp, "algorithm property not found on tls-key-0")
	assert.Equal(t, "RSA", algoProp.CurrentValue)
	assert.Equal(t, "ECDSA", algoProp.DesiredValue)

	ecdsaProp := tlsPropMap["ecdsaCurve"]
	require.NotNil(t, ecdsaProp, "ecdsaCurve property not found on tls-key-0")
	assert.Nil(t, ecdsaProp.CurrentValue)
	assert.Equal(t, "P256", ecdsaProp.DesiredValue)
}

// TestNextCommandSmallScaleDeploymentsPreview tests parsing of real Deployments API engine events
// from the small-scale-10 fixture. This is the same drift scenario as TestNextCommandSmallScaleRealPreview
// but uses engine events downloaded from a Pulumi Deployments preview (via /preview/{updateID}/events)
// rather than local `pulumi preview --json` output. The event format differs: engine events use
// resourcePreEvent/resOutputsEvent wrappers with metadata.op, whereas preview JSON uses steps[].op.
func TestNextCommandSmallScaleDeploymentsPreview(t *testing.T) {
	summary, full := runProcessTestFile(t, "testdata/small_scale_10_deployments.json")

	assert.Equal(t, "changes_needed", summary.Status)
	require.Len(t, full.Resources, 5, "Expected 5 drifted resources")

	resourceMap := make(map[string]*ResourceChange)
	for i := range full.Resources {
		resourceMap[full.Resources[i].Name] = &full.Resources[i]
	}

	assert.Equal(t, "delete_from_code", resourceMap["cmd-3"].Action, "create -> delete_from_code")
	assert.Equal(t, "add_to_code", resourceMap["random-str-extra-0"].Action, "delete -> add_to_code")
	assert.Equal(t, "update_code", resourceMap["cmd-0"].Action, "update -> update_code")
	assert.Equal(t, "update_code", resourceMap["random-str-0"].Action, "replace -> update_code")
	assert.Equal(t, "update_code", resourceMap["tls-key-0"].Action, "replace -> update_code")

	for _, res := range full.Resources {
		if res.Action == "update_code" {
			require.NotEmpty(t, res.Properties,
				"Resource %s (%s) with action update_code must have non-empty Properties", res.Name, res.Type)
		}
	}

	randomStr := resourceMap["random-str-0"]
	require.NotNil(t, randomStr)
	propMap := make(map[string]*PropertyChange)
	for i := range randomStr.Properties {
		propMap[randomStr.Properties[i].Path] = &randomStr.Properties[i]
	}

	lengthProp := propMap["length"]
	require.NotNil(t, lengthProp, "length property not found on random-str-0")
	assert.Equal(t, float64(16), lengthProp.CurrentValue)
	assert.Equal(t, float64(32), lengthProp.DesiredValue)

	specialProp := propMap["special"]
	require.NotNil(t, specialProp, "special property not found on random-str-0")
	assert.Equal(t, false, specialProp.CurrentValue)
	assert.Equal(t, true, specialProp.DesiredValue)
}

// TestNextCommandBackwardCompatibility ensures old JSON format still works
func TestNextCommandBackwardCompatibility(t *testing.T) {
	eventsContent := `{
		"steps": [
			{
				"op": "update",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::legacy-bucket",
				"oldState": {
					"type": "aws:s3/bucket:Bucket",
					"inputs": {
						"versioning": {
							"enabled": false
						}
					},
					"outputs": {
						"versioning": {
							"enabled": false
						}
					}
				},
				"newState": {
					"type": "aws:s3/bucket:Bucket",
					"inputs": {
						"versioning": {
							"enabled": true
						}
					},
					"outputs": {
						"versioning": {
							"enabled": true
						}
					}
				},
				"detailedDiff": {
					"versioning.enabled": {
						"kind": "update"
					}
				}
			}
		]
	}`

	summary, full := runProcessTest(t, []byte(eventsContent))

	assert.Equal(t, "changes_needed", summary.Status)
	require.Len(t, full.Resources, 1)

	resource := full.Resources[0]
	assert.Equal(t, "update_code", resource.Action)
	assert.Equal(t, "legacy-bucket", resource.Name)
	assert.Equal(t, "aws:s3/bucket:Bucket", resource.Type)

	require.Len(t, resource.Properties, 1)
	prop := resource.Properties[0]
	assert.Equal(t, "versioning.enabled", prop.Path)
	assert.Equal(t, true, prop.CurrentValue)
	assert.Equal(t, false, prop.DesiredValue)
}

// TestNextCommandFileNotFound tests error handling when events file doesn't exist
func TestNextCommandFileNotFound(t *testing.T) {
	// For error cases, capture stdout directly since runNextTest expects an output file
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := newRootCmd()
	cmd.SetArgs([]string{"next", "--events-file", "/tmp/non-existent-file.ndjson"})
	_ = cmd.Execute()

	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	var result NextSummaryOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err)

	assert.Equal(t, "error", result.Status)
	assert.Contains(t, result.Error, "failed to read events file")
}
