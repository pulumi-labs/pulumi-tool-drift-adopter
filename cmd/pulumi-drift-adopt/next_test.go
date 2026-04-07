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
	"path/filepath"
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
			name:           "empty steps array",
			eventsContent:  `{"steps": []}`,
			expectedStatus: "error",
			expectedError:  "preview output contains no steps",
		},
		{
			name:           "empty events array",
			eventsContent:  `{"events": []}`,
			expectedStatus: "error",
			expectedError:  "preview output contains no events",
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
			name: "delete operation - resource should be added to code",
			eventsContent: `{
				"steps": [{
					"op": "delete",
					"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::missing-bucket",
					"oldState": {
						"type": "aws:s3/bucket:Bucket",
						"outputs": {
							"bucket": "missing-bucket"
						}
					},
					"detailedDiff": {}
				}]
			}`,
			expectedStatus:  "changes_needed",
			expectResources: true,
		},
		{
			name:           "invalid JSON",
			eventsContent:  `{invalid json`,
			expectedStatus: "error",
			expectedError:  "failed to parse preview output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary events file
			tmpDir := t.TempDir()
			eventsFile := filepath.Join(tmpDir, "events.ndjson")
			err := os.WriteFile(eventsFile, []byte(tt.eventsContent), 0644)
			require.NoError(t, err, "Failed to create test events file")

			// Capture stdout
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			// Run the command
			cmd := newRootCmd()
			cmd.SetArgs([]string{"next", "--events-file", eventsFile})
			_ = cmd.Execute()

			// Restore stdout and read captured output
			_ = w.Close()
			os.Stdout = oldStdout
			var output []byte
			output, err = io.ReadAll(r)
			require.NoError(t, err, "Failed to read captured output")

			// Parse JSON output
			var result NextOutput
			err = json.Unmarshal(output, &result)
			require.NoError(t, err, "Failed to parse output JSON: %s", string(output))

			// Verify status
			assert.Equal(t, tt.expectedStatus, result.Status, "Status mismatch")

			// Verify error message if expected
			if tt.expectedError != "" {
				assert.Contains(t, result.Error, tt.expectedError, "Error message mismatch")
			}

			// Verify resources presence
			if tt.expectResources {
				assert.NotEmpty(t, result.Resources, "Expected resources but got none")
			} else {
				assert.Empty(t, result.Resources, "Expected no resources but got some")
			}
		})
	}
}

