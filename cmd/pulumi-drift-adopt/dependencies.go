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

	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
)

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

// loadMetadata reads a ResourceMetadata from a JSON file.
func loadMetadata(path string) (*ResourceMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read metadata file: %w", err)
	}
	var meta ResourceMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("failed to parse metadata file: %w", err)
	}
	return &meta, nil
}

// saveMetadata writes a ResourceMetadata to an auto-generated temp file.
// Returns the path written to.
func saveMetadata(meta *ResourceMetadata) (string, error) {
	f, err := os.CreateTemp("", "drift-adopter-metadata-*.json")
	if err != nil {
		return "", fmt.Errorf("failed to create metadata file: %w", err)
	}
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(meta); err != nil {
		_ = f.Close()
		return "", fmt.Errorf("failed to write metadata: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}

// Backward-compatible aliases
func loadDepMap(path string) (DependencyMap, error) {
	meta, err := loadMetadata(path)
	if err != nil {
		return nil, err
	}
	return meta.Dependencies, nil
}

func saveDepMap(depMap DependencyMap) (string, error) {
	return saveMetadata(&ResourceMetadata{Dependencies: depMap})
}

// findMatchingOutput searches a resource's outputs for one whose value exactly matches
// the given input value using Pulumi SDK's PropertyValue.DeepEquals for order-independent
// comparison. Returns the output property name, or "" if no match found.
//
// Both inputValue and outputs come from JSON-unmarshaled state data (exported with
// --show-secrets), so all values are plaintext — no secret sentinel wrappers to worry about.
func findMatchingOutput(inputValue interface{}, outputs map[string]interface{}) string {
	if outputs == nil {
		return ""
	}
	inputPV := resource.NewPropertyValue(inputValue)
	for key, outputValue := range outputs {
		if inputPV.DeepEquals(resource.NewPropertyValue(outputValue)) {
			return key
		}
	}
	return ""
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
