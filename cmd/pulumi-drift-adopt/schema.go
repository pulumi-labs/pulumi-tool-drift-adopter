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
)

// getInputPropertiesFromSchema fetches provider schemas and extracts inputProperties
// for each resource type present in the preview steps. Returns a map of
// resource type -> set of input property names.
func getInputPropertiesFromSchema(steps []auto.PreviewStep, projectDir string) (map[string][]string, error) {
	// Collect unique resource types and their provider names
	typesByProvider := make(map[string]map[string]bool) // provider -> set of resource types
	for _, step := range steps {
		if step.Op == "same" || step.Op == "read" || step.Op == "refresh" {
			continue
		}
		resType := extractResourceType(step)
		if resType == "" {
			continue
		}
		provider := extractProviderName(resType)
		if provider == "" {
			continue
		}
		if typesByProvider[provider] == nil {
			typesByProvider[provider] = make(map[string]bool)
		}
		typesByProvider[provider][resType] = true
	}

	result := make(map[string][]string)

	for provider, resourceTypes := range typesByProvider {
		schema, err := fetchProviderSchema(provider, projectDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to fetch schema for provider %s: %v\n", provider, err)
			continue
		}

		for resType := range resourceTypes {
			resDef, ok := schema.Resources[resType]
			if !ok {
				continue
			}
			var inputNames []string
			for name := range resDef.InputProperties {
				inputNames = append(inputNames, name)
			}
			result[resType] = inputNames
		}
	}

	return result, nil
}

// extractProviderName extracts the provider package name from a resource type token.
// e.g., "aws:s3/bucket:Bucket" -> "aws", "random:index/randomString:RandomString" -> "random"
func extractProviderName(resourceType string) string {
	idx := strings.IndexByte(resourceType, ':')
	if idx <= 0 {
		return ""
	}
	return resourceType[:idx]
}

// providerSchema is a minimal representation of a Pulumi provider schema,
// containing only what we need for input property filtering.
type providerSchema struct {
	Resources map[string]resourceSchema `json:"resources"`
}

type resourceSchema struct {
	InputProperties map[string]json.RawMessage `json:"inputProperties"`
}

// fetchProviderSchema runs `pulumi package get-schema <provider>` and parses the result.
func fetchProviderSchema(provider, projectDir string) (*providerSchema, error) {
	cmd := exec.Command("pulumi", "package", "get-schema", provider)
	cmd.Dir = projectDir
	cmd.Stderr = os.Stderr

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("pulumi package get-schema %s failed: %w", provider, err)
	}

	var schema providerSchema
	if err := json.Unmarshal(output, &schema); err != nil {
		return nil, fmt.Errorf("failed to parse schema for %s: %w", provider, err)
	}

	return &schema, nil
}

// buildInputPropertySet converts the inputProperties map (type -> []string) to a
// lookup-friendly format (type -> map[string]bool) for O(1) checks.
func buildInputPropertySet(inputProps map[string][]string) map[string]map[string]bool {
	result := make(map[string]map[string]bool, len(inputProps))
	for resType, props := range inputProps {
		set := make(map[string]bool, len(props))
		for _, p := range props {
			set[p] = true
		}
		result[resType] = set
	}
	return result
}
