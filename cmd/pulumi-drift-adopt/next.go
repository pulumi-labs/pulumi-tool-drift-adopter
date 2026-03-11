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
	"github.com/spf13/cobra"
)

var nextCmd = &cobra.Command{
	Use:   "next",
	Short: "Run preview and show what code changes are needed",
	Long: `Runs pulumi preview with --refresh to detect drift and analyzes the output.

The command automatically refreshes state to match actual infrastructure, then shows
differences between code and state:
- Old values (state) = what we want (desired)
- New values (code) = what currently exists in code (current/incorrect)

The tool inverts the preview logic to tell you what to change in your code.`,
	RunE: runNext,
}

func init() {
	nextCmd.Flags().String("stack", "", "Pulumi stack name (optional, uses current stack if not specified)")
	nextCmd.Flags().Int("max-resources", -1, "Maximum number of resources to return per batch (-1 = unlimited)")
	nextCmd.Flags().String("events-file", "", "Path to engine events file (skips calling preview)")
	nextCmd.Flags().StringSlice("exclude-urns", nil, "List of resource URNs to exclude from results")
}

// NextOutput is the JSON structure returned by the next command
type NextOutput struct {
	Status    string           `json:"status"` // "changes_needed", "clean", "stop_with_skipped", "error"
	Error     string           `json:"error,omitempty"`
	Summary   *NextSummary     `json:"summary,omitempty"`
	Resources []ResourceChange `json:"resources,omitempty"`
	Skipped   []ResourceChange `json:"skipped,omitempty"`
}

// NextSummary provides aggregate counts of drift for quick orientation
type NextSummary struct {
	Total        int                       `json:"total"`
	ByAction     map[string]int            `json:"byAction"`
	ByType       map[string]int            `json:"byType"`
	ByTypeAction map[string]map[string]int `json:"byTypeAction"`
}

// ResourceChange describes a resource that needs code changes
type ResourceChange struct {
	Action          string                 `json:"action"` // "update_code", "delete_from_code", "add_to_code"
	URN             string                 `json:"urn"`
	Type            string                 `json:"type"`
	Name            string                 `json:"name"`
	Properties      []PropertyChange       `json:"properties,omitempty"`
	InputProperties map[string]interface{} `json:"inputProperties,omitempty"`
	Reason          string                 `json:"reason,omitempty"` // Why skipped: "excluded", "missing_properties"
}

// PropertyChange describes a property that needs to be changed
type PropertyChange struct {
	Path         string      `json:"path"`
	CurrentValue interface{} `json:"currentValue"` // What's in code now (RHS from preview)
	DesiredValue interface{} `json:"desiredValue"` // What it should be (LHS from preview/state)
	Kind         string      `json:"kind"`         // add, delete, update
}

func runNext(cmd *cobra.Command, _ []string) error {
	projectDir, _ := cmd.Flags().GetString("project")
	stack, _ := cmd.Flags().GetString("stack")
	maxResources, _ := cmd.Flags().GetInt("max-resources")
	eventsFile, _ := cmd.Flags().GetString("events-file")
	excludeURNs, _ := cmd.Flags().GetStringSlice("exclude-urns")

	// Get preview output from file or command
	output, err := getPreviewOutput(eventsFile, projectDir, stack)
	if err != nil {
		return err
	}

	// Parse preview output into steps
	steps, err := parsePreviewOutput(output)
	if err != nil {
		return err
	}

	// Convert steps to resource changes
	resources := convertStepsToResources(steps)

	// Output result with resource limit and exclusions
	return outputResult(resources, maxResources, excludeURNs)
}

