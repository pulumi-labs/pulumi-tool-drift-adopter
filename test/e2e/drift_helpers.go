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
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/pulumi/providertest/pulumitest"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/stretchr/testify/require"
)

// DriftTestMetrics tracks the work done by the LLM during drift adoption
type DriftTestMetrics struct {
	ToolCallsCount       int                `json:"tool_calls_count"`        // Total tool calls
	BashCallsCount       int                `json:"bash_calls_count"`        // Bash tool calls
	ReadFileCallsCount   int                `json:"read_file_calls_count"`   // Read file tool calls
	WriteFileCallsCount  int                `json:"write_file_calls_count"`  // Write file tool calls
	DriftAdoptCallsCount int                `json:"drift_adopt_calls_count"` // pulumi-drift-adopt calls
	InputTokens          int64              `json:"input_tokens"`
	OutputTokens         int64              `json:"output_tokens"`
	TotalTokens          int64              `json:"total_tokens"`
	IterationsCount      int                `json:"iterations_count"`
	ResourcesWithDrift   int                `json:"resources_with_drift"` // Total resources that had drift
	DriftAdoptResults    []DriftAdoptResult `json:"drift_adopt_results"`  // Results from each drift-adopt call
}

// DriftAdoptResult captures the output from a single drift-adopt tool invocation
type DriftAdoptResult struct {
	Status    string `json:"status"`          // "changes_needed", "clean", "error"
	Resources int    `json:"resources_count"` // Number of resources with drift
}

// TestStack contains information about a deployed test stack
type TestStack struct {
	Test             *pulumitest.PulumiTest
	WorkingDir       string
	BackendURL       string
	ConfigPassphrase string
	Resources        map[string]interface{} // Resource name -> resource details
}

// AWSResourceDrift provides functions for creating drift with AWS CLI
type AWSResourceDrift struct {
	Region string
}

// DriftTestConfig contains configuration for running drift adoption tests
type DriftTestConfig struct {
	ExampleDir    string
	MaxIterations int
	AWSRegion     string
}

// CreateTestStack creates and deploys a Pulumi stack from an example directory
// The example directory should have project files (Pulumi.yaml, package.json) and
// an original/ subdirectory with the program files
func CreateTestStack(t *testing.T, exampleDir string) *TestStack {
	// Convert to absolute path to avoid issues after CopyToTempDir changes working directory
	absExampleDir, err := filepath.Abs(exampleDir)
	require.NoError(t, err, "Failed to get absolute path for example directory")

	t.Logf("📁 Creating test stack from %s...", absExampleDir)
	test := pulumitest.NewPulumiTest(t, absExampleDir).CopyToTempDir(t)
	testDir := test.WorkingDir()
	t.Logf("   Working directory: %s", testDir)

	// Copy original program files into the working directory
	t.Log("   📝 Copying original program files...")
	originalDir := filepath.Join(absExampleDir, "original")
	test.UpdateSource(t, originalDir)

	t.Log("🚀 Deploying initial stack (this may take a few minutes)...")
	upResult := test.Up(t)
	t.Logf("   Deployment summary: %+v", upResult.Summary)

	// Get environment variables from the workspace
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

	// Convert outputs to resources map
	resources := make(map[string]interface{})
	for key, output := range upResult.Outputs {
		resources[key] = output.Value
	}

	return &TestStack{
		Test:             test,
		WorkingDir:       testDir,
		BackendURL:       backendURL,
		ConfigPassphrase: configPassphrase,
		Resources:        resources,
	}
}

// Destroy cleans up the test stack
func (ts *TestStack) Destroy(t *testing.T) {
	t.Log("🧹 Cleaning up: Destroying stack...")
	ts.Test.Destroy(t)
}

// VerifyDriftExists runs refresh and preview to verify drift is present
func (ts *TestStack) VerifyDriftExists(t *testing.T) int {
	t.Log("🔍 Verifying drift exists...")
	t.Log("   Running refresh to capture out-of-band changes...")
	ts.Test.Refresh(t)

	t.Log("   Running preview to detect drift...")
	previewResult := ts.Test.Preview(t)
	updateCount := previewResult.ChangeSummary[apitype.OpUpdate]
	t.Logf("   Detected %d resource(s) with drift", updateCount)

	return updateCount
}

