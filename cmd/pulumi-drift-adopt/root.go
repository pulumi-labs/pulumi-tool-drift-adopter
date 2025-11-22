package main

import (
	"github.com/spf13/cobra"
)

var (
	version = "dev"
)

var rootCmd = &cobra.Command{
	Use:   "pulumi-drift-adopt",
	Short: "Pulumi Drift Adoption Tool",
	Long: `A tool to help adopt infrastructure drift back into Pulumi IaC.

This tool follows an agent-oriented pattern, designed to be called by AI agents
(like Claude) to iteratively adopt drift changes into your codebase.`,
	Version: version,
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().String("project", ".", "Project directory")

	// Add commands
	rootCmd.AddCommand(nextCmd)
}
