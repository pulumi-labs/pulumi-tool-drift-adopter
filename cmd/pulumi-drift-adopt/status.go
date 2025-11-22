package main

import (
	"fmt"

	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show drift adoption status",
	Long: `Displays the current status of drift adoption plan.

Shows:
- Total steps and completion progress
- Step breakdown by status (pending/completed/failed/skipped)
- Current step if in progress
- Failed steps with error messages`,
	RunE: runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	planFile, _ := cmd.Flags().GetString("plan")

	// Load plan
	plan, err := driftadopt.ReadPlanFile(planFile)
	if err != nil {
		return fmt.Errorf("read plan: %w", err)
	}

	// Calculate status counts
	statusCounts := plan.CountByStatus()

	// Display header
	fmt.Println("📊 Drift Adoption Status")
	fmt.Println()

	// Display overall progress
	completed := statusCounts[driftadopt.StepCompleted] + statusCounts[driftadopt.StepSkipped]
	progress := float64(completed) / float64(plan.TotalSteps) * 100
	fmt.Printf("Progress: %d/%d (%.1f%%)\n", completed, plan.TotalSteps, progress)
	fmt.Println()

	// Display status breakdown
	fmt.Println("Status breakdown:")
	fmt.Printf("  ✅ Completed: %d\n", statusCounts[driftadopt.StepCompleted])
	fmt.Printf("  ⏳ Pending:   %d\n", statusCounts[driftadopt.StepPending])
	fmt.Printf("  ❌ Failed:    %d\n", statusCounts[driftadopt.StepFailed])
	fmt.Printf("  ⏭️  Skipped:   %d\n", statusCounts[driftadopt.StepSkipped])
	fmt.Println()

	// Show next pending step if any
	nextStep := plan.GetNextPendingStep()
	if nextStep != nil {
		fmt.Printf("Next step: %s (Order: %d)\n", nextStep.ID, nextStep.Order)
		fmt.Printf("  Resources: %d\n", len(nextStep.Resources))
		fmt.Println()
	}

	// Show failed steps if any
	failedSteps := plan.GetFailedSteps()
	if len(failedSteps) > 0 {
		fmt.Println("Failed steps:")
		for _, step := range failedSteps {
			fmt.Printf("  - %s (Order: %d)\n", step.ID, step.Order)
			if step.LastError != "" {
				fmt.Printf("    Error: %s\n", step.LastError)
			}
		}
		fmt.Println()
	}

	// Show recent diffs if available
	projectDir, _ := cmd.Flags().GetString("project")
	recorder := driftadopt.NewDiffRecorder(projectDir)

	// Try to show last 3 diffs
	fmt.Println("Recent changes:")
	diffCount := 0
	for i := 1; i <= 10 && diffCount < 3; i++ {
		diffID := fmt.Sprintf("diff-%03d", i)
		if diff, err := recorder.GetDiff(diffID); err == nil {
			fmt.Printf("  %s - Step: %s (%s)\n", diffID, diff.StepID, diff.Timestamp.Format("2006-01-02 15:04:05"))
			diffCount++
		}
	}
	if diffCount == 0 {
		fmt.Println("  (none)")
	}
	fmt.Println()

	// Show next command suggestion
	if nextStep != nil {
		fmt.Println("Next:")
		fmt.Println("  pulumi-drift-adopt next")
	} else if len(failedSteps) > 0 {
		fmt.Println("Action needed:")
		fmt.Println("  Fix failed steps or skip them")
		fmt.Println("  pulumi-drift-adopt next")
	} else {
		fmt.Println("✅ All steps completed!")
	}

	return nil
}
