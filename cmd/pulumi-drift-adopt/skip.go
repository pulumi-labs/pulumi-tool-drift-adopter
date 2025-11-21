package main

import (
	"fmt"

	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt"
	"github.com/spf13/cobra"
)

var skipCmd = &cobra.Command{
	Use:   "skip",
	Short: "Skip a chunk in the drift adoption plan",
	Long: `Marks a chunk as skipped and moves to the next chunk.

Use this when:
- A chunk is too complex to adopt automatically
- Manual intervention is needed
- The drift should be handled separately
- You want to defer the chunk for later

Skipped chunks won't block progress on subsequent chunks.`,
	RunE: runSkip,
}

var skipChunkID string
var skipReason string

func init() {
	skipCmd.Flags().StringVar(&skipChunkID, "chunk", "", "Chunk ID to skip (required)")
	skipCmd.Flags().StringVar(&skipReason, "reason", "", "Reason for skipping (optional)")
	skipCmd.MarkFlagRequired("chunk")
}

func runSkip(cmd *cobra.Command, args []string) error {
	planFile, _ := cmd.Flags().GetString("plan")

	// Load plan
	plan, err := driftadopt.ReadPlanFile(planFile)
	if err != nil {
		return fmt.Errorf("read plan: %w", err)
	}

	// Find and update chunk
	chunk := plan.GetChunk(skipChunkID)
	if chunk == nil {
		return fmt.Errorf("chunk not found: %s", skipChunkID)
	}

	// Display chunk info
	fmt.Printf("⏭️  Skipping chunk: %s (Order: %d)\n", skipChunkID, chunk.Order)
	fmt.Println()
	fmt.Printf("Resources in chunk: %d\n", len(chunk.Resources))
	for _, res := range chunk.Resources {
		fmt.Printf("  - %s\n", res.URN)
	}
	fmt.Println()

	// Update chunk status
	chunk.Status = driftadopt.ChunkSkipped
	if skipReason != "" {
		chunk.LastError = fmt.Sprintf("Skipped: %s", skipReason)
	} else {
		chunk.LastError = "Skipped by user"
	}

	// Save plan
	if err := driftadopt.WritePlanFile(planFile, plan); err != nil {
		return fmt.Errorf("write plan: %w", err)
	}

	fmt.Println("✅ Chunk skipped")
	fmt.Println()

	// Show next steps
	nextChunk := plan.GetNextPendingChunk()
	if nextChunk != nil {
		fmt.Printf("Next chunk: %s (Order: %d)\n", nextChunk.ID, nextChunk.Order)
		fmt.Println()
	} else {
		failedChunks := plan.GetFailedChunks()
		if len(failedChunks) > 0 {
			fmt.Println("Remaining failed chunks to address:")
			for _, fc := range failedChunks {
				fmt.Printf("  - %s\n", fc.ID)
			}
			fmt.Println()
		} else {
			fmt.Println("All chunks complete!")
			fmt.Println()
		}
	}

	fmt.Println("Next:")
	fmt.Println("  pulumi-drift-adopt next")

	return nil
}