// VerifyNoDrift runs preview to verify no drift remains
func (ts *TestStack) VerifyNoDrift(t *testing.T) (updates, creates, deletes int) {
	t.Log("🔍 Running preview to verify drift is fixed...")
	finalPreview := ts.Test.Preview(t)

	updates = finalPreview.ChangeSummary[apitype.OpUpdate]
	creates = finalPreview.ChangeSummary[apitype.OpCreate]
	deletes = finalPreview.ChangeSummary[apitype.OpDelete]

	t.Logf("   Final change summary: %d updates, %d creates, %d deletes",
		updates, creates, deletes)

	return updates, creates, deletes
}

// NewAWSResourceDrift creates a new AWS resource drift helper
func NewAWSResourceDrift(region string) *AWSResourceDrift {
	if region == "" {
		region = "us-west-2"
	}
	return &AWSResourceDrift{Region: region}
}

// GetResourceState retrieves the current state of a resource using CloudControl API
func (ard *AWSResourceDrift) GetResourceState(typeName, identifier string) (map[string]interface{}, error) {
	cmd := exec.Command("aws", "cloudcontrol", "get-resource",
		"--type-name", typeName,
		"--identifier", identifier,
		"--region", ard.Region)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to get resource state: %w\n%s", err, output)
	}

	var result struct {
		ResourceDescription struct {
			Properties string `json:"Properties"`
		} `json:"ResourceDescription"`
	}

	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("failed to parse resource state: %w", err)
	}

	var properties map[string]interface{}
	if err := json.Unmarshal([]byte(result.ResourceDescription.Properties), &properties); err != nil {
		return nil, fmt.Errorf("failed to parse properties: %w", err)
	}

	return properties, nil
}

// UpdateResourceProperties updates specific properties of a resource using CloudControl API
// patchOperations is a JSON patch document following RFC 6902
func (ard *AWSResourceDrift) UpdateResourceProperties(typeName, identifier string, patchOperations []map[string]interface{}) error {
	patchJSON, err := json.Marshal(patchOperations)
	if err != nil {
		return fmt.Errorf("failed to marshal patch operations: %w", err)
	}

	fmt.Printf("[DEBUG] CloudControl update-resource:\n")
	fmt.Printf("  Type: %s\n", typeName)
	fmt.Printf("  Identifier: %s\n", identifier)
	fmt.Printf("  Patch: %s\n", string(patchJSON))
	fmt.Printf("  Region: %s\n", ard.Region)

	cmd := exec.Command("aws", "cloudcontrol", "update-resource",
		"--type-name", typeName,
		"--identifier", identifier,
		"--patch-document", string(patchJSON),
		"--region", ard.Region)

	output, err := cmd.CombinedOutput()
	fmt.Printf("[DEBUG] CloudControl response: %s\n", string(output))
	if err != nil {
		return fmt.Errorf("failed to update resource: %w\n%s", err, output)
	}

	// Parse response to get request token
	var response struct {
		ProgressEvent struct {
			RequestToken    string `json:"RequestToken"`
			OperationStatus string `json:"OperationStatus"`
		} `json:"ProgressEvent"`
	}
	if err := json.Unmarshal(output, &response); err != nil {
		return fmt.Errorf("failed to parse CloudControl response: %w", err)
	}

	// If operation is already complete, return
	if response.ProgressEvent.OperationStatus == "SUCCESS" {
		return nil
	}

	// Wait for async operation to complete
	fmt.Printf("[DEBUG] Waiting for async operation to complete (token: %s)...\n", response.ProgressEvent.RequestToken)
	return ard.waitForOperation(response.ProgressEvent.RequestToken)
}

