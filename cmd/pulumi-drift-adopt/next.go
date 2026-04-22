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
	"context"
	"fmt"
	"os"

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
	ParseErrors  int          `json:"parseErrors,omitempty"`
	Error        string       `json:"error,omitempty"`
}

// NextSummary contains aggregate counts for the output.
type NextSummary struct {
	Total        int                       `json:"total"`
	ByAction     map[string]int            `json:"byAction"`
	ByType       map[string]int            `json:"byType"`
	ByTypeAction map[string]map[string]int `json:"byTypeAction"`
}

// ResourceMetadata holds pre-computed dependency and schema information for reuse across tool calls.
type ResourceMetadata struct {
	// Dependencies maps resource URN -> property path -> DependencyRef.
	Dependencies map[string]map[string]DependencyRef `json:"dependencies"`
	// InputProperties maps resource type -> set of input property names from provider schema.
	// Properties in DetailedDiff whose top-level key is NOT in this set are computed-only outputs.
	InputProperties map[string][]string `json:"inputProperties,omitempty"`
	// StateLookup is the parsed state export, used for supplementing secret values.
	// Not serialized — only available during the initial run (not from cached metadata).
	StateLookup map[string]*apitype.ResourceV3 `json:"-"`
}

// DependencyMap is an alias for the dependencies portion of ResourceMetadata.
// Used as a convenience type throughout the codebase.
type DependencyMap = map[string]map[string]DependencyRef

// DependencyRef describes a cross-resource dependency for a single property.
type DependencyRef struct {
	ResourceName   string `json:"resourceName"`
	ResourceType   string `json:"resourceType"`
	OutputProperty string `json:"outputProperty,omitempty"`
}

// ResourceChange represents a single resource that needs code changes.
type ResourceChange struct {
	URN             string           `json:"urn"`
	Name            string           `json:"name"`
	Type            string           `json:"type"`
	Action          string           `json:"action"`
	Properties      []PropertyChange `json:"properties,omitempty"`
	DependencyLevel int              `json:"dependencyLevel,omitempty"`
	Reason          string           `json:"reason,omitempty"`
}

// PropertyChange represents a single property change within a resource.
// The intent is conveyed by currentValue and desiredValue:
//   - currentValue=X, desiredValue=Y → update property
//   - currentValue=nil, desiredValue=Y → add property to code
//   - currentValue=X, desiredValue=nil → remove property from code
type PropertyChange struct {
	Path         string         `json:"path"`
	CurrentValue interface{}    `json:"currentValue,omitempty"`
	DesiredValue interface{}    `json:"desiredValue,omitempty"`
	DependsOn    *DependencyRef `json:"dependsOn,omitempty"`
}

// Action constants for drift-adoption operations.
const (
	ActionAddToCode      = "add_to_code"
	ActionDeleteFromCode = "delete_from_code"
	ActionUpdateCode     = "update_code"
)

// Status constants for drift-adoption results.
const (
	StatusChangesNeeded   = "changes_needed"
	StatusClean           = "clean"
	StatusStopWithSkipped = "stop_with_skipped"
	StatusError           = "error"
)

// maxStringValueLen is the maximum length of a string property value before truncation.
// Values longer than this are replaced with a placeholder to keep output compact.
const maxStringValueLen = 200

func runNext(cmd *cobra.Command, _ []string) error {
	projectDir, _ := cmd.Flags().GetString("project")
	stack, _ := cmd.Flags().GetString("stack")
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

	// Parse preview output into steps (once — shared by dep map building and resource conversion)
	steps, parseErrors, err := parsePreviewOutput(output)
	if err != nil {
		return err
	}

	var meta *ResourceMetadata

	if depMapFile != "" {
		// Load pre-computed metadata — skip state export and schema fetch entirely
		meta, err = loadMetadata(depMapFile)
		if err != nil {
			return err
		}
	} else {
		// Load state for dependency resolution (in-memory only, no file written)
		stateLookup, err := getStateExport(projectDir, stack)
		if err != nil {
			return err
		}

		depMap := mergeStateLookupAndBuildDepMap(steps, stateLookup)

		// Fetch provider schemas to determine input vs computed-only properties
		inputProps, err := getInputPropertiesFromSchema(steps, projectDir)
		if err != nil {
			return err
		}

		meta = &ResourceMetadata{
			Dependencies:    depMap,
			InputProperties: inputProps,
			StateLookup:     stateLookup,
		}
	}

	// Save metadata for reuse in subsequent calls (skip if loaded from file)
	var metaPath string
	if depMapFile != "" {
		metaPath = depMapFile
	} else {
		metaPath, err = saveMetadata(meta)
		if err != nil {
			return err
		}
	}

	return processNext(steps, parseErrors, meta, excludeURNs, metaPath, outputFile, projectDir, stack)
}

