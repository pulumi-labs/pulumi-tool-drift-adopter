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

// runNextTest executes the next command with the given args and returns both the
// stdout summary and the full output parsed from the output file.
// It automatically adds --output-file pointing to a temp file in the test's temp dir.
func runNextTest(t *testing.T, args []string) (NextSummaryOutput, NextOutput) {
	t.Helper()
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "output.json")
	args = append(args, "--output-file", outputFile)

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := newRootCmd()
	cmd.SetArgs(args)
	_ = cmd.Execute()

	_ = w.Close()
	os.Stdout = oldStdout
	stdoutBytes, err := io.ReadAll(r)
	require.NoError(t, err, "Failed to read captured stdout")

	// Parse stdout as NextSummaryOutput
	var summary NextSummaryOutput
	err = json.Unmarshal(stdoutBytes, &summary)
	require.NoError(t, err, "Failed to parse stdout as NextSummaryOutput: %s", string(stdoutBytes))

	// Parse output file as NextOutput (may not exist for errors)
	var full NextOutput
	if summary.OutputFile != "" {
		data, err := os.ReadFile(summary.OutputFile)
		require.NoError(t, err, "Failed to read output file: %s", summary.OutputFile)
		err = json.Unmarshal(data, &full)
		require.NoError(t, err, "Failed to parse output file: %s", string(data))
	}

	return summary, full
}

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

			summary, full := runNextTest(t, []string{"next", "--events-file", eventsFile})

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

			tmpDir := t.TempDir()
			eventsFile := filepath.Join(tmpDir, "events.ndjson")
			err := os.WriteFile(eventsFile, []byte(eventsContent), 0644)
			require.NoError(t, err)

			summary, full := runNextTest(t, []string{"next", "--events-file", eventsFile})

			assert.Equal(t, "changes_needed", summary.Status)
			require.NotEmpty(t, full.Resources)
			assert.Equal(t, tt.expectedAction, full.Resources[0].Action)
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
			tmpDir := t.TempDir()
			eventsFile := filepath.Join(tmpDir, "events.ndjson")
			err := os.WriteFile(eventsFile, []byte(eventsContent), 0644)
			require.NoError(t, err)

			args := []string{"next", "--events-file", eventsFile}
			if tt.maxResources != "" {
				args = append(args, "--max-resources", tt.maxResources)
			}

			summary, full := runNextTest(t, args)

			assert.Equal(t, "changes_needed", summary.Status)
			assert.Len(t, full.Resources, tt.expectedCount, "Resource count mismatch")
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

	summary, full := runNextTest(t, []string{"next", "--events-file", eventsFile})

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
	assert.Equal(t, "update", envProp.Kind)

	require.NotNil(t, versioningProp, "Versioning property not found")
	assert.Equal(t, false, versioningProp.CurrentValue)
	assert.Equal(t, true, versioningProp.DesiredValue)
	assert.Equal(t, "update", versioningProp.Kind)
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

// TestNextCommandNDJSONRealFormat tests parsing actual NDJSON with full engine event structure
func TestNextCommandNDJSONRealFormat(t *testing.T) {
	summary, full := runNextTest(t, []string{"next", "--events-file", "testdata/ndjson_update.ndjson"})

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
	assert.Equal(t, "update", envProp.Kind)

	require.NotNil(t, managedByProp, "tags.ManagedBy property not found")
	assert.Equal(t, nil, managedByProp.CurrentValue)
	assert.Equal(t, "pulumi", managedByProp.DesiredValue)
	assert.Equal(t, "add", managedByProp.Kind)
}

// TestNextCommandNDJSONMixedEvents tests that non-resourcePreEvent lines are properly skipped
func TestNextCommandNDJSONMixedEvents(t *testing.T) {
	summary, full := runNextTest(t, []string{"next", "--events-file", "testdata/ndjson_with_diagnostics.ndjson"})

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
	assert.Equal(t, "update", prop.Kind)
}

// TestNextCommandNDJSONEmptyFile tests NDJSON with no resource events returns clean
func TestNextCommandNDJSONEmptyFile(t *testing.T) {
	summary, full := runNextTest(t, []string{"next", "--events-file", "testdata/ndjson_empty.ndjson"})

	assert.Equal(t, "clean", summary.Status)
	assert.Empty(t, full.Resources)
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
			args := []string{"next", "--events-file", "testdata/ndjson_multiple_resources.ndjson"}
			if tt.maxResources != "" {
				args = append(args, "--max-resources", tt.maxResources)
			}

			summary, full := runNextTest(t, args)

			assert.Equal(t, "changes_needed", summary.Status)
			assert.Len(t, full.Resources, tt.expectedCount, "Resource count mismatch")

			if len(full.Resources) > 0 {
				resource := full.Resources[0]
				assert.Equal(t, "update_code", resource.Action)
				assert.Equal(t, "bucket-1", resource.Name)
				assert.Equal(t, "aws:s3/bucket:Bucket", resource.Type)
			}
		})
	}
}

