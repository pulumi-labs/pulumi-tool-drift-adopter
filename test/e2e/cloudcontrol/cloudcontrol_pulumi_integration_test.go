//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCloudControlWithPulumiIntegration tests the EXACT scenario from the full E2E test
// to understand why CloudControl didn't work but S3 API did
func TestCloudControlWithPulumiIntegration(t *testing.T) {
	// Step 1: Create and deploy test stack (same as full test)
	exampleDir := filepath.Join("..", "..", "examples", "simple-s3")
	testStack := CreateTestStack(t, exampleDir)
	defer testStack.Destroy(t)

	bucketName, ok := testStack.Resources["bucketName"].(string)
	require.True(t, ok, "bucketName should be a string")
	require.NotEmpty(t, bucketName, "Bucket name should not be empty")
	t.Logf("✅ Deployed bucket: %s", bucketName)

	// Step 2A: Create drift using CloudControl API
	t.Log("🔧 Creating drift with CloudControl API...")
	driftHelper := NewAWSResourceDrift("us-west-2")

	// First, check current state
	t.Log("Checking current state with CloudControl...")
	state, err := driftHelper.GetResourceState("AWS::S3::Bucket", bucketName)
	if err != nil {
		t.Logf("❌ GetResourceState failed: %v", err)
		t.Fatal("Cannot proceed without being able to read state")
	}
	t.Logf("Current state Tags field: %+v", state["Tags"])
	t.Logf("Current state BucketName: %+v", state["BucketName"])
	t.Logf("Current state Arn: %+v", state["Arn"])
	t.Logf("Full state keys: %+v", getMapKeys(state))

	// Create drift with CloudControl
	err = driftHelper.CreateResourceDrift("AWS::S3::Bucket", bucketName, map[string]interface{}{
		"Tags": []map[string]string{
			{"Key": "Environment", "Value": "production"},
			{"Key": "ManagedBy", "Value": "cloudcontrol"},
		},
	})

	if err != nil {
		t.Logf("❌ CloudControl CreateResourceDrift failed: %v", err)
		t.Log("This explains why the full test failed!")
		t.FailNow()
	}
	t.Log("✅ CloudControl CreateResourceDrift succeeded")

	// Wait for tags to appear (eventual consistency test)
	t.Log("⏳ Waiting for Tags property to appear (testing eventual consistency)...")
	err = driftHelper.WaitForResourceProperty("AWS::S3::Bucket", bucketName, "Tags", true, 30*time.Second)
	if err != nil {
		t.Logf("❌ Tags did not appear after 30 seconds: %v", err)
		t.Log("This confirms CloudControl doesn't work with Pulumi-created resources!")
		t.Log("It's NOT an eventual consistency issue - CloudControl truly fails.")
	} else {
		t.Log("✅ Tags appeared after waiting (eventual consistency confirmed)")
	}

	// Verify tags were actually added
	t.Log("Verifying tags with CloudControl GetResourceState...")
	state, err = driftHelper.GetResourceState("AWS::S3::Bucket", bucketName)
	require.NoError(t, err)
	t.Logf("State after drift: %+v", state["Tags"])

	// Also verify with S3 API
	t.Log("Verifying tags with S3 API...")
	err = verifyS3Tags(bucketName, "us-west-2", []string{"Environment", "ManagedBy"})
	require.NoError(t, err, "Tags should be readable via S3 API")
	t.Log("✅ Tags verified with S3 API")

	// Step 3: Verify drift is detected by Pulumi (same as full test)
	t.Log("🔍 Running Pulumi refresh to capture out-of-band changes...")
	testStack.Test.Refresh(t)

	t.Log("🔍 Running Pulumi preview to detect drift...")
	previewResult := testStack.Test.Preview(t)
	updateCount := previewResult.ChangeSummary[apitype.OpUpdate]

	t.Logf("Detected %d resource(s) with drift", updateCount)

	if updateCount == 0 {
		t.Log("❌ PROBLEM FOUND: Pulumi did not detect the drift created by CloudControl!")
		t.Log("This explains why the full E2E test failed with CloudControl.")
		t.Log("")
		t.Log("Possible reasons:")
		t.Log("1. Pulumi refresh doesn't see CloudControl changes immediately")
		t.Log("2. There's a CloudControl-specific issue with S3 bucket resources")
		t.Log("3. The way CloudControl updates resources differs from direct API calls")
	} else {
		t.Log("✅ SUCCESS: Pulumi detected the CloudControl changes!")
		t.Log("This means CloudControl should work in the full test.")
	}

	assert.True(t, updateCount > 0, "Should have updates detected (drift exists)")
}

// TestS3APIWithPulumiIntegration tests the same but with S3 API (control test)
func TestS3APIWithPulumiIntegration(t *testing.T) {
	// Step 1: Create and deploy test stack
	exampleDir := filepath.Join("..", "..", "examples", "simple-s3")
	testStack := CreateTestStack(t, exampleDir)
	defer testStack.Destroy(t)

	bucketName, ok := testStack.Resources["bucketName"].(string)
	require.True(t, ok)
	require.NotEmpty(t, bucketName)
	t.Logf("✅ Deployed bucket: %s", bucketName)

	// Step 2B: Create drift using S3 API (for comparison)
	t.Log("🔧 Creating drift with S3 API...")
	driftHelper := NewAWSResourceDrift("us-west-2")

	err := driftHelper.CreateS3BucketTagDrift(bucketName, map[string]string{
		"Environment": "production",
		"ManagedBy":   "s3api",
	})
	require.NoError(t, err)
	t.Log("✅ S3 API CreateS3BucketTagDrift succeeded")

	// Step 3: Verify drift is detected by Pulumi
	t.Log("🔍 Running Pulumi refresh...")
	testStack.Test.Refresh(t)

	t.Log("🔍 Running Pulumi preview...")
	previewResult := testStack.Test.Preview(t)
	updateCount := previewResult.ChangeSummary[apitype.OpUpdate]

	t.Logf("Detected %d resource(s) with drift", updateCount)

	if updateCount == 0 {
		t.Log("❌ Even S3 API didn't work! Something else is wrong.")
	} else {
		t.Log("✅ S3 API drift was detected by Pulumi (as expected)")
	}

	assert.True(t, updateCount > 0, "Should have updates detected")
}

// getMapKeys returns all keys from a map
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// verifyS3Tags checks if specific tags exist on an S3 bucket
func verifyS3Tags(bucketName, region string, expectedKeys []string) error {
	cmd := exec.Command("aws", "s3api", "get-bucket-tagging",
		"--bucket", bucketName,
		"--region", region)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to get tags: %w\n%s", err, output)
	}

	var tagResult struct {
		TagSet []map[string]string `json:"TagSet"`
	}
	if err := json.Unmarshal(output, &tagResult); err != nil {
		return fmt.Errorf("failed to parse tags: %w", err)
	}

	// Check if expected keys exist
	foundKeys := make(map[string]bool)
	for _, tag := range tagResult.TagSet {
		foundKeys[tag["Key"]] = true
	}

	for _, key := range expectedKeys {
		if !foundKeys[key] {
			return fmt.Errorf("expected tag key %s not found", key)
		}
	}

	return nil
}
