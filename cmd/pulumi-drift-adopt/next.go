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

// newNextCmd creates a fresh "next" subcommand with all flags registered.
func newNextCmd() *cobra.Command {
	cmd := &cobra.Command{
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
	cmd.Flags().String("stack", "", "Pulumi stack name (optional, uses current stack if not specified)")
	cmd.Flags().Int("max-resources", -1, "Maximum number of resources to return per batch (-1 = unlimited)")
	cmd.Flags().String("events-file", "", "Path to engine events file (skips calling preview)")
	cmd.Flags().StringSlice("exclude-urns", nil, "List of resource URNs to exclude from results")
	cmd.Flags().String("dep-map-file", "", "Path to dependency map from a previous run (skips stack export)")
	cmd.Flags().Bool("skip-refresh", false, "Omit --refresh from pulumi preview")
	cmd.Flags().String("output-file", "", "Path for full output file (defaults to auto-generated temp file)")
	return cmd
}

// NextOutput is the full JSON structure written to the output file
type NextOutput struct {
	Status     string           `json:"status"` // "changes_needed", "clean", "stop_with_skipped", "error"
	Error      string           `json:"error,omitempty"`
	Summary    *NextSummary     `json:"summary,omitempty"`
	Resources  []ResourceChange `json:"resources,omitempty"`
	Skipped    []ResourceChange `json:"skipped,omitempty"`
	DepMapFile string           `json:"depMapFile,omitempty"`
}

// NextSummaryOutput is the compact JSON written to stdout.
// The agent reads full resource details from OutputFile using its Read tool.
type NextSummaryOutput struct {
	Status       string       `json:"status"`
	Error        string       `json:"error,omitempty"`
	Summary      *NextSummary `json:"summary,omitempty"`
	OutputFile   string       `json:"outputFile,omitempty"`
	DepMapFile   string       `json:"depMapFile,omitempty"`
	SkippedCount int          `json:"skippedCount,omitempty"`
}

// NextSummary provides aggregate counts of drift for quick orientation
type NextSummary struct {
	Total        int                       `json:"total"`
	ByAction     map[string]int            `json:"byAction"`
	ByType       map[string]int            `json:"byType"`
	ByTypeAction map[string]map[string]int `json:"byTypeAction"`
}

// DependencyMap maps resource URN → property path → dependency metadata.
// Contains no secret values — only resource names, types, and output property names.
type DependencyMap map[string]map[string]DependencyRef

// DependencyRef describes a single property-level dependency on another resource.
type DependencyRef struct {
	ResourceName   string `json:"resourceName"`
	ResourceType   string `json:"resourceType"`
	OutputProperty string `json:"outputProperty,omitempty"`
}

// ResourceChange describes a resource that needs code changes
type ResourceChange struct {
	Action          string                 `json:"action"` // "update_code", "delete_from_code", "add_to_code"
	URN             string                 `json:"urn"`
	Type            string                 `json:"type"`
	Name            string                 `json:"name"`
	DependencyLevel int                    `json:"dependencyLevel,omitempty"`
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
	depMapFile, _ := cmd.Flags().GetString("dep-map-file")
	skipRefresh, _ := cmd.Flags().GetBool("skip-refresh")
	outputFile, _ := cmd.Flags().GetString("output-file")

	// Get preview output from file or command
	output, err := getPreviewOutput(eventsFile, projectDir, stack, skipRefresh)
	if err != nil {
		return err
	}

	// Parse preview output into steps
	steps, err := parsePreviewOutput(output)
	if err != nil {
		return err
	}

	var depMap DependencyMap
	var stateLookup map[string]*apitype.ResourceV3

	if depMapFile != "" {
		// Load pre-computed dependency map — skip state export entirely
		depMap, err = loadDepMap(depMapFile)
		if err != nil {
			return err
		}
		// Still build step lookup for fallback resolution (preview-only resources)
		stateLookup = buildStateLookupFromSteps(steps)
	} else {
		// Load state for dependency resolution (in-memory only, no file written)
		stateLookup, err = getStateExport(projectDir, stack)
		if err != nil {
			return err
		}

		// Supplement with state from preview steps (OldState of delete operations)
		stepLookup := buildStateLookupFromSteps(steps)
		if stateLookup == nil {
			stateLookup = stepLookup
		} else {
			for urn, res := range stepLookup {
				if _, exists := stateLookup[urn]; !exists {
					stateLookup[urn] = res
				}
			}
		}

		// Build complete dependency map from state — discards secret values
		depMap = buildDepMapFromState(stateLookup)
	}

	// Convert steps to resource changes
	resources := convertStepsToResources(steps, depMap, stateLookup)

	// Sort by dependency order to reduce context pressure: leaf nodes first
	resources = sortResourcesByDependencies(resources)

	// Save dependency map for reuse in subsequent calls
	depMapPath, err := saveDepMap(depMap, depMapFile)
	if err != nil {
		// Non-fatal — proceed without dep map path
		depMapPath = ""
	}

	// Output result with resource limit and exclusions
	return outputResult(resources, maxResources, excludeURNs, depMapPath, outputFile)
}

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
	cmdArgs := []string{"preview", "--json", "--non-interactive"}
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

// convertStepsToResources converts preview steps to resource changes for drift adoption.
// depMap is used for dependency resolution; stateLookup is a fallback for resolution
// when depMap doesn't cover a property (e.g., preview-only steps not in state).
func convertStepsToResources(steps []auto.PreviewStep, depMap DependencyMap, stateLookup map[string]*apitype.ResourceV3) []ResourceChange {
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
			inputProps := extractInputProperties(*step, depMap, stateLookup)
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
	case "add", "add-replace":
		// Preview wants to ADD to infrastructure = property in code but not in state
		// Action: DELETE from code
		return "delete"
	case "delete", "delete-replace":
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
		// State export failure is non-fatal — proceed without dependency resolution
		return nil, nil
	}

	return parseStateExport(output)
}

