package main

import (
	"fmt"

	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt"
	"github.com/spf13/cobra"
)

var showChunkCmd = &cobra.Command{
	Use:   "show-chunk",
	Short: "Display detailed information about a chunk",
	Long: `Shows chunk details including resources, current code, and expected changes.

This command is designed for agent consumption - it outputs structured information
that agents can use to generate code updates.`,
	RunE: runShowChunk,
}

var chunkID string

func init() {
	showChunkCmd.Flags().StringVar(&chunkID, "chunk", "", "Chunk ID to display (required)")
	showChunkCmd.MarkFlagRequired("chunk")
}

func runShowChunk(cmd *cobra.Command, args []string) error {
	planFile, _ := cmd.Flags().GetString("plan")
	projectDir, _ := cmd.Flags().GetString("project")

	// Load plan
	plan, err := driftadopt.ReadPlanFile(planFile)
	if err != nil {
		return fmt.Errorf("read plan: %w", err)
	}

	// Get chunk info
	guide := driftadopt.NewChunkGuide(projectDir)
	info, err := guide.ShowChunk(plan, chunkID)
	if err != nil {
		return fmt.Errorf("show chunk: %w", err)
	}

	// Display chunk information
	fmt.Printf("Chunk: %s (Order: %d, Status: %s)\n", info.ChunkID, plan.GetChunk(chunkID).Order, info.Status)
	fmt.Println()

	// Display resources
	fmt.Println("Resources:")
	for _, res := range info.Resources {
		fmt.Printf("  • %s (%s)\n", res.Name, res.Type)
		fmt.Printf("    URN: %s\n", res.URN)
		if res.SourceFile != "" {
			fmt.Printf("    Source: %s", res.SourceFile)
			if res.SourceLine > 0 {
				fmt.Printf(":%d", res.SourceLine)
			}
			fmt.Println()
		}
	}
	fmt.Println()

	// Display expected changes
	fmt.Println("Expected Changes:")
	for _, change := range info.ExpectedChanges {
		fmt.Printf("  • %s\n", change)
	}
	fmt.Println()

	// Display current code
	if len(info.CurrentCode) > 0 {
		fmt.Println("Current Code:")
		for filePath, code := range info.CurrentCode {
			fmt.Printf("  File: %s\n", filePath)
			fmt.Println("  ```")
			fmt.Println(code)
			fmt.Println("  ```")
			fmt.Println()
		}
	}

	// Display dependencies
	if len(info.Dependencies) > 0 {
		fmt.Printf("Dependencies: %v\n", info.Dependencies)
		fmt.Println()
	}

	return nil
}