// getPreviewOutput retrieves preview output from either a file or by running pulumi preview
func getPreviewOutput(eventsFile, projectDir, stack string) ([]byte, error) {
	if eventsFile != "" {
		// Read events file instead of calling preview
		output, err := os.ReadFile(eventsFile)
		if err != nil {
			return nil, outputError(fmt.Sprintf("failed to read events file: %v", err))
		}
		return output, nil
	}

	// Build pulumi preview command with JSON output and refresh
	cmdArgs := []string{"preview", "--json", "--non-interactive", "--refresh"}
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

// parsePreviewOutput parses preview output in either single JSON or NDJSON format
func parsePreviewOutput(output []byte) ([]auto.PreviewStep, error) {
	// Probe the top-level JSON keys to determine format
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(output, &probe); err == nil {
		// Format 1: {"steps": [...]} (from pulumi preview --json)
		if _, hasSteps := probe["steps"]; hasSteps {
			var previewResult struct {
				Steps []auto.PreviewStep `json:"steps"`
			}
			if err := json.Unmarshal(output, &previewResult); err == nil {
				return previewResult.Steps, nil
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
func parseEngineEvents(rawEvents []json.RawMessage) ([]auto.PreviewStep, error) {
	var steps []auto.PreviewStep
	for _, raw := range rawEvents {
		if step, ok := parseEngineEvent(raw); ok {
			steps = append(steps, step)
		}
	}
	return steps, nil
}

// parseEngineEvent parses a single engine event JSON object. Returns a PreviewStep and true
// for resourcePreEvent events; returns false for all other event types (prelude, summary, etc.).
func parseEngineEvent(data []byte) (auto.PreviewStep, bool) {
	var event struct {
		Type             string `json:"type"`
		ResourcePreEvent *struct {
			Metadata json.RawMessage `json:"metadata"`
		} `json:"resourcePreEvent"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return auto.PreviewStep{}, false
	}
	if event.Type != "resourcePreEvent" || event.ResourcePreEvent == nil {
		return auto.PreviewStep{}, false
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
		return auto.PreviewStep{}, false
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
	}, true
}

// parseNDJSON parses NDJSON format with resourcePreEvent objects from pulumi-service MCP tool
func parseNDJSON(output []byte) ([]auto.PreviewStep, error) {
	var steps []auto.PreviewStep
	lines := strings.Split(string(output), "\n")
	validLinesFound := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Check if it's valid JSON
		var raw json.RawMessage
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		validLinesFound++

		if step, ok := parseEngineEvent([]byte(line)); ok {
			steps = append(steps, step)
		}
	}

	// If we found no valid JSON lines at all, the input is malformed
	if validLinesFound == 0 && len(strings.TrimSpace(string(output))) > 0 {
		return nil, outputError("failed to parse preview output: no valid JSON found")
	}

	return steps, nil
}

// convertStepsToResources converts preview steps to resource changes for drift adoption
func convertStepsToResources(steps []auto.PreviewStep) []ResourceChange {
	var resources []ResourceChange

	for i := range steps {
		step := &steps[i]

		// Skip operations that don't require code changes
		action := getActionForOperation(step.Op)
		if action == "" {
			continue
		}

		// Normalize DetailedDiff so extractPropertyChanges has a single code path
		normalizeDetailedDiff(step)

		// Extract resource metadata
		resourceType := extractResourceType(*step)
		name := extractResourceName(string(step.URN))

		if action == "add_to_code" {
			// For add_to_code, use flat key-value map (more compact than PropertyChange array)
			inputProps := extractInputProperties(*step)
			resources = append(resources, ResourceChange{
				Action:          action,
				URN:             string(step.URN),
				Type:            resourceType,
				Name:            name,
				InputProperties: inputProps,
			})
		} else {
			// For update_code and delete_from_code, use property change array
			properties := extractPropertyChanges(*step)
			resources = append(resources, ResourceChange{
				Action:     action,
				URN:        string(step.URN),
				Type:       resourceType,
				Name:       name,
				Properties: properties,
			})
		}
	}

	return resources
}

// getActionForOperation inverts the preview operation to determine the code action
func getActionForOperation(op string) string {
	switch op {
	case "create":
		// Preview wants to CREATE = resource is in code but not in state
		// Action: DELETE from code
		return "delete_from_code"
	case "delete":
		// Preview wants to DELETE = resource is in state but not in code
		// Action: ADD to code
		return "add_to_code"
	case "update", "replace":
		// Preview wants to UPDATE/REPLACE = resource exists in both but differs
		// Action: UPDATE code to match state
		return "update_code"
	default:
		// Skip "same", "refresh", etc.
		return ""
	}
}

// invertPropertyKind inverts the property change kind from preview perspective to code change perspective
func invertPropertyKind(previewKind string) string {
	switch previewKind {
	case "add":
		// Preview wants to ADD to infrastructure = property in code but not in state
		// Action: DELETE from code
		return "delete"
	case "delete":
		// Preview wants to DELETE from infrastructure = property in state but not in code
		// Action: ADD to code
		return "add"
	case "update", "update-replace":
		// Update is symmetric - need to update code to match state
		return "update"
	default:
		// Pass through other kinds unchanged
		return previewKind
	}
}

// normalizeDetailedDiff synthesizes DetailedDiff entries from ReplaceReasons/DiffReasons
// for replace/update steps that have an empty DetailedDiff. This lets extractPropertyChanges
// use a single code path for all update/replace operations.
func normalizeDetailedDiff(step *auto.PreviewStep) {
	if len(step.DetailedDiff) > 0 || (step.Op != "replace" && step.Op != "update") {
		return
	}
	diffKeys := step.ReplaceReasons
	if len(diffKeys) == 0 {
		diffKeys = step.DiffReasons
	}
	if len(diffKeys) == 0 {
		return
	}
	step.DetailedDiff = make(map[string]auto.PropertyDiff, len(diffKeys))
	for _, key := range diffKeys {
		step.DetailedDiff[string(key)] = auto.PropertyDiff{Kind: "update", InputDiff: true}
	}
}

// resolvePropertyValue looks up a property value from a resource state,
// trying Outputs first (unless inputsOnly) then falling back to Inputs.
func resolvePropertyValue(state *apitype.ResourceV3, path string, inputsOnly bool) interface{} {
	if state == nil {
		return nil
	}
	if !inputsOnly && state.Outputs != nil {
		if v := getNestedProperty(state.Outputs, path); v != nil {
			return v
		}
	}
	if state.Inputs != nil {
		return getNestedProperty(state.Inputs, path)
	}
	return nil
}

// extractInputProperties returns a flat key-value map of input properties for add_to_code resources.
// Prefers Inputs (what the user originally wrote) over Outputs (which include computed values).
func extractInputProperties(step auto.PreviewStep) map[string]interface{} {
	if step.OldState == nil {
		return nil
	}
	source := step.OldState.Inputs
	if len(source) == 0 {
		source = step.OldState.Outputs
	}
	return source
}

// extractResourceType extracts the resource type from old or new state
func extractResourceType(step auto.PreviewStep) string {
	if step.OldState != nil {
		return string(step.OldState.Type)
	}
	if step.NewState != nil {
		return string(step.NewState.Type)
	}
	return ""
}

// extractPropertyChanges extracts property changes from a preview step.
// For update/replace ops, normalizeDetailedDiff must be called first so that
// DetailedDiff is always populated when diff information is available.
func extractPropertyChanges(step auto.PreviewStep) []PropertyChange {
	var properties []PropertyChange

	// For delete operations (add_to_code), DetailedDiff is empty but we need all properties from state.
	// Prefer Inputs (what the user originally wrote) over Outputs (which include computed values).
	if step.Op == "delete" && len(step.DetailedDiff) == 0 && step.OldState != nil {
		source := step.OldState.Inputs
		if len(source) == 0 {
			source = step.OldState.Outputs
		}
		if source != nil {
			extractAllProperties(source, "", &properties)
		}
		return properties
	}

	for path, diff := range step.DetailedDiff {
		inputsOnly := diff.InputDiff
		currentValue := resolvePropertyValue(step.NewState, path, inputsOnly)
		desiredValue := resolvePropertyValue(step.OldState, path, inputsOnly)

		kind := invertPropertyKind(diff.Kind)
		// For entries synthesized by normalizeDetailedDiff (Kind="update", InputDiff=true),
		// refine kind based on nil values (preserving original behavior where
		// ReplaceReasons-derived entries get "delete"/"add" based on nil values).
		if inputsOnly && diff.Kind == "update" {
			if currentValue == nil {
				kind = "delete"
			} else if desiredValue == nil {
				kind = "add"
			}
		}

		properties = append(properties, PropertyChange{
			Path:         path,
			CurrentValue: currentValue,
			DesiredValue: desiredValue,
			Kind:         kind,
		})
	}

	return properties
}

// extractAllProperties recursively extracts all properties from a map for add_to_code operations
func extractAllProperties(props map[string]interface{}, prefix string, properties *[]PropertyChange) {
	for key, value := range props {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}

		// If value is a nested map, recurse
		if nestedMap, ok := value.(map[string]interface{}); ok {
			extractAllProperties(nestedMap, path, properties)
		} else {
			// Leaf property - add it
			*properties = append(*properties, PropertyChange{
				Path:         path,
				CurrentValue: nil,   // Not in code
				DesiredValue: value, // From state
				Kind:         "add", // Need to add to code
			})
		}
	}
}

// outputResult outputs the final JSON result with filtering, exclusions, and resource limiting
func outputResult(resources []ResourceChange, maxResources int, excludeURNs []string) error {
	// Build exclude set for O(1) lookup
	excludeSet := make(map[string]bool, len(excludeURNs))
	for _, urn := range excludeURNs {
		excludeSet[urn] = true
	}

	// Partition resources into actionable and skipped
	var actionable, skipped []ResourceChange
	for _, res := range resources {
		if excludeSet[res.URN] {
			res.Reason = "excluded"
			skipped = append(skipped, res)
		} else if res.Action == "add_to_code" && len(res.InputProperties) == 0 {
			res.Reason = "missing_properties"
			skipped = append(skipped, res)
		} else if res.Action == "update_code" && len(res.Properties) == 0 {
			res.Reason = "missing_properties"
			skipped = append(skipped, res)
		} else {
			actionable = append(actionable, res)
		}
	}

	// Compute summary from full actionable set (before truncation)
	var summary *NextSummary
	if len(actionable) > 0 {
		summary = &NextSummary{
			Total:        len(actionable),
			ByAction:     make(map[string]int),
			ByType:       make(map[string]int),
			ByTypeAction: make(map[string]map[string]int),
		}
		for _, res := range actionable {
			summary.ByAction[res.Action]++
			summary.ByType[res.Type]++
			if summary.ByTypeAction[res.Type] == nil {
				summary.ByTypeAction[res.Type] = make(map[string]int)
			}
			summary.ByTypeAction[res.Type][res.Action]++
		}
	}

	// Apply resource limit to actionable bucket only
	if maxResources > 0 && len(actionable) > maxResources {
		actionable = actionable[:maxResources]
	}

	// Determine status
	result := NextOutput{}
	switch {
	case len(actionable) > 0:
		result.Status = "changes_needed"
		result.Summary = summary
		result.Resources = actionable
	case len(skipped) > 0:
		result.Status = "stop_with_skipped"
	default:
		result.Status = "clean"
	}
	if len(skipped) > 0 {
		result.Skipped = skipped
	}

	// Output JSON
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return fmt.Errorf("failed to encode output: %w", err)
	}

	return nil
}

func outputError(errMsg string) error {
	output := NextOutput{
		Status: "error",
		Error:  errMsg,
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		return fmt.Errorf("failed to encode error message %s with error %w", errMsg, err)
	}
	return fmt.Errorf("%s", errMsg)
}

func extractResourceName(urn string) string {
	// URN format: urn:pulumi:stack::project::type::name
	parts := strings.Split(urn, "::")
	if len(parts) >= 4 {
		return parts[len(parts)-1]
	}
	return ""
}

// getNestedProperty extracts a value from a nested map using a dot-separated path
func getNestedProperty(props map[string]interface{}, path string) interface{} {
	parts := strings.Split(path, ".")
	var current interface{} = props

	for _, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current, ok = m[part]
		if !ok {
			return nil
		}
	}

	return current
}