// TestNextCommandActionMapping tests that operations are correctly inverted for drift adoption
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
						"outputs": {"bucket": "test-bucket"}
					},
					"newState": {
						"type": "aws:s3/bucket:Bucket",
						"outputs": {"bucket": "test-bucket"}
					},
					"detailedDiff": {}
				}]
			}`

			tmpDir := t.TempDir()
			eventsFile := filepath.Join(tmpDir, "events.ndjson")
			err := os.WriteFile(eventsFile, []byte(eventsContent), 0644)
			require.NoError(t, err)

			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			cmd := newRootCmd()
			cmd.SetArgs([]string{"next", "--events-file", eventsFile})
			_ = cmd.Execute()

			_ = w.Close()
			os.Stdout = oldStdout
			output, err := io.ReadAll(r)
			require.NoError(t, err)

			var result NextOutput
			err = json.Unmarshal(output, &result)
			require.NoError(t, err)

			assert.Equal(t, "changes_needed", result.Status)
			require.NotEmpty(t, result.Resources)
			assert.Equal(t, tt.expectedAction, result.Resources[0].Action)
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

	tmpDir := t.TempDir()
	eventsFile := filepath.Join(tmpDir, "events.ndjson")
	err := os.WriteFile(eventsFile, []byte(eventsContent), 0644)
	require.NoError(t, err)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := newRootCmd()
	cmd.SetArgs([]string{"next", "--events-file", eventsFile})
	_ = cmd.Execute()

	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err)

	assert.Equal(t, "changes_needed", result.Status)
	require.Len(t, result.Resources, 1)

	resource := result.Resources[0]
	assert.Equal(t, "update_code", resource.Action)
	assert.Equal(t, "aws:s3/bucket:Bucket", resource.Type)
	assert.Equal(t, "my-bucket", resource.Name)
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

// TestNextCommandFileNotFound tests error handling when events file doesn't exist
func TestNextCommandFileNotFound(t *testing.T) {
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

	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err)

	assert.Equal(t, "error", result.Status)
	assert.Contains(t, result.Error, "failed to read events file")
}

// TestNextCommandNDJSONRealFormat tests parsing actual NDJSON with full engine event structure
func TestNextCommandNDJSONRealFormat(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := newRootCmd()
	cmd.SetArgs([]string{"next", "--events-file", "testdata/ndjson_update.ndjson"})
	_ = cmd.Execute()

	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	assert.Equal(t, "changes_needed", result.Status)
	require.Len(t, result.Resources, 1)

	resource := result.Resources[0]
	assert.Equal(t, "update_code", resource.Action)
	assert.Equal(t, "my-bucket", resource.Name)
	assert.Equal(t, "aws:s3/bucket:Bucket", resource.Type)

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
	assert.Nil(t, managedByProp.CurrentValue)
	assert.Equal(t, "pulumi", managedByProp.DesiredValue)
}

// TestNextCommandNDJSONMixedEvents tests that non-resourcePreEvent lines are properly skipped
func TestNextCommandNDJSONMixedEvents(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := newRootCmd()
	cmd.SetArgs([]string{"next", "--events-file", "testdata/ndjson_with_diagnostics.ndjson"})
	_ = cmd.Execute()

	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	assert.Equal(t, "changes_needed", result.Status)
	require.Len(t, result.Resources, 1)

	resource := result.Resources[0]
	assert.Equal(t, "update_code", resource.Action)
	assert.Equal(t, "test-bucket", resource.Name)

	require.Len(t, resource.Properties, 1)
	prop := resource.Properties[0]
	assert.Equal(t, "versioning.enabled", prop.Path)
	assert.Equal(t, false, prop.CurrentValue)
	assert.Equal(t, true, prop.DesiredValue)
}

// TestNextCommandNDJSONEmptyFile tests NDJSON with no resource events returns clean
func TestNextCommandNDJSONEmptyFile(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := newRootCmd()
	cmd.SetArgs([]string{"next", "--events-file", "testdata/ndjson_empty.ndjson"})
	_ = cmd.Execute()

	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	assert.Equal(t, "clean", result.Status)
	assert.Empty(t, result.Resources)
}

// TestNextCommandNDJSONMultipleResources tests parsing multiple resources from NDJSON
func TestNextCommandNDJSONMultipleResources(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := newRootCmd()
	cmd.SetArgs([]string{"next", "--events-file", "testdata/ndjson_multiple_resources.ndjson"})
	_ = cmd.Execute()

	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	assert.Equal(t, "changes_needed", result.Status)
	assert.Len(t, result.Resources, 3, "Resource count mismatch")

	if len(result.Resources) > 0 {
		resource := result.Resources[0]
		assert.Equal(t, "update_code", resource.Action)
		assert.Equal(t, "bucket-1", resource.Name)
		assert.Equal(t, "aws:s3/bucket:Bucket", resource.Type)
	}
}

// TestNextCommandNDJSONCreateDelete tests create and delete operations from NDJSON
func TestNextCommandNDJSONCreateDelete(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := newRootCmd()
	cmd.SetArgs([]string{"next", "--events-file", "testdata/ndjson_create_delete.ndjson"})
	_ = cmd.Execute()

	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	assert.Equal(t, "changes_needed", result.Status)
	require.Len(t, result.Resources, 2)

	var createResource, deleteResource *ResourceChange
	for i := range result.Resources {
		if result.Resources[i].Name == "extra-bucket" {
			createResource = &result.Resources[i]
		}
		if result.Resources[i].Name == "missing-bucket" {
			deleteResource = &result.Resources[i]
		}
	}

	require.NotNil(t, createResource, "extra-bucket not found")
	assert.Equal(t, "delete_from_code", createResource.Action)

	require.NotNil(t, deleteResource, "missing-bucket not found")
	assert.Equal(t, "add_to_code", deleteResource.Action)
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
					"outputs": {
						"versioning": {
							"enabled": false
						}
					}
				},
				"newState": {
					"type": "aws:s3/bucket:Bucket",
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

	tmpDir := t.TempDir()
	eventsFile := filepath.Join(tmpDir, "events.json")
	err := os.WriteFile(eventsFile, []byte(eventsContent), 0644)
	require.NoError(t, err)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := newRootCmd()
	cmd.SetArgs([]string{"next", "--events-file", eventsFile})
	_ = cmd.Execute()

	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	assert.Equal(t, "changes_needed", result.Status)
	require.Len(t, result.Resources, 1)

	resource := result.Resources[0]
	assert.Equal(t, "update_code", resource.Action)
	assert.Equal(t, "legacy-bucket", resource.Name)

	require.Len(t, resource.Properties, 1)
	prop := resource.Properties[0]
	assert.Equal(t, "versioning.enabled", prop.Path)
	assert.Equal(t, true, prop.CurrentValue)
	assert.Equal(t, false, prop.DesiredValue)
}

// TestNextCommandRealPulumiServiceNDJSON tests parsing of real NDJSON from pulumi-service
func TestNextCommandRealPulumiServiceNDJSON(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := newRootCmd()
	cmd.SetArgs([]string{"next", "--events-file", "testdata/simple-s3-drift.ndjson"})
	_ = cmd.Execute()

	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	assert.Equal(t, "changes_needed", result.Status, "Expected drift to be detected")
	require.NotEmpty(t, result.Resources, "Expected at least one resource with drift")

	var bucketResource *ResourceChange
	for i := range result.Resources {
		if result.Resources[i].Name == "test-bucket" {
			bucketResource = &result.Resources[i]
			break
		}
	}

	require.NotNil(t, bucketResource, "Expected to find S3 bucket resource")
	assert.Equal(t, "update_code", bucketResource.Action)
	assert.Contains(t, bucketResource.URN, "aws:s3/bucket:Bucket")

	var tagChanges []*PropertyChange
	for i := range bucketResource.Properties {
		if len(bucketResource.Properties[i].Path) >= 4 && bucketResource.Properties[i].Path[:4] == "tags" {
			tagChanges = append(tagChanges, &bucketResource.Properties[i])
		}
	}
	require.NotEmpty(t, tagChanges, "Expected tag changes to be detected")
}
