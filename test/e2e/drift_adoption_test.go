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

// TestDriftAdoptionV2 tests drift adoption using provider-agnostic approach with various scenarios
func TestDriftAdoptionV2(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name          string
		exampleDir    string
		verifyOutputs func(t *testing.T, resources map[string]interface{})
		verifyCode    func(t *testing.T, codeContent string)
	}{
		{
			name:       "simple-s3",
			exampleDir: filepath.Join("..", "..", "examples", "simple-s3"),
			verifyOutputs: func(t *testing.T, resources map[string]interface{}) {
				bucketName, ok := resources["bucketName"].(string)
				require.True(t, ok, "bucketName should be a string in outputs")
				require.NotEmpty(t, bucketName, "Bucket name should not be empty")
				t.Logf("   ✅ Deployed bucket: %s", bucketName)
			},
			verifyCode: func(t *testing.T, codeContent string) {
				assert.Contains(t, codeContent, "Environment",
					"Code should now contain Environment tag")
				assert.Contains(t, codeContent, "production",
					"Code should now contain production value")
			},
		},
		{
			name:       "multi-resource",
			exampleDir: filepath.Join("..", "..", "examples", "multi-resource"),
			verifyOutputs: func(t *testing.T, resources map[string]interface{}) {
				bucketNameA, ok := resources["bucketNameA"].(string)
				require.True(t, ok, "bucketNameA should be a string in outputs")
				bucketNameB, ok := resources["bucketNameB"].(string)
				require.True(t, ok, "bucketNameB should be a string in outputs")
				bucketNameC, ok := resources["bucketNameC"].(string)
				require.True(t, ok, "bucketNameC should be a string in outputs")
				t.Logf("   ✅ Deployed buckets: %s, %s, %s", bucketNameA, bucketNameB, bucketNameC)
			},
			verifyCode: func(t *testing.T, codeContent string) {
				assert.NotContains(t, codeContent, "bucket-b",
					"Code should no longer contain bucket-b resource")
				assert.NotContains(t, codeContent, "bucketNameB",
					"Code should no longer contain bucketNameB export")
				assert.Contains(t, codeContent, "bucket-a",
					"Code should still contain bucket-a")
				assert.Contains(t, codeContent, "bucket-c",
					"Code should still contain bucket-c")
			},
		},
		{
			name:       "loop-resources",
			exampleDir: filepath.Join("..", "..", "examples", "loop-resources"),
			verifyOutputs: func(t *testing.T, resources map[string]interface{}) {
				logsBucketId, ok := resources["logsBucketId"].(string)
				require.True(t, ok, "logsBucketId should be a string in outputs")
				dataBucketId, ok := resources["dataBucketId"].(string)
				require.True(t, ok, "dataBucketId should be a string in outputs")
				backupBucketId, ok := resources["backupBucketId"].(string)
				require.True(t, ok, "backupBucketId should be a string in outputs")
				t.Logf("   ✅ Deployed 3 buckets from loop")
				t.Logf("   logs-bucket ID: %s", logsBucketId)
				t.Logf("   data-bucket ID: %s", dataBucketId)
				t.Logf("   backup-bucket ID: %s", backupBucketId)
			},
			verifyCode: func(t *testing.T, codeContent string) {
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
			},
		},
	}

	for _, tc := range testCases {
		tc := tc // capture range variable
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			config := DriftTestConfig{
				ExampleDir:    tc.exampleDir,
				MaxIterations: 10,
			}

			// Step 1: Create and deploy test stack
			testStack := CreateTestStack(t, config.ExampleDir)
			defer testStack.Destroy(t)

			// Verify initial outputs
			tc.verifyOutputs(t, testStack.Resources)

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

			tc.verifyCode(t, codeContent)
			t.Log("   ✅ Code correctly updated")

			// Step 6: Verify no drift remains
			updates, creates, deletes := testStack.VerifyNoDrift(t)
			assert.Equal(t, 0, updates, "Should have no updates (drift is fixed)")
			assert.Equal(t, 0, creates, "Should have no creates")
			assert.Equal(t, 0, deletes, "Should have no deletes")

			t.Logf("✅✅✅ Drift adoption complete for %s! ✅✅✅", tc.name)
		})
	}
}
