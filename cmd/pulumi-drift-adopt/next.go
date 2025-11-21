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

	// Gate 3: Check for pending chunks
	nextChunk := plan.GetNextPendingChunk()
	if nextChunk != nil {
		fmt.Printf("📋 Next chunk ready: %s (Order: %d)\n", nextChunk.ID, nextChunk.Order)
		fmt.Println()
		fmt.Println("View chunk details:")
		fmt.Printf("  pulumi-drift-adopt show-chunk --chunk %s\n", nextChunk.ID)
		fmt.Println()
		fmt.Println("After generating code changes, apply them:")
		fmt.Printf("  pulumi-drift-adopt apply-diff --chunk %s --files <files>\n", nextChunk.ID)
		return nil
	}

	// Gate 4: Check for failed chunks
	failedChunks := plan.GetFailedChunks()
	if len(failedChunks) > 0 {
		fmt.Printf("❌ %d chunk(s) failed\n", len(failedChunks))
		fmt.Println()
		fmt.Println("Failed chunks:")
		for _, chunk := range failedChunks {
			fmt.Printf("  - %s: %s\n", chunk.ID, chunk.LastError)
		}
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  1. Fix the code and try again:")
		fmt.Printf("     pulumi-drift-adopt apply-diff --chunk %s --files <files>\n", failedChunks[0].ID)
		fmt.Println("  2. Skip the chunk:")
		fmt.Printf("     pulumi-drift-adopt skip --chunk %s\n", failedChunks[0].ID)
		fmt.Println("  3. Rollback changes:")
		fmt.Println("     pulumi-drift-adopt rollback --diff <diff-id>")
		return nil
	}

	// Gate 5: All complete!
	statusCounts := plan.CountByStatus()
	fmt.Println("✅ STOP - Drift adoption complete!")
	fmt.Println()
	fmt.Println("Summary:")
	fmt.Printf("  Total chunks: %d\n", plan.TotalChunks)
	fmt.Printf("  Completed: %d\n", statusCounts[driftadopt.ChunkCompleted])
	if statusCounts[driftadopt.ChunkSkipped] > 0 {
		fmt.Printf("  Skipped: %d\n", statusCounts[driftadopt.ChunkSkipped])
	}
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Review changes: git diff")
	fmt.Println("  2. Test changes: pulumi preview")
	fmt.Println("  3. Commit changes: git add . && git commit")
	fmt.Println("  4. Create PR: gh pr create")

	return nil
}
