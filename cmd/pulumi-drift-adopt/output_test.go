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

	summary, full := runProcessTest(t, []byte(eventsContent))

	assert.Equal(t, StatusChangesNeeded, summary.Status)
	require.Len(t, full.Resources, 2, "Expected 2 actionable resources")
	assert.Equal(t, 1, summary.SkippedCount, "Expected 1 skipped resource")

	require.Len(t, full.Skipped, 1, "Expected 1 skipped resource in output file")
	assert.Equal(t, "incomplete-instance", full.Skipped[0].Name)
	assert.Equal(t, "missing_properties", full.Skipped[0].Reason)
	assert.Equal(t, ActionUpdateCode, full.Skipped[0].Action)
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

	summary, full := runProcessTestWithOptions(t, []byte(eventsContent), nil, []string{"urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket-a"}, "")

	assert.Equal(t, StatusChangesNeeded, summary.Status)
	require.Len(t, full.Resources, 1)
	assert.Equal(t, "bucket-b", full.Resources[0].Name)

	require.Len(t, full.Skipped, 1)
	assert.Equal(t, "bucket-a", full.Skipped[0].Name)
	assert.Equal(t, "excluded", full.Skipped[0].Reason)
	assert.Equal(t, ActionUpdateCode, full.Skipped[0].Action)
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

	summary, full := runProcessTest(t, []byte(eventsContent))

	assert.Equal(t, StatusStopWithSkipped, summary.Status)
	assert.Empty(t, full.Resources)
	assert.Equal(t, 1, summary.SkippedCount)
	require.Len(t, full.Skipped, 1)
	assert.Equal(t, "missing_properties", full.Skipped[0].Reason)
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

	summary, _ := runProcessTest(t, []byte(eventsContent))

	assert.Equal(t, StatusChangesNeeded, summary.Status)
	require.NotNil(t, summary.Summary, "summary should be present for changes_needed")

	s := summary.Summary
	assert.Equal(t, 4, s.Total)

	assert.Equal(t, 2, s.ByAction[ActionAddToCode])
	assert.Equal(t, 1, s.ByAction[ActionUpdateCode])
	assert.Equal(t, 1, s.ByAction[ActionDeleteFromCode])

	assert.Equal(t, 2, s.ByType["aws:s3/bucket:Bucket"])
	assert.Equal(t, 2, s.ByType["random:index/randomString:RandomString"])

	assert.Equal(t, 2, s.ByTypeAction["aws:s3/bucket:Bucket"][ActionAddToCode])
	assert.Equal(t, 1, s.ByTypeAction["random:index/randomString:RandomString"][ActionUpdateCode])
	assert.Equal(t, 1, s.ByTypeAction["random:index/randomString:RandomString"][ActionDeleteFromCode])
}

// TestNextCommandSummaryAbsentForClean verifies no summary when status is clean
func TestNextCommandSummaryAbsentForClean(t *testing.T) {
	summary, _ := runProcessTest(t, []byte(`{"steps": [{"op": "same", "urn": "urn:pulumi:dev::test::pulumi:pulumi:Stack::test-dev"}]}`))

	assert.Equal(t, StatusClean, summary.Status)
	assert.Nil(t, summary.Summary, "summary should be nil for clean status")
}

func TestSkipRefreshFlagAccepted(t *testing.T) {
	// --skip-refresh only affects the pulumi preview command (not run when using --events-file).
	// Verify the flag is accepted and clean input produces clean output.
	summary, _ := runProcessTest(t, []byte(`{"steps": [{"op": "same", "urn": "urn:pulumi:dev::test::pulumi:pulumi:Stack::test-dev"}]}`))
	assert.Equal(t, StatusClean, summary.Status)
}

// TestNextCommandTempFileDefault verifies that when --output-file is omitted (empty), a temp file is created
func TestNextCommandTempFileDefault(t *testing.T) {
	// Capture stdout from processNext with empty outputFile
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := processNext(nil, 0, nil, nil, "", "")

	_ = w.Close()
	os.Stdout = oldStdout
	require.NoError(t, err)

	stdoutBytes, err := io.ReadAll(r)
	require.NoError(t, err)

	var summary NextSummaryOutput
	require.NoError(t, json.Unmarshal(stdoutBytes, &summary))

	assert.Equal(t, StatusClean, summary.Status)
	assert.NotEmpty(t, summary.OutputFile, "outputFile should be set even when outputFile is empty")
	assert.Contains(t, summary.OutputFile, "drift-adopter-output-", "should use temp file naming convention")

	// Verify the temp file exists and is parseable
	data, err := os.ReadFile(summary.OutputFile)
	require.NoError(t, err)
	var full NextOutput
	require.NoError(t, json.Unmarshal(data, &full))
	assert.Equal(t, StatusClean, full.Status)

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

	summary, full := runProcessTest(t, []byte(eventsContent))

	assert.Equal(t, StatusChangesNeeded, summary.Status)
	assert.NotEmpty(t, summary.OutputFile)

	// Verify full output in the file
	require.Len(t, full.Resources, 1)
	assert.Equal(t, ActionUpdateCode, full.Resources[0].Action)

	// Also verify explicit output-file path works
	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "custom-output.json")

	steps, parseErrors, err := parsePreviewOutput([]byte(eventsContent))
	require.NoError(t, err)

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = processNext(steps, parseErrors, nil, nil, "", outputFile)

	_ = w.Close()
	os.Stdout = oldStdout
	require.NoError(t, err)

	stdoutBytes, err := io.ReadAll(r)
	require.NoError(t, err)

	var summary2 NextSummaryOutput
	require.NoError(t, json.Unmarshal(stdoutBytes, &summary2))
	assert.Equal(t, outputFile, summary2.OutputFile, "outputFile should match specified path")

	// Verify the file was written to the specified path
	data, err := os.ReadFile(outputFile)
	require.NoError(t, err)
	var full2 NextOutput
	require.NoError(t, json.Unmarshal(data, &full2))
	assert.Equal(t, StatusChangesNeeded, full2.Status)
	require.Len(t, full2.Resources, 1)
}
