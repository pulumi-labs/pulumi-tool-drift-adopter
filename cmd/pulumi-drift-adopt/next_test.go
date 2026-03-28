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

	"github.com/stretchr/testify/require"
)

// runProcessTest calls processNext directly with the given preview output bytes and no dep map.
// Use this for tests that only need to verify parsing, property extraction, and output —
// without needing CLI flags, state export, or a live Pulumi stack.
func runProcessTest(t *testing.T, input []byte) (NextSummaryOutput, NextOutput) {
	t.Helper()
	return runProcessTestWithOptions(t, input, nil, nil, "")
}

// runProcessTestWithOptions calls processNext with the given preview output, metadata, and exclude URNs.
func runProcessTestWithOptions(t *testing.T, input []byte, meta *ResourceMetadata, excludeURNs []string, depMapPath string) (NextSummaryOutput, NextOutput) {
	t.Helper()

	steps, parseErrors, err := parsePreviewOutput(input)
	require.NoError(t, err, "parsePreviewOutput failed")

	tmpDir := t.TempDir()
	outputFile := filepath.Join(tmpDir, "output.json")

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	outErr := processNext(steps, parseErrors, meta, excludeURNs, depMapPath, outputFile)

	_ = w.Close()
	os.Stdout = oldStdout
	require.NoError(t, outErr, "processNext failed")

	stdoutBytes, err := io.ReadAll(r)
	require.NoError(t, err)

	var summary NextSummaryOutput
	err = json.Unmarshal(stdoutBytes, &summary)
	require.NoError(t, err, "Failed to parse stdout: %s", string(stdoutBytes))

	var full NextOutput
	if summary.OutputFile != "" {
		data, err := os.ReadFile(summary.OutputFile)
		require.NoError(t, err)
		err = json.Unmarshal(data, &full)
		require.NoError(t, err)
	}

	return summary, full
}

// runProcessTestFile is a convenience wrapper that reads from a file path.
func runProcessTestFile(t *testing.T, path string) (NextSummaryOutput, NextOutput) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return runProcessTest(t, data)
}

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
