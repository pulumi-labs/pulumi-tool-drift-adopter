package main

import (
	"fmt"
	"os"

	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt"
	"github.com/spf13/cobra"
)

var nextCmd = &cobra.Command{
	Use:   "next",
	Short: "Show the next step in the drift adoption workflow",
	Long: `Sequential gate pattern orchestrator that guides you through drift adoption.

Call this command repeatedly to get guidance on what to do next. When all steps
are complete, it will output "STOP".`,
	RunE: runNext,
}

func runNext(cmd *cobra.Command, args []string) error {
	planFile, _ := cmd.Flags().GetString("plan")

	// Gate 1: Check if plan exists
	if _, err := os.Stat(planFile); err != nil {
		fmt.Println("❌ No drift plan found")
		fmt.Println()
		fmt.Println("Create a drift adoption plan:")
		fmt.Printf("  pulumi-drift-adopt generate-plan --stack <stack-name>\n")
		return nil
	}

	// Gate 2: Load plan
	plan, err := driftadopt.ReadPlanFile(planFile)
	if err != nil {
		fmt.Printf("❌ Failed to read plan: %v\n", err)
		return nil
	}

	// Gate 3: Check for pending steps
	nextStep := plan.GetNextPendingStep()
	if nextStep != nil {
		fmt.Printf("📋 Next step ready: %s (Order: %d)\n", nextStep.ID, nextStep.Order)
		fmt.Println()
		fmt.Println("View step details:")
		fmt.Printf("  pulumi-drift-adopt show-step --step %s\n", nextStep.ID)
		fmt.Println()
		fmt.Println("After generating code changes, apply them:")
		fmt.Printf("  pulumi-drift-adopt apply-diff --step %s --files <files>\n", nextStep.ID)
		return nil
	}

	// Gate 4: Check for failed steps
	failedSteps := plan.GetFailedSteps()
	if len(failedSteps) > 0 {
		fmt.Printf("❌ %d step(s) failed\n", len(failedSteps))
		fmt.Println()
		fmt.Println("Failed steps:")
		for _, step := range failedSteps {
			fmt.Printf("  - %s: %s\n", step.ID, step.LastError)
		}
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  1. Fix the code and try again:")
		fmt.Printf("     pulumi-drift-adopt apply-diff --step %s --files <files>\n", failedSteps[0].ID)
		fmt.Println("  2. Skip the step:")
		fmt.Printf("     pulumi-drift-adopt skip --step %s\n", failedSteps[0].ID)
		fmt.Println("  3. Rollback changes:")
		fmt.Println("     pulumi-drift-adopt rollback --diff <diff-id>")
		return nil
	}

	// Gate 5: All complete!
	statusCounts := plan.CountByStatus()
	fmt.Println("✅ STOP - Drift adoption complete!")
	fmt.Println()
	fmt.Println("Summary:")
	fmt.Printf("  Total steps: %d\n", plan.TotalSteps)
	fmt.Printf("  Completed: %d\n", statusCounts[driftadopt.StepCompleted])
	if statusCounts[driftadopt.StepSkipped] > 0 {
		fmt.Printf("  Skipped: %d\n", statusCounts[driftadopt.StepSkipped])
	}
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Review changes: git diff")
	fmt.Println("  2. Test changes: pulumi preview")
	fmt.Println("  3. Commit changes: git add . && git commit")
	fmt.Println("  4. Create PR: gh pr create")

	return nil
}
