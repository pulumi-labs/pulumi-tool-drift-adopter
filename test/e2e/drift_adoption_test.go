//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/pulumi/providertest/pulumitest"
	"github.com/pulumi/providertest/pulumitest/opttest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDriftAdoptionWorkflow tests the complete drift adoption workflow:
// 1. Deploy a Pulumi stack with an S3 bucket
// 2. Create drift by manually modifying the bucket with AWS CLI
// 3. Use Claude SDK to invoke the drift-adopt skill
// 4. Verify Claude updated the code correctly
// 5. Run preview to confirm no remaining drift
func TestDriftAdoptionWorkflow(t *testing.T) {
	ctx := context.Background()

	// Ensure required environment variables are set
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	require.NotEmpty(t, apiKey, "ANTHROPIC_API_KEY must be set")

	awsRegion := os.Getenv("AWS_REGION")
	if awsRegion == "" {
		awsRegion = "us-west-2"
	}

	// Step 1: Create a temporary directory for the Pulumi program
	testDir := t.TempDir()

	// Create a simple TypeScript Pulumi program with an S3 bucket
	err := createPulumiProgram(testDir)
	require.NoError(t, err, "Failed to create Pulumi program")

	// Step 2: Deploy the stack using pulumitest
	t.Log("Deploying initial stack...")
	test := pulumitest.NewPulumiTest(t, testDir,
		opttest.SkipInstall(), // Assume dependencies are already installed
	)

	test.Up()
	defer test.Destroy()

	// Get the bucket name from outputs
	bucketName := test.GetOutput("bucketName").AsString()
	require.NotEmpty(t, bucketName, "Bucket name should be in outputs")
	t.Logf("Deployed bucket: %s", bucketName)

	// Step 3: Create drift by modifying the bucket with AWS CLI
	t.Log("Creating drift by adding tags to bucket...")
	err = createDrift(bucketName, awsRegion)
	require.NoError(t, err, "Failed to create drift")

	// Step 4: Verify drift exists by running preview
	// Note: The drift-adopt tool will automatically run refresh, so we don't need to do it manually
	t.Log("Verifying drift exists...")
	previewResult := test.Preview()
	assert.True(t, previewResult.ChangeSummary.Update > 0,
		"Should have updates detected (drift exists)")

	// Step 5: Use Claude SDK to run drift adoption workflow
	t.Log("Invoking Claude to fix drift...")
	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	err = runClaudeDriftAdoption(ctx, client, testDir)
	require.NoError(t, err, "Claude drift adoption failed")

	// Step 6: Verify the code was updated
	t.Log("Verifying code was updated...")
	codeContent, err := os.ReadFile(filepath.Join(testDir, "index.ts"))
	require.NoError(t, err)

	// Check that the code now includes the tags that were added via AWS CLI
	assert.Contains(t, string(codeContent), "Environment",
		"Code should now contain Environment tag")
	assert.Contains(t, string(codeContent), "production",
		"Code should now contain production value")

	// Step 7: Run preview again to verify no drift remains
	t.Log("Running preview to verify drift is fixed...")
	finalPreview := test.Preview()

	assert.Equal(t, 0, finalPreview.ChangeSummary.Update,
		"Should have no updates (drift is fixed)")
	assert.Equal(t, 0, finalPreview.ChangeSummary.Create,
		"Should have no creates")
	assert.Equal(t, 0, finalPreview.ChangeSummary.Delete,
		"Should have no deletes")

	t.Log("✅ Drift adoption complete!")
}

