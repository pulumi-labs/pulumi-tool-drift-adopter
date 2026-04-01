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
	"strings"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
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
	cmd.Flags().String("project", ".", "Pulumi project directory (default: current directory)")
	cmd.Flags().String("events-file", "", "Read preview events from file instead of running pulumi preview")
	cmd.Flags().StringSlice("exclude-urns", nil, "URNs to exclude from output")
	cmd.Flags().Bool("skip-refresh", false, "Skip --refresh flag on pulumi preview (use existing state)")
	return cmd
}

// NextOutput is the JSON structure returned by the next command
type NextOutput struct {
	Status    string           `json:"status"` // "changes_needed", "clean", "error"
	Error     string           `json:"error,omitempty"`
	Resources []ResourceChange `json:"resources,omitempty"`
}

// ResourceChange describes a resource that needs code changes
type ResourceChange struct {
	Action     string           `json:"action"` // "update_code", "delete_from_code", "add_to_code"
	URN        string           `json:"urn"`
	Type       string           `json:"type"`
	Name       string           `json:"name"`
	Properties []PropertyChange `json:"properties,omitempty"`
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
	eventsFile, _ := cmd.Flags().GetString("events-file")
	excludeURNs, _ := cmd.Flags().GetStringSlice("exclude-urns")
	skipRefresh, _ := cmd.Flags().GetBool("skip-refresh")

	// Get preview output from file or command
	output, err := getPreviewOutput(eventsFile, projectDir, stack, skipRefresh)
	if err != nil {
		return err
	}

	// Parse preview output into steps
	steps, _, err := parsePreviewOutput(output)
	if err != nil {
		return err
	}

	// Convert steps to resource changes
	resources := convertStepsToResources(steps)

	// Output result
	return outputResult(resources, excludeURNs)
}

// convertStepsToResources converts preview steps to resource changes for drift adoption
func convertStepsToResources(steps []auto.PreviewStep) []ResourceChange {
	var resources []ResourceChange

	for _, step := range steps {
		// Skip operations that don't require code changes
		action := getActionForOperation(step.Op)
		if action == "" {
			continue
		}

		// Extract resource metadata
		resourceType := extractResourceType(step)
		name := extractResourceName(string(step.URN))

		// Parse property changes
		properties := extractPropertyChanges(step)

		resources = append(resources, ResourceChange{
			Action:     action,
			URN:        string(step.URN),
			Type:       resourceType,
			Name:       name,
			Properties: properties,
		})
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

// extractPropertyChanges extracts property changes from a preview step
func extractPropertyChanges(step auto.PreviewStep) []PropertyChange {
	var properties []PropertyChange

	// For delete operations (add_to_code), DetailedDiff is empty but we need all properties from state
	if step.Op == "delete" && len(step.DetailedDiff) == 0 && step.OldState != nil && step.OldState.Outputs != nil {
		// Extract all properties from OldState.Outputs
		extractAllProperties(step.OldState.Outputs, "", &properties)
		return properties
	}

	// For other operations, use DetailedDiff
	for path, diff := range step.DetailedDiff {
		// Get actual values from old/new states
		var currentValue, desiredValue interface{}

		// OldState contains what's in state (what we want)
		if step.OldState != nil && step.OldState.Outputs != nil {
			desiredValue = getNestedProperty(step.OldState.Outputs, path)
		}

		// NewState contains what the code will produce (what currently exists or will be created)
		if step.NewState != nil && step.NewState.Outputs != nil {
			currentValue = getNestedProperty(step.NewState.Outputs, path)
		}

		properties = append(properties, PropertyChange{
			Path:         path,
			CurrentValue: currentValue,
			DesiredValue: desiredValue,
			Kind:         invertPropertyKind(diff.Kind),
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

// outputResult outputs the final JSON result with optional URN exclusion
func outputResult(resources []ResourceChange, excludeURNs []string) error {
	// Build exclude set for O(1) lookup
	excludeSet := make(map[string]bool, len(excludeURNs))
	for _, urn := range excludeURNs {
		excludeSet[urn] = true
	}

	// Filter excluded URNs
	var filtered []ResourceChange
	for _, res := range resources {
		if !excludeSet[res.URN] {
			filtered = append(filtered, res)
		}
	}
	resources = filtered

	// Determine status
	result := NextOutput{}
	if len(resources) == 0 {
		result.Status = "clean"
	} else {
		result.Status = "changes_needed"
		result.Resources = resources
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
