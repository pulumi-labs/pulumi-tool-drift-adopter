package driftadopt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ReadPlanFile loads a drift adoption plan from a JSON file
func ReadPlanFile(path string) (*DriftPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var plan DriftPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("unmarshal plan: %w", err)
	}

	return &plan, nil
}

// WritePlanFile saves a drift adoption plan to a JSON file
// The file is formatted with indentation for human readability
func WritePlanFile(path string, plan *DriftPlan) error {
	// Create parent directories if they don't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory: %w", err)
	}

	// Marshal with indentation for readability
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal plan: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write plan file: %w", err)
	}

	return nil
}