// TestNextCommandNDJSONCreateDelete tests create and delete operations from NDJSON
func TestNextCommandNDJSONCreateDelete(t *testing.T) {
	summary, full := runNextTest(t, []string{"next", "--events-file", "testdata/ndjson_create_delete.ndjson"})

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
	summary, full := runNextTest(t, []string{"next", "--events-file", "testdata/ndjson_replace.ndjson"})

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

	summary, full := runNextTest(t, []string{"next", "--events-file", eventsFile})

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
	assert.Equal(t, "update", lengthProp.Kind)
	assert.Equal(t, float64(32), lengthProp.CurrentValue)
	assert.Equal(t, float64(16), lengthProp.DesiredValue)
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

	summary, full := runNextTest(t, []string{"next", "--events-file", eventsFile})

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
	assert.Equal(t, "update", prop.Kind)
}

// TestNextCommandReplaceWithNullDetailedDiff tests that standard JSON replace ops with null detailedDiff
// still produce property changes by falling back to replaceReasons/diffReasons.
func TestNextCommandReplaceWithNullDetailedDiff(t *testing.T) {
	summary, full := runNextTest(t, []string{"next", "--events-file", "testdata/standard_json_replace.json"})

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

	summary, full := runNextTest(t, []string{"next", "--events-file", eventsFile})

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

// TestNextCommandEngineEventsJSON tests parsing the Pulumi Cloud GetEngineEvents API format
func TestNextCommandEngineEventsJSON(t *testing.T) {
	summary, full := runNextTest(t, []string{"next", "--events-file", "testdata/engine_events_update.json"})

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
	assert.Equal(t, "update", envProp.Kind)

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
	summary, full := runNextTest(t, []string{"next", "--events-file", "testdata/small_scale_10_replace.json"})

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
	assert.Equal(t, "delete", ecdsaProp.Kind)
	assert.Nil(t, ecdsaProp.CurrentValue)
	assert.Equal(t, "P256", ecdsaProp.DesiredValue)
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

	summary, full := runNextTest(t, []string{"next", "--events-file", eventsFile})

	assert.Equal(t, "changes_needed", summary.Status)
	require.Len(t, full.Resources, 2, "Expected 2 actionable resources")
	assert.Equal(t, 1, summary.SkippedCount, "Expected 1 skipped resource")

	require.Len(t, full.Skipped, 1, "Expected 1 skipped resource in output file")
	assert.Equal(t, "incomplete-instance", full.Skipped[0].Name)
	assert.Equal(t, "missing_properties", full.Skipped[0].Reason)
	assert.Equal(t, "update_code", full.Skipped[0].Action)
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

	summary, full := runNextTest(t, []string{
		"next", "--events-file", eventsFile,
		"--exclude-urns", "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket-a",
	})

	assert.Equal(t, "changes_needed", summary.Status)
	require.Len(t, full.Resources, 1)
	assert.Equal(t, "bucket-b", full.Resources[0].Name)

	require.Len(t, full.Skipped, 1)
	assert.Equal(t, "bucket-a", full.Skipped[0].Name)
	assert.Equal(t, "excluded", full.Skipped[0].Reason)
	assert.Equal(t, "update_code", full.Skipped[0].Action)
	assert.NotEmpty(t, full.Skipped[0].Properties)
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

	summary, full := runNextTest(t, []string{"next", "--events-file", eventsFile})

	assert.Equal(t, "stop_with_skipped", summary.Status)
	assert.Empty(t, full.Resources)
	assert.Equal(t, 1, summary.SkippedCount)
	require.Len(t, full.Skipped, 1)
	assert.Equal(t, "missing_properties", full.Skipped[0].Reason)
}

// TestNextCommandRealPulumiServiceNDJSON tests parsing of real NDJSON from pulumi-service integration test
// This reproduces the parsing bug found during integration testing where the tool reported:
// "failed to parse preview output: invalid character '{' after top-level value"
func TestNextCommandRealPulumiServiceNDJSON(t *testing.T) {
	summary, full := runNextTest(t, []string{"next", "--events-file", "testdata/simple-s3-drift.ndjson"})

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

// TestNextCommandSmallScaleDeploymentsPreview tests parsing of real Deployments API engine events
// from the small-scale-10 fixture. This is the same drift scenario as TestNextCommandSmallScaleRealPreview
// but uses engine events downloaded from a Pulumi Deployments preview (via /preview/{updateID}/events)
// rather than local `pulumi preview --json` output. The event format differs: engine events use
// resourcePreEvent/resOutputsEvent wrappers with metadata.op, whereas preview JSON uses steps[].op.
func TestNextCommandSmallScaleDeploymentsPreview(t *testing.T) {
	summary, full := runNextTest(t, []string{"next", "--events-file", "testdata/small_scale_10_deployments.json"})

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

	tmpDir := t.TempDir()
	eventsFile := filepath.Join(tmpDir, "events.json")
	err := os.WriteFile(eventsFile, []byte(eventsContent), 0644)
	require.NoError(t, err)

	summary, full := runNextTest(t, []string{"next", "--events-file", eventsFile})

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

	tmpDir := t.TempDir()
	eventsFile := filepath.Join(tmpDir, "events.json")
	err := os.WriteFile(eventsFile, []byte(eventsContent), 0644)
	require.NoError(t, err)

	summary, full := runNextTest(t, []string{"next", "--events-file", eventsFile})

	assert.Equal(t, "changes_needed", summary.Status)
	require.Len(t, full.Resources, 1)

	res := full.Resources[0]
	assert.Equal(t, "add_to_code", res.Action)
	assert.NotNil(t, res.InputProperties, "should have InputProperties map from outputs fallback")
	assert.Nil(t, res.Properties, "should NOT have Properties array for add_to_code")
	assert.Len(t, res.InputProperties, 2, "should have 2 output properties as fallback")
}

// TestNextCommandSummary verifies the summary field with multiple types and actions
func TestNextCommandSummary(t *testing.T) {
	eventsContent := `{
		"steps": [
			{
				"op": "delete",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket-0",
				"oldState": {"type": "aws:s3/bucket:Bucket", "inputs": {"bucket": "bucket-0"}},
				"detailedDiff": {}
			},
			{
				"op": "delete",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket-1",
				"oldState": {"type": "aws:s3/bucket:Bucket", "inputs": {"bucket": "bucket-1"}},
				"detailedDiff": {}
			},
			{
				"op": "update",
				"urn": "urn:pulumi:dev::test::random:index/randomString:RandomString::rand-0",
				"oldState": {"type": "random:index/randomString:RandomString", "outputs": {"length": 32}},
				"newState": {"type": "random:index/randomString:RandomString", "outputs": {"length": 16}},
				"detailedDiff": {"length": {"kind": "update"}}
			},
			{
				"op": "create",
				"urn": "urn:pulumi:dev::test::random:index/randomString:RandomString::rand-extra",
				"newState": {"type": "random:index/randomString:RandomString"},
				"detailedDiff": {}
			}
		]
	}`

	tmpDir := t.TempDir()
	eventsFile := filepath.Join(tmpDir, "events.json")
	err := os.WriteFile(eventsFile, []byte(eventsContent), 0644)
	require.NoError(t, err)

	summary, _ := runNextTest(t, []string{"next", "--events-file", eventsFile})

	assert.Equal(t, "changes_needed", summary.Status)
	require.NotNil(t, summary.Summary, "summary should be present for changes_needed")

	s := summary.Summary
	assert.Equal(t, 4, s.Total)

	assert.Equal(t, 2, s.ByAction["add_to_code"])
	assert.Equal(t, 1, s.ByAction["update_code"])
	assert.Equal(t, 1, s.ByAction["delete_from_code"])

	assert.Equal(t, 2, s.ByType["aws:s3/bucket:Bucket"])
	assert.Equal(t, 2, s.ByType["random:index/randomString:RandomString"])

	assert.Equal(t, 2, s.ByTypeAction["aws:s3/bucket:Bucket"]["add_to_code"])
	assert.Equal(t, 1, s.ByTypeAction["random:index/randomString:RandomString"]["update_code"])
	assert.Equal(t, 1, s.ByTypeAction["random:index/randomString:RandomString"]["delete_from_code"])
}

// TestNextCommandSummaryBeforeTruncation verifies summary counts full set even when max-resources truncates
func TestNextCommandSummaryBeforeTruncation(t *testing.T) {
	eventsContent := `{
		"steps": [
			{
				"op": "update",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::b1",
				"oldState": {"type": "aws:s3/bucket:Bucket", "outputs": {"tags": {"Env": "prod"}}},
				"newState": {"type": "aws:s3/bucket:Bucket", "outputs": {"tags": {"Env": "dev"}}},
				"detailedDiff": {"tags.Env": {"kind": "update"}}
			},
			{
				"op": "update",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::b2",
				"oldState": {"type": "aws:s3/bucket:Bucket", "outputs": {"tags": {"Env": "prod"}}},
				"newState": {"type": "aws:s3/bucket:Bucket", "outputs": {"tags": {"Env": "dev"}}},
				"detailedDiff": {"tags.Env": {"kind": "update"}}
			},
			{
				"op": "update",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::b3",
				"oldState": {"type": "aws:s3/bucket:Bucket", "outputs": {"tags": {"Env": "prod"}}},
				"newState": {"type": "aws:s3/bucket:Bucket", "outputs": {"tags": {"Env": "dev"}}},
				"detailedDiff": {"tags.Env": {"kind": "update"}}
			}
		]
	}`

	tmpDir := t.TempDir()
	eventsFile := filepath.Join(tmpDir, "events.json")
	err := os.WriteFile(eventsFile, []byte(eventsContent), 0644)
	require.NoError(t, err)

	summary, full := runNextTest(t, []string{"next", "--events-file", eventsFile, "--max-resources", "1"})

	assert.Equal(t, "changes_needed", summary.Status)
	assert.Len(t, full.Resources, 1, "should only return 1 resource due to max-resources")

	require.NotNil(t, summary.Summary)
	assert.Equal(t, 3, summary.Summary.Total, "summary should count all 3 resources before truncation")
	assert.Equal(t, 3, summary.Summary.ByAction["update_code"])
	assert.Equal(t, 3, summary.Summary.ByType["aws:s3/bucket:Bucket"])
}

// TestNextCommandSummaryAbsentForClean verifies no summary when status is clean
func TestNextCommandSummaryAbsentForClean(t *testing.T) {
	eventsContent := `{"steps": []}`

	tmpDir := t.TempDir()
	eventsFile := filepath.Join(tmpDir, "events.json")
	err := os.WriteFile(eventsFile, []byte(eventsContent), 0644)
	require.NoError(t, err)

	summary, _ := runNextTest(t, []string{"next", "--events-file", eventsFile})

	assert.Equal(t, "clean", summary.Status)
	assert.Nil(t, summary.Summary, "summary should be nil for clean status")
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

	tmpDir := t.TempDir()
	eventsFile := filepath.Join(tmpDir, "events.json")
	err := os.WriteFile(eventsFile, []byte(eventsContent), 0644)
	require.NoError(t, err)

	summary, full := runNextTest(t, []string{"next", "--events-file", eventsFile, "--max-resources", "-1"})

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

func TestStateFileParsing(t *testing.T) {
	// State export with one resource that has PropertyDependencies
	stateContent := `{
		"version": 3,
		"deployment": {
			"manifest": {"time": "2026-01-01T00:00:00Z", "magic": "test", "version": "v3.0.0"},
			"resources": [
				{
					"urn": "urn:pulumi:dev::test::tls:index/privateKey:PrivateKey::ca-key",
					"type": "tls:index/privateKey:PrivateKey",
					"inputs": {"algorithm": "RSA", "rsaBits": 4096},
					"outputs": {
						"algorithm": "RSA",
						"rsaBits": 4096,
						"privateKeyPem": "-----BEGIN RSA PRIVATE KEY-----\nfake-key\n-----END RSA PRIVATE KEY-----\n"
					}
				}
			]
		}
	}`

	stateFile := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(stateFile, []byte(stateContent), 0644))

	result, err := parseStateFile(stateFile)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "tls:index/privateKey:PrivateKey", string(result["urn:pulumi:dev::test::tls:index/privateKey:PrivateKey::ca-key"].Type))
}

func TestDependencyResolution(t *testing.T) {
	// Load test fixtures
	eventsData, err := os.ReadFile(filepath.Join("testdata", "events_with_deps.json"))
	require.NoError(t, err)
	stateData, err := os.ReadFile(filepath.Join("testdata", "state_with_deps.json"))
	require.NoError(t, err)

	steps, err := parsePreviewOutput(eventsData)
	require.NoError(t, err)

	stateLookup, err := parseStateExport(stateData)
	require.NoError(t, err)

	resources := convertStepsToResources(steps, stateLookup)
	require.Len(t, resources, 2)

	// Find the ca-cert resource (SelfSignedCert)
	var caCert *ResourceChange
	var serverCert *ResourceChange
	for i := range resources {
		switch resources[i].Name {
		case "ca-cert":
			caCert = &resources[i]
		case "server-cert":
			serverCert = &resources[i]
		}
	}
	require.NotNil(t, caCert, "ca-cert resource not found")
	require.NotNil(t, serverCert, "server-cert resource not found")

	// ca-cert.privateKeyPem should have dependsOn metadata (no literal value)
	pkPem := caCert.InputProperties["privateKeyPem"]
	pkMap, ok := pkPem.(map[string]interface{})
	require.True(t, ok, "privateKeyPem should be a map with dependsOn, got %T", pkPem)
	assert.Nil(t, pkMap["value"], "value should be omitted when dependsOn is present")

	depInfo, ok := pkMap["dependsOn"].(map[string]interface{})
	require.True(t, ok, "dependsOn should be a map")
	assert.Equal(t, "ca-key", depInfo["resourceName"])
	assert.Equal(t, "tls:index/privateKey:PrivateKey", depInfo["resourceType"])
	assert.Equal(t, "privateKeyPem", depInfo["outputProperty"])

	// ca-cert.validityPeriodHours should be a plain value (no deps)
	assert.Equal(t, float64(87600), caCert.InputProperties["validityPeriodHours"])

	// server-cert.caPrivateKeyPem should resolve to ca-key
	caKeyPem := serverCert.InputProperties["caPrivateKeyPem"]
	caKeyMap, ok := caKeyPem.(map[string]interface{})
	require.True(t, ok, "caPrivateKeyPem should be a map with dependsOn")
	caKeyDep, ok := caKeyMap["dependsOn"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "ca-key", caKeyDep["resourceName"])
	assert.Equal(t, "privateKeyPem", caKeyDep["outputProperty"])

	// server-cert.caCertPem should resolve to ca-cert
	caCertPem := serverCert.InputProperties["caCertPem"]
	caCertMap, ok := caCertPem.(map[string]interface{})
	require.True(t, ok, "caCertPem should be a map with dependsOn")
	caCertDep, ok := caCertMap["dependsOn"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "ca-cert", caCertDep["resourceName"])
	assert.Equal(t, "tls:index/selfSignedCert:SelfSignedCert", caCertDep["resourceType"])
	assert.Equal(t, "certPem", caCertDep["outputProperty"])

	// server-cert.certRequestPem has empty deps — should be plain value
	_, isCsrMap := serverCert.InputProperties["certRequestPem"].(map[string]interface{})
	assert.False(t, isCsrMap, "certRequestPem has empty deps, should be plain value")
}

func TestDependencyResolutionEdgeCases(t *testing.T) {
	t.Run("no state lookup - returns plain values", func(t *testing.T) {
		eventsData, err := os.ReadFile(filepath.Join("testdata", "events_with_deps.json"))
		require.NoError(t, err)

		steps, err := parsePreviewOutput(eventsData)
		require.NoError(t, err)

		// nil stateLookup — should return plain values
		resources := convertStepsToResources(steps, nil)
		require.NotEmpty(t, resources)

		for _, res := range resources {
			if res.Action != "add_to_code" {
				continue
			}
			for key, val := range res.InputProperties {
				_, isMap := val.(map[string]interface{})
				if isMap {
					// Should NOT have dependsOn when no state lookup
					m := val.(map[string]interface{})
					_, hasDep := m["dependsOn"]
					assert.False(t, hasDep, "property %s should not have dependsOn without state lookup", key)
				}
			}
		}
	})

	t.Run("dep URN not in state - returns plain value", func(t *testing.T) {
		eventsContent := `{
			"steps": [{
				"op": "delete",
				"urn": "urn:pulumi:dev::test::tls:index/selfSignedCert:SelfSignedCert::orphan-cert",
				"oldState": {
					"type": "tls:index/selfSignedCert:SelfSignedCert",
					"inputs": {"privateKeyPem": "some-pem-value"},
					"propertyDependencies": {
						"privateKeyPem": ["urn:pulumi:dev::test::tls:index/privateKey:PrivateKey::missing-key"]
					}
				}
			}]
		}`
		stateContent := `{"version": 3, "deployment": {"manifest": {"time": "2026-01-01T00:00:00Z", "magic": "test", "version": "v3.0.0"}, "resources": []}}`

		steps, err := parsePreviewOutput([]byte(eventsContent))
		require.NoError(t, err)
		stateLookup, err := parseStateExport([]byte(stateContent))
		require.NoError(t, err)

		resources := convertStepsToResources(steps, stateLookup)
		require.Len(t, resources, 1)

		// Should be plain string since dep URN is missing from state
		assert.Equal(t, "some-pem-value", resources[0].InputProperties["privateKeyPem"])
	})

	t.Run("engine events format with propertyDependencies", func(t *testing.T) {
		// NDJSON engine events format: "old" (not "oldState"), "diffKind" (not "kind")
		ndjsonContent := `{"type":"resourcePreEvent","resourcePreEvent":{"metadata":{"op":"delete","urn":"urn:pulumi:dev::test::tls:index/selfSignedCert:SelfSignedCert::ndjson-cert","old":{"type":"tls:index/selfSignedCert:SelfSignedCert","inputs":{"privateKeyPem":"ndjson-pem-value","validityPeriodHours":8760},"outputs":{"certPem":"ndjson-cert-pem","privateKeyPem":"ndjson-pem-value"},"propertyDependencies":{"privateKeyPem":["urn:pulumi:dev::test::tls:index/privateKey:PrivateKey::ndjson-key"]}}}}}
{"type":"resourcePreEvent","resourcePreEvent":{"metadata":{"op":"delete","urn":"urn:pulumi:dev::test::tls:index/privateKey:PrivateKey::ndjson-key","old":{"type":"tls:index/privateKey:PrivateKey","inputs":{"algorithm":"RSA"},"outputs":{"algorithm":"RSA","privateKeyPem":"ndjson-pem-value"}}}}}`

		steps, err := parsePreviewOutput([]byte(ndjsonContent))
		require.NoError(t, err)
		require.Len(t, steps, 2)

		// Build lookup from steps (no external state)
		stateLookup := buildStateLookupFromSteps(steps)
		resources := convertStepsToResources(steps, stateLookup)

		var cert *ResourceChange
		for i := range resources {
			if resources[i].Name == "ndjson-cert" {
				cert = &resources[i]
			}
		}
		require.NotNil(t, cert)

		// PropertyDependencies should survive NDJSON parsing and resolve
		pkPem, ok := cert.InputProperties["privateKeyPem"].(map[string]interface{})
		require.True(t, ok, "privateKeyPem should have dependsOn from engine events format")
		dep, ok := pkPem["dependsOn"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "ndjson-key", dep["resourceName"])
		assert.Equal(t, "privateKeyPem", dep["outputProperty"])
	})

	t.Run("value not found in dep outputs - bare dependsOn with single depURN", func(t *testing.T) {
		eventsContent := `{
			"steps": [{
				"op": "delete",
				"urn": "urn:pulumi:dev::test::tls:index/selfSignedCert:SelfSignedCert::no-match-cert",
				"oldState": {
					"type": "tls:index/selfSignedCert:SelfSignedCert",
					"inputs": {"privateKeyPem": "value-that-doesnt-match-any-output"},
					"propertyDependencies": {
						"privateKeyPem": ["urn:pulumi:dev::test::tls:index/privateKey:PrivateKey::some-key"]
					}
				}
			}]
		}`
		stateContent := `{
			"version": 3,
			"deployment": {
				"manifest": {"time": "2026-01-01T00:00:00Z", "magic": "test", "version": "v3.0.0"},
				"resources": [{
					"urn": "urn:pulumi:dev::test::tls:index/privateKey:PrivateKey::some-key",
					"type": "tls:index/privateKey:PrivateKey",
					"inputs": {"algorithm": "RSA"},
					"outputs": {"privateKeyPem": "different-pem-value", "algorithm": "RSA"}
				}]
			}
		}`

		steps, err := parsePreviewOutput([]byte(eventsContent))
		require.NoError(t, err)
		stateLookup, err := parseStateExport([]byte(stateContent))
		require.NoError(t, err)

		resources := convertStepsToResources(steps, stateLookup)
		require.Len(t, resources, 1)

		// Value doesn't match any output, but single depURN → bare dependsOn (no outputProperty)
		pkPem, ok := resources[0].InputProperties["privateKeyPem"].(map[string]interface{})
		require.True(t, ok, "privateKeyPem should have bare dependsOn, got %T", resources[0].InputProperties["privateKeyPem"])
		dep, ok := pkPem["dependsOn"].(map[string]interface{})
		require.True(t, ok, "dependsOn should be a map")
		assert.Equal(t, "some-key", dep["resourceName"])
		assert.Equal(t, "tls:index/privateKey:PrivateKey", dep["resourceType"])
		assert.Nil(t, dep["outputProperty"], "bare dependsOn should not have outputProperty")
	})

	t.Run("plaintext secret output matches correctly (show-secrets)", func(t *testing.T) {
		// With --show-secrets, secret outputs appear as plaintext — no sentinel wrapper.
		// findMatchingOutput should match them normally.
		eventsContent := `{
			"steps": [{
				"op": "delete",
				"urn": "urn:pulumi:dev::test::command:local:Command::deploy-cmd",
				"oldState": {
					"type": "command:local:Command",
					"inputs": {"triggers": ["secret-password-value"]},
					"propertyDependencies": {
						"triggers": ["urn:pulumi:dev::test::random:index/randomPassword:RandomPassword::api-pass"]
					}
				}
			}]
		}`
		stateContent := `{
			"version": 3,
			"deployment": {
				"manifest": {"time": "2026-01-01T00:00:00Z", "magic": "test", "version": "v3.0.0"},
				"resources": [{
					"urn": "urn:pulumi:dev::test::random:index/randomPassword:RandomPassword::api-pass",
					"type": "random:index/randomPassword:RandomPassword",
					"inputs": {"length": 16},
					"outputs": {"result": "secret-password-value", "length": 16}
				}]
			}
		}`

		steps, err := parsePreviewOutput([]byte(eventsContent))
		require.NoError(t, err)
		stateLookup, err := parseStateExport([]byte(stateContent))
		require.NoError(t, err)

		resources := convertStepsToResources(steps, stateLookup)
		require.Len(t, resources, 1)

		// triggers is an array ["secret-password-value"] which won't match string "secret-password-value"
		// So it falls back to bare dependsOn (structural mismatch: array vs string)
		triggers, ok := resources[0].InputProperties["triggers"].(map[string]interface{})
		require.True(t, ok, "triggers should have dependsOn metadata, got %T", resources[0].InputProperties["triggers"])
		dep, ok := triggers["dependsOn"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "api-pass", dep["resourceName"])
		assert.Equal(t, "random:index/randomPassword:RandomPassword", dep["resourceType"])
	})

	t.Run("bare dependsOn when structural mismatch - array input vs string output", func(t *testing.T) {
		// Input is ["some-value"] (array), output is "some-value" (string).
		// Exact JSON match fails, but bare dependsOn should be emitted for single depURN.
		eventsContent := `{
			"steps": [{
				"op": "delete",
				"urn": "urn:pulumi:dev::test::command:local:Command::worker-cmd",
				"oldState": {
					"type": "command:local:Command",
					"inputs": {"triggers": ["hex-output-value"]},
					"propertyDependencies": {
						"triggers": ["urn:pulumi:dev::test::random:index/randomId:RandomId::worker-id"]
					}
				}
			}]
		}`
		stateContent := `{
			"version": 3,
			"deployment": {
				"manifest": {"time": "2026-01-01T00:00:00Z", "magic": "test", "version": "v3.0.0"},
				"resources": [{
					"urn": "urn:pulumi:dev::test::random:index/randomId:RandomId::worker-id",
					"type": "random:index/randomId:RandomId",
					"inputs": {"byteLength": 8},
					"outputs": {"hex": "hex-output-value", "b64Std": "base64value"}
				}]
			}
		}`

		steps, err := parsePreviewOutput([]byte(eventsContent))
		require.NoError(t, err)
		stateLookup, err := parseStateExport([]byte(stateContent))
		require.NoError(t, err)

		resources := convertStepsToResources(steps, stateLookup)
		require.Len(t, resources, 1)

		// Array vs string mismatch → bare dependsOn
		triggers, ok := resources[0].InputProperties["triggers"].(map[string]interface{})
		require.True(t, ok, "triggers should have bare dependsOn")
		dep, ok := triggers["dependsOn"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "worker-id", dep["resourceName"])
		assert.Equal(t, "random:index/randomId:RandomId", dep["resourceType"])
		assert.Nil(t, dep["outputProperty"], "bare dependsOn should not have outputProperty")
	})

	t.Run("multiple depURNs with no match returns plain value", func(t *testing.T) {
		// When there are multiple depURNs and no exact match, we can't emit bare dependsOn
		// because it would be ambiguous which resource to reference.
		eventsContent := `{
			"steps": [{
				"op": "delete",
				"urn": "urn:pulumi:dev::test::command:local:Command::multi-dep-cmd",
				"oldState": {
					"type": "command:local:Command",
					"inputs": {"triggers": ["ambiguous-value"]},
					"propertyDependencies": {
						"triggers": [
							"urn:pulumi:dev::test::random:index/randomPassword:RandomPassword::pass-a",
							"urn:pulumi:dev::test::random:index/randomPassword:RandomPassword::pass-b"
						]
					}
				}
			}]
		}`
		stateContent := `{
			"version": 3,
			"deployment": {
				"manifest": {"time": "2026-01-01T00:00:00Z", "magic": "test", "version": "v3.0.0"},
				"resources": [
					{
						"urn": "urn:pulumi:dev::test::random:index/randomPassword:RandomPassword::pass-a",
						"type": "random:index/randomPassword:RandomPassword",
						"inputs": {"length": 16},
						"outputs": {"result": "different-a", "length": 16}
					},
					{
						"urn": "urn:pulumi:dev::test::random:index/randomPassword:RandomPassword::pass-b",
						"type": "random:index/randomPassword:RandomPassword",
						"inputs": {"length": 16},
						"outputs": {"result": "different-b", "length": 16}
					}
				]
			}
		}`

		steps, err := parsePreviewOutput([]byte(eventsContent))
		require.NoError(t, err)
		stateLookup, err := parseStateExport([]byte(stateContent))
		require.NoError(t, err)

		resources := convertStepsToResources(steps, stateLookup)
		require.Len(t, resources, 1)

		// Multiple depURNs, no exact match → plain value (no bare dependsOn due to ambiguity)
		triggers := resources[0].InputProperties["triggers"]
		triggerArr, ok := triggers.([]interface{})
		require.True(t, ok, "triggers should be plain array value, got %T", triggers)
		assert.Equal(t, "ambiguous-value", triggerArr[0])
	})
}

func TestRunNextStateFileFlag(t *testing.T) {
	tmpDir := t.TempDir()
	eventsFile := filepath.Join(tmpDir, "events.json")
	stateFile := filepath.Join(tmpDir, "state.json")

	eventsData, err := os.ReadFile(filepath.Join("testdata", "events_with_deps.json"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(eventsFile, eventsData, 0644))

	stateData, err := os.ReadFile(filepath.Join("testdata", "state_with_deps.json"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(stateFile, stateData, 0644))

	summary, full := runNextTest(t, []string{"next", "--events-file", eventsFile, "--state-file", stateFile})

	assert.Equal(t, "changes_needed", summary.Status)

	var caCert *ResourceChange
	for i := range full.Resources {
		if full.Resources[i].Name == "ca-cert" {
			caCert = &full.Resources[i]
		}
	}
	require.NotNil(t, caCert)

	pkPem, ok := caCert.InputProperties["privateKeyPem"].(map[string]interface{})
	require.True(t, ok, "privateKeyPem should have dependsOn metadata")
	dep, ok := pkPem["dependsOn"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "ca-key", dep["resourceName"])
}

func TestDependencyResolutionFromPreviewOnly(t *testing.T) {
	// Preview events where OldState has PropertyDependencies AND
	// another step has the dependent resource with outputs
	eventsContent := `{
		"steps": [
			{
				"op": "delete",
				"urn": "urn:pulumi:dev::test::tls:index/privateKey:PrivateKey::inline-key",
				"oldState": {
					"type": "tls:index/privateKey:PrivateKey",
					"inputs": {"algorithm": "RSA", "rsaBits": 4096},
					"outputs": {
						"algorithm": "RSA",
						"privateKeyPem": "-----BEGIN RSA PRIVATE KEY-----\ninline-key-pem\n-----END RSA PRIVATE KEY-----\n"
					}
				}
			},
			{
				"op": "delete",
				"urn": "urn:pulumi:dev::test::tls:index/selfSignedCert:SelfSignedCert::inline-cert",
				"oldState": {
					"type": "tls:index/selfSignedCert:SelfSignedCert",
					"inputs": {
						"privateKeyPem": "-----BEGIN RSA PRIVATE KEY-----\ninline-key-pem\n-----END RSA PRIVATE KEY-----\n",
						"validityPeriodHours": 8760
					},
					"propertyDependencies": {
						"privateKeyPem": ["urn:pulumi:dev::test::tls:index/privateKey:PrivateKey::inline-key"]
					}
				}
			}
		]
	}`

	steps, err := parsePreviewOutput([]byte(eventsContent))
	require.NoError(t, err)

	// Build state lookup from the preview steps themselves (no external state file)
	stateLookup := buildStateLookupFromSteps(steps)

	resources := convertStepsToResources(steps, stateLookup)

	var cert *ResourceChange
	for i := range resources {
		if resources[i].Name == "inline-cert" {
			cert = &resources[i]
		}
	}
	require.NotNil(t, cert)

	pkPem, ok := cert.InputProperties["privateKeyPem"].(map[string]interface{})
	require.True(t, ok, "should have dependsOn")
	dep := pkPem["dependsOn"].(map[string]interface{})
	assert.Equal(t, "inline-key", dep["resourceName"])
	assert.Equal(t, "privateKeyPem", dep["outputProperty"])
}

func TestStateFilePathInOutput(t *testing.T) {
	tmpDir := t.TempDir()
	eventsFile := filepath.Join(tmpDir, "events.json")
	stateFile := filepath.Join(tmpDir, "state.json")

	eventsData, err := os.ReadFile(filepath.Join("testdata", "events_with_deps.json"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(eventsFile, eventsData, 0644))

	stateData, err := os.ReadFile(filepath.Join("testdata", "state_with_deps.json"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(stateFile, stateData, 0644))

	summary, full := runNextTest(t, []string{"next", "--events-file", eventsFile, "--state-file", stateFile})

	assert.Equal(t, stateFile, summary.StateFilePath, "stateFilePath should match provided --state-file path")
	assert.Equal(t, stateFile, full.StateFilePath, "stateFilePath should also be in output file")
}

func TestSkipRefreshFlagAccepted(t *testing.T) {
	eventsContent := `{"steps": []}`
	stateContent := `{"version": 3, "deployment": {"resources": []}}`

	tmpDir := t.TempDir()
	eventsFile := filepath.Join(tmpDir, "events.json")
	stateFile := filepath.Join(tmpDir, "state.json")
	require.NoError(t, os.WriteFile(eventsFile, []byte(eventsContent), 0644))
	require.NoError(t, os.WriteFile(stateFile, []byte(stateContent), 0644))

	summary, _ := runNextTest(t, []string{"next", "--events-file", eventsFile, "--state-file", stateFile, "--skip-refresh"})
	assert.Equal(t, "clean", summary.Status)
}

// TestNextCommandTempFileDefault verifies that when --output-file is omitted, a temp file is created
func TestNextCommandTempFileDefault(t *testing.T) {
	eventsContent := `{"steps": []}`
	tmpDir := t.TempDir()
	eventsFile := filepath.Join(tmpDir, "events.json")
	require.NoError(t, os.WriteFile(eventsFile, []byte(eventsContent), 0644))

	// Don't use runNextTest here since it always adds --output-file
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := newRootCmd()
	cmd.SetArgs([]string{"next", "--events-file", eventsFile})
	_ = cmd.Execute()

	_ = w.Close()
	os.Stdout = oldStdout
	stdoutBytes, err := io.ReadAll(r)
	require.NoError(t, err)

	var summary NextSummaryOutput
	require.NoError(t, json.Unmarshal(stdoutBytes, &summary))

	assert.Equal(t, "clean", summary.Status)
	assert.NotEmpty(t, summary.OutputFile, "outputFile should be set even when --output-file is omitted")
	assert.Contains(t, summary.OutputFile, "drift-adopter-output-", "should use temp file naming convention")

	// Verify the temp file exists and is parseable
	data, err := os.ReadFile(summary.OutputFile)
	require.NoError(t, err)
	var full NextOutput
	require.NoError(t, json.Unmarshal(data, &full))
	assert.Equal(t, "clean", full.Status)

	// Clean up temp file
	_ = os.Remove(summary.OutputFile)
}

// TestNextCommandOutputFileFlag verifies that --output-file writes to the specified path
func TestNextCommandOutputFileFlag(t *testing.T) {
	eventsContent := `{
		"steps": [{
			"op": "update",
			"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket",
			"oldState": {"type": "aws:s3/bucket:Bucket", "outputs": {"tags": {"Env": "prod"}}},
			"newState": {"type": "aws:s3/bucket:Bucket", "outputs": {"tags": {"Env": "dev"}}},
			"detailedDiff": {"tags.Env": {"kind": "update"}}
		}]
	}`

	tmpDir := t.TempDir()
	eventsFile := filepath.Join(tmpDir, "events.json")
	outputFile := filepath.Join(tmpDir, "custom-output.json")
	require.NoError(t, os.WriteFile(eventsFile, []byte(eventsContent), 0644))

	summary, full := runNextTest(t, []string{"next", "--events-file", eventsFile})

	assert.Equal(t, "changes_needed", summary.Status)
	assert.NotEmpty(t, summary.OutputFile)

	// Verify full output in the file
	require.Len(t, full.Resources, 1)
	assert.Equal(t, "update_code", full.Resources[0].Action)

	// Also verify explicit --output-file path works
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := newRootCmd()
	cmd.SetArgs([]string{"next", "--events-file", eventsFile, "--output-file", outputFile})
	_ = cmd.Execute()

	_ = w.Close()
	os.Stdout = oldStdout
	stdoutBytes, err := io.ReadAll(r)
	require.NoError(t, err)

	var summary2 NextSummaryOutput
	require.NoError(t, json.Unmarshal(stdoutBytes, &summary2))
	assert.Equal(t, outputFile, summary2.OutputFile, "outputFile should match --output-file flag")

	// Verify the file was written to the specified path
	data, err := os.ReadFile(outputFile)
	require.NoError(t, err)
	var full2 NextOutput
	require.NoError(t, json.Unmarshal(data, &full2))
	assert.Equal(t, "changes_needed", full2.Status)
	require.Len(t, full2.Resources, 1)
}
