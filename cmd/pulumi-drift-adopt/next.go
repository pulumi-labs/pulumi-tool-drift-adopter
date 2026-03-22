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
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
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
	cmd.Flags().Int("max-resources", 0, "Maximum number of resources to return (0 = all)")
	cmd.Flags().String("events-file", "", "Read preview events from file instead of running pulumi preview")
	cmd.Flags().StringSlice("exclude-urns", nil, "URNs to exclude from output")
	cmd.Flags().String("dep-map-file", "", "Path to dependency map JSON file (reuse across runs to skip state export)")
	cmd.Flags().Bool("skip-refresh", false, "Skip --refresh flag on pulumi preview (use existing state)")
	cmd.Flags().String("output-file", "", "Path to write full output JSON (default: auto-generated temp file)")
	return cmd
}

// NextOutput is the full output written to a file, containing all resource details.
type NextOutput struct {
	Status     string           `json:"status"`
	Summary    *NextSummary     `json:"summary,omitempty"`
	Resources  []ResourceChange `json:"resources,omitempty"`
	Skipped    []ResourceChange `json:"skipped,omitempty"`
	DepMapFile string           `json:"depMapFile,omitempty"`
}

// NextSummaryOutput is the compact summary written to stdout.
type NextSummaryOutput struct {
	Status       string       `json:"status"`
	Summary      *NextSummary `json:"summary,omitempty"`
	OutputFile   string       `json:"outputFile,omitempty"`
	DepMapFile   string       `json:"depMapFile,omitempty"`
	SkippedCount int          `json:"skippedCount,omitempty"`
	Error        string       `json:"error,omitempty"`
}

// NextSummary contains aggregate counts for the output.
type NextSummary struct {
	Total        int                       `json:"total"`
	ByAction     map[string]int            `json:"byAction"`
	ByType       map[string]int            `json:"byType"`
	ByTypeAction map[string]map[string]int `json:"byTypeAction"`
}

// DependencyMap maps resource URN -> property path -> DependencyRef.
type DependencyMap map[string]map[string]DependencyRef

// DependencyRef describes a cross-resource dependency for a single property.
type DependencyRef struct {
	ResourceName   string `json:"resourceName"`
	ResourceType   string `json:"resourceType"`
	OutputProperty string `json:"outputProperty,omitempty"`
}

// ResourceChange represents a single resource that needs code changes.
type ResourceChange struct {
	URN             string                 `json:"urn"`
	Name            string                 `json:"name"`
	Type            string                 `json:"type"`
	Action          string                 `json:"action"`
	Properties      []PropertyChange       `json:"properties,omitempty"`
	InputProperties map[string]interface{} `json:"inputProperties,omitempty"`
	DependencyLevel int                    `json:"dependencyLevel,omitempty"`
	Reason          string                 `json:"reason,omitempty"`
}

// PropertyChange represents a single property change within a resource.
type PropertyChange struct {
	Path         string      `json:"path"`
	Kind         string      `json:"kind"`
	CurrentValue interface{} `json:"currentValue,omitempty"`
	DesiredValue interface{} `json:"desiredValue,omitempty"`
}

// maxStringValueLen is the maximum length of a string property value before truncation.
// Values longer than this are replaced with a placeholder to keep output compact.
const maxStringValueLen = 200

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

// convertStepsToResources converts preview steps to resource changes for drift adoption.
// depMap is used for dependency resolution; stateLookup is a fallback for resolution
// when depMap doesn't cover a property (e.g., preview-only steps not in state).
func convertStepsToResources(steps []auto.PreviewStep, depMap DependencyMap, stateLookup map[string]*apitype.ResourceV3) []ResourceChange {
	var resources []ResourceChange
	for i := range steps {
		step := &steps[i]
		action := getActionForOperation(step.Op)
		if action == "" {
			continue
		}

		// Normalize DetailedDiff from ReplaceReasons/DiffReasons for consistent handling
		normalizeDetailedDiff(step)

		resourceType := extractResourceType(*step)
		name := extractResourceName(string(step.URN))

		res := ResourceChange{
			URN:    string(step.URN),
			Name:   name,
			Type:   resourceType,
			Action: action,
		}

		switch action {
		case "add_to_code":
			// For resources that need to be added, extract all input properties from state
			res.InputProperties = extractInputProperties(*step, depMap, stateLookup)
		case "remove_from_code":
			// No properties needed for removal
		default:
			// For update/replace, extract changed properties
			res.Properties = extractPropertyChanges(*step)
		}

		resources = append(resources, res)
	}
	return resources
}

// getActionForOperation maps a Pulumi preview operation to a drift-adoption action.
func getActionForOperation(op string) string {
	switch op {
	case "delete":
		// Preview wants to DELETE from infrastructure = resource exists in state but not in code
		// Action: ADD resource to code
		return "add_to_code"
	case "create":
		// Preview wants to CREATE in infrastructure = resource exists in code but not in state
		// Action: REMOVE resource from code (or it's intentionally new)
		return "delete_from_code"
	case "update":
		// Preview wants to UPDATE infrastructure = code differs from state
		// Action: UPDATE code to match state
		return "update_code"
	case "replace":
		// Preview wants to REPLACE infrastructure = code change requires replacement
		// Action: UPDATE code to match state (replace implies update)
		return "update_code"
	default:
		// same, read, refresh, etc. — no code changes needed
		return ""
	}
}
