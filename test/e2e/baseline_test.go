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

//go:build e2e

// Package e2e contains end-to-end tests for the drift adoption workflow.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDriftAdoptionBaseline tests drift adoption WITHOUT the drift-adopt tool/skill
// This provides a baseline to compare against the tool-assisted approach
func TestDriftAdoptionBaseline(t *testing.T) {
	t.Parallel()

	testCases := GetStandardTestCases()

	for _, tc := range testCases {
		tc := tc // capture range variable
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			config := DriftTestConfig{
				ExampleDir:    tc.ExampleDir,
				MaxIterations: 20,
			}

			// Step 1: Create and deploy test stack
			testStack := CreateTestStack(t, config.ExampleDir)
			defer testStack.Destroy(t)

			// Verify initial outputs
			tc.VerifyOutputs(t, testStack.Resources)

			// Step 2: Create drift using drifted program (provider-agnostic)
			t.Log("🔧 Step 2: Creating drift using drifted program...")
			err := testStack.CreateDriftWithProgram(t, config.ExampleDir)
			require.NoError(t, err, "Failed to create drift with drifted program")

			// Step 3: Verify drift exists
			t.Log("🔍 Step 3: Verifying drift exists...")
			testStack.Test.Refresh(t)
			preview := testStack.Test.Preview(t)

			changeCount := preview.ChangeSummary[apitype.OpUpdate] + preview.ChangeSummary[apitype.OpCreate]
			assert.True(t, changeCount > 0, "Should have changes detected (drift exists)")
			t.Logf("   ✅ Drift detected: %d change(s)", changeCount)

			// Step 4: Use Claude (baseline - no tool) to fix drift
			t.Log("🤖 Step 4: Invoking Claude BASELINE (no drift-adopt tool) to fix drift...")
			metrics, err := RunDriftAdoptionBaseline(ctx, testStack, config.MaxIterations, t)
			require.NoError(t, err, "Claude baseline drift adoption failed")
			t.Log("   ✅ Claude completed drift adoption (baseline)")

			// Log metrics
			t.Log("📊 Baseline Drift Adoption Metrics:")
			t.Logf("   Total iterations: %d", metrics.IterationsCount)
			t.Logf("   Total tool calls: %d", metrics.ToolCallsCount)
			t.Logf("   Bash calls: %d", metrics.BashCallsCount)
			t.Logf("   Read file calls: %d", metrics.ReadFileCallsCount)
			t.Logf("   Write file calls: %d", metrics.WriteFileCallsCount)
			t.Logf("   Token usage: %d input, %d output, %d total",
				metrics.InputTokens, metrics.OutputTokens, metrics.TotalTokens)

			// Step 5: Verify the code was updated
			t.Log("✅ Step 5: Verifying code was updated...")
			codeContent, err := readFile(testStack.WorkingDir, "index.ts")
			require.NoError(t, err)

			tc.VerifyCode(t, codeContent)
			t.Log("   ✅ Code correctly updated")

			// Step 6: Verify no drift remains
			updates, creates, deletes := testStack.VerifyNoDrift(t)
			assert.Equal(t, 0, updates, "Should have no updates (drift is fixed)")
			assert.Equal(t, 0, creates, "Should have no creates")
			assert.Equal(t, 0, deletes, "Should have no deletes")

			t.Logf("✅✅✅ Baseline drift adoption complete for %s! ✅✅✅", tc.Name)
		})
	}
}

