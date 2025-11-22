package driftadopt

import (
	"context"
	"fmt"

	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt/validator"
)

// ApplyOrchestrator orchestrates the full diff application workflow
type ApplyOrchestrator struct {
	projectDir    string
	diffApplier   *DiffApplier
	validator     validator.Validator
	previewRunner PreviewRunner
	diffMatcher   *DiffMatcher
}

// PreviewRunner runs pulumi preview and returns diffs
type PreviewRunner interface {
	RunPreview(ctx context.Context, projectPath string) ([]ResourceDiff, error)
}

// ApplyResult contains the result of applying a diff
type ApplyResult struct {
	StepID        string
	Status         StepStatus
	DiffID         string
	CompileSuccess bool
	PreviewOutput  string
	DiffMatches    bool
	ErrorMessage   string
	Suggestions    []string
}

// NewApplyOrchestrator creates a new apply orchestrator
func NewApplyOrchestrator(projectDir string, validator validator.Validator, previewRunner PreviewRunner) *ApplyOrchestrator {
	return &ApplyOrchestrator{
		projectDir:    projectDir,
		diffApplier:   NewDiffApplier(projectDir),
		validator:     validator,
		previewRunner: previewRunner,
		diffMatcher:   NewDiffMatcher(),
	}
}

// ApplyDiff applies code changes and validates them
func (o *ApplyOrchestrator) ApplyDiff(ctx context.Context, planFile, stepID string, changes []FileChange) (*ApplyResult, error) {
	// Load plan
	plan, err := ReadPlanFile(planFile)
	if err != nil {
		return nil, fmt.Errorf("read plan file: %w", err)
	}

	// Get step
	step := plan.GetStep(stepID)
	if step == nil {
		return nil, fmt.Errorf("step not found: %s", stepID)
	}

	// Mark step as in progress
	step.Status = StepInProgress
	if err := WritePlanFile(planFile, plan); err != nil {
		return nil, fmt.Errorf("update plan: %w", err)
	}

	result := &ApplyResult{
		StepID:        stepID,
		CompileSuccess: false,
		DiffMatches:    false,
	}

	// Step 1: Apply changes
	diffID, err := o.diffApplier.ApplyChanges(stepID, changes)
	if err != nil {
		result.Status = StepFailed
		result.ErrorMessage = fmt.Sprintf("Failed to apply changes: %v", err)
		result.Suggestions = []string{"Check file paths and permissions"}
		o.updatePlanStatus(planFile, stepID, result)
		return result, nil
	}

	result.DiffID = diffID

	// Step 2: Validate compilation
	validationResult, err := o.validator.Validate(ctx, o.projectDir)
	if err != nil {
		result.Status = StepFailed
		result.ErrorMessage = fmt.Sprintf("Validation error: %v", err)
		result.Suggestions = []string{"Check validator configuration"}
		o.rollbackAndUpdatePlan(planFile, stepID, diffID, result)
		return result, nil
	}

	if !validationResult.Success {
		result.Status = StepFailed
		result.CompileSuccess = false
		result.ErrorMessage = "Compilation failed"
		result.Suggestions = o.formatCompilationErrors(validationResult.Errors)
		o.rollbackAndUpdatePlan(planFile, stepID, diffID, result)
		return result, nil
	}

	result.CompileSuccess = true

	// Step 3: Run preview
	if o.previewRunner == nil {
		// No preview runner configured (testing mode)
		result.Status = StepCompleted
		result.DiffMatches = true
		o.updatePlanStatus(planFile, stepID, result)
		return result, nil
	}

	previewDiffs, err := o.previewRunner.RunPreview(ctx, o.projectDir)
	if err != nil {
		result.Status = StepFailed
		result.ErrorMessage = fmt.Sprintf("Preview failed: %v", err)
		result.Suggestions = []string{"Check Pulumi configuration", "Verify stack exists"}
		o.rollbackAndUpdatePlan(planFile, stepID, diffID, result)
		return result, nil
	}

	// Step 4: Match diffs
	matchResult := o.diffMatcher.Matches(step.Resources, previewDiffs)

	if !matchResult.Matches {
		result.Status = StepFailed
		result.DiffMatches = false
		result.ErrorMessage = "Preview mismatch"
		result.Suggestions = o.formatMatchErrors(matchResult)
		o.rollbackAndUpdatePlan(planFile, stepID, diffID, result)
		return result, nil
	}

	// Success!
	result.Status = StepCompleted
	result.DiffMatches = true
	o.updatePlanStatus(planFile, stepID, result)

	return result, nil
}

// rollbackAndUpdatePlan rolls back changes and updates plan
func (o *ApplyOrchestrator) rollbackAndUpdatePlan(planFile, stepID, diffID string, result *ApplyResult) {
	// Rollback the diff
	if diffID != "" {
		o.diffApplier.GetRecorder().Rollback(diffID)
	}

	// Update plan
	o.updatePlanStatus(planFile, stepID, result)
}

// updatePlanStatus updates the step status in the plan file
func (o *ApplyOrchestrator) updatePlanStatus(planFile, stepID string, result *ApplyResult) {
	plan, err := ReadPlanFile(planFile)
	if err != nil {
		return
	}

	step := plan.GetStep(stepID)
	if step == nil {
		return
	}

	step.Status = result.Status
	if result.ErrorMessage != "" {
		step.LastError = result.ErrorMessage
	}

	WritePlanFile(planFile, plan)
}

// formatCompilationErrors formats compilation errors as suggestions
func (o *ApplyOrchestrator) formatCompilationErrors(errors []validator.CompilationError) []string {
	var suggestions []string
	suggestions = append(suggestions, "Fix the following compilation errors:")

	for i, err := range errors {
		if i >= 5 {
			suggestions = append(suggestions, fmt.Sprintf("... and %d more errors", len(errors)-5))
			break
		}
		suggestions = append(suggestions, fmt.Sprintf("  %s:%d:%d - %s", err.File, err.Line, err.Column, err.Message))
	}

	suggestions = append(suggestions, fmt.Sprintf("Rollback with: pulumi-drift-adopt rollback %s", ""))

	return suggestions
}

// formatMatchErrors formats diff match errors as suggestions
func (o *ApplyOrchestrator) formatMatchErrors(matchResult *MatchResult) []string {
	var suggestions []string

	if len(matchResult.MissingChanges) > 0 {
		suggestions = append(suggestions, "Missing expected changes:")
		for _, change := range matchResult.MissingChanges {
			suggestions = append(suggestions, fmt.Sprintf("  - %s: %v => %v", change.Path, change.OldValue, change.NewValue))
		}
	}

	if len(matchResult.UnexpectedChanges) > 0 {
		suggestions = append(suggestions, "Unexpected changes:")
		for _, change := range matchResult.UnexpectedChanges {
			suggestions = append(suggestions, fmt.Sprintf("  - %s: %v => %v", change.Path, change.OldValue, change.NewValue))
		}
	}

	suggestions = append(suggestions, "Review the code changes and try again")

	return suggestions
}
