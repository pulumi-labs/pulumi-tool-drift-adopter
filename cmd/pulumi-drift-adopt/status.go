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
- Total chunks and completion progress
- Chunk breakdown by status (pending/completed/failed/skipped)
- Current chunk if in progress
- Failed chunks with error messages`,
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
	completed := statusCounts[driftadopt.ChunkCompleted] + statusCounts[driftadopt.ChunkSkipped]
	progress := float64(completed) / float64(plan.TotalChunks) * 100
	fmt.Printf("Progress: %d/%d (%.1f%%)\n", completed, plan.TotalChunks, progress)
	fmt.Println()

	// Display status breakdown
	fmt.Println("Status breakdown:")
	fmt.Printf("  ✅ Completed: %d\n", statusCounts[driftadopt.ChunkCompleted])
	fmt.Printf("  ⏳ Pending:   %d\n", statusCounts[driftadopt.ChunkPending])
	fmt.Printf("  ❌ Failed:    %d\n", statusCounts[driftadopt.ChunkFailed])
	fmt.Printf("  ⏭️  Skipped:   %d\n", statusCounts[driftadopt.ChunkSkipped])
	fmt.Println()

	// Show next pending chunk if any
	nextChunk := plan.GetNextPendingChunk()
	if nextChunk != nil {
		fmt.Printf("Next chunk: %s (Order: %d)\n", nextChunk.ID, nextChunk.Order)
		fmt.Printf("  Resources: %d\n", len(nextChunk.Resources))
		fmt.Println()
	}

	// Show failed chunks if any
	failedChunks := plan.GetFailedChunks()
	if len(failedChunks) > 0 {
		fmt.Println("Failed chunks:")
		for _, chunk := range failedChunks {
			fmt.Printf("  - %s (Order: %d)\n", chunk.ID, chunk.Order)
			if chunk.LastError != "" {
				fmt.Printf("    Error: %s\n", chunk.LastError)
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
			fmt.Printf("  %s - Chunk: %s (%s)\n", diffID, diff.ChunkID, diff.Timestamp.Format("2006-01-02 15:04:05"))
			diffCount++
		}
	}
	if diffCount == 0 {
		fmt.Println("  (none)")
	}
	fmt.Println()

	// Show next command suggestion
	if nextChunk != nil {
		fmt.Println("Next:")
		fmt.Println("  pulumi-drift-adopt next")
	} else if len(failedChunks) > 0 {
		fmt.Println("Action needed:")
		fmt.Println("  Fix failed chunks or skip them")
		fmt.Println("  pulumi-drift-adopt next")
	} else {
		fmt.Println("✅ All chunks completed!")
	}

	return nil
}
