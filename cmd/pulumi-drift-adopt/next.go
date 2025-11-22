package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var nextCmd = &cobra.Command{
	Use:   "next",
	Short: "Run preview and show what code changes are needed",
	Long: `Runs pulumi preview and analyzes the output to determine what code changes are needed.

This command assumes 'pulumi refresh' has already been run, so the state represents
the actual infrastructure. The preview shows differences between code and state:
- Old values (state) = what we want (desired)
- New values (code) = what currently exists in code (current/incorrect)

The tool inverts the preview logic to tell you what to change in your code.`,
	RunE: runNext,
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

func runNext(cmd *cobra.Command, args []string) error {
	projectDir, _ := cmd.Flags().GetString("project")

	// Run pulumi preview with JSON output
	previewCmd := exec.Command("pulumi", "preview", "--json", "--non-interactive")
	previewCmd.Dir = projectDir
	previewCmd.Stderr = os.Stderr

	stdout, err := previewCmd.StdoutPipe()
	if err != nil {
		return outputError(fmt.Sprintf("failed to create stdout pipe: %v", err))
	}

	if err := previewCmd.Start(); err != nil {
		return outputError(fmt.Sprintf("failed to start pulumi preview: %v", err))
	}

	// Parse newline-delimited JSON events
	var resources []ResourceChange
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue // Skip non-JSON lines
		}

		// We're looking for resource-step events
		if event["type"] != "resource-step" {
			continue
		}

		metadata, ok := event["metadata"].(map[string]interface{})
		if !ok {
			continue
		}

		op, _ := metadata["op"].(string)
		urn, _ := metadata["urn"].(string)
		resourceType, _ := metadata["type"].(string)

		// Extract resource name from URN
		name := extractResourceName(urn)

		// Invert the preview operation logic
		var action string
		switch op {
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
			continue
		}

		// Parse detailed diff for properties
		var properties []PropertyChange
		if detailedDiff, ok := metadata["detailedDiff"].(map[string]interface{}); ok {
			for path, detail := range detailedDiff {
				if detailMap, ok := detail.(map[string]interface{}); ok {
					kind, _ := detailMap["kind"].(string)
					lhs := detailMap["lhs"] // Old value (state) = DESIRED
					rhs := detailMap["rhs"] // New value (code) = CURRENT

					properties = append(properties, PropertyChange{
						Path:         path,
						CurrentValue: rhs,
						DesiredValue: lhs,
						Kind:         kind,
					})
				}
			}
		}

		resources = append(resources, ResourceChange{
			Action:     action,
			URN:        urn,
			Type:       resourceType,
			Name:       name,
			Properties: properties,
		})
	}

	if err := previewCmd.Wait(); err != nil {
		// Preview failed - likely a code error
		return outputError(fmt.Sprintf("pulumi preview failed: %v", err))
	}

	// Determine status
	output := NextOutput{}
	if len(resources) == 0 {
		output.Status = "clean"
	} else {
		output.Status = "changes_needed"
		output.Resources = resources
	}

	// Output JSON
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
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
	encoder.Encode(output)
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