// waitForOperation waits for an async CloudControl operation to complete
func (ard *AWSResourceDrift) waitForOperation(requestToken string) error {
	maxWait := 60 * time.Second
	checkInterval := 2 * time.Second
	startTime := time.Now()

	for {
		if time.Since(startTime) > maxWait {
			return fmt.Errorf("timeout waiting for CloudControl operation to complete")
		}

		cmd := exec.Command("aws", "cloudcontrol", "get-resource-request-status",
			"--request-token", requestToken,
			"--region", ard.Region)

		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to get operation status: %w\n%s", err, output)
		}

		var status struct {
			ProgressEvent struct {
				OperationStatus string `json:"OperationStatus"`
				StatusMessage   string `json:"StatusMessage"`
			} `json:"ProgressEvent"`
		}
		if err := json.Unmarshal(output, &status); err != nil {
			return fmt.Errorf("failed to parse operation status: %w", err)
		}

		fmt.Printf("[DEBUG] Operation status: %s\n", status.ProgressEvent.OperationStatus)

		switch status.ProgressEvent.OperationStatus {
		case "SUCCESS":
			fmt.Printf("[DEBUG] Operation completed successfully\n")
			return nil
		case "FAILED":
			return fmt.Errorf("CloudControl operation failed: %s", status.ProgressEvent.StatusMessage)
		case "IN_PROGRESS":
			// Continue waiting
			time.Sleep(checkInterval)
		default:
			return fmt.Errorf("unknown operation status: %s", status.ProgressEvent.OperationStatus)
		}
	}
}

// CreateResourceDrift creates drift by updating arbitrary properties on a resource
func (ard *AWSResourceDrift) CreateResourceDrift(typeName, identifier string, propertyUpdates map[string]interface{}) error {
	// First get current state to determine which properties exist
	currentState, err := ard.GetResourceState(typeName, identifier)
	if err != nil {
		return fmt.Errorf("failed to get current state: %w", err)
	}

	// Build JSON patch operations to update properties
	var patchOps []map[string]interface{}
	for path, value := range propertyUpdates {
		// Use "replace" if property exists, "add" if it doesn't
		op := "add"
		if _, exists := currentState[path]; exists {
			op = "replace"
		}

		patchOps = append(patchOps, map[string]interface{}{
			"op":    op,
			"path":  "/" + path,
			"value": value,
		})
	}

	return ard.UpdateResourceProperties(typeName, identifier, patchOps)
}

// DeleteResource deletes a resource using CloudControl API
func (ard *AWSResourceDrift) DeleteResource(typeName, identifier string) error {
	cmd := exec.Command("aws", "cloudcontrol", "delete-resource",
		"--type-name", typeName,
		"--identifier", identifier,
		"--region", ard.Region)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete resource: %w\n%s", err, output)
	}

	return nil
}

// WaitForResourceProperty waits for a specific property to exist (or not exist) on a resource
// Returns nil if the condition is met within timeout, error otherwise
func (ard *AWSResourceDrift) WaitForResourceProperty(
	typeName, identifier, propertyPath string,
	expectedExists bool,
	timeout time.Duration,
) error {
	startTime := time.Now()
	checkInterval := 2 * time.Second

	for {
		// Check if we've exceeded the timeout
		if time.Since(startTime) > timeout {
			if expectedExists {
				return fmt.Errorf("timeout: property %s did not appear after %v", propertyPath, timeout)
			}
			return fmt.Errorf("timeout: property %s still exists after %v", propertyPath, timeout)
		}

		// Get current state
		state, err := ard.GetResourceState(typeName, identifier)
		if err != nil {
			return fmt.Errorf("failed to get resource state: %w", err)
		}

		// Check if property exists
		value, exists := state[propertyPath]

		// For expectedExists=true, also check if value is non-nil
		if expectedExists {
			if exists && value != nil {
				// Property exists and has a value
				return nil
			}
		} else {
			if !exists || value == nil {
				// Property doesn't exist or is nil
				return nil
			}
		}

		// Wait before checking again
		time.Sleep(checkInterval)
	}
}

// Convenience functions for common resource types using service-specific APIs
// Note: S3 uses S3 API directly instead of CloudControl for better compatibility

