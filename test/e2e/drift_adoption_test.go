//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/pulumi/providertest/pulumitest"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDriftAdoptionWorkflow tests the complete drift adoption workflow:
// 1. Use pulumitest to copy the simple-s3 example to a temp directory
// 2. Deploy the stack (S3 bucket without tags)
// 3. Create drift by manually modifying the bucket with AWS CLI
// 4. Use Claude SDK to invoke the drift-adopt skill
// 5. Verify Claude updated the code correctly
// 6. Run preview to confirm no remaining drift
func TestDriftAdoptionWorkflow(t *testing.T) {
	ctx := context.Background()

	// Ensure required environment variables are set
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	require.NotEmpty(t, apiKey, "ANTHROPIC_API_KEY must be set")

	awsRegion := os.Getenv("AWS_REGION")
	if awsRegion == "" {
		awsRegion = "us-west-2"
	}

	// Step 1: Create a PulumiTest from the example and copy to temp directory
	exampleDir := filepath.Join("..", "..", "examples", "simple-s3")
	t.Logf("📁 Step 1: Copying example from %s to temp directory...", exampleDir)
	test := pulumitest.NewPulumiTest(t, exampleDir).CopyToTempDir(t)
	testDir := test.WorkingDir()
	t.Logf("   Working directory: %s", testDir)

	// Step 2: Deploy the stack using pulumitest
	t.Log("🚀 Step 2: Deploying initial stack (this may take a few minutes)...")
	upResult := test.Up(t)
	defer func() {
		t.Log("🧹 Cleaning up: Destroying stack...")
		test.Destroy(t)
	}()
	t.Logf("   Deployment summary: %+v", upResult.Summary)

	// Get the bucket name from outputs
	bucketNameOutput, ok := upResult.Outputs["bucketName"]
	require.True(t, ok, "bucketName should be in outputs")
	bucketName, ok := bucketNameOutput.Value.(string)
	require.True(t, ok, "bucketName should be a string")
	require.NotEmpty(t, bucketName, "Bucket name should not be empty")
	t.Logf("   ✅ Deployed bucket: %s", bucketName)

	// Step 3: Create drift by modifying the bucket with AWS CLI
	t.Log("🔧 Step 3: Creating drift by adding tags to bucket...")
	t.Logf("   Adding tags: Environment=production, ManagedBy=manual")
	err := createDrift(bucketName, awsRegion)
	require.NoError(t, err, "Failed to create drift")
	t.Log("   ✅ Tags added successfully")

	// Step 4: Verify drift exists by running refresh then preview
	t.Log("🔍 Step 4: Verifying drift exists...")
	t.Log("   Running refresh to capture out-of-band changes...")
	test.Refresh(t)
	t.Log("   Running preview to detect drift...")
	previewResult := test.Preview(t)
	updateCount := previewResult.ChangeSummary[apitype.OpUpdate]
	t.Logf("   Detected %d resource(s) with drift", updateCount)
	assert.True(t, updateCount > 0, "Should have updates detected (drift exists)")

	// Step 5: Use Claude SDK to run drift adoption workflow
	t.Log("🤖 Step 5: Invoking Claude to fix drift (this may take several minutes)...")
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	// Get Pulumi environment variables from the workspace
	stack := test.CurrentStack()
	require.NotNil(t, stack, "Stack should exist")
	workspace := stack.Workspace()
	envVars := workspace.GetEnvVars()
	backendURL, ok := envVars["PULUMI_BACKEND_URL"]
	if !ok || backendURL == "" {
		t.Fatal("PULUMI_BACKEND_URL not found in workspace environment")
	}
	configPassphrase, ok := envVars["PULUMI_CONFIG_PASSPHRASE"]
	if !ok {
		t.Fatal("PULUMI_CONFIG_PASSPHRASE not found in workspace environment")
	}
	t.Logf("   Using Pulumi backend: %s", backendURL)
	t.Logf("   Config passphrase length: %d characters", len(configPassphrase))

	err = runClaudeDriftAdoption(ctx, client, testDir, backendURL, configPassphrase, t)
	require.NoError(t, err, "Claude drift adoption failed")
	t.Log("   ✅ Claude completed drift adoption")

	// Step 6: Verify the code was updated
	t.Log("✅ Step 6: Verifying code was updated...")
	codeContent, err := os.ReadFile(filepath.Join(testDir, "index.ts"))
	require.NoError(t, err)

	// Check that the code now includes the tags that were added via AWS CLI
	assert.Contains(t, string(codeContent), "Environment",
		"Code should now contain Environment tag")
	assert.Contains(t, string(codeContent), "production",
		"Code should now contain production value")
	t.Log("   ✅ Code contains expected tags")

	// Step 7: Run preview again to verify no drift remains
	t.Log("🔍 Step 7: Running preview to verify drift is fixed...")
	finalPreview := test.Preview(t)

	finalUpdateCount := finalPreview.ChangeSummary[apitype.OpUpdate]
	finalCreateCount := finalPreview.ChangeSummary[apitype.OpCreate]
	finalDeleteCount := finalPreview.ChangeSummary[apitype.OpDelete]

	t.Logf("   Final change summary: %d updates, %d creates, %d deletes",
		finalUpdateCount, finalCreateCount, finalDeleteCount)

	assert.Equal(t, 0, finalUpdateCount, "Should have no updates (drift is fixed)")
	assert.Equal(t, 0, finalCreateCount, "Should have no creates")
	assert.Equal(t, 0, finalDeleteCount, "Should have no deletes")

	t.Log("✅✅✅ Drift adoption workflow complete! ✅✅✅")
}

