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
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
)

// getPreviewOutput retrieves preview output from either a file or by running pulumi preview
func getPreviewOutput(eventsFile, projectDir, stack string, skipRefresh bool) ([]byte, error) {
	if eventsFile != "" {
		// Read events file instead of calling preview
		output, err := os.ReadFile(eventsFile)
		if err != nil {
			return nil, outputError(fmt.Sprintf("failed to read events file: %v", err))
		}
		return output, nil
	}

	// Build pulumi preview command with JSON output
	cmdArgs := []string{"preview", "--json", "--non-interactive", "--show-secrets"}
	if !skipRefresh {
		cmdArgs = append(cmdArgs, "--refresh")
	}
	if stack != "" {
		cmdArgs = append(cmdArgs, "--stack", stack)
	}

	previewCmd := exec.Command("pulumi", cmdArgs...)
	previewCmd.Dir = projectDir
	previewCmd.Stderr = os.Stderr

	output, err := previewCmd.Output()
	if err != nil {
		return nil, outputError(fmt.Sprintf("pulumi preview failed: %v", err))
	}
	return output, nil
}

// parsePreviewOutput parses preview output in either single JSON or NDJSON format.
// Returns the parsed steps, a count of entries that failed to parse, and any fatal error.
func parsePreviewOutput(output []byte) ([]auto.PreviewStep, int, error) {
	// Probe the top-level JSON keys to determine format
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(output, &probe); err == nil {
		// Format 1: {"steps": [...]} (from pulumi preview --json)
		if _, hasSteps := probe["steps"]; hasSteps {
			var rawSteps struct {
				Steps []json.RawMessage `json:"steps"`
			}
			if err := json.Unmarshal(output, &rawSteps); err == nil {
				var steps []auto.PreviewStep
				parseErrors := 0
				for _, raw := range rawSteps.Steps {
					var step auto.PreviewStep
					if err := json.Unmarshal(raw, &step); err != nil {
						parseErrors++
						continue
					}
					steps = append(steps, step)
				}
				if parseErrors > 0 {
					fmt.Fprintf(os.Stderr, "warning: %d preview steps failed to parse\n", parseErrors)
				}
				return steps, parseErrors, nil
			}
		}

		// Format 2: {"events": [...]} (from Pulumi Cloud GetEngineEvents API)
		if rawEvents, hasEvents := probe["events"]; hasEvents {
			var events []json.RawMessage
			if err := json.Unmarshal(rawEvents, &events); err == nil && len(events) > 0 {
				return parseEngineEvents(events)
			}
		}
	}

	// Format 3: NDJSON (newline-delimited JSON)
	// Format: {"resourcePreEvent": {...}}\n... (from pulumi_preview MCP tool)
	return parseNDJSON(output)
}

// parseEngineEvents parses engine events from the Pulumi Cloud API response format.
// Each event is a JSON object with {timestamp, type, resourcePreEvent: {metadata: {...}}}.
// Returns the parsed steps, a count of events that failed to parse, and any fatal error.
func parseEngineEvents(rawEvents []json.RawMessage) ([]auto.PreviewStep, int, error) {
	var steps []auto.PreviewStep
	parseErrors := 0
	for _, raw := range rawEvents {
		step, ok, err := parseEngineEvent(raw)
		if err != nil {
			parseErrors++
			continue
		}
		if ok {
			steps = append(steps, step)
		}
	}
	if parseErrors > 0 {
		fmt.Fprintf(os.Stderr, "warning: %d engine events failed to parse\n", parseErrors)
	}
	return steps, parseErrors, nil
}

