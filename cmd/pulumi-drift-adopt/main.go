// Package main implements the pulumi-drift-adopt CLI tool for detecting and adopting infrastructure drift.
package main

import (
	"os"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