// CreateS3BucketTagDrift adds tags to an S3 bucket to create drift using S3 API
func (ard *AWSResourceDrift) CreateS3BucketTagDrift(bucketName string, tags map[string]string) error {
	// Build tag set JSON for S3 API
	var tagSet []map[string]string
	for key, value := range tags {
		tagSet = append(tagSet, map[string]string{
			"Key":   key,
			"Value": value,
		})
	}

	tagSetJSON, err := json.Marshal(map[string]interface{}{"TagSet": tagSet})
	if err != nil {
		return fmt.Errorf("failed to marshal tags: %w", err)
	}

	cmd := exec.Command("aws", "s3api", "put-bucket-tagging",
		"--bucket", bucketName,
		"--tagging", string(tagSetJSON),
		"--region", ard.Region)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to add tags: %w\n%s", err, output)
	}

	return nil
}

// UpdateS3BucketProperty updates a property of an S3 bucket to create drift
// For more advanced scenarios, use CreateResourceDrift with "AWS::S3::Bucket"
func (ard *AWSResourceDrift) UpdateS3BucketProperty(bucketName, property string, value interface{}) error {
	switch property {
	case "Versioning", "VersioningConfiguration":
		// Handle versioning specifically
		versioningConfig, ok := value.(map[string]interface{})
		if !ok {
			return fmt.Errorf("versioning configuration must be a map[string]interface{}")
		}
		status, ok := versioningConfig["Status"].(string)
		if !ok {
			return fmt.Errorf("versioning Status must be a string")
		}

		cmd := exec.Command("aws", "s3api", "put-bucket-versioning",
			"--bucket", bucketName,
			"--versioning-configuration", fmt.Sprintf("Status=%s", status),
			"--region", ard.Region)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to update versioning: %w\n%s", err, output)
		}
	default:
		// For other properties, try CloudControl API
		return ard.CreateResourceDrift("AWS::S3::Bucket", bucketName, map[string]interface{}{
			property: value,
		})
	}
	return nil
}

// DeleteS3Bucket deletes an S3 bucket to create drift
func (ard *AWSResourceDrift) DeleteS3Bucket(bucketName string) error {
	// First, empty the bucket using S3 API
	cmd := exec.Command("aws", "s3", "rm", fmt.Sprintf("s3://%s", bucketName),
		"--recursive",
		"--region", ard.Region)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Ignore errors if bucket is already empty
		fmt.Printf("Warning emptying bucket: %s\n", output)
	}

	// Then delete using S3 API (more reliable than CloudControl for S3)
	cmd = exec.Command("aws", "s3api", "delete-bucket",
		"--bucket", bucketName,
		"--region", ard.Region)
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete bucket: %w\n%s", err, output)
	}

	return nil
}