// parseEngineEvent parses a single engine event JSON object. Returns a PreviewStep and true
// for resourcePreEvent events; returns (_, false, nil) for non-resource event types (prelude,
// summary, etc.); returns (_, false, error) when the event JSON is corrupt.
func parseEngineEvent(data []byte) (auto.PreviewStep, bool, error) {
	var event struct {
		Type             string `json:"type"`
		ResourcePreEvent *struct {
			Metadata json.RawMessage `json:"metadata"`
		} `json:"resourcePreEvent"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return auto.PreviewStep{}, false, fmt.Errorf("failed to unmarshal engine event: %w", err)
	}
	if event.Type != "resourcePreEvent" || event.ResourcePreEvent == nil {
		return auto.PreviewStep{}, false, nil
	}

	// Parse metadata using pulumi-service format:
	// - Uses "old"/"new" instead of "oldState"/"newState"
	// - Uses "diffKind" instead of "kind"
	// - Includes "type" field for resource type
	var customStep struct {
		Op           string                     `json:"op"`
		URN          string                     `json:"urn"`
		Type         string                     `json:"type"`
		Provider     string                     `json:"provider,omitempty"`
		Old          *apitype.ResourceV3        `json:"old,omitempty"`
		New          *apitype.ResourceV3        `json:"new,omitempty"`
		Diffs        []string                   `json:"diffs,omitempty"`
		DetailedDiff map[string]json.RawMessage `json:"detailedDiff"`
	}

	if err := json.Unmarshal(event.ResourcePreEvent.Metadata, &customStep); err != nil {
		return auto.PreviewStep{}, false, fmt.Errorf("failed to unmarshal resource event metadata: %w", err)
	}

	// Convert DetailedDiff from "diffKind" to standard "kind" format
	standardDetailedDiff := make(map[string]auto.PropertyDiff)
	for path, rawDiff := range customStep.DetailedDiff {
		var customDiff struct {
			DiffKind  string `json:"diffKind"`
			InputDiff bool   `json:"inputDiff"`
		}
		if err := json.Unmarshal(rawDiff, &customDiff); err == nil {
			standardDetailedDiff[path] = auto.PropertyDiff{
				Kind:      customDiff.DiffKind,
				InputDiff: customDiff.InputDiff,
			}
		}
	}

	// For replace operations, DetailedDiff is often empty but Diffs contains
	// the changed property keys. Synthesize DetailedDiff entries from Diffs
	// so that extractPropertyChanges can find them.
	if len(standardDetailedDiff) == 0 && len(customStep.Diffs) > 0 {
		for _, key := range customStep.Diffs {
			standardDetailedDiff[key] = auto.PropertyDiff{
				Kind:      "update",
				InputDiff: true,
			}
		}
	}

	// Convert to standard PreviewStep format
	return auto.PreviewStep{
		Op:           customStep.Op,
		URN:          resource.URN(customStep.URN),
		Provider:     customStep.Provider,
		OldState:     customStep.Old, // Map "old" -> "OldState"
		NewState:     customStep.New, // Map "new" -> "NewState"
		DetailedDiff: standardDetailedDiff,
	}, true, nil
}

// parseNDJSON parses NDJSON format with resourcePreEvent objects from pulumi-service MCP tool.
// Returns the parsed steps, a count of lines that failed to parse, and any fatal error.
func parseNDJSON(output []byte) ([]auto.PreviewStep, int, error) {
	var steps []auto.PreviewStep
	lines := strings.Split(string(output), "\n")
	validLinesFound := 0
	jsonErrors := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Check if it's valid JSON
		var raw json.RawMessage
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			jsonErrors++
			continue
		}
		validLinesFound++

		step, ok, err := parseEngineEvent([]byte(line))
		if err != nil {
			jsonErrors++
			continue
		}
		if ok {
			steps = append(steps, step)
		}
	}

	// If we found no valid JSON lines at all, the input is malformed
	if validLinesFound == 0 && len(strings.TrimSpace(string(output))) > 0 {
		return nil, 0, outputError("failed to parse preview output: no valid JSON found")
	}

	if jsonErrors > 0 {
		fmt.Fprintf(os.Stderr, "warning: %d NDJSON lines failed to parse\n", jsonErrors)
	}

	return steps, jsonErrors, nil
}

// getStateExport runs pulumi stack export and returns the parsed lookup map in memory.
// No file is written to disk.
func getStateExport(projectDir, stack string) (map[string]*apitype.ResourceV3, error) {
	// Run pulumi stack export to get full state (--show-secrets so secret
	// outputs are plaintext, enabling value matching in findMatchingOutput)
	cmdArgs := []string{"stack", "export", "--show-secrets"}
	if stack != "" {
		cmdArgs = append(cmdArgs, "--stack", stack)
	}

	exportCmd := exec.Command("pulumi", cmdArgs...)
	exportCmd.Dir = projectDir
	exportCmd.Stderr = os.Stderr

	output, err := exportCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("pulumi stack export failed: %w", err)
	}

	return parseStateExport(output)
}

// parseStateExport parses pulumi stack export JSON into a URN-to-resource lookup map.
func parseStateExport(data []byte) (map[string]*apitype.ResourceV3, error) {
	var export struct {
		Version    int `json:"version"`
		Deployment struct {
			Resources []apitype.ResourceV3 `json:"resources"`
		} `json:"deployment"`
	}
	if err := json.Unmarshal(data, &export); err != nil {
		return nil, fmt.Errorf("failed to parse state export: %w", err)
	}

	lookup := make(map[string]*apitype.ResourceV3, len(export.Deployment.Resources))
	for i := range export.Deployment.Resources {
		res := &export.Deployment.Resources[i]
		lookup[string(res.URN)] = res
	}
	return lookup, nil
}

// buildStateLookupFromSteps builds a URN-to-resource lookup from preview steps.
// This allows dependency resolution even without a separate state file, using
// OldState from delete operations (which contain full resource state).
func buildStateLookupFromSteps(steps []auto.PreviewStep) map[string]*apitype.ResourceV3 {
	lookup := make(map[string]*apitype.ResourceV3)
	for i := range steps {
		if steps[i].OldState != nil {
			lookup[string(steps[i].URN)] = steps[i].OldState
		}
	}
	return lookup
}
