// Copyright 2026, Pulumi Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"github.com/spf13/cobra"
)

var (
	version = "dev"
)

// newRootCmd creates a fresh root command tree with all subcommands and flags registered.
func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pulumi-drift-adopt",
		Short: "Pulumi Drift Adoption Tool",
		Long: `A tool to help adopt infrastructure drift back into Pulumi IaC.

This tool follows an agent-oriented pattern, designed to be called by AI agents
(like Claude) to iteratively adopt drift changes into your codebase.`,
		Version: version,
	}
	cmd.PersistentFlags().String("project", ".", "Project directory")
	cmd.AddCommand(newNextCmd())
	return cmd
}

// rootCmd is the package-level command used by main().
var rootCmd = newRootCmd()