// RunDriftAdoptionWithClaude runs Claude to adopt drift and returns metrics
func RunDriftAdoptionWithClaude(
	ctx context.Context,
	testStack *TestStack,
	maxIterations int,
	t *testing.T,
) (*DriftTestMetrics, error) {
	// Get API key
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	require.NotEmpty(t, apiKey, "ANTHROPIC_API_KEY must be set")

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	// Read the drift-adopt skill
	_, testFile, _, _ := runtime.Caller(0)
	testDir := filepath.Dir(testFile)
	skillPath := filepath.Join(testDir, "..", "..", "skills", "drift-adopt", "SKILL.md")
	t.Logf("   📖 Reading skill from: %s", skillPath)
	skillContent, err := os.ReadFile(skillPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read skill: %w", err)
	}
	t.Logf("   ✅ Loaded skill (%d bytes)", len(skillContent))

	// Initialize metrics
	metrics := &DriftTestMetrics{}

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
	t.Logf("   🔄 Starting conversation loop (max %d iterations)", maxIterations)
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
			Tools: []anthropic.ToolUnionParam{
				{
					OfTool: &anthropic.ToolParam{
						Name:        "bash",
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
						Name:        "read_file",
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
						Name:        "write_file",
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

					// Check if this is a drift-adopt command
					if strings.Contains(cmd, "pulumi-drift-adopt") {
						metrics.DriftAdoptCallsCount++
						result, err = executeBash(testStack.WorkingDir, testStack.BackendURL, testStack.ConfigPassphrase, cmd)

						// Parse the drift-adopt output to track resources
						if err == nil {
							driftResult := parseDriftAdoptOutput(result)
							metrics.DriftAdoptResults = append(metrics.DriftAdoptResults, driftResult)
							if driftResult.Status == "changes_needed" {
								metrics.ResourcesWithDrift += driftResult.Resources
							}
						}
					} else {
						result, err = executeBash(testStack.WorkingDir, testStack.BackendURL, testStack.ConfigPassphrase, cmd)
					}

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

// parseDriftAdoptOutput parses the JSON output from pulumi-drift-adopt next
func parseDriftAdoptOutput(output string) DriftAdoptResult {
	var nextOutput struct {
		Status    string     `json:"status"`
		Resources []struct{} `json:"resources"`
	}

	if err := json.Unmarshal([]byte(output), &nextOutput); err != nil {
		return DriftAdoptResult{Status: "error", Resources: 0}
	}

	return DriftAdoptResult{
		Status:    nextOutput.Status,
		Resources: len(nextOutput.Resources),
	}
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

// CreateDriftWithProgram creates drift using a provider-agnostic approach with UpdateSource:
// 1. Export state from original deployment
// 2. UpdateSource to drifted program and deploy (modifies infrastructure)
// 3. Import original state back (resets state, creating drift)
// 4. UpdateSource back to original program (for Claude to see and fix)
// This simulates external changes without provider-specific CLI tools
func (ts *TestStack) CreateDriftWithProgram(t *testing.T, exampleDir string) error {
	// Convert to absolute path to avoid issues with relative paths
	absExampleDir, err := filepath.Abs(exampleDir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path for example directory: %w", err)
	}

	t.Log("🔧 Creating drift using drifted program...")

	// Step 1: Export current state to temp file
	// Use the test's working directory to avoid collisions when tests run in parallel
	stateFile := filepath.Join(ts.WorkingDir, "drift-test-state.json")
	t.Logf("   📤 Exporting state to: %s", stateFile)

	exportCmd := exec.Command("pulumi", "stack", "export", "--file", stateFile)
	exportCmd.Dir = ts.WorkingDir
	exportCmd.Env = append(os.Environ(),
		fmt.Sprintf("PULUMI_BACKEND_URL=%s", ts.BackendURL),
		fmt.Sprintf("PULUMI_CONFIG_PASSPHRASE=%s", ts.ConfigPassphrase),
	)

	exportOutput, err := exportCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to export state: %w\n%s", err, exportOutput)
	}
	defer os.Remove(stateFile) // Clean up state file when done

	t.Log("   ✅ State exported")

	// Step 2: UpdateSource to drifted program
	t.Log("   🔄 Updating source to drifted program...")
	driftedDir := filepath.Join(absExampleDir, "drifted")
	ts.Test.UpdateSource(t, driftedDir)
	t.Log("   ✅ Drifted program files in place")

	// Step 3: Deploy the drifted program (this modifies infrastructure)
	t.Log("   🏗️  Deploying drifted program to modify infrastructure...")
	ts.Test.Up(t)
	t.Log("   ✅ Drifted program deployed (infrastructure modified)")

	// Step 4: Import original state back (resets state, creates drift)
	t.Log("   📥 Importing original state (resetting to pre-drift state)...")

	importCmd := exec.Command("pulumi", "stack", "import", "--file", stateFile)
	importCmd.Dir = ts.WorkingDir
	importCmd.Env = append(os.Environ(),
		fmt.Sprintf("PULUMI_BACKEND_URL=%s", ts.BackendURL),
		fmt.Sprintf("PULUMI_CONFIG_PASSPHRASE=%s", ts.ConfigPassphrase),
	)

	importOutput, err := importCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to import state: %w\n%s", err, importOutput)
	}

	t.Log("   ✅ Original state imported (state reset, drift created)")

	// Step 5: UpdateSource back to original program (for Claude to see and fix)
	t.Log("   🔙 Restoring original program files...")
	originalDir := filepath.Join(absExampleDir, "original")
	ts.Test.UpdateSource(t, originalDir)
	t.Log("   ✅ Original program files restored")
	t.Log("   💡 State thinks infrastructure is at original values, but it's actually at drifted values")

	return nil
}