// createPulumiProgram creates a simple TypeScript Pulumi program
func createPulumiProgram(dir string) error {
	// Create Pulumi.yaml
	pulumiYaml := `name: drift-test
runtime: nodejs
description: Test project for drift adoption`

	if err := os.WriteFile(filepath.Join(dir, "Pulumi.yaml"), []byte(pulumiYaml), 0644); err != nil {
		return err
	}

	// Create package.json
	packageJson := `{
  "name": "drift-test",
  "main": "index.ts",
  "devDependencies": {
    "@types/node": "^20.0.0",
    "typescript": "^5.0.0"
  },
  "dependencies": {
    "@pulumi/pulumi": "^3.0.0",
    "@pulumi/aws": "^6.0.0"
  }
}`

	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(packageJson), 0644); err != nil {
		return err
	}

	// Create tsconfig.json
	tsconfigJson := `{
  "compilerOptions": {
    "strict": true,
    "outDir": "bin",
    "target": "es2016",
    "module": "commonjs",
    "moduleResolution": "node",
    "sourceMap": true,
    "experimentalDecorators": true,
    "pretty": true,
    "noFallthroughCasesInSwitch": true,
    "noImplicitReturns": true,
    "forceConsistentCasingInFileNames": true
  },
  "files": ["index.ts"]
}`

	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(tsconfigJson), 0644); err != nil {
		return err
	}

	// Create index.ts with a simple S3 bucket (no tags initially)
	indexTs := `import * as pulumi from "@pulumi/pulumi";
import * as aws from "@pulumi/aws";

// Create an S3 bucket without tags
const bucket = new aws.s3.Bucket("test-bucket", {
    bucket: "drift-test-bucket-" + pulumi.getStack(),
    forceDestroy: true,
});

export const bucketName = bucket.id;
`

	if err := os.WriteFile(filepath.Join(dir, "index.ts"), []byte(indexTs), 0644); err != nil {
		return err
	}

	// Run npm install
	cmd := exec.Command("npm", "install")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("npm install failed: %w\n%s", err, output)
	}

	return nil
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
func runClaudeDriftAdoption(ctx context.Context, client *anthropic.Client, projectDir string) error {
	// Read the drift-adopt skill
	skillPath := filepath.Join(projectDir, "..", "..", "skills", "drift-adopt.md")
	skillContent, err := os.ReadFile(skillPath)
	if err != nil {
		return fmt.Errorf("failed to read skill: %w", err)
	}

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
	for i := 0; i < maxIterations; i++ {
		// Call Claude
		response, err := client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.F(anthropic.ModelClaude_3_5_Sonnet_20241022),
			MaxTokens: anthropic.Int(4096),
			System: anthropic.F([]anthropic.TextBlockParam{
				anthropic.NewTextBlock(systemMsg),
			}),
			Messages: anthropic.F(messages),
			Tools: anthropic.F([]anthropic.ToolParam{
				// Tool for running bash commands
				{
					Name:        anthropic.F("bash"),
					Description: anthropic.F("Execute bash commands in the project directory"),
					InputSchema: anthropic.F(map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"command": map[string]interface{}{
								"type":        "string",
								"description": "The bash command to execute",
							},
						},
						"required": []string{"command"},
					}),
				},
				// Tool for reading files
				{
					Name:        anthropic.F("read_file"),
					Description: anthropic.F("Read the contents of a file"),
					InputSchema: anthropic.F(map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"path": map[string]interface{}{
								"type":        "string",
								"description": "The file path to read",
							},
						},
						"required": []string{"path"},
					}),
				},
				// Tool for writing files
				{
					Name:        anthropic.F("write_file"),
					Description: anthropic.F("Write contents to a file"),
					InputSchema: anthropic.F(map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"path": map[string]interface{}{
								"type":        "string",
								"description": "The file path to write",
							},
							"content": map[string]interface{}{
								"type":        "string",
								"description": "The content to write",
							},
						},
						"required": []string{"path", "content"},
					}),
				},
			}),
		})

		if err != nil {
			return fmt.Errorf("Claude API error: %w", err)
		}

		// Check stop reason
		if response.StopReason == "end_turn" {
			// Claude is done
			break
		}

		// Process tool uses
		if response.StopReason == "tool_use" {
			toolResults := []anthropic.MessageParam{}

			for _, block := range response.Content {
				if block.Type != "tool_use" {
					continue
				}

				toolUse := block.AsUnion().ToolUse
				toolName := toolUse.Name
				toolInput := toolUse.Input

				var result string
				var err error

				switch toolName {
				case "bash":
					cmd := toolInput["command"].(string)
					result, err = executeBash(projectDir, cmd)
				case "read_file":
					path := toolInput["path"].(string)
					result, err = readFile(projectDir, path)
				case "write_file":
					path := toolInput["path"].(string)
					content := toolInput["content"].(string)
					err = writeFile(projectDir, path, content)
					if err == nil {
						result = "File written successfully"
					}
				}

				// Create tool result
				if err != nil {
					result = fmt.Sprintf("Error: %v", err)
				}

				toolResults = append(toolResults, anthropic.NewToolResultBlock(
					toolUse.ID,
					result,
					false,
				))
			}

			// Add assistant message and tool results to conversation
			messages = append(messages, anthropic.NewAssistantMessage(response.Content...))
			messages = append(messages, anthropic.NewUserMessage(toolResults...))

			continue
		}

		// Unknown stop reason
		return fmt.Errorf("unexpected stop reason: %s", response.StopReason)
	}

	return nil
}

// executeBash executes a bash command in the project directory
func executeBash(projectDir, command string) (string, error) {
	cmd := exec.Command("bash", "-c", command)
	cmd.Dir = projectDir
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