// mergeStateLookupAndBuildDepMap supplements a state lookup with OldState from preview steps,
// then builds the complete dependency map.
func mergeStateLookupAndBuildDepMap(steps []auto.PreviewStep, stateLookup map[string]*apitype.ResourceV3) DependencyMap {
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
	return buildDepMapFromState(stateLookup)
}

// processNext is the core pipeline: convert parsed steps to resources and output results.
// It is separated from runNext so that tests can call it directly without needing CLI flags,
// state export, or a live Pulumi stack.
// projectDir and stack are used to write secret values to stack config when available.
func processNext(steps []auto.PreviewStep, parseErrors int, meta *ResourceMetadata, excludeURNs []string, depMapPath, outputFile, projectDir, stack string) error {
	resources, secretConfigs := convertStepsToResources(steps, meta)
	resources = sortResourcesByDependencies(resources)

	// Write collected secrets to stack config
	if len(secretConfigs) > 0 {
		if stack == "" {
			fmt.Fprintf(os.Stderr, "warning: secret values found but no stack specified — secrets written as plaintext configRefs without encryption\n")
		} else {
			if err := writeSecretConfigs(projectDir, stack, secretConfigs); err != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to write secrets to stack config: %v — configRefs will reference unset keys\n", err)
			}
		}
	}

	return outputResult(resources, excludeURNs, depMapPath, outputFile, parseErrors)
}

// convertStepsToResources converts preview steps to resource changes for drift adoption.
// meta provides dependency resolution and schema-based input property filtering.
// Returns the resource list and a map of config keys to plaintext secret values that
// should be written to stack config (nil if no secrets were resolved).
func convertStepsToResources(steps []auto.PreviewStep, meta *ResourceMetadata) ([]ResourceChange, map[string]string) {
	var depMap DependencyMap
	var inputPropSet map[string]map[string]bool
	var stateLookup map[string]*apitype.ResourceV3
	if meta != nil {
		depMap = meta.Dependencies
		inputPropSet = buildInputPropertySet(meta.InputProperties)
		stateLookup = meta.StateLookup
	}

	var resources []ResourceChange
	var allSecrets map[string]string
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
		case ActionAddToCode:
			res.Properties = extractInputProperties(*step, depMap)
		case ActionDeleteFromCode:
			// No properties needed for removal
		default:
			res.Properties = extractPropertyChanges(*step, inputPropSet)
			if depMap != nil {
				urnDeps := depMap[string(step.URN)]
				if len(urnDeps) > 0 {
					for i := range res.Properties {
						if ref, ok := urnDeps[res.Properties[i].Path]; ok {
							res.Properties[i].DependsOn = &ref
						}
					}
				}
			}
		}

		// Supplement "[secret]" values with real values from state export.
		// Applies to both update_code and add_to_code properties.
		if stateLookup != nil && action != ActionDeleteFromCode {
			if secrets := supplementSecretValues(res.Properties, string(step.URN), stateLookup, name); secrets != nil {
				if allSecrets == nil {
					allSecrets = make(map[string]string)
				}
				for k, v := range secrets {
					allSecrets[k] = v
				}
			}
		}

		resources = append(resources, res)
	}
	return resources, allSecrets
}

// writeSecretConfigs writes secret values to Pulumi stack config using the automation API.
// Each secret is stored as an encrypted config value under the drift-adopt namespace.
func writeSecretConfigs(projectDir, stack string, secrets map[string]string) error {
	ctx := context.Background()
	opts := []auto.LocalWorkspaceOption{auto.WorkDir(projectDir)}
	ws, err := auto.NewLocalWorkspace(ctx, opts...)
	if err != nil {
		return fmt.Errorf("failed to create workspace: %w", err)
	}

	configMap := make(auto.ConfigMap, len(secrets))
	for key, value := range secrets {
		configMap[key] = auto.ConfigValue{Value: value, Secret: true}
	}

	if err := ws.SetAllConfig(ctx, stack, configMap); err != nil {
		return fmt.Errorf("failed to set config: %w", err)
	}
	return nil
}

// getActionForOperation maps a Pulumi preview operation to a drift-adoption action.
func getActionForOperation(op string) string {
	switch op {
	case "delete":
		// Preview wants to DELETE from infrastructure = resource exists in state but not in code
		// Action: ADD resource to code
		return ActionAddToCode
	case "create":
		// Preview wants to CREATE in infrastructure = resource exists in code but not in state
		// Action: REMOVE resource from code (or it's intentionally new)
		return ActionDeleteFromCode
	case "update":
		// Preview wants to UPDATE infrastructure = code differs from state
		// Action: UPDATE code to match state
		return ActionUpdateCode
	case "replace":
		// Preview wants to REPLACE infrastructure = code change requires replacement
		// Action: UPDATE code to match state (replace implies update)
		return ActionUpdateCode
	default:
		// same, read, refresh, etc. — no code changes needed
		return ""
	}
}
