package main

import (
	"fmt"

	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt"
	"github.com/spf13/cobra"
)

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Rollback applied code changes",
	Long: `Rolls back code changes from a previously applied diff.

This command restores the original file contents before a diff was applied.
Use this to undo changes from a failed or incorrect apply-diff attempt.`,
	RunE: runRollback,
}

var rollbackDiffID string

func init() {
	rollbackCmd.Flags().StringVar(&rollbackDiffID, "diff", "", "Diff ID to rollback (required)")
	rollbackCmd.MarkFlagRequired("diff")
}

func runRollback(cmd *cobra.Command, args []string) error {
	projectDir, _ := cmd.Flags().GetString("project")

	// Create recorder to handle rollback
	recorder := driftadopt.NewDiffRecorder(projectDir)

	// Get diff info
	diff, err := recorder.GetDiff(rollbackDiffID)
	if err != nil {
		return fmt.Errorf("get diff: %w", err)
	}

	// Display what will be rolled back
	fmt.Printf("🔄 Rolling back diff: %s\n", rollbackDiffID)
	fmt.Printf("Step: %s\n", diff.StepID)
	fmt.Printf("Applied: %s\n", diff.Timestamp.Format("2006-01-02 15:04:05"))
	fmt.Println()
	fmt.Printf("Files to restore: %d\n", len(diff.Files))
	for filePath := range diff.Files {
		fmt.Printf("  - %s\n", filePath)
	}
	fmt.Println()

	// Perform rollback
	if err := recorder.Rollback(rollbackDiffID); err != nil {
		return fmt.Errorf("rollback: %w", err)
	}

	fmt.Println("✅ Successfully rolled back changes")
	fmt.Println()
	fmt.Println("Next:")
	fmt.Println("  pulumi-drift-adopt next")

	return nil
}
