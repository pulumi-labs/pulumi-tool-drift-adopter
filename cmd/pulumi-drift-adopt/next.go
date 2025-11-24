package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
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
		return outputError(fmt.Sprintf("pulumi preview failed: %v", err))
	}

	// Parse the JSON output structure
	var previewResult struct {
		Steps []auto.PreviewStep `json:"steps"`
	}

	if err := json.Unmarshal(output, &previewResult); err != nil {
		return outputError(fmt.Sprintf("failed to parse preview output: %v", err))
	}

	// Parse steps for resources that need changes
	var resources []ResourceChange
	for _, step := range previewResult.Steps {
		// Extract resource type from old or new state
		var resourceType string
		if step.OldState != nil {
			resourceType = string(step.OldState.Type)
		}
		if resourceType == "" && step.NewState != nil {
			resourceType = string(step.NewState.Type)
		}

		// Extract resource name from URN
		name := extractResourceName(string(step.URN))

		// Invert the preview operation logic
		var action string
		switch step.Op {
		case "create":
			// Preview wants to CREATE = resource is in code but not in state
			// Action: DELETE from code
			action = "delete_from_code"
		case "delete":
			// Preview wants to DELETE = resource is in state but not in code
			// Action: ADD to code
			action = "add_to_code"
		case "update", "replace":
			// Preview wants to UPDATE/REPLACE = resource exists in both but differs
			// Action: UPDATE code to match state
			action = "update_code"
		default:
			continue // Skip "same", "refresh", etc.
		}

		// Parse detailed diff for properties
		var properties []PropertyChange
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
				Kind:         diff.Kind,
			})
		}

		resources = append(resources, ResourceChange{
			Action:     action,
			URN:        string(step.URN),
			Type:       resourceType,
			Name:       name,
			Properties: properties,
		})
	}

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
