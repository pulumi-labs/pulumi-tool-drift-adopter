//go:build e2e

// Package e2e contains end-to-end tests for the drift adoption workflow.
package e2e

import (
	"context"
	"path/filepath"
	"testing"

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

// TestDriftAdoptionResourceDeletion tests drift adoption when a resource is deleted
func TestDriftAdoptionResourceDeletion(t *testing.T) {
	ctx := context.Background()
	config := DriftTestConfig{
		ExampleDir:    filepath.Join("..", "..", "examples", "multi-resource"),
		MaxIterations: 10,
		AWSRegion:     "us-west-2",
	}

	// Step 1: Create and deploy test stack with multiple buckets
	testStack := CreateTestStack(t, config.ExampleDir)
	defer testStack.Destroy(t)

	// Get all bucket names from outputs
	bucketNameA, ok := testStack.Resources["bucketNameA"].(string)
	require.True(t, ok, "bucketNameA should be a string in outputs")
	bucketNameB, ok := testStack.Resources["bucketNameB"].(string)
	require.True(t, ok, "bucketNameB should be a string in outputs")
	bucketNameC, ok := testStack.Resources["bucketNameC"].(string)
	require.True(t, ok, "bucketNameC should be a string in outputs")
	t.Logf("   ✅ Deployed buckets: %s, %s, %s", bucketNameA, bucketNameB, bucketNameC)

	// Step 2: Create drift by deleting bucket B
	t.Log("🔧 Step 2: Creating drift by deleting bucket-b...")
	driftHelper := NewAWSResourceDrift(config.AWSRegion)
	err := driftHelper.DeleteS3Bucket(bucketNameB)
	require.NoError(t, err, "Failed to delete bucket-b")
	t.Logf("   ✅ Deleted bucket: %s", bucketNameB)

	// Step 3: Verify drift exists (should show a create)
	// When a resource is deleted externally, after refresh Pulumi sees:
	// - Code wants the resource
	// - State doesn't have it
	// - Preview shows it as a CREATE operation
	t.Log("🔍 Step 3: Verifying drift exists...")
	testStack.Test.Refresh(t)
	preview := testStack.Test.Preview(t)

	createCount := preview.ChangeSummary[apitype.OpCreate]
	assert.True(t, createCount > 0, "Should have create operations (drift exists)")
	t.Logf("   ✅ Drift detected: %d resource(s) to create", createCount)

	// Step 4: Use Claude to fix drift
	t.Log("🤖 Step 4: Invoking Claude to fix drift (this may take several minutes)...")
	metrics, err := RunDriftAdoptionWithClaude(ctx, testStack, config.MaxIterations, t)
	require.NoError(t, err, "Claude drift adoption failed")
	t.Log("   ✅ Claude completed drift adoption")

	// Log metrics
	t.Log("📊 Drift Adoption Metrics:")
	t.Logf("   Total iterations: %d", metrics.IterationsCount)
	t.Logf("   Total tool calls: %d", metrics.ToolCallsCount)
	t.Logf("   Token usage: %d input, %d output, %d total",
		metrics.InputTokens, metrics.OutputTokens, metrics.TotalTokens)

	// Step 5: Verify the code was updated to remove bucket B
	t.Log("✅ Step 5: Verifying code was updated...")
	codeContent, err := readFile(testStack.WorkingDir, "index.ts")
	require.NoError(t, err)

	// Check that bucket B is no longer in the code
	assert.NotContains(t, codeContent, "bucket-b",
		"Code should no longer contain bucket-b resource")
	assert.NotContains(t, codeContent, "bucketNameB",
		"Code should no longer contain bucketNameB export")

	// Check that other buckets are still there
	assert.Contains(t, codeContent, "bucket-a",
		"Code should still contain bucket-a")
	assert.Contains(t, codeContent, "bucket-c",
		"Code should still contain bucket-c")
	t.Log("   ✅ Code correctly updated - bucket-b removed")

	// Step 6: Verify no drift remains
	updates, creates, deletes := testStack.VerifyNoDrift(t)
	assert.Equal(t, 0, updates, "Should have no updates")
	assert.Equal(t, 0, creates, "Should have no creates")
	assert.Equal(t, 0, deletes, "Should have no deletes (drift is fixed)")

	t.Log("✅✅✅ Resource deletion drift adoption complete! ✅✅✅")
}

// TestDriftAdoptionLoopResourceDeletion tests drift adoption when a resource created in a loop is deleted
func TestDriftAdoptionLoopResourceDeletion(t *testing.T) {
	ctx := context.Background()
	config := DriftTestConfig{
		ExampleDir:    filepath.Join("..", "..", "examples", "loop-resources"),
		MaxIterations: 10,
		AWSRegion:     "us-west-2",
	}

	// Step 1: Create and deploy test stack with loop-created buckets
	testStack := CreateTestStack(t, config.ExampleDir)
	defer testStack.Destroy(t)

	// Get bucket IDs from individual outputs
	logsBucketId, ok := testStack.Resources["logsBucketId"].(string)
	require.True(t, ok, "logsBucketId should be a string in outputs")
	dataBucketId, ok := testStack.Resources["dataBucketId"].(string)
	require.True(t, ok, "dataBucketId should be a string in outputs")
	backupBucketId, ok := testStack.Resources["backupBucketId"].(string)
	require.True(t, ok, "backupBucketId should be a string in outputs")

	t.Logf("   ✅ Deployed 3 buckets from loop")
	t.Logf("   logs-bucket ID: %s", logsBucketId)
	t.Logf("   data-bucket ID: %s", dataBucketId)
	t.Logf("   backup-bucket ID: %s", backupBucketId)

	// Step 2: Create drift by deleting the middle bucket (data-bucket)
	t.Log("🔧 Step 2: Creating drift by deleting data-bucket (from loop)...")
	driftHelper := NewAWSResourceDrift(config.AWSRegion)
	err := driftHelper.DeleteS3Bucket(dataBucketId)
	require.NoError(t, err, "Failed to delete data-bucket")
	t.Logf("   ✅ Deleted bucket: %s", dataBucketId)

	// Step 3: Verify drift exists (should show a create)
	// When a resource is deleted externally, after refresh Pulumi sees:
	// - Code wants the resource
	// - State doesn't have it
	// - Preview shows it as a CREATE operation
	t.Log("🔍 Step 3: Verifying drift exists...")
	testStack.Test.Refresh(t)
	preview := testStack.Test.Preview(t)

	createCount := preview.ChangeSummary[apitype.OpCreate]
	assert.True(t, createCount > 0, "Should have create operations (drift exists)")
	t.Logf("   ✅ Drift detected: %d resource(s) to create", createCount)

	// Step 4: Use Claude to fix drift
	t.Log("🤖 Step 4: Invoking Claude to fix drift (this may take several minutes)...")
	metrics, err := RunDriftAdoptionWithClaude(ctx, testStack, config.MaxIterations, t)
	require.NoError(t, err, "Claude drift adoption failed")
	t.Log("   ✅ Claude completed drift adoption")

	// Log metrics
	t.Log("📊 Drift Adoption Metrics:")
	t.Logf("   Total iterations: %d", metrics.IterationsCount)
	t.Logf("   Total tool calls: %d", metrics.ToolCallsCount)
	t.Logf("   Token usage: %d input, %d output, %d total",
		metrics.InputTokens, metrics.OutputTokens, metrics.TotalTokens)

	// Step 5: Verify the code was updated to remove data-bucket from the array
	t.Log("✅ Step 5: Verifying code was updated...")
	codeContent, err := readFile(testStack.WorkingDir, "index.ts")
	require.NoError(t, err)

	// Check that data-bucket is no longer in the bucketNames array
	assert.Contains(t, codeContent, `bucketNames = ["logs-bucket", "backup-bucket"]`,
		"Code should have updated bucketNames array without data-bucket")

	// Check that dataBucketId export is removed
	assert.NotContains(t, codeContent, "dataBucketId",
		"Code should no longer contain dataBucketId export")

	// Check that other bucket exports are still there
	assert.Contains(t, codeContent, "logsBucketId",
		"Code should still contain logsBucketId export")
	assert.Contains(t, codeContent, "backupBucketId",
		"Code should still contain backupBucketId export")

	// Check that the loop structure is maintained
	assert.Contains(t, codeContent, "bucketNames",
		"Code should still have bucketNames array")
	assert.Contains(t, codeContent, "for (const name of bucketNames)",
		"Code should still have the loop structure")

	t.Log("   ✅ Code correctly updated - data-bucket removed from array")

	// Step 6: Verify no drift remains
	updates, creates, deletes := testStack.VerifyNoDrift(t)
	assert.Equal(t, 0, updates, "Should have no updates")
	assert.Equal(t, 0, creates, "Should have no creates")
	assert.Equal(t, 0, deletes, "Should have no deletes (drift is fixed)")

	t.Log("✅✅✅ Loop resource deletion drift adoption complete! ✅✅✅")
}

// TestDriftAdoptionWorkflowV2 tests drift adoption using provider-agnostic approach
func TestDriftAdoptionWorkflowV2(t *testing.T) {
	ctx := context.Background()
	config := DriftTestConfig{
		ExampleDir:    filepath.Join("..", "..", "examples", "simple-s3"),
		MaxIterations: 10,
	}

	// Step 1: Create and deploy test stack
	testStack := CreateTestStack(t, config.ExampleDir)
	defer testStack.Destroy(t)

	bucketName, ok := testStack.Resources["bucketName"].(string)
	require.True(t, ok, "bucketName should be a string in outputs")
	require.NotEmpty(t, bucketName, "Bucket name should not be empty")
	t.Logf("   ✅ Deployed bucket: %s", bucketName)

	// Step 2: Create drift using drifted program (provider-agnostic)
	t.Log("🔧 Step 2: Creating drift using drifted program...")
	err := testStack.CreateDriftWithProgram(t, config.ExampleDir)
	require.NoError(t, err, "Failed to create drift with drifted program")

	// Step 3: Verify drift exists
	updateCount := testStack.VerifyDriftExists(t)
	assert.True(t, updateCount > 0, "Should have updates detected (drift exists)")

	// Step 4: Use Claude to fix drift
	t.Log("🤖 Step 4: Invoking Claude to fix drift (this may take several minutes)...")
	metrics, err := RunDriftAdoptionWithClaude(ctx, testStack, config.MaxIterations, t)
	require.NoError(t, err, "Claude drift adoption failed")
	t.Log("   ✅ Claude completed drift adoption")

	// Log metrics
	t.Log("📊 Drift Adoption Metrics:")
	t.Logf("   Total iterations: %d", metrics.IterationsCount)
	t.Logf("   Total tool calls: %d", metrics.ToolCallsCount)
	t.Logf("   Token usage: %d input, %d output, %d total",
		metrics.InputTokens, metrics.OutputTokens, metrics.TotalTokens)

	// Step 5: Verify the code was updated
	t.Log("✅ Step 5: Verifying code was updated...")
	codeContent, err := readFile(testStack.WorkingDir, "index.ts")
	require.NoError(t, err)

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

	t.Log("✅✅✅ Provider-agnostic drift adoption workflow complete! ✅✅✅")
}

// TestDriftAdoptionResourceDeletionV2 tests resource deletion drift using provider-agnostic approach
func TestDriftAdoptionResourceDeletionV2(t *testing.T) {
	ctx := context.Background()
	config := DriftTestConfig{
		ExampleDir:    filepath.Join("..", "..", "examples", "multi-resource"),
		MaxIterations: 10,
	}

	// Step 1: Create and deploy test stack
	testStack := CreateTestStack(t, config.ExampleDir)
	defer testStack.Destroy(t)

	bucketNameA, ok := testStack.Resources["bucketNameA"].(string)
	require.True(t, ok, "bucketNameA should be a string in outputs")
	bucketNameB, ok := testStack.Resources["bucketNameB"].(string)
	require.True(t, ok, "bucketNameB should be a string in outputs")
	bucketNameC, ok := testStack.Resources["bucketNameC"].(string)
	require.True(t, ok, "bucketNameC should be a string in outputs")
	t.Logf("   ✅ Deployed buckets: %s, %s, %s", bucketNameA, bucketNameB, bucketNameC)

	// Step 2: Create drift using drifted program (deletes bucket-b)
	t.Log("🔧 Step 2: Creating drift using drifted program (bucket-b will be deleted)...")
	err := testStack.CreateDriftWithProgram(t, config.ExampleDir)
	require.NoError(t, err, "Failed to create drift with drifted program")

	// Step 3: Verify drift exists (should show a create for missing bucket-b)
	t.Log("🔍 Step 3: Verifying drift exists...")
	testStack.Test.Refresh(t)
	preview := testStack.Test.Preview(t)

	createCount := preview.ChangeSummary[apitype.OpCreate]
	assert.True(t, createCount > 0, "Should have create operations (drift exists)")
	t.Logf("   ✅ Drift detected: %d resource(s) to create", createCount)

	// Step 4: Use Claude to fix drift
	t.Log("🤖 Step 4: Invoking Claude to fix drift (this may take several minutes)...")
	metrics, err := RunDriftAdoptionWithClaude(ctx, testStack, config.MaxIterations, t)
	require.NoError(t, err, "Claude drift adoption failed")
	t.Log("   ✅ Claude completed drift adoption")

	// Log metrics
	t.Log("📊 Drift Adoption Metrics:")
	t.Logf("   Total iterations: %d", metrics.IterationsCount)
	t.Logf("   Total tool calls: %d", metrics.ToolCallsCount)
	t.Logf("   Token usage: %d input, %d output, %d total",
		metrics.InputTokens, metrics.OutputTokens, metrics.TotalTokens)

	// Step 5: Verify the code was updated
	t.Log("✅ Step 5: Verifying code was updated...")
	codeContent, err := readFile(testStack.WorkingDir, "index.ts")
	require.NoError(t, err)

	assert.NotContains(t, codeContent, "bucket-b",
		"Code should no longer contain bucket-b resource")
	assert.NotContains(t, codeContent, "bucketNameB",
		"Code should no longer contain bucketNameB export")
	assert.Contains(t, codeContent, "bucket-a",
		"Code should still contain bucket-a")
	assert.Contains(t, codeContent, "bucket-c",
		"Code should still contain bucket-c")
	t.Log("   ✅ Code correctly updated - bucket-b removed")

	// Step 6: Verify no drift remains
	updates, creates, deletes := testStack.VerifyNoDrift(t)
	assert.Equal(t, 0, updates, "Should have no updates")
	assert.Equal(t, 0, creates, "Should have no creates")
	assert.Equal(t, 0, deletes, "Should have no deletes (drift is fixed)")

	t.Log("✅✅✅ Provider-agnostic resource deletion drift adoption complete! ✅✅✅")
}

// TestDriftAdoptionLoopResourceDeletionV2 tests loop resource deletion drift using provider-agnostic approach
func TestDriftAdoptionLoopResourceDeletionV2(t *testing.T) {
	ctx := context.Background()
	config := DriftTestConfig{
		ExampleDir:    filepath.Join("..", "..", "examples", "loop-resources"),
		MaxIterations: 10,
	}

	// Step 1: Create and deploy test stack
	testStack := CreateTestStack(t, config.ExampleDir)
	defer testStack.Destroy(t)

	logsBucketId, ok := testStack.Resources["logsBucketId"].(string)
	require.True(t, ok, "logsBucketId should be a string in outputs")
	dataBucketId, ok := testStack.Resources["dataBucketId"].(string)
	require.True(t, ok, "dataBucketId should be a string in outputs")
	backupBucketId, ok := testStack.Resources["backupBucketId"].(string)
	require.True(t, ok, "backupBucketId should be a string in outputs")

	t.Logf("   ✅ Deployed 3 buckets from loop")
	t.Logf("   logs-bucket ID: %s", logsBucketId)
	t.Logf("   data-bucket ID: %s", dataBucketId)
	t.Logf("   backup-bucket ID: %s", backupBucketId)

	// Step 2: Create drift using drifted program (removes data-bucket from array)
	t.Log("🔧 Step 2: Creating drift using drifted program (data-bucket will be deleted)...")
	err := testStack.CreateDriftWithProgram(t, config.ExampleDir)
	require.NoError(t, err, "Failed to create drift with drifted program")

	// Step 3: Verify drift exists (should show a create for missing data-bucket)
	t.Log("🔍 Step 3: Verifying drift exists...")
	testStack.Test.Refresh(t)
	preview := testStack.Test.Preview(t)

	createCount := preview.ChangeSummary[apitype.OpCreate]
	assert.True(t, createCount > 0, "Should have create operations (drift exists)")
	t.Logf("   ✅ Drift detected: %d resource(s) to create", createCount)

	// Step 4: Use Claude to fix drift
	t.Log("🤖 Step 4: Invoking Claude to fix drift (this may take several minutes)...")
	metrics, err := RunDriftAdoptionWithClaude(ctx, testStack, config.MaxIterations, t)
	require.NoError(t, err, "Claude drift adoption failed")
	t.Log("   ✅ Claude completed drift adoption")

	// Log metrics
	t.Log("📊 Drift Adoption Metrics:")
	t.Logf("   Total iterations: %d", metrics.IterationsCount)
	t.Logf("   Total tool calls: %d", metrics.ToolCallsCount)
	t.Logf("   Token usage: %d input, %d output, %d total",
		metrics.InputTokens, metrics.OutputTokens, metrics.TotalTokens)

	// Step 5: Verify the code was updated
	t.Log("✅ Step 5: Verifying code was updated...")
	codeContent, err := readFile(testStack.WorkingDir, "index.ts")
	require.NoError(t, err)

	assert.Contains(t, codeContent, `bucketNames = ["logs-bucket", "backup-bucket"]`,
		"Code should have updated bucketNames array without data-bucket")
	assert.NotContains(t, codeContent, "dataBucketId",
		"Code should no longer contain dataBucketId export")
	assert.Contains(t, codeContent, "logsBucketId",
		"Code should still contain logsBucketId export")
	assert.Contains(t, codeContent, "backupBucketId",
		"Code should still contain backupBucketId export")
	assert.Contains(t, codeContent, "bucketNames",
		"Code should still have bucketNames array")
	assert.Contains(t, codeContent, "for (const name of bucketNames)",
		"Code should still have the loop structure")

	t.Log("   ✅ Code correctly updated - data-bucket removed from array")

	// Step 6: Verify no drift remains
	updates, creates, deletes := testStack.VerifyNoDrift(t)
	assert.Equal(t, 0, updates, "Should have no updates")
	assert.Equal(t, 0, creates, "Should have no creates")
	assert.Equal(t, 0, deletes, "Should have no deletes (drift is fixed)")

	t.Log("✅✅✅ Provider-agnostic loop resource deletion drift adoption complete! ✅✅✅")
}
