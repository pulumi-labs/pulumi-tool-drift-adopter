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
		name           string
		eventsContent  string
		expectedStatus string
		expectedError  string
		expectResources bool
	}{
		{
			name: "clean state - no changes",
			eventsContent: `{"steps": []}`,
			expectedStatus: "clean",
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
			expectedStatus: "changes_needed",
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
			expectedStatus: "changes_needed",
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
			expectedStatus: "changes_needed",
			expectResources: true,
		},
		{
			name: "invalid JSON",
			eventsContent: `{invalid json`,
			expectedStatus: "error",
			expectedError: "failed to parse preview output",
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
			w.Close()
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
			w.Close()
			os.Stdout = oldStdout
			output, err := io.ReadAll(r)
			require.NoError(t, err)

			// Parse output
			var result NextOutput
			err = json.Unmarshal(output, &result)
			require.NoError(t, err)

			// Verify action
			assert.Equal(t, "changes_needed", result.Status)
			require.NotEmpty(t, result.Resources)
			assert.Equal(t, tt.expectedAction, result.Resources[0].Action)
		})
	}
}

// TestNextCommandMaxResourcesLimit tests the --max-resources flag with events file
func TestNextCommandMaxResourcesLimit(t *testing.T) {
	// Create events with 5 resources
	eventsContent := `{
		"steps": [
			{
				"op": "update",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket1",
				"oldState": {"type": "aws:s3/bucket:Bucket", "outputs": {}},
				"newState": {"type": "aws:s3/bucket:Bucket", "outputs": {}},
				"detailedDiff": {}
			},
			{
				"op": "update",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket2",
				"oldState": {"type": "aws:s3/bucket:Bucket", "outputs": {}},
				"newState": {"type": "aws:s3/bucket:Bucket", "outputs": {}},
				"detailedDiff": {}
			},
			{
				"op": "update",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket3",
				"oldState": {"type": "aws:s3/bucket:Bucket", "outputs": {}},
				"newState": {"type": "aws:s3/bucket:Bucket", "outputs": {}},
				"detailedDiff": {}
			},
			{
				"op": "update",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket4",
				"oldState": {"type": "aws:s3/bucket:Bucket", "outputs": {}},
				"newState": {"type": "aws:s3/bucket:Bucket", "outputs": {}},
				"detailedDiff": {}
			},
			{
				"op": "update",
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket5",
				"oldState": {"type": "aws:s3/bucket:Bucket", "outputs": {}},
				"newState": {"type": "aws:s3/bucket:Bucket", "outputs": {}},
				"detailedDiff": {}
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
			name:          "no limit (0)",
			maxResources:  "0",
			expectedCount: 5,
		},
		{
			name:          "default limit (10) - all resources returned",
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
			w.Close()
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
	w.Close()
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
	w.Close()
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
