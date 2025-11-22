package driftadopt

import (
	"fmt"
	"os"
)

// StepGuide provides guidance to agents about steps
type StepGuide struct {
	projectDir string
}

// StepInfo contains information about a step for agent consumption
type StepInfo struct {
	StepID         string
	Resources       []ResourceDiff
	CurrentCode     map[string]string // filepath -> code
	ExpectedChanges []string          // Human-readable descriptions
	Dependencies    []string
	Status          StepStatus
}

// NewStepGuide creates a new step guide
func NewStepGuide(projectDir string) *StepGuide {
	return &StepGuide{projectDir: projectDir}
}

// ShowStep provides detailed information about a step
func (g *StepGuide) ShowStep(plan *DriftPlan, stepID string) (*StepInfo, error) {
	step := plan.GetStep(stepID)
	if step == nil {
		return nil, fmt.Errorf("step not found: %s", stepID)
	}

	// Read current code for affected files
	currentCode := make(map[string]string)
	for _, res := range step.Resources {
		if res.SourceFile != "" {
			content, err := os.ReadFile(res.SourceFile)
			if err != nil {
				return nil, fmt.Errorf("read source file %s: %w", res.SourceFile, err)
			}
			currentCode[res.SourceFile] = string(content)
		}
	}

	// Format expected changes as human-readable descriptions
	var expectedChanges []string
	for _, res := range step.Resources {
		for _, prop := range res.PropertyDiff {
			expectedChanges = append(expectedChanges, g.FormatPropertyChange(prop))
		}
	}

	return &StepInfo{
		StepID:         step.ID,
		Resources:       step.Resources,
		CurrentCode:     currentCode,
		ExpectedChanges: expectedChanges,
		Dependencies:    step.Dependencies,
		Status:          step.Status,
	}, nil
}

// FormatPropertyChange formats a property change as a human-readable string
func (g *StepGuide) FormatPropertyChange(prop PropChange) string {
	switch prop.DiffKind {
	case "add":
		return fmt.Sprintf("Add %s = %v", prop.Path, prop.NewValue)
	case "delete":
		return fmt.Sprintf("Delete %s (was: %v)", prop.Path, prop.OldValue)
	case "update":
		return fmt.Sprintf("Update %s: %v => %v", prop.Path, prop.OldValue, prop.NewValue)
	default:
		return fmt.Sprintf("%s %s: %v => %v", prop.DiffKind, prop.Path, prop.OldValue, prop.NewValue)
	}
}
