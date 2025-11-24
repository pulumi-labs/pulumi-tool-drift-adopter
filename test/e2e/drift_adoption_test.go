//go:build e2e

package e2e

import (
	"context"
	"path/filepath"
	"testing"

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
	testDriftAdoptionWithConfig(t, DriftTestConfig{
		ExampleDir:    filepath.Join("..", "..", "examples", "simple-s3"),
		MaxIterations: 10,
		AWSRegion:     "us-west-2",
	})
}

// testDriftAdoptionWithConfig runs a drift adoption test with the given configuration
func testDriftAdoptionWithConfig(t *testing.T, config DriftTestConfig) {
	ctx := context.Background()

	// Step 1: Create and deploy test stack
	testStack := CreateTestStack(t, config.ExampleDir)
	defer testStack.Destroy(t)

	// Get the bucket name from outputs
	bucketName, ok := testStack.Resources["bucketName"].(string)
	require.True(t, ok, "bucketName should be a string in outputs")
	require.NotEmpty(t, bucketName, "Bucket name should not be empty")
	t.Logf("   ✅ Deployed bucket: %s", bucketName)

	// Step 2: Create drift by adding tags to the bucket
	t.Log("🔧 Step 2: Creating drift by adding tags to bucket...")
	t.Logf("   Adding tags: Environment=production, ManagedBy=manual")
	driftHelper := NewAWSResourceDrift(config.AWSRegion)
	err := driftHelper.CreateS3BucketTagDrift(bucketName, map[string]string{
		"Environment": "production",
		"ManagedBy":   "manual",
	})
	require.NoError(t, err, "Failed to create drift")
	t.Log("   ✅ Tags added successfully")

	// Step 3: Verify drift exists
	updateCount := testStack.VerifyDriftExists(t)
	assert.True(t, updateCount > 0, "Should have updates detected (drift exists)")

	// Step 4: Use Claude to fix drift and collect metrics
	t.Log("🤖 Step 4: Invoking Claude to fix drift (this may take several minutes)...")
	metrics, err := RunDriftAdoptionWithClaude(ctx, testStack, config.MaxIterations, t)
	require.NoError(t, err, "Claude drift adoption failed")
	t.Log("   ✅ Claude completed drift adoption")

	// Log metrics
	t.Log("📊 Drift Adoption Metrics:")
	t.Logf("   Total iterations: %d", metrics.IterationsCount)
	t.Logf("   Total tool calls: %d", metrics.ToolCallsCount)
	t.Logf("   - Bash calls: %d", metrics.BashCallsCount)
	t.Logf("   - Read file calls: %d", metrics.ReadFileCallsCount)
	t.Logf("   - Write file calls: %d", metrics.WriteFileCallsCount)
	t.Logf("   - Drift adopt calls: %d", metrics.DriftAdoptCallsCount)
	t.Logf("   Token usage: %d input, %d output, %d total",
		metrics.InputTokens, metrics.OutputTokens, metrics.TotalTokens)
	t.Logf("   Resources with drift: %d", metrics.ResourcesWithDrift)

	// Step 5: Verify the code was updated
	t.Log("✅ Step 5: Verifying code was updated...")
	codeContent, err := readFile(testStack.WorkingDir, "index.ts")
	require.NoError(t, err)

	// Check that the code now includes the tags that were added via AWS CLI
	assert.Contains(t, codeContent, "Environment",
		"Code should now contain Environment tag")
	assert.Contains(t, codeContent, "production",
		"Code should now contain production value")
	t.Log("   ✅ Code contains expected tags")

	// Step 6: Verify no drift remains
	updates, creates, deletes := testStack.VerifyNoDrift(t)
	assert.Equal(t, 0, updates, "Should have no updates (drift is fixed)")
	assert.Equal(t, 0, creates, "Should have no creates")
	assert.Equal(t, 0, deletes, "Should have no deletes")

	t.Log("✅✅✅ Drift adoption workflow complete! ✅✅✅")
}
