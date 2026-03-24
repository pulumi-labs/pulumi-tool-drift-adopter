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
	"fmt"
	"strings"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
)

// extractInputProperties returns a flat key-value map of input properties for add_to_code resources.
// When a depMap is available, properties are enriched with dependsOn metadata from pre-computed
// dependency references. When no depMap is available, falls back to stateLookup-based resolution.
//
// Properties without dependencies remain plain values (backward-compatible), with long
// strings truncated to maxStringValueLen characters.
// Properties with dependencies become: {"dependsOn": {"resourceName": ..., "resourceType": ..., "outputProperty": ...}}
func extractInputProperties(step auto.PreviewStep, depMap DependencyMap) map[string]interface{} {
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

	// If no dep map entries, return with truncation only
	if len(urnDeps) == 0 {
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
				} else {
					resolvedArr[i] = truncateValue(elem)
				}
			}
			result[key] = resolvedArr
			continue
		}

		// Scalar: check dep map
		if ref, ok := urnDeps[key]; ok {
			result[key] = depRefToDependsOn(ref)
		} else {
			result[key] = truncateValue(value)
		}
	}
	return result
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

// invertPropertyKind inverts the property change kind from preview perspective to code perspective.
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
