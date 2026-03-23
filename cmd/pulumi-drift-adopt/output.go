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
)

// outputResult outputs the final JSON result with filtering, exclusions, and resource limiting.
// Full output is written to a file; a compact summary is written to stdout.
func outputResult(resources []ResourceChange, excludeURNs []string, depMapFile, outputFile string) error {
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