// RunDriftAdoptionBaseline runs Claude WITHOUT the drift-adopt tool/skill
// This provides a baseline to compare against the tool-assisted approach
func RunDriftAdoptionBaseline(
	ctx context.Context,
	testStack *TestStack,
	maxIterations int,
	t *testing.T,
) (*DriftTestMetrics, error) {
	// Get API key
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	require.NotEmpty(t, apiKey, "ANTHROPIC_API_KEY must be set")

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	// Initialize metrics
	metrics := &DriftTestMetrics{}

	// Baseline system message - NO drift-adopt skill
	// Just basic instructions for fixing drift with pulumi preview
	systemMsg := `You are helping fix infrastructure drift in a Pulumi project.

Drift means the actual infrastructure state differs from what's in the code.

To fix drift:
1. Run 'pulumi preview --refresh' to see differences
2. Update the code to match the actual infrastructure state
3. Run 'pulumi preview' again to verify no changes remain
4. Repeat until 'pulumi preview' shows no changes

The pulumi CLI is available in your environment. Use bash to run commands, read_file to read code, and write_file to update code.`

	// Create conversation with Claude
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(
			"I need you to fix infrastructure drift in my Pulumi code. " +
				"The project is in the current directory. " +
				"Please check for drift and update the code to match the actual infrastructure. " +
				"Continue until there is no drift.")),
	}

	// Iterative conversation loop
	t.Logf("   🔄 Starting baseline conversation loop (max %d iterations)", maxIterations)
	for i := 0; i < maxIterations; i++ {
		metrics.IterationsCount = i + 1
		t.Logf("   ➡️  Iteration %d: Calling Claude API...", i+1)

		// Call Claude
		response, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.ModelClaudeSonnet4_5_20250929,
			MaxTokens: 4096,
			System: []anthropic.TextBlockParam{
				{Text: systemMsg},
			},
			Messages: messages,
			Tools:    getClaudeTools(),
		})

		if err != nil {
			return metrics, fmt.Errorf("claude API error: %w", err)
		}

		// Track token usage
		metrics.InputTokens += int64(response.Usage.InputTokens)
		metrics.OutputTokens += int64(response.Usage.OutputTokens)
		metrics.TotalTokens = metrics.InputTokens + metrics.OutputTokens

		t.Logf("      ⬅️  Response: stop_reason=%s, usage=%+v", response.StopReason, response.Usage)

		// Log any text content from Claude
		for _, block := range response.Content {
			if block.Type == "text" && block.Text != "" {
				t.Logf("      💬 Claude: %s", truncateString(block.Text, 200))
			}
		}

		// Check stop reason
		if response.StopReason == anthropic.StopReasonEndTurn {
			// Claude is done
			t.Logf("   ✅ Claude finished (end_turn after %d iterations)", i+1)
			return metrics, nil
		}

		// Process tool uses
		if response.StopReason == anthropic.StopReasonToolUse {
			var toolResults []anthropic.ContentBlockParamUnion
			toolCount := 0
			for _, block := range response.Content {
				if block.Type == "tool_use" {
					toolCount++
				}
			}
			t.Logf("      🔧 Claude requested %d tool(s)", toolCount)
			metrics.ToolCallsCount += toolCount

			for _, block := range response.Content {
				if block.Type != "tool_use" {
					continue
				}

				// Extract tool use information directly from the block
				toolID := block.ID
				toolName := block.Name
				var toolInput map[string]interface{}
				if err := json.Unmarshal(block.Input, &toolInput); err != nil {
					return metrics, fmt.Errorf("failed to parse tool input: %w", err)
				}

				var result string
				var err error

				switch toolName {
				case "bash":
					metrics.BashCallsCount++
					cmd := toolInput["command"].(string)
					t.Logf("         🔨 bash: %s", truncateString(cmd, 100))
					result, err = executeBaselineBash(testStack.WorkingDir, testStack.BackendURL, testStack.ConfigPassphrase, cmd)

					if err != nil {
						t.Logf("         ❌ Error: %v", err)
					} else {
						t.Logf("         ✅ Output: %s", truncateString(result, 150))
					}
				case "read_file":
					metrics.ReadFileCallsCount++
					path := toolInput["path"].(string)
					t.Logf("         📖 read_file: %s", path)
					result, err = readFile(testStack.WorkingDir, path)
					if err != nil {
						t.Logf("         ❌ Error: %v", err)
					} else {
						t.Logf("         ✅ Read %d bytes", len(result))
					}
				case "write_file":
					metrics.WriteFileCallsCount++
					path := toolInput["path"].(string)
					content := toolInput["content"].(string)
					t.Logf("         ✏️  write_file: %s (%d bytes)", path, len(content))
					err = writeFile(testStack.WorkingDir, path, content)
					if err == nil {
						result = "File written successfully"
						t.Logf("         ✅ File written")
					} else {
						t.Logf("         ❌ Error: %v", err)
					}
				}

				// Create tool result
				if err != nil {
					result = fmt.Sprintf("Error: %v", err)
				}

				toolResults = append(toolResults, anthropic.NewToolResultBlock(
					toolID,
					result,
					err != nil,
				))
			}

			// Convert response.Content to ContentBlockParamUnion for assistant message
			var assistantContent []anthropic.ContentBlockParamUnion
			for _, block := range response.Content {
				switch block.Type {
				case "text":
					assistantContent = append(assistantContent, anthropic.NewTextBlock(block.Text))
				case "tool_use":
					assistantContent = append(assistantContent, anthropic.NewToolUseBlock(
						block.ID,
						block.Input,
						block.Name,
					))
				}
			}

			// Add assistant message and tool results to conversation
			messages = append(messages, anthropic.NewAssistantMessage(assistantContent...))
			messages = append(messages, anthropic.NewUserMessage(toolResults...))

			continue
		}

		// Unknown stop reason
		t.Logf("   ❌ Unexpected stop reason: %s", response.StopReason)
		return metrics, fmt.Errorf("unexpected stop reason: %s", response.StopReason)
	}

	t.Log("   ⚠️  Reached maximum iterations without completion")
	return metrics, fmt.Errorf("reached maximum iterations (%d) without completion", maxIterations)
}

// executeBaselineBash executes a bash command in the project directory
// Similar to executeBash but WITHOUT pulumi-drift-adopt in PATH
func executeBaselineBash(projectDir, backendURL, configPassphrase, command string) (string, error) {
	// Reject any attempts to use pulumi-drift-adopt
	if strings.Contains(command, "pulumi-drift-adopt") {
		return "", fmt.Errorf("pulumi-drift-adopt tool is not available in baseline mode")
	}

	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = projectDir

	// DO NOT add pulumi-drift-adopt to PATH - that's the whole point of baseline
	// Just use the existing PATH with pulumi CLI
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("PULUMI_BACKEND_URL=%s", backendURL),
		fmt.Sprintf("PULUMI_CONFIG_PASSPHRASE=%s", configPassphrase),
	)

	output, err := cmd.CombinedOutput()
	return string(output), err
}