// createDrift uses AWS CLI to add tags to the bucket, creating drift
func createDrift(bucketName, region string) error {
	tagSet := `{"TagSet": [{"Key": "Environment", "Value": "production"}, {"Key": "ManagedBy", "Value": "manual"}]}`

	cmd := exec.Command("aws", "s3api", "put-bucket-tagging",
		"--bucket", bucketName,
		"--tagging", tagSet,
		"--region", region)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to add tags: %w\n%s", err, output)
	}

	return nil
}

// runClaudeDriftAdoption uses Claude SDK to run the drift adoption workflow
func runClaudeDriftAdoption(ctx context.Context, client anthropic.Client, projectDir, backendURL, configPassphrase string, t *testing.T) error {
	// Read the drift-adopt skill from the project root (not the temp directory)
	// Get the test file's directory and go up to find the skills directory
	_, testFile, _, _ := runtime.Caller(0)
	testDir := filepath.Dir(testFile)
	skillPath := filepath.Join(testDir, "..", "..", "skills", "drift-adopt.md")
	t.Logf("   📖 Reading skill from: %s", skillPath)
	skillContent, err := os.ReadFile(skillPath)
	if err != nil {
		return fmt.Errorf("failed to read skill: %w", err)
	}
	t.Logf("   ✅ Loaded skill (%d bytes)", len(skillContent))

	// Initial system message
	systemMsg := string(skillContent)

	// Create conversation with Claude
	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(
			"I need you to adopt infrastructure drift into my Pulumi code. " +
			"The project is in the current directory. " +
			"Please run the drift adoption tool and make the necessary code changes. " +
			"Continue until the drift is fully adopted.")),
	}

	// Iterative conversation loop
	maxIterations := 10
	t.Logf("   🔄 Starting conversation loop (max %d iterations)", maxIterations)
	for i := 0; i < maxIterations; i++ {
		t.Logf("   ➡️  Iteration %d: Calling Claude API...", i+1)
		// Call Claude
		response, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.ModelClaudeSonnet4_5_20250929,
			MaxTokens: 4096,
			System: []anthropic.TextBlockParam{
				{Text: systemMsg},
			},
			Messages: messages,
			Tools: []anthropic.ToolUnionParam{
				{
					OfTool: &anthropic.ToolParam{
						Name: "bash",
						Description: anthropic.String("Execute bash commands in the project directory"),
						InputSchema: anthropic.ToolInputSchemaParam{
							Type: "object",
							Properties: map[string]interface{}{
								"command": map[string]interface{}{
									"type":        "string",
									"description": "The bash command to execute",
								},
							},
							Required: []string{"command"},
						},
					},
				},
				{
					OfTool: &anthropic.ToolParam{
						Name: "read_file",
						Description: anthropic.String("Read the contents of a file"),
						InputSchema: anthropic.ToolInputSchemaParam{
							Type: "object",
							Properties: map[string]interface{}{
								"path": map[string]interface{}{
									"type":        "string",
									"description": "The file path to read",
								},
							},
							Required: []string{"path"},
						},
					},
				},
				{
					OfTool: &anthropic.ToolParam{
						Name: "write_file",
						Description: anthropic.String("Write contents to a file"),
						InputSchema: anthropic.ToolInputSchemaParam{
							Type: "object",
							Properties: map[string]interface{}{
								"path": map[string]interface{}{
									"type":        "string",
									"description": "The file path to write",
								},
								"content": map[string]interface{}{
									"type":        "string",
									"description": "The content to write",
								},
							},
							Required: []string{"path", "content"},
						},
					},
				},
			},
		})

		if err != nil {
			return fmt.Errorf("Claude API error: %w", err)
		}

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
			return nil
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

			for _, block := range response.Content {
				if block.Type != "tool_use" {
					continue
				}

				// Extract tool use information directly from the block
				toolID := block.ID
				toolName := block.Name
				var toolInput map[string]interface{}
				if err := json.Unmarshal(block.Input, &toolInput); err != nil {
					return fmt.Errorf("failed to parse tool input: %w", err)
				}

				var result string
				var err error

				switch toolName {
				case "bash":
					cmd := toolInput["command"].(string)
					t.Logf("         🔨 bash: %s", truncateString(cmd, 100))
					result, err = executeBash(projectDir, backendURL, configPassphrase, cmd)
					if err != nil {
						t.Logf("         ❌ Error: %v", err)
					} else {
						t.Logf("         ✅ Output: %s", truncateString(result, 150))
					}
				case "read_file":
					path := toolInput["path"].(string)
					t.Logf("         📖 read_file: %s", path)
					result, err = readFile(projectDir, path)
					if err != nil {
						t.Logf("         ❌ Error: %v", err)
					} else {
						t.Logf("         ✅ Read %d bytes", len(result))
					}
				case "write_file":
					path := toolInput["path"].(string)
					content := toolInput["content"].(string)
					t.Logf("         ✏️  write_file: %s (%d bytes)", path, len(content))
					err = writeFile(projectDir, path, content)
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
				if block.Type == "text" {
					assistantContent = append(assistantContent, anthropic.NewTextBlock(block.Text))
				} else if block.Type == "tool_use" {
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
		return fmt.Errorf("unexpected stop reason: %s", response.StopReason)
	}

	t.Log("   ⚠️  Reached maximum iterations without completion")
	return fmt.Errorf("reached maximum iterations (%d) without completion", maxIterations)
}

// executeBash executes a bash command in the project directory
func executeBash(projectDir, backendURL, configPassphrase, command string) (string, error) {
	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = projectDir

	// Add the project's bin directory to PATH so Claude can find pulumi-drift-adopt
	_, testFile, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(testFile), "..", "..")
	binDir := filepath.Join(projectRoot, "bin")

	// Preserve existing PATH and prepend bin directory
	currentPath := os.Getenv("PATH")
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("PATH=%s:%s", binDir, currentPath),
		fmt.Sprintf("PULUMI_BACKEND_URL=%s", backendURL),
		fmt.Sprintf("PULUMI_CONFIG_PASSPHRASE=%s", configPassphrase),
	)

	output, err := cmd.CombinedOutput()
	return string(output), err
}

// readFile reads a file from the project directory
func readFile(projectDir, path string) (string, error) {
	fullPath := filepath.Join(projectDir, path)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

// writeFile writes content to a file in the project directory
func writeFile(projectDir, path, content string) error {
	fullPath := filepath.Join(projectDir, path)
	return os.WriteFile(fullPath, []byte(content), 0644)
}

// truncateString truncates a string to maxLen characters, adding "..." if truncated
func truncateString(s string, maxLen int) string {
	// Replace newlines with spaces for cleaner logging
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	// Collapse multiple spaces
	for strings.Contains(s, "  ") {
		s = strings.ReplaceAll(s, "  ", " ")
	}
	s = strings.TrimSpace(s)

	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
