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
			name:            "clean state - no changes",
			eventsContent:   `{"steps": []}`,
			expectedStatus:  "clean",
			expectResources: false,
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
			rootCmd.SetArgs([]string{"next", "--events-file", eventsFile})
			_ = rootCmd.Execute()

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
						"outputs": {"bucket": "test-bucket", "tags": {"Env": "prod"}}
					},
					"newState": {
						"type": "aws:s3/bucket:Bucket",
						"outputs": {"bucket": "test-bucket", "tags": {"Env": "dev"}}
					},
					"detailedDiff": {"tags.Env": {"kind": "update"}}
				}]
			}`

			// Create temporary events file
			tmpDir := t.TempDir()
			eventsFile := filepath.Join(tmpDir, "events.ndjson")
			err := os.WriteFile(eventsFile, []byte(eventsContent), 0644)
			require.NoError(t, err)

			// Capture stdout
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			// Run the command
			rootCmd.SetArgs([]string{"next", "--events-file", eventsFile})
			_ = rootCmd.Execute()

			// Restore stdout and read output
			_ = w.Close()
			os.Stdout = oldStdout
			output, err := io.ReadAll(r)
			require.NoError(t, err)

			// Parse output
			var result NextOutput
			err = json.Unmarshal(output, &result)
			require.NoError(t, err)

			// Verify action — for create/delete, resource is in Resources;
			// for update/replace, detailedDiff provides properties so it's also in Resources
			assert.Equal(t, "changes_needed", result.Status)
			require.NotEmpty(t, result.Resources)
			assert.Equal(t, tt.expectedAction, result.Resources[0].Action)
		})
	}
}

// TestNextCommandMaxResourcesLimit tests the --max-resources flag with events file
func TestNextCommandMaxResourcesLimit(t *testing.T) {
	// Create events with 5 resources, each with a property change so they remain actionable
	eventsContent := `{
		"steps": [
			{
				"op": "update",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket1",
				"oldState": {"type": "aws:s3/bucket:Bucket", "outputs": {"tags": {"Env": "prod"}}},
				"newState": {"type": "aws:s3/bucket:Bucket", "outputs": {"tags": {"Env": "dev"}}},
				"detailedDiff": {"tags.Env": {"kind": "update"}}
			},
			{
				"op": "update",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket2",
				"oldState": {"type": "aws:s3/bucket:Bucket", "outputs": {"tags": {"Env": "prod"}}},
				"newState": {"type": "aws:s3/bucket:Bucket", "outputs": {"tags": {"Env": "dev"}}},
				"detailedDiff": {"tags.Env": {"kind": "update"}}
			},
			{
				"op": "update",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket3",
				"oldState": {"type": "aws:s3/bucket:Bucket", "outputs": {"tags": {"Env": "prod"}}},
				"newState": {"type": "aws:s3/bucket:Bucket", "outputs": {"tags": {"Env": "dev"}}},
				"detailedDiff": {"tags.Env": {"kind": "update"}}
			},
			{
				"op": "update",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket4",
				"oldState": {"type": "aws:s3/bucket:Bucket", "outputs": {"tags": {"Env": "prod"}}},
				"newState": {"type": "aws:s3/bucket:Bucket", "outputs": {"tags": {"Env": "dev"}}},
				"detailedDiff": {"tags.Env": {"kind": "update"}}
			},
			{
				"op": "update",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket5",
				"oldState": {"type": "aws:s3/bucket:Bucket", "outputs": {"tags": {"Env": "prod"}}},
				"newState": {"type": "aws:s3/bucket:Bucket", "outputs": {"tags": {"Env": "dev"}}},
				"detailedDiff": {"tags.Env": {"kind": "update"}}
			}
		]
	}`

	tests := []struct {
		name          string
		maxResources  string
		expectedCount int
	}{
		{
			name:          "limit to 2 resources",
			maxResources:  "2",
			expectedCount: 2,
		},
		{
			name:          "no limit (-1)",
			maxResources:  "-1",
			expectedCount: 5,
		},
		{
			name:          "default limit (unlimited) - all resources returned",
			maxResources:  "",
			expectedCount: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary events file
			tmpDir := t.TempDir()
			eventsFile := filepath.Join(tmpDir, "events.ndjson")
			err := os.WriteFile(eventsFile, []byte(eventsContent), 0644)
			require.NoError(t, err)

			// Capture stdout
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			// Build args
			args := []string{"next", "--events-file", eventsFile}
			if tt.maxResources != "" {
				args = append(args, "--max-resources", tt.maxResources)
			}

			// Run the command
			rootCmd.SetArgs(args)
			_ = rootCmd.Execute()

			// Restore stdout and read output
			_ = w.Close()
			os.Stdout = oldStdout
			output, err := io.ReadAll(r)
			require.NoError(t, err)

			// Parse output
			var result NextOutput
			err = json.Unmarshal(output, &result)
			require.NoError(t, err)

			// Verify resource count
			assert.Equal(t, "changes_needed", result.Status)
			assert.Len(t, result.Resources, tt.expectedCount, "Resource count mismatch")
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

	// Create temporary events file
	tmpDir := t.TempDir()
	eventsFile := filepath.Join(tmpDir, "events.ndjson")
	err := os.WriteFile(eventsFile, []byte(eventsContent), 0644)
	require.NoError(t, err)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run the command
	rootCmd.SetArgs([]string{"next", "--events-file", eventsFile})
	_ = rootCmd.Execute()

	// Restore stdout and read output
	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	// Parse output
	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err)

	// Verify output structure
	assert.Equal(t, "changes_needed", result.Status)
	require.Len(t, result.Resources, 1)

	resource := result.Resources[0]
	assert.Equal(t, "update_code", resource.Action)
	assert.Equal(t, "aws:s3/bucket:Bucket", resource.Type)
	assert.Equal(t, "my-bucket", resource.Name)

	// Verify properties
	assert.Len(t, resource.Properties, 2)

	// Find and verify each property
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
	assert.Equal(t, "update", envProp.Kind)

	require.NotNil(t, versioningProp, "Versioning property not found")
	assert.Equal(t, false, versioningProp.CurrentValue)
	assert.Equal(t, true, versioningProp.DesiredValue)
	assert.Equal(t, "update", versioningProp.Kind)
}

// TestNextCommandFileNotFound tests error handling when events file doesn't exist
func TestNextCommandFileNotFound(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run the command with non-existent file
	rootCmd.SetArgs([]string{"next", "--events-file", "/tmp/non-existent-file.ndjson"})
	_ = rootCmd.Execute()

	// Restore stdout and read output
	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	// Parse output
	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err)

	// Verify error status
	assert.Equal(t, "error", result.Status)
	assert.Contains(t, result.Error, "failed to read events file")
}

// TestNextCommandNDJSONRealFormat tests parsing actual NDJSON with full engine event structure
func TestNextCommandNDJSONRealFormat(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run command with realistic NDJSON fixture
	rootCmd.SetArgs([]string{"next", "--events-file", "testdata/ndjson_update.ndjson"})
	_ = rootCmd.Execute()

	// Restore stdout and read output
	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	// Parse output
	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	// Verify status and resource
	assert.Equal(t, "changes_needed", result.Status)
	require.Len(t, result.Resources, 1)

	resource := result.Resources[0]
	assert.Equal(t, "update_code", resource.Action)
	assert.Equal(t, "my-bucket", resource.Name)
	assert.Equal(t, "aws:s3/bucket:Bucket", resource.Type)
	assert.Contains(t, resource.URN, "my-bucket")

	// Verify properties - should have 2 changes (update + delete)
	require.Len(t, resource.Properties, 2)

	// Find properties by path
	var envProp, managedByProp *PropertyChange
	for i := range resource.Properties {
		if resource.Properties[i].Path == "tags.Environment" {
			envProp = &resource.Properties[i]
		}
		if resource.Properties[i].Path == "tags.ManagedBy" {
			managedByProp = &resource.Properties[i]
		}
	}

	// Verify tags.Environment update
	require.NotNil(t, envProp, "tags.Environment property not found")
	assert.Equal(t, "dev", envProp.CurrentValue)
	assert.Equal(t, "production", envProp.DesiredValue)
	assert.Equal(t, "update", envProp.Kind)

	// Verify tags.ManagedBy addition (preview says "delete" but we invert to "add" for code changes)
	require.NotNil(t, managedByProp, "tags.ManagedBy property not found")
	assert.Equal(t, nil, managedByProp.CurrentValue)
	assert.Equal(t, "pulumi", managedByProp.DesiredValue)
	assert.Equal(t, "add", managedByProp.Kind)
}

// TestNextCommandNDJSONMixedEvents tests that non-resourcePreEvent lines are properly skipped
func TestNextCommandNDJSONMixedEvents(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run command with NDJSON containing diagnostics and policy events
	rootCmd.SetArgs([]string{"next", "--events-file", "testdata/ndjson_with_diagnostics.ndjson"})
	_ = rootCmd.Execute()

	// Restore stdout and read output
	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	// Parse output
	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	// Should only have one resource (the resourcePreEvent), others skipped
	assert.Equal(t, "changes_needed", result.Status)
	require.Len(t, result.Resources, 1)

	resource := result.Resources[0]
	assert.Equal(t, "update_code", resource.Action)
	assert.Equal(t, "test-bucket", resource.Name)
	assert.Equal(t, "aws:s3/bucket:Bucket", resource.Type)

	// Verify property change
	require.Len(t, resource.Properties, 1)
	prop := resource.Properties[0]
	assert.Equal(t, "versioning.enabled", prop.Path)
	assert.Equal(t, false, prop.CurrentValue)
	assert.Equal(t, true, prop.DesiredValue)
	assert.Equal(t, "update", prop.Kind)
}

// TestNextCommandNDJSONEmptyFile tests NDJSON with no resource events returns clean
func TestNextCommandNDJSONEmptyFile(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run command with NDJSON containing only metadata events
	rootCmd.SetArgs([]string{"next", "--events-file", "testdata/ndjson_empty.ndjson"})
	_ = rootCmd.Execute()

	// Restore stdout and read output
	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	// Parse output
	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	// Should return clean status with no resources
	assert.Equal(t, "clean", result.Status)
	assert.Empty(t, result.Resources)
}

// TestNextCommandNDJSONMultipleResources tests parsing multiple resources from NDJSON
func TestNextCommandNDJSONMultipleResources(t *testing.T) {
	tests := []struct {
		name          string
		maxResources  string
		expectedCount int
	}{
		{
			name:          "all resources (default limit)",
			maxResources:  "",
			expectedCount: 3,
		},
		{
			name:          "limited to 2 resources",
			maxResources:  "2",
			expectedCount: 2,
		},
		{
			name:          "unlimited (-1)",
			maxResources:  "-1",
			expectedCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture stdout
			oldStdout := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			// Build args
			args := []string{"next", "--events-file", "testdata/ndjson_multiple_resources.ndjson"}
			if tt.maxResources != "" {
				args = append(args, "--max-resources", tt.maxResources)
			}

			// Run command
			rootCmd.SetArgs(args)
			_ = rootCmd.Execute()

			// Restore stdout and read output
			_ = w.Close()
			os.Stdout = oldStdout
			output, err := io.ReadAll(r)
			require.NoError(t, err)

			// Parse output
			var result NextOutput
			err = json.Unmarshal(output, &result)
			require.NoError(t, err, "Failed to parse output: %s", string(output))

			// Verify resource count
			assert.Equal(t, "changes_needed", result.Status)
			assert.Len(t, result.Resources, tt.expectedCount, "Resource count mismatch")

			// Verify first resource details if present
			if len(result.Resources) > 0 {
				resource := result.Resources[0]
				assert.Equal(t, "update_code", resource.Action)
				assert.Equal(t, "bucket-1", resource.Name)
				assert.Equal(t, "aws:s3/bucket:Bucket", resource.Type)
			}
		})
	}
}

// TestNextCommandNDJSONCreateDelete tests create and delete operations from NDJSON
func TestNextCommandNDJSONCreateDelete(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run command with NDJSON containing create and delete operations
	rootCmd.SetArgs([]string{"next", "--events-file", "testdata/ndjson_create_delete.ndjson"})
	_ = rootCmd.Execute()

	// Restore stdout and read output
	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	// Parse output
	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	// Should have 2 resources (1 create, 1 delete)
	assert.Equal(t, "changes_needed", result.Status)
	require.Len(t, result.Resources, 2)

	// Find resources by name
	var createResource, deleteResource *ResourceChange
	for i := range result.Resources {
		if result.Resources[i].Name == "extra-bucket" {
			createResource = &result.Resources[i]
		}
		if result.Resources[i].Name == "missing-bucket" {
			deleteResource = &result.Resources[i]
		}
	}

	// Verify create operation inverts to delete_from_code
	require.NotNil(t, createResource, "extra-bucket not found")
	assert.Equal(t, "delete_from_code", createResource.Action)
	assert.Equal(t, "aws:s3/bucket:Bucket", createResource.Type)

	// Verify delete operation inverts to add_to_code
	require.NotNil(t, deleteResource, "missing-bucket not found")
	assert.Equal(t, "add_to_code", deleteResource.Action)
	assert.Equal(t, "aws:s3/bucket:Bucket", deleteResource.Type)
}

// TestNextCommandNDJSONReplace tests that replace operations extract properties from Diffs field
func TestNextCommandNDJSONReplace(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run command with NDJSON containing replace operations
	rootCmd.SetArgs([]string{"next", "--events-file", "testdata/ndjson_replace.ndjson"})
	_ = rootCmd.Execute()

	// Restore stdout and read output
	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	// Parse output
	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	// Should have 2 replace resources
	assert.Equal(t, "changes_needed", result.Status)
	require.Len(t, result.Resources, 2)

	// Find resources by name
	var randomString, privateKey *ResourceChange
	for i := range result.Resources {
		if result.Resources[i].Name == "my-random-string" {
			randomString = &result.Resources[i]
		}
		if result.Resources[i].Name == "my-private-key" {
			privateKey = &result.Resources[i]
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
	assert.Equal(t, "update", lengthProp.Kind)
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
	assert.Equal(t, "update", algoProp.Kind)
	// Old inputs has algorithm="RSA" (desired), new inputs has algorithm="ECDSA" (current)
	assert.Equal(t, "ECDSA", algoProp.CurrentValue)
	assert.Equal(t, "RSA", algoProp.DesiredValue)
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

	tmpDir := t.TempDir()
	eventsFile := filepath.Join(tmpDir, "events.json")
	err := os.WriteFile(eventsFile, []byte(eventsContent), 0644)
	require.NoError(t, err)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	rootCmd.SetArgs([]string{"next", "--events-file", eventsFile})
	_ = rootCmd.Execute()

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
	require.NotEmpty(t, resource.Properties, "Replace resource should have properties")

	// Find length property - values come from Outputs first, then fallback to Inputs
	var lengthProp *PropertyChange
	for i := range resource.Properties {
		if resource.Properties[i].Path == "length" {
			lengthProp = &resource.Properties[i]
		}
	}
	require.NotNil(t, lengthProp, "length property not found")
	assert.Equal(t, "update", lengthProp.Kind)
	assert.Equal(t, float64(32), lengthProp.CurrentValue)
	assert.Equal(t, float64(16), lengthProp.DesiredValue)
}

// TestNextCommandBackwardCompatibility ensures old JSON format still works
func TestNextCommandBackwardCompatibility(t *testing.T) {
	// Create events in old format (single JSON object with steps array)
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

	// Create temporary events file
	tmpDir := t.TempDir()
	eventsFile := filepath.Join(tmpDir, "events.json")
	err := os.WriteFile(eventsFile, []byte(eventsContent), 0644)
	require.NoError(t, err)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run command
	rootCmd.SetArgs([]string{"next", "--events-file", eventsFile})
	_ = rootCmd.Execute()

	// Restore stdout and read output
	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	// Parse output
	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	// Verify old format still works
	assert.Equal(t, "changes_needed", result.Status)
	require.Len(t, result.Resources, 1)

	resource := result.Resources[0]
	assert.Equal(t, "update_code", resource.Action)
	assert.Equal(t, "legacy-bucket", resource.Name)
	assert.Equal(t, "aws:s3/bucket:Bucket", resource.Type)

	// Verify property extraction still works
	require.Len(t, resource.Properties, 1)
	prop := resource.Properties[0]
	assert.Equal(t, "versioning.enabled", prop.Path)
	assert.Equal(t, true, prop.CurrentValue)
	assert.Equal(t, false, prop.DesiredValue)
	assert.Equal(t, "update", prop.Kind)
}

// TestNextCommandReplaceWithNullDetailedDiff tests that standard JSON replace ops with null detailedDiff
// still produce property changes by falling back to replaceReasons/diffReasons.
func TestNextCommandReplaceWithNullDetailedDiff(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run command with standard JSON fixture containing a replace with null detailedDiff
	rootCmd.SetArgs([]string{"next", "--events-file", "testdata/standard_json_replace.json"})
	_ = rootCmd.Execute()

	// Restore stdout and read output
	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	// Parse output
	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	assert.Equal(t, "changes_needed", result.Status)
	require.Len(t, result.Resources, 3)

	// Find resources by name
	var replaceRes, updateRes, createRes *ResourceChange
	for i := range result.Resources {
		switch result.Resources[i].Name {
		case "tls-key-0":
			replaceRes = &result.Resources[i]
		case "cmd-4":
			updateRes = &result.Resources[i]
		case "cmd-38":
			createRes = &result.Resources[i]
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
	assert.Equal(t, "update", algoProp.Kind)
	// NewState.inputs has the current code value, OldState.inputs has the desired state value
	assert.Equal(t, "RSA", algoProp.CurrentValue)
	assert.Equal(t, "ECDSA", algoProp.DesiredValue)

	require.NotNil(t, ecdsaProp, "ecdsaCurve property not found")
	// ecdsaCurve is in OldState.inputs (desired) but not NewState.inputs (current) -> kind=delete
	assert.Equal(t, "delete", ecdsaProp.Kind)
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

	tmpDir := t.TempDir()
	eventsFile := filepath.Join(tmpDir, "events.json")
	err := os.WriteFile(eventsFile, []byte(eventsContent), 0644)
	require.NoError(t, err)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	rootCmd.SetArgs([]string{"next", "--events-file", eventsFile})
	_ = rootCmd.Execute()

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

	// Should only have algorithm and ecdsaCurve (from replaceReasons), NOT id or publicKeyPem
	propertyPaths := make(map[string]bool)
	for _, prop := range resource.Properties {
		propertyPaths[prop.Path] = true
	}

	assert.True(t, propertyPaths["algorithm"], "algorithm should be in properties")
	assert.True(t, propertyPaths["ecdsaCurve"], "ecdsaCurve should be in properties")
	assert.False(t, propertyPaths["id"], "id (output-only) should NOT be in properties")
	assert.False(t, propertyPaths["publicKeyPem"], "publicKeyPem (output-only) should NOT be in properties")

	// Verify values
	for _, prop := range resource.Properties {
		if prop.Path == "algorithm" {
			assert.Equal(t, "update", prop.Kind)
			assert.Equal(t, "RSA", prop.CurrentValue)
			assert.Equal(t, "ECDSA", prop.DesiredValue)
		}
		if prop.Path == "ecdsaCurve" {
			assert.Equal(t, "delete", prop.Kind)
			assert.Nil(t, prop.CurrentValue)
			assert.Equal(t, "P256", prop.DesiredValue)
		}
	}
}

// TestNextCommandEngineEventsJSON tests parsing the Pulumi Cloud GetEngineEvents API format:
// {"events": [...], "continuationToken": ...} where each event has the same structure as NDJSON lines.
func TestNextCommandEngineEventsJSON(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run command with engine events JSON fixture
	rootCmd.SetArgs([]string{"next", "--events-file", "testdata/engine_events_update.json"})
	_ = rootCmd.Execute()

	// Restore stdout and read output
	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	// Parse output
	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	// Verify status and resource
	assert.Equal(t, "changes_needed", result.Status)
	require.Len(t, result.Resources, 1)

	resource := result.Resources[0]
	assert.Equal(t, "update_code", resource.Action)
	assert.Equal(t, "my-bucket", resource.Name)
	assert.Equal(t, "aws:s3/bucket:Bucket", resource.Type)
	assert.Contains(t, resource.URN, "my-bucket")

	// Verify properties - should have 2 changes (update + delete → add after inversion)
	require.Len(t, resource.Properties, 2)

	// Find properties by path
	var envProp, managedByProp *PropertyChange
	for i := range resource.Properties {
		if resource.Properties[i].Path == "tags.Environment" {
			envProp = &resource.Properties[i]
		}
		if resource.Properties[i].Path == "tags.ManagedBy" {
			managedByProp = &resource.Properties[i]
		}
	}

	// Verify tags.Environment update
	require.NotNil(t, envProp, "tags.Environment property not found")
	assert.Equal(t, "dev", envProp.CurrentValue)
	assert.Equal(t, "production", envProp.DesiredValue)
	assert.Equal(t, "update", envProp.Kind)

	// Verify tags.ManagedBy addition (preview says "delete" but we invert to "add" for code changes)
	require.NotNil(t, managedByProp, "tags.ManagedBy property not found")
	assert.Equal(t, nil, managedByProp.CurrentValue)
	assert.Equal(t, "pulumi", managedByProp.DesiredValue)
	assert.Equal(t, "add", managedByProp.Kind)
}

// TestNextCommandSmallScaleRealPreview tests parsing of real `pulumi preview --json` output from the
// small-scale-10 fixture. This fixture has 5 drifted resources: 2 replaces (random-str-0, tls-key-0),
// 1 update (cmd-0), 1 create (cmd-3), 1 delete (random-str-extra-0).
// The key assertion is that every update_code resource has non-empty Properties.
func TestNextCommandSmallScaleRealPreview(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run command with real preview JSON fixture (unlimited resources)
	rootCmd.SetArgs([]string{"next", "--events-file", "testdata/small_scale_10_replace.json"})
	_ = rootCmd.Execute()

	// Restore stdout and read output
	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	// Parse output
	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	t.Logf("Output: %s", string(output))

	// Should have 5 drifted resources (same, providers, etc. are filtered out)
	assert.Equal(t, "changes_needed", result.Status)
	require.Len(t, result.Resources, 5, "Expected 5 drifted resources")

	// Build resource map by name
	resourceMap := make(map[string]*ResourceChange)
	for i := range result.Resources {
		resourceMap[result.Resources[i].Name] = &result.Resources[i]
	}

	// Verify correct actions
	assert.Equal(t, "delete_from_code", resourceMap["cmd-3"].Action, "create -> delete_from_code")
	assert.Equal(t, "add_to_code", resourceMap["random-str-extra-0"].Action, "delete -> add_to_code")
	assert.Equal(t, "update_code", resourceMap["cmd-0"].Action, "update -> update_code")
	assert.Equal(t, "update_code", resourceMap["random-str-0"].Action, "replace -> update_code")
	assert.Equal(t, "update_code", resourceMap["tls-key-0"].Action, "replace -> update_code")

	// KEY ASSERTION: every update_code resource must have non-empty Properties
	for _, res := range result.Resources {
		if res.Action == "update_code" {
			require.NotEmpty(t, res.Properties,
				"Resource %s (%s) with action update_code must have non-empty Properties", res.Name, res.Type)
		}
	}

	// Spot-check random-str-0 replace properties: length (32→16), special (true→false)
	randomStr := resourceMap["random-str-0"]
	require.NotNil(t, randomStr)
	propMap := make(map[string]*PropertyChange)
	for i := range randomStr.Properties {
		propMap[randomStr.Properties[i].Path] = &randomStr.Properties[i]
	}

	lengthProp := propMap["length"]
	require.NotNil(t, lengthProp, "length property not found on random-str-0")
	assert.Equal(t, float64(16), lengthProp.CurrentValue, "random-str-0 length currentValue (from newState.inputs)")
	assert.Equal(t, float64(32), lengthProp.DesiredValue, "random-str-0 length desiredValue (from oldState.inputs)")

	specialProp := propMap["special"]
	require.NotNil(t, specialProp, "special property not found on random-str-0")
	assert.Equal(t, false, specialProp.CurrentValue, "random-str-0 special currentValue")
	assert.Equal(t, true, specialProp.DesiredValue, "random-str-0 special desiredValue")

	// Spot-check tls-key-0 replace properties: algorithm (ECDSA→RSA), ecdsaCurve (P256→nil)
	tlsKey := resourceMap["tls-key-0"]
	require.NotNil(t, tlsKey)
	tlsPropMap := make(map[string]*PropertyChange)
	for i := range tlsKey.Properties {
		tlsPropMap[tlsKey.Properties[i].Path] = &tlsKey.Properties[i]
	}

	algoProp := tlsPropMap["algorithm"]
	require.NotNil(t, algoProp, "algorithm property not found on tls-key-0")
	assert.Equal(t, "RSA", algoProp.CurrentValue, "tls-key-0 algorithm currentValue (from newState.inputs)")
	assert.Equal(t, "ECDSA", algoProp.DesiredValue, "tls-key-0 algorithm desiredValue (from oldState.inputs)")

	ecdsaProp := tlsPropMap["ecdsaCurve"]
	require.NotNil(t, ecdsaProp, "ecdsaCurve property not found on tls-key-0")
	assert.Equal(t, "delete", ecdsaProp.Kind, "ecdsaCurve should be a delete (in old but not in new inputs)")
	assert.Nil(t, ecdsaProp.CurrentValue, "tls-key-0 ecdsaCurve currentValue should be nil")
	assert.Equal(t, "P256", ecdsaProp.DesiredValue, "tls-key-0 ecdsaCurve desiredValue")
}

// TestNextCommandSkipsIncompleteResources tests that update_code resources with empty properties
// are moved to the Skipped bucket with reason "missing_properties".
func TestNextCommandSkipsIncompleteResources(t *testing.T) {
	// A replace op with null detailedDiff and no replaceReasons/diffReasons
	// will produce empty properties — this should be skipped automatically.
	eventsContent := `{
		"steps": [
			{
				"op": "replace",
				"urn": "urn:pulumi:dev::test::aws:ec2/instance:Instance::incomplete-instance",
				"oldState": {
					"type": "aws:ec2/instance:Instance",
					"inputs": {"ami": "ami-old"},
					"outputs": {"ami": "ami-old", "id": "i-123"}
				},
				"newState": {
					"type": "aws:ec2/instance:Instance",
					"inputs": {"ami": "ami-new"},
					"outputs": {"ami": "ami-new"}
				},
				"detailedDiff": null
			},
			{
				"op": "update",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::good-bucket",
				"oldState": {
					"type": "aws:s3/bucket:Bucket",
					"outputs": {"tags": {"Env": "prod"}}
				},
				"newState": {
					"type": "aws:s3/bucket:Bucket",
					"outputs": {"tags": {"Env": "dev"}}
				},
				"detailedDiff": {
					"tags.Env": {"kind": "update"}
				}
			},
			{
				"op": "create",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::extra-bucket",
				"newState": {
					"type": "aws:s3/bucket:Bucket",
					"outputs": {"bucket": "extra"}
				},
				"detailedDiff": {}
			}
		]
	}`

	tmpDir := t.TempDir()
	eventsFile := filepath.Join(tmpDir, "events.json")
	err := os.WriteFile(eventsFile, []byte(eventsContent), 0644)
	require.NoError(t, err)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	rootCmd.SetArgs([]string{"next", "--events-file", eventsFile})
	_ = rootCmd.Execute()

	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	// The good-bucket (update with properties) and extra-bucket (delete_from_code) should be actionable
	assert.Equal(t, "changes_needed", result.Status)
	require.Len(t, result.Resources, 2, "Expected 2 actionable resources")

	// The incomplete replace should be in Skipped
	require.Len(t, result.Skipped, 1, "Expected 1 skipped resource")
	assert.Equal(t, "incomplete-instance", result.Skipped[0].Name)
	assert.Equal(t, "missing_properties", result.Skipped[0].Reason)
	assert.Equal(t, "update_code", result.Skipped[0].Action)
}

// TestNextCommandExcludeURNs tests the --exclude-urns flag moves excluded resources to Skipped.
func TestNextCommandExcludeURNs(t *testing.T) {
	eventsContent := `{
		"steps": [
			{
				"op": "update",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket-a",
				"oldState": {
					"type": "aws:s3/bucket:Bucket",
					"outputs": {"tags": {"Env": "prod"}}
				},
				"newState": {
					"type": "aws:s3/bucket:Bucket",
					"outputs": {"tags": {"Env": "dev"}}
				},
				"detailedDiff": {"tags.Env": {"kind": "update"}}
			},
			{
				"op": "update",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket-b",
				"oldState": {
					"type": "aws:s3/bucket:Bucket",
					"outputs": {"versioning": {"enabled": true}}
				},
				"newState": {
					"type": "aws:s3/bucket:Bucket",
					"outputs": {"versioning": {"enabled": false}}
				},
				"detailedDiff": {"versioning.enabled": {"kind": "update"}}
			}
		]
	}`

	tmpDir := t.TempDir()
	eventsFile := filepath.Join(tmpDir, "events.json")
	err := os.WriteFile(eventsFile, []byte(eventsContent), 0644)
	require.NoError(t, err)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	rootCmd.SetArgs([]string{
		"next", "--events-file", eventsFile,
		"--exclude-urns", "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket-a",
	})
	_ = rootCmd.Execute()

	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	// bucket-b should be actionable
	assert.Equal(t, "changes_needed", result.Status)
	require.Len(t, result.Resources, 1)
	assert.Equal(t, "bucket-b", result.Resources[0].Name)

	// bucket-a should be skipped with reason "excluded"
	require.Len(t, result.Skipped, 1)
	assert.Equal(t, "bucket-a", result.Skipped[0].Name)
	assert.Equal(t, "excluded", result.Skipped[0].Reason)
	assert.Equal(t, "update_code", result.Skipped[0].Action)
	// Excluded resources should retain their properties
	assert.NotEmpty(t, result.Skipped[0].Properties)
}

// TestNextCommandStopWithSkippedStatus tests that when all resources are skipped, status is "stop_with_skipped".
func TestNextCommandStopWithSkippedStatus(t *testing.T) {
	// Single resource that will have empty properties (incomplete)
	eventsContent := `{
		"steps": [{
			"op": "replace",
			"urn": "urn:pulumi:dev::test::aws:ec2/instance:Instance::only-resource",
			"oldState": {
				"type": "aws:ec2/instance:Instance",
				"inputs": {"ami": "ami-old"},
				"outputs": {"ami": "ami-old"}
			},
			"newState": {
				"type": "aws:ec2/instance:Instance",
				"inputs": {"ami": "ami-new"},
				"outputs": {"ami": "ami-new"}
			},
			"detailedDiff": null
		}]
	}`

	tmpDir := t.TempDir()
	eventsFile := filepath.Join(tmpDir, "events.json")
	err := os.WriteFile(eventsFile, []byte(eventsContent), 0644)
	require.NoError(t, err)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	rootCmd.SetArgs([]string{"next", "--events-file", eventsFile})
	_ = rootCmd.Execute()

	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	assert.Equal(t, "stop_with_skipped", result.Status)
	assert.Empty(t, result.Resources)
	require.Len(t, result.Skipped, 1)
	assert.Equal(t, "missing_properties", result.Skipped[0].Reason)
}

// TestNextCommandRealPulumiServiceNDJSON tests parsing of real NDJSON from pulumi-service integration test
// This reproduces the parsing bug found during integration testing where the tool reported:
// "failed to parse preview output: invalid character '{' after top-level value"
func TestNextCommandRealPulumiServiceNDJSON(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run command with real NDJSON file from pulumi-service integration test
	rootCmd.SetArgs([]string{"next", "--events-file", "testdata/simple-s3-drift.ndjson"})
	_ = rootCmd.Execute()

	// Restore stdout and read output
	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	// Parse output
	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	// Debug output
	t.Logf("Output: %s", string(output))
	t.Logf("Status: %s", result.Status)
	t.Logf("Error: %s", result.Error)
	t.Logf("Resource count: %d", len(result.Resources))

	// Should detect drift in S3 bucket tags
	assert.Equal(t, "changes_needed", result.Status, "Expected drift to be detected")
	require.NotEmpty(t, result.Resources, "Expected at least one resource with drift")

	// Find the S3 bucket resource
	// Note: Type field may be empty in NDJSON parsing (known issue)
	var bucketResource *ResourceChange
	for i := range result.Resources {
		if result.Resources[i].Name == "test-bucket" {
			bucketResource = &result.Resources[i]
			break
		}
	}

	require.NotNil(t, bucketResource, "Expected to find S3 bucket resource")
	assert.Equal(t, "update_code", bucketResource.Action)
	assert.Equal(t, "test-bucket", bucketResource.Name)
	assert.Contains(t, bucketResource.URN, "aws:s3/bucket:Bucket", "URN should contain resource type")

	// Verify tag changes are detected
	var tagChanges []*PropertyChange
	for i := range bucketResource.Properties {
		if len(bucketResource.Properties[i].Path) >= 4 && bucketResource.Properties[i].Path[:4] == "tags" {
			tagChanges = append(tagChanges, &bucketResource.Properties[i])
		}
	}

	require.NotEmpty(t, tagChanges, "Expected tag changes to be detected")
	t.Logf("Found %d tag changes", len(tagChanges))
	for _, change := range tagChanges {
		t.Logf("  - %s: current=%v, desired=%v, kind=%s",
			change.Path, change.CurrentValue, change.DesiredValue, change.Kind)
	}
}

// TestNextCommandSmallScaleDeploymentsPreview tests parsing of real Deployments API engine events
// from the small-scale-10 fixture. This is the same drift scenario as TestNextCommandSmallScaleRealPreview
// but uses engine events downloaded from a Pulumi Deployments preview (via /preview/{updateID}/events)
// rather than local `pulumi preview --json` output. The event format differs: engine events use
// resourcePreEvent/resOutputsEvent wrappers with metadata.op, whereas preview JSON uses steps[].op.
func TestNextCommandSmallScaleDeploymentsPreview(t *testing.T) {
	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run command with Deployments engine events fixture (unlimited resources)
	rootCmd.SetArgs([]string{"next", "--events-file", "testdata/small_scale_10_deployments.json"})
	_ = rootCmd.Execute()

	// Restore stdout and read output
	_ = w.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(r)
	require.NoError(t, err)

	// Parse output
	var result NextOutput
	err = json.Unmarshal(output, &result)
	require.NoError(t, err, "Failed to parse output: %s", string(output))

	t.Logf("Output: %s", string(output))

	// Should detect drift: same 5 resources as the local preview test
	assert.Equal(t, "changes_needed", result.Status)
	require.Len(t, result.Resources, 5, "Expected 5 drifted resources")

	// Build resource map by name
	resourceMap := make(map[string]*ResourceChange)
	for i := range result.Resources {
		resourceMap[result.Resources[i].Name] = &result.Resources[i]
	}

	// Verify correct actions
	assert.Equal(t, "delete_from_code", resourceMap["cmd-3"].Action, "create -> delete_from_code")
	assert.Equal(t, "add_to_code", resourceMap["random-str-extra-0"].Action, "delete -> add_to_code")
	assert.Equal(t, "update_code", resourceMap["cmd-0"].Action, "update -> update_code")
	assert.Equal(t, "update_code", resourceMap["random-str-0"].Action, "replace -> update_code")
	assert.Equal(t, "update_code", resourceMap["tls-key-0"].Action, "replace -> update_code")

	// Every update_code resource must have non-empty Properties
	for _, res := range result.Resources {
		if res.Action == "update_code" {
			require.NotEmpty(t, res.Properties,
				"Resource %s (%s) with action update_code must have non-empty Properties", res.Name, res.Type)
		}
	}

	// Spot-check random-str-0 replace properties: length (32→16), special (true→false)
	randomStr := resourceMap["random-str-0"]
	require.NotNil(t, randomStr)
	propMap := make(map[string]*PropertyChange)
	for i := range randomStr.Properties {
		propMap[randomStr.Properties[i].Path] = &randomStr.Properties[i]
	}

	lengthProp := propMap["length"]
	require.NotNil(t, lengthProp, "length property not found on random-str-0")
	assert.Equal(t, float64(16), lengthProp.CurrentValue, "random-str-0 length currentValue")
	assert.Equal(t, float64(32), lengthProp.DesiredValue, "random-str-0 length desiredValue")

	specialProp := propMap["special"]
	require.NotNil(t, specialProp, "special property not found on random-str-0")
	assert.Equal(t, false, specialProp.CurrentValue, "random-str-0 special currentValue")
	assert.Equal(t, true, specialProp.DesiredValue, "random-str-0 special desiredValue")
}