// parseStateFile reads a pulumi stack export JSON and returns a URN-to-resource lookup map.
func parseStateFile(path string) (map[string]*apitype.ResourceV3, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}
	return parseStateExport(data)
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

// buildDepMapFromState iterates ALL resources in the state lookup and resolves
// every property dependency, producing a complete DependencyMap. The map contains
// only resource names, types, and output property names — no secret values.
func buildDepMapFromState(lookup map[string]*apitype.ResourceV3) DependencyMap {
	depMap := make(DependencyMap)
	for urn, res := range lookup {
		if len(res.PropertyDependencies) == 0 {
			continue
		}

		source := res.Inputs
		if len(source) == 0 {
			source = res.Outputs
		}
		if len(source) == 0 {
			continue
		}

		propRefs := make(map[string]DependencyRef)

		for propKey, depURNs := range res.PropertyDependencies {
			if len(depURNs) == 0 {
				continue
			}
			value := source[string(propKey)]
			if value == nil {
				continue
			}

			// For map values: resolve each map entry individually
			if m, ok := value.(map[string]interface{}); ok {
				for mk, mv := range m {
					path := string(propKey) + "." + mk
					if ref := resolveDepRef(mv, depURNs, lookup); ref != nil {
						propRefs[path] = *ref
					}
				}
				continue
			}

			// For array values: resolve each element individually
			if arr, ok := value.([]interface{}); ok {
				for i, elem := range arr {
					path := fmt.Sprintf("%s[%d]", string(propKey), i)
					if ref := resolveDepRef(elem, depURNs, lookup); ref != nil {
						propRefs[path] = *ref
					}
				}
				continue
			}

			// Scalar: try to resolve the dependency directly
			if ref := resolveDepRef(value, depURNs, lookup); ref != nil {
				propRefs[string(propKey)] = *ref
			}
		}

		if len(propRefs) > 0 {
			depMap[urn] = propRefs
		}
	}
	return depMap
}

// resolveDepRef attempts to match a value against dependent resource outputs,
// returning a DependencyRef if found. Falls back to bare ref (no outputProperty)
// when exactly one dep URN exists.
func resolveDepRef(value interface{}, depURNs []resource.URN, lookup map[string]*apitype.ResourceV3) *DependencyRef {
	for _, depURN := range depURNs {
		depRes, ok := lookup[string(depURN)]
		if !ok {
			continue
		}
		outputProp := findMatchingOutput(value, depRes.Outputs)
		if outputProp != "" {
			return &DependencyRef{
				ResourceName:   extractResourceName(string(depURN)),
				ResourceType:   string(depRes.Type),
				OutputProperty: outputProp,
			}
		}
	}
	// Bare fallback when exactly one dep URN
	if len(depURNs) == 1 {
		if depRes, ok := lookup[string(depURNs[0])]; ok {
			return &DependencyRef{
				ResourceName: extractResourceName(string(depURNs[0])),
				ResourceType: string(depRes.Type),
			}
		}
	}
	return nil
}

// loadDepMap reads a dependency map from a JSON file.
func loadDepMap(path string) (DependencyMap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read dep map file: %w", err)
	}
	var depMap DependencyMap
	if err := json.Unmarshal(data, &depMap); err != nil {
		return nil, fmt.Errorf("failed to parse dep map file: %w", err)
	}
	return depMap, nil
}

