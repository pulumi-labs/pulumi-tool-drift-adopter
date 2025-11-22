package main

import (
	"fmt"

	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt"
	"github.com/spf13/cobra"
)

var skipCmd = &cobra.Command{
	Use:   "skip",
	Short: "Skip a step in the drift adoption plan",
	Long: `Marks a step as skipped and moves to the next step.

Use this when:
- A step is too complex to adopt automatically
- Manual intervention is needed
- The drift should be handled separately
- You want to defer the step for later

Skipped steps won't block progress on subsequent steps.`,
	RunE: runSkip,
}

var skipStepID string
var skipReason string

func init() {
	skipCmd.Flags().StringVar(&skipStepID, "step", "", "Step ID to skip (required)")
	skipCmd.Flags().StringVar(&skipReason, "reason", "", "Reason for skipping (optional)")
	skipCmd.MarkFlagRequired("step")
}

func runSkip(cmd *cobra.Command, args []string) error {
	planFile, _ := cmd.Flags().GetString("plan")

	// Load plan
	plan, err := driftadopt.ReadPlanFile(planFile)
	if err != nil {
		return fmt.Errorf("read plan: %w", err)
	}

	// Find and update step
	step := plan.GetStep(skipStepID)
	if step == nil {
		return fmt.Errorf("step not found: %s", skipStepID)
	}

	// Display step info
	fmt.Printf("⏭️  Skipping step: %s (Order: %d)\n", skipStepID, step.Order)
	fmt.Println()
	fmt.Printf("Resources in step: %d\n", len(step.Resources))
	for _, res := range step.Resources {
		fmt.Printf("  - %s\n", res.URN)
	}
	fmt.Println()

	// Update step status
	step.Status = driftadopt.StepSkipped
	if skipReason != "" {
		step.LastError = fmt.Sprintf("Skipped: %s", skipReason)
	} else {
		step.LastError = "Skipped by user"
	}

	// Save plan
	if err := driftadopt.WritePlanFile(planFile, plan); err != nil {
		return fmt.Errorf("write plan: %w", err)
	}

	fmt.Println("✅ Step skipped")
	fmt.Println()

	// Show next steps
	nextStep := plan.GetNextPendingStep()
	if nextStep != nil {
		fmt.Printf("Next step: %s (Order: %d)\n", nextStep.ID, nextStep.Order)
		fmt.Println()
	} else {
		failedSteps := plan.GetFailedSteps()
		if len(failedSteps) > 0 {
			fmt.Println("Remaining failed steps to address:")
			for _, fc := range failedSteps {
				fmt.Printf("  - %s\n", fc.ID)
			}
			fmt.Println()
		} else {
			fmt.Println("All steps complete!")
			fmt.Println()
		}
	}

	fmt.Println("Next:")
	fmt.Println("  pulumi-drift-adopt next")

	return nil
}
