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
	"strings"

	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/plugin"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/sig"
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
// inputPropSet provides schema-based filtering: if the top-level property key is not
// a known input property for this resource type, it's a computed-only output and is skipped.
func extractPropertyChanges(step auto.PreviewStep, inputPropSet map[string]map[string]bool) []PropertyChange {
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

	// Get the set of known input properties for this resource type (from provider schema)
	resourceType := extractResourceType(step)
	var knownInputs map[string]bool
	if inputPropSet != nil {
		knownInputs = inputPropSet[resourceType]
	}

	for path, diff := range step.DetailedDiff {
		// Schema-based filtering: skip computed-only output properties.
		// Extract the top-level property key from the path (e.g., "tags" from "tags.Environment",
		// "ingress" from "ingress[0].fromPort").
		if knownInputs != nil {
			topKey := topLevelKey(path)
			if !knownInputs[topKey] {
				continue // computed-only output, user can't set in code
			}
		}

		inputsOnly := diff.InputDiff

		// For currentValue (what code says): ALWAYS use Inputs. During preview,
		// NewState.Outputs may contain stale data from a prior provider version
		// since Update/Create hasn't been called yet.
		currentValue := resolveInputValue(step.NewState, path)

		// For desiredValue (what infra has): follow the inputDiff flag.
		// Outputs when comparing against provider state, Inputs when input-to-input.
		desiredValue := resolvePropertyValue(step.OldState, path, inputsOnly)

		// Filter unknown sentinels — these are preview placeholders for values
		// that haven't been computed yet (e.g., cascading replaces).
		currentValue = filterUnknownSentinel(currentValue)
		desiredValue = filterUnknownSentinel(desiredValue)

		// Skip properties where both values are nil — computed-only fields
		// in diff metadata with no actionable values for the agent.
		if currentValue == nil && desiredValue == nil {
			continue
		}

		properties = append(properties, PropertyChange{
			Path:         path,
			CurrentValue: currentValue,
			DesiredValue: desiredValue,
		})
	}

	return properties
}

// pulumiSecretSigKey is the envelope key that identifies secret values in state exports.
// From github.com/pulumi/pulumi/sdk/v3/go/common/resource/sig.Key.
// State export with --show-secrets wraps secret values as:
//
//	{sig.Key: sig.Secret, "plaintext": "\"actual-value\""}
var pulumiSecretSigKey = sig.Key

// supplementSecretValues replaces "[secret]" property values with real values from the
// state export. The state export (run with --show-secrets) contains actual secret values
// in a Pulumi envelope format that we can unwrap.
func supplementSecretValues(properties []PropertyChange, urn string, stateLookup map[string]*apitype.ResourceV3) {
	stateRes := stateLookup[urn]
	if stateRes == nil {
		return
	}

	for i := range properties {
		if properties[i].DesiredValue == "[secret]" {
			// desiredValue comes from OldState (infrastructure) — look up in state export
			if real := resolveSecretFromState(stateRes.Inputs, properties[i].Path); real != nil {
				properties[i].DesiredValue = real
			} else if real := resolveSecretFromState(stateRes.Outputs, properties[i].Path); real != nil {
				properties[i].DesiredValue = real
			}
		}
		// Note: currentValue "[secret]" is NOT supplemented. It comes from NewState (code),
		// and the state export only has the deployed version. The agent reads the actual
		// code value directly from the source file.
	}
}

// resolveSecretFromState looks up a property path in state data and unwraps
// the Pulumi secret envelope if present. Returns the plaintext value or nil.
func resolveSecretFromState(props map[string]interface{}, path string) interface{} {
	if props == nil {
		return nil
	}
	v := getNestedProperty(props, path)
	return unwrapSecret(v)
}

// unwrapSecret unwraps a Pulumi secret envelope, returning the plaintext value.
// The envelope format in state exports is: {sig.Key: sig.Secret, "plaintext": "\"value\""}
// If the value is not a secret envelope, returns nil.
func unwrapSecret(v interface{}) interface{} {
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	if _, hasSig := m[pulumiSecretSigKey]; !hasSig {
		return nil
	}
	plaintext, ok := m["plaintext"]
	if !ok {
		return nil
	}
	// The plaintext value is JSON-encoded (e.g., "\"actual-value\"").
	// Use the SDK's NewPropertyValue to parse it correctly.
	if s, ok := plaintext.(string); ok {
		var parsed interface{}
		if err := json.Unmarshal([]byte(s), &parsed); err == nil {
			return parsed
		}
		return s
	}
	return plaintext
}

// filterUnknownSentinel returns nil if the value is any of the Pulumi engine's
// unknown sentinel UUIDs, otherwise returns the value unchanged.
// Sentinels from github.com/pulumi/pulumi/sdk/v3/go/common/resource/plugin.
func filterUnknownSentinel(v interface{}) interface{} {
	s, ok := v.(string)
	if !ok {
		return v
	}
	switch s {
	case plugin.UnknownStringValue,
		plugin.UnknownBoolValue,
		plugin.UnknownNumberValue,
		plugin.UnknownArrayValue,
		plugin.UnknownAssetValue,
		plugin.UnknownArchiveValue,
		plugin.UnknownObjectValue:
		return nil
	}
	return v
}

// topLevelKey extracts the top-level property key from a DetailedDiff path.
// "tags.Environment" → "tags", "ingress[0].fromPort" → "ingress", "description" → "description"
func topLevelKey(path string) string {
	if i := strings.IndexByte(path, '.'); i >= 0 {
		path = path[:i]
	}
	if i := strings.IndexByte(path, '['); i >= 0 {
		path = path[:i]
	}
	return path
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
			// Leaf property — not in code, needs to be added
			*properties = append(*properties, PropertyChange{
				Path:         path,
				DesiredValue: value,
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

// resolveInputValue looks up a property value from Inputs only.
// Used for NewState (currentValue) where Outputs may be stale.
func resolveInputValue(state *apitype.ResourceV3, path string) interface{} {
	if state == nil || state.Inputs == nil {
		return nil
	}
	return getNestedProperty(state.Inputs, path)
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

// getNestedProperty extracts a value from a nested map using a Pulumi property path.
// Supports dot-separated keys ("tags.Environment"), array indices ("triggers[0]"),
// bracket-quoted keys with dots ("tags[\"kubernetes.io/name\"]"), and consecutive
// indices ("matrix[0][1]"). Uses the Pulumi SDK's PropertyPath parser for correctness.
func getNestedProperty(props map[string]interface{}, path string) interface{} {
	pp, err := resource.ParsePropertyPath(path)
	if err != nil {
		return nil
	}

	var current interface{} = props
	for _, segment := range pp {
		switch s := segment.(type) {
		case string:
			m, ok := current.(map[string]interface{})
			if !ok {
				return nil
			}
			current, ok = m[s]
			if !ok {
				return nil
			}
		case int:
			arr, ok := current.([]interface{})
			if !ok {
				return nil
			}
			if s < 0 || s >= len(arr) {
				return nil
			}
			current = arr[s]
		default:
			return nil
		}
	}
	return current
}