// saveDepMap writes a dependency map to the specified path, or to an auto-generated
// temp file if path is empty. Returns the path written to.
func saveDepMap(depMap DependencyMap, path string) (string, error) {
	var f *os.File
	var err error
	if path != "" {
		f, err = os.Create(path)
	} else {
		f, err = os.CreateTemp("", "drift-adopter-depmap-*.json")
	}
	if err != nil {
		return "", fmt.Errorf("failed to create dep map file: %w", err)
	}
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(depMap); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("failed to write dep map: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// maxStringValueLen is the maximum length of a string property value before truncation.
// Values longer than this are replaced with a placeholder to keep output compact.
const maxStringValueLen = 200

// extractInputProperties returns a flat key-value map of input properties for add_to_code resources.
// When a depMap is available, properties are enriched with dependsOn metadata from pre-computed
// dependency references. When no depMap is available, falls back to stateLookup-based resolution.
//
// Properties without dependencies remain plain values (backward-compatible), with long
// strings truncated to maxStringValueLen characters.
// Properties with dependencies become: {"dependsOn": {"resourceName": ..., "resourceType": ..., "outputProperty": ...}}
// (the literal value is omitted — the agent should use the resource reference).
func extractInputProperties(step auto.PreviewStep, depMap DependencyMap, stateLookup map[string]*apitype.ResourceV3) map[string]interface{} {
	if step.OldState == nil {
		return nil
	}
	source := step.OldState.Inputs
	if len(source) == 0 {
		source = step.OldState.Outputs
	}
	if len(source) == 0 {
		return nil
	}

	urn := string(step.URN)

	// Check for dep map entries for this resource
	urnDeps := depMap[urn]

	// If no dep map entries and no property dependencies, return with truncation only
	propDeps := step.OldState.PropertyDependencies
	if len(urnDeps) == 0 && (stateLookup == nil || len(propDeps) == 0) {
		return truncateStringValues(source)
	}

	// Enrich properties that have dependencies
	result := make(map[string]interface{}, len(source))
	for key, value := range source {
		// For map values: resolve each map entry individually
		if m, ok := value.(map[string]interface{}); ok {
			resolvedMap := make(map[string]interface{}, len(m))
			for mk, mv := range m {
				path := key + "." + mk
				if ref, ok := urnDeps[path]; ok {
					resolvedMap[mk] = depRefToDependsOn(ref)
				} else if dep := resolveDependencyFallback(mv, key, propDeps, stateLookup); dep != nil {
					resolvedMap[mk] = dep
				} else {
					resolvedMap[mk] = truncateValue(mv)
				}
			}
			result[key] = resolvedMap
			continue
		}

		// For array values: resolve each element individually
		if arr, ok := value.([]interface{}); ok {
			resolvedArr := make([]interface{}, len(arr))
			for i, elem := range arr {
				path := fmt.Sprintf("%s[%d]", key, i)
				if ref, ok := urnDeps[path]; ok {
					resolvedArr[i] = depRefToDependsOn(ref)
				} else if dep := resolveDependencyFallback(elem, key, propDeps, stateLookup); dep != nil {
					resolvedArr[i] = dep
				} else {
					resolvedArr[i] = truncateValue(elem)
				}
			}
			result[key] = resolvedArr
			continue
		}

		// Scalar: check dep map first, then fallback
		if ref, ok := urnDeps[key]; ok {
			result[key] = depRefToDependsOn(ref)
		} else if dep := resolveDependencyFallback(value, key, propDeps, stateLookup); dep != nil {
			result[key] = dep
		} else {
			result[key] = truncateValue(value)
		}
	}
	return result
}

// depRefToDependsOn converts a DependencyRef to the dependsOn map format used in output.
func depRefToDependsOn(ref DependencyRef) map[string]interface{} {
	dep := map[string]interface{}{
		"resourceName": ref.ResourceName,
		"resourceType": ref.ResourceType,
	}
	if ref.OutputProperty != "" {
		dep["outputProperty"] = ref.OutputProperty
	}
	return map[string]interface{}{"dependsOn": dep}
}

// resolveDependencyFallback attempts resolution using stateLookup when dep map doesn't cover a property.
func resolveDependencyFallback(value interface{}, key string, propDeps map[resource.PropertyKey][]resource.URN, stateLookup map[string]*apitype.ResourceV3) map[string]interface{} {
	if stateLookup == nil || len(propDeps) == 0 {
		return nil
	}
	depURNs := propDeps[resource.PropertyKey(key)]
	if len(depURNs) == 0 {
		return nil
	}
	return resolveDependency(value, depURNs, stateLookup)
}

// truncateStringValues returns a shallow copy of props with long string values truncated.
func truncateStringValues(props map[string]interface{}) map[string]interface{} {
	needsTruncation := false
	for _, v := range props {
		if s, ok := v.(string); ok && len(s) > maxStringValueLen {
			needsTruncation = true
			break
		}
	}
	if !needsTruncation {
		return props
	}
	result := make(map[string]interface{}, len(props))
	for k, v := range props {
		result[k] = truncateValue(v)
	}
	return result
}

// truncateValue truncates a string value if it exceeds maxStringValueLen.
// Non-string values are returned as-is.
func truncateValue(v interface{}) interface{} {
	if s, ok := v.(string); ok && len(s) > maxStringValueLen {
		return fmt.Sprintf("<string: %d chars>", len(s))
	}
	return v
}

// resolveDependency attempts to match a resolved input value against the outputs
// of dependent resources to identify the source output property.
func resolveDependency(value interface{}, depURNs []resource.URN, stateLookup map[string]*apitype.ResourceV3) map[string]interface{} {
	for _, depURN := range depURNs {
		depRes, ok := stateLookup[string(depURN)]
		if !ok {
			continue
		}

		// Search outputs for exact value match
		outputProp := findMatchingOutput(value, depRes.Outputs)
		if outputProp == "" {
			continue
		}

		// Omit the literal value — the agent should use the resource reference
		return map[string]interface{}{
			"dependsOn": map[string]interface{}{
				"resourceName":   extractResourceName(string(depURN)),
				"resourceType":   string(depRes.Type),
				"outputProperty": outputProp,
			},
		}
	}

	// Fallback: when value match fails but exactly one dep URN exists,
	// emit bare dependsOn (without outputProperty) so the agent knows
	// which resource to reference even when structural mismatch prevents
	// exact value matching (e.g., input is ["value"] array, output is "value" string).
	if len(depURNs) == 1 {
		depRes, ok := stateLookup[string(depURNs[0])]
		if ok {
			return map[string]interface{}{
				"dependsOn": map[string]interface{}{
					"resourceName": extractResourceName(string(depURNs[0])),
					"resourceType": string(depRes.Type),
				},
			}
		}
	}
	return nil
}

// findMatchingOutput searches a resource's outputs for one whose value exactly matches
// the given input value. Returns the output property name, or "" if no match found.
//
// Known limitation: Pulumi secret values are wrapped in a sentinel structure
// ({"4dabf18193072939515e22adb298388d": "1b47061264138c4ac30d75fd1eb44270", "ciphertext": "..."}).
// If an output is a secret, the exact-match comparison will fail because the input has the
// plaintext value while the output has the encrypted wrapper. In this case, the function
// falls back to returning "" and the property is emitted as a plain value without dependsOn.
func findMatchingOutput(inputValue interface{}, outputs map[string]interface{}) string {
	if outputs == nil {
		return ""
	}

	// Marshal input value for reliable comparison (handles nested structures)
	inputJSON, err := json.Marshal(inputValue)
	if err != nil {
		return ""
	}

	for key, outputValue := range outputs {
		// Skip Pulumi secret-wrapped values (sentinel: "4dabf18193072939515e22adb298388d")
		if m, ok := outputValue.(map[string]interface{}); ok {
			if _, isSecret := m["4dabf18193072939515e22adb298388d"]; isSecret {
				continue
			}
		}

		outputJSON, err := json.Marshal(outputValue)
		if err != nil {
			continue
		}
		if string(inputJSON) == string(outputJSON) {
			return key
		}
	}
	return ""
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

		// Skip properties where both values are nil — computed-only fields
		// in diff metadata with no actionable values for the agent.
		if currentValue == nil && desiredValue == nil {
			continue
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

// collectDependencyNames recursively scans a value for dependsOn entries, appending
// any referenced resourceName values that exist in nameSet to deps (deduped via seen).
func collectDependencyNames(value interface{}, nameSet map[string]bool, seen map[string]bool, deps *[]string) {
	switch v := value.(type) {
	case map[string]interface{}:
		// If this map is itself a dependsOn wrapper, extract the resourceName and stop
		if dep, ok := v["dependsOn"]; ok {
			if depMap, ok := dep.(map[string]interface{}); ok {
				if name, ok := depMap["resourceName"].(string); ok && nameSet[name] && !seen[name] {
					seen[name] = true
					*deps = append(*deps, name)
				}
			}
			return // don't recurse further into the dependsOn structure
		}
		// Otherwise recurse into map values (handles map properties after element-level resolution)
		for _, mv := range v {
			collectDependencyNames(mv, nameSet, seen, deps)
		}
	case []interface{}:
		for _, elem := range v {
			collectDependencyNames(elem, nameSet, seen, deps)
		}
	}
}

// extractDependencyNames returns the names of resources (within nameSet) that res depends on,
// by scanning inputProperties for dependsOn entries at any nesting depth.
func extractDependencyNames(res ResourceChange, nameSet map[string]bool) []string {
	var deps []string
	seen := make(map[string]bool)
	for _, value := range res.InputProperties {
		collectDependencyNames(value, nameSet, seen, &deps)
	}
	return deps
}

// sortResourcesByDependencies sorts resources in dependency order (leaf nodes first)
// using Kahn's topological sort algorithm, and assigns DependencyLevel to each resource.
// Resources with DependencyLevel 0 have no cross-batch dependencies; higher levels depend
// on lower-level resources. Cycles are appended at maxLevel+1.
func sortResourcesByDependencies(resources []ResourceChange) []ResourceChange {
	n := len(resources)
	if n == 0 {
		return resources
	}

	// Build name->index map and name set for resources in this batch
	nameToIdx := make(map[string]int, n)
	for i, res := range resources {
		nameToIdx[res.Name] = i
	}
	nameSet := make(map[string]bool, n)
	for name := range nameToIdx {
		nameSet[name] = true
	}

	// Build dependency graph:
	//   inDegree[i] = number of batch resources that resource[i] depends on
	//   dependedBy[j] = list of indices that depend ON resource[j]
	inDegree := make([]int, n)
	dependedBy := make([][]int, n)

	for i, res := range resources {
		depNames := extractDependencyNames(res, nameSet)
		seen := make(map[int]bool)
		for _, name := range depNames {
			j, ok := nameToIdx[name]
			if !ok || j == i || seen[j] {
				continue
			}
			seen[j] = true
			inDegree[i]++
			dependedBy[j] = append(dependedBy[j], i)
		}
	}

	// Kahn's BFS: process zero-inDegree nodes first, assigning levels
	levels := make([]int, n)
	remaining := make([]int, n)
	copy(remaining, inDegree)

	var queue []int
	for i := 0; i < n; i++ {
		if remaining[i] == 0 {
			queue = append(queue, i)
		}
	}

	order := make([]int, 0, n)
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		order = append(order, curr)
		for _, dep := range dependedBy[curr] {
			remaining[dep]--
			if levels[curr]+1 > levels[dep] {
				levels[dep] = levels[curr] + 1
			}
			if remaining[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	// Append cyclic resources at maxLevel+1
	maxLevel := 0
	for _, l := range levels {
		if l > maxLevel {
			maxLevel = l
		}
	}
	for i := 0; i < n; i++ {
		if remaining[i] > 0 {
			levels[i] = maxLevel + 1
			order = append(order, i)
		}
	}

	// Build result in topological order, setting DependencyLevel
	result := make([]ResourceChange, 0, n)
	for _, origIdx := range order {
		res := resources[origIdx]
		res.DependencyLevel = levels[origIdx]
		result = append(result, res)
	}
	return result
}

// outputResult outputs the final JSON result with filtering, exclusions, and resource limiting.
// Full output is written to a file; a compact summary is written to stdout.
func outputResult(resources []ResourceChange, maxResources int, excludeURNs []string, depMapFile, outputFile string) error {
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

	// Apply explicit --max-resources N (N > 0) truncation
	if maxResources > 0 && len(actionable) > maxResources {
		actionable = actionable[:maxResources]
	}

	// Build full output
	result := NextOutput{
		DepMapFile: depMapFile,
	}
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

	// Write full output to file
	outputFilePath, err := writeOutputFile(result, outputFile)
	if err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}

	// Write compact summary to stdout
	summaryOutput := NextSummaryOutput{
		Status:       result.Status,
		Summary:      result.Summary,
		OutputFile:   outputFilePath,
		DepMapFile:   depMapFile,
		SkippedCount: len(skipped),
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(summaryOutput); err != nil {
		return fmt.Errorf("failed to encode summary output: %w", err)
	}

	return nil
}

// writeOutputFile writes the full NextOutput to a file. If outputFile is empty, a temp file is created.
func writeOutputFile(result NextOutput, outputFile string) (string, error) {
	var f *os.File
	var err error
	if outputFile != "" {
		f, err = os.Create(outputFile)
	} else {
		f, err = os.CreateTemp("", "drift-adopter-output-*.json")
	}
	if err != nil {
		return "", err
	}
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}

func outputError(errMsg string) error {
	output := NextSummaryOutput{
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
