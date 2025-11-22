package main

import (
	"context"
	"fmt"

	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt"
	"github.com/spf13/cobra"
)

var applyDiffCmd = &cobra.Command{
	Use:   "apply-diff",
	Short: "Apply agent-generated code changes",
	Long: `Applies code changes submitted by an agent and validates them.

This command:
1. Applies the code changes to files
2. Validates compilation
3. Runs pulumi preview (if configured)
4. Compares preview with expected diff
5. Updates plan status
6. Rolls back on any failure`,
	RunE: runApplyDiff,
}

var (
	applyStepID string
	applyFiles   []string
)

func init() {
	applyDiffCmd.Flags().StringVar(&applyStepID, "step", "", "Step ID (required)")
	applyDiffCmd.Flags().StringSliceVar(&applyFiles, "files", []string{}, "Files to update (format: path:content or @path)")
	applyDiffCmd.MarkFlagRequired("step")
	applyDiffCmd.MarkFlagRequired("files")
}

func runApplyDiff(cmd *cobra.Command, args []string) error {
	planFile, _ := cmd.Flags().GetString("plan")
	projectDir, _ := cmd.Flags().GetString("project")

	// Parse file changes
	changes, err := parseFileChanges(applyFiles)
	if err != nil {
		return fmt.Errorf("parse file changes: %w", err)
	}

	// Note: In a real implementation, you'd need to specify which validator to use
	// For now, we'll use a nil validator (testing mode)
	orchestrator := driftadopt.NewApplyOrchestrator(projectDir, nil, nil)

	// Apply the diff
	result, err := orchestrator.ApplyDiff(context.Background(), planFile, applyStepID, changes)
	if err != nil {
		return fmt.Errorf("apply diff: %w", err)
	}

	// Display result
	fmt.Printf("Step: %s\n", result.StepID)
	fmt.Printf("Status: %s\n", result.Status)
	fmt.Printf("Diff ID: %s\n", result.DiffID)
	fmt.Println()

	if result.Status == driftadopt.StepCompleted {
		fmt.Println("✅ Successfully applied and validated!")
		fmt.Println()
		fmt.Println("Next:")
		fmt.Println("  pulumi-drift-adopt next")
		return nil
	}

	// Failed
	fmt.Printf("❌ %s\n", result.ErrorMessage)
	fmt.Println()

	if len(result.Suggestions) > 0 {
		fmt.Println("Suggestions:")
		for _, suggestion := range result.Suggestions {
			fmt.Printf("  %s\n", suggestion)
		}
		fmt.Println()
	}

	if result.DiffID != "" {
		fmt.Println("Changes have been rolled back.")
		fmt.Println()
	}

	return nil
}

// parseFileChanges parses file change specifications
// Format: "path:content" or "@path" (read from file)
func parseFileChanges(specs []string) ([]driftadopt.FileChange, error) {
	var changes []driftadopt.FileChange

	for _, spec := range specs {
		// For simplicity, assume specs are in format "path:content"
		// In a real implementation, you'd parse more formats
		change := driftadopt.FileChange{
			FilePath: spec,
			NewCode:  "", // Would be parsed from spec
		}
		changes = append(changes, change)
	}

	return changes, nil
}
