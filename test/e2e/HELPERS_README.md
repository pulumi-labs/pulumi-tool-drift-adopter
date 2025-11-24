# E2E Test Helpers for Drift Adoption

This document describes the reusable helper functions for testing drift adoption workflows with Claude.

## Overview

The test framework has been refactored into reusable components in `drift_helpers.go` that allow you to:
- Create and manage test stacks
- Simulate drift with AWS CLI commands
- Run Claude to adopt drift with full metrics tracking
- Verify drift is resolved

## Architecture

### Key Components

1. **DriftTestMetrics** - Tracks all work done by the LLM
2. **TestStack** - Manages a deployed test stack
3. **AWSResourceDrift** - Helpers for creating drift with AWS CLI
4. **DriftTestConfig** - Configuration for test scenarios
5. **Helper Functions** - Reusable functions for common operations

## DriftTestMetrics

Tracks comprehensive metrics for benchmarking:

```go
type DriftTestMetrics struct {
    ToolCallsCount       int   `json:"tool_calls_count"`        // Total tool calls
    BashCallsCount       int   `json:"bash_calls_count"`        // Bash tool calls
    ReadFileCallsCount   int   `json:"read_file_calls_count"`   // Read file tool calls
    WriteFileCallsCount  int   `json:"write_file_calls_count"`  // Write file tool calls
    DriftAdoptCallsCount int   `json:"drift_adopt_calls_count"` // pulumi-drift-adopt calls
    InputTokens          int64 `json:"input_tokens"`
    OutputTokens         int64 `json:"output_tokens"`
    TotalTokens          int64 `json:"total_tokens"`
    IterationsCount      int   `json:"iterations_count"`
    ResourcesWithDrift   int   `json:"resources_with_drift"`    // Total resources that had drift
    DriftAdoptResults    []DriftAdoptResult `json:"drift_adopt_results"` // Results from each call
}
```

### Example Metrics from a Test Run

```
📊 Drift Adoption Metrics:
   Total iterations: 6
   Total tool calls: 5
   - Bash calls: 3
   - Read file calls: 1
   - Write file calls: 1
   - Drift adopt calls: 2
   Token usage: 16454 input, 927 output, 17381 total
   Resources with drift: 1
```

## Creating a New Test

### Basic Pattern

```go
func TestMyDriftScenario(t *testing.T) {
    testDriftAdoptionWithConfig(t, DriftTestConfig{
        ExampleDir:    filepath.Join("..", "..", "examples", "my-example"),
        MaxIterations: 10,
        AWSRegion:     "us-west-2",
    })
}
```

### Advanced Pattern with Custom Drift

```go
func TestComplexDriftScenario(t *testing.T) {
    ctx := context.Background()

    // Step 1: Create and deploy test stack
    testStack := CreateTestStack(t, filepath.Join("..", "..", "examples", "vpc-example"))
    defer testStack.Destroy(t)

    // Step 2: Get resource info from outputs
    vpcID, ok := testStack.Resources["vpcId"].(string)
    require.True(t, ok)

    // Step 3: Create complex drift
    driftHelper := NewAWSResourceDrift("us-west-2")

    // Add tags to VPC
    err := driftHelper.CreateVPCTagDrift(vpcID, map[string]string{
        "Environment": "production",
        "CostCenter":  "engineering",
    })
    require.NoError(t, err)

    // Step 4: Verify drift exists
    updateCount := testStack.VerifyDriftExists(t)
    assert.True(t, updateCount > 0)

    // Step 5: Run Claude with custom max iterations
    metrics, err := RunDriftAdoptionWithClaude(ctx, testStack, 15, t)
    require.NoError(t, err)

    // Step 6: Log metrics for benchmarking
    t.Logf("Metrics: %+v", metrics)

    // Step 7: Verify drift is resolved
    updates, creates, deletes := testStack.VerifyNoDrift(t)
    assert.Equal(t, 0, updates)
}
```

## Helper Functions Reference

### Stack Management

#### CreateTestStack
```go
func CreateTestStack(t *testing.T, exampleDir string) *TestStack
```
Creates and deploys a test stack from an example directory.

**Returns:** `*TestStack` with deployed resources and environment info

#### TestStack.Destroy
```go
func (ts *TestStack) Destroy(t *testing.T)
```
Cleans up the test stack. Always use with `defer` after creation.

#### TestStack.VerifyDriftExists
```go
func (ts *TestStack) VerifyDriftExists(t *testing.T) int
```
Runs refresh and preview to detect drift.

**Returns:** Number of resources with drift

#### TestStack.VerifyNoDrift
```go
func (ts *TestStack) VerifyNoDrift(t *testing.T) (updates, creates, deletes int)
```
Runs preview to verify no drift remains.

**Returns:** Counts of planned updates, creates, and deletes

### Creating Drift

#### NewAWSResourceDrift
```go
func NewAWSResourceDrift(region string) *AWSResourceDrift
```
Creates a drift helper for AWS resources. Defaults to `us-west-2` if region is empty.

#### Generic CloudControl API Functions

##### GetResourceState
```go
func (ard *AWSResourceDrift) GetResourceState(typeName, identifier string) (map[string]interface{}, error)
```
Retrieves the current state of any AWS resource using CloudControl API.

**Example:**
```go
state, err := driftHelper.GetResourceState("AWS::S3::Bucket", bucketName)
if err != nil {
    t.Fatal(err)
}
t.Logf("Current bucket state: %+v", state)
```

##### CreateResourceDrift
```go
func (ard *AWSResourceDrift) CreateResourceDrift(typeName, identifier string, propertyUpdates map[string]interface{}) error
```
Creates drift by updating arbitrary properties on any AWS resource.

**Example:**
```go
// Add tags to any resource
err := driftHelper.CreateResourceDrift("AWS::EC2::VPC", vpcID, map[string]interface{}{
    "Tags": []map[string]string{
        {"Key": "Environment", "Value": "production"},
        {"Key": "Team", "Value": "platform"},
    },
})

// Update bucket configuration
err := driftHelper.CreateResourceDrift("AWS::S3::Bucket", bucketName, map[string]interface{}{
    "VersioningConfiguration": map[string]interface{}{
        "Status": "Enabled",
    },
})

// Update DynamoDB table
err := driftHelper.CreateResourceDrift("AWS::DynamoDB::Table", tableName, map[string]interface{}{
    "PointInTimeRecoverySpecification": map[string]interface{}{
        "PointInTimeRecoveryEnabled": true,
    },
})
```

##### UpdateResourceProperties
```go
func (ard *AWSResourceDrift) UpdateResourceProperties(typeName, identifier string, patchOperations []map[string]interface{}) error
```
Updates resource properties using JSON Patch (RFC 6902) operations for fine-grained control.

**Example:**
```go
// Use JSON Patch operations for advanced scenarios
patchOps := []map[string]interface{}{
    {
        "op":    "add",
        "path":  "/Tags",
        "value": []map[string]string{{"Key": "Owner", "Value": "team-platform"}},
    },
    {
        "op":    "replace",
        "path":  "/PublicAccessBlockConfiguration/BlockPublicAcls",
        "value": true,
    },
}
err := driftHelper.UpdateResourceProperties("AWS::S3::Bucket", bucketName, patchOps)
```

##### DeleteResource
```go
func (ard *AWSResourceDrift) DeleteResource(typeName, identifier string) error
```
Deletes any AWS resource using CloudControl API.

**Example:**
```go
// Delete a VPC
err := driftHelper.DeleteResource("AWS::EC2::VPC", vpcID)

// Delete a Lambda function
err := driftHelper.DeleteResource("AWS::Lambda::Function", functionName)

// Delete a DynamoDB table
err := driftHelper.DeleteResource("AWS::DynamoDB::Table", tableName)
```

#### Convenience Functions for S3 (Using S3 API)

**Note:** S3 functions use the S3 API directly for better compatibility. For other S3 properties not covered by convenience functions, you can still use the generic `CreateResourceDrift()` with CloudControl.

##### CreateS3BucketTagDrift
```go
func (ard *AWSResourceDrift) CreateS3BucketTagDrift(bucketName string, tags map[string]string) error
```
Adds tags to an S3 bucket using S3 API (`s3api put-bucket-tagging`).

**Example:**
```go
err := driftHelper.CreateS3BucketTagDrift(bucketName, map[string]string{
    "Environment": "production",
    "ManagedBy":   "manual",
})
```

##### UpdateS3BucketProperty
```go
func (ard *AWSResourceDrift) UpdateS3BucketProperty(bucketName, property string, value interface{}) error
```
Updates specific bucket properties. Handles versioning via S3 API, falls back to CloudControl for other properties.

**Example:**
```go
// Versioning - uses S3 API
err := driftHelper.UpdateS3BucketProperty(bucketName, "Versioning", map[string]interface{}{
    "Status": "Enabled",
})

// Other properties - tries CloudControl
err := driftHelper.UpdateS3BucketProperty(bucketName, "SomeOtherProperty", value)
```

**Pro Tip:** For unsupported properties or more control, use the generic functions:
```go
// Use CloudControl directly
err := driftHelper.CreateResourceDrift("AWS::S3::Bucket", bucketName, map[string]interface{}{
    "PublicAccessBlockConfiguration": map[string]interface{}{
        "BlockPublicAcls": true,
    },
})
```

##### DeleteS3Bucket
```go
func (ard *AWSResourceDrift) DeleteS3Bucket(bucketName string) error
```
Deletes an S3 bucket using S3 API (automatically empties bucket first).

**Example:**
```go
err := driftHelper.DeleteS3Bucket(bucketName)
```

### Running Claude

#### RunDriftAdoptionWithClaude
```go
func RunDriftAdoptionWithClaude(
    ctx context.Context,
    testStack *TestStack,
    maxIterations int,
    t *testing.T,
) (*DriftTestMetrics, error)
```
Runs Claude to adopt drift and returns comprehensive metrics.

**Parameters:**
- `ctx` - Context for API calls
- `testStack` - The test stack with drift
- `maxIterations` - Maximum Claude iterations (recommended: 10-20)
- `t` - Testing instance

**Returns:** `*DriftTestMetrics` with full benchmark data

## CloudControl API + Service-Specific APIs

The framework uses a **hybrid approach** combining:

1. **AWS CloudControl API** - Generic interface for most resources
2. **Service-Specific APIs** - For resources requiring special handling (e.g., S3)

### Why This Hybrid Approach?

**CloudControl API Benefits:**
- **Unified Interface** - One API pattern for all AWS resources
- **No Service-Specific Code Needed** - Works with any resource type
- **Consistent Behavior** - Same patterns across resources
- **Future-Proof** - Automatically supports new AWS resource types

**Service-Specific APIs (S3, etc.):**
- **Better Compatibility** - Some resources have quirks that CloudControl doesn't handle well
- **More Reliable** - Direct service APIs are more battle-tested
- **Specific Features** - Access service-specific features not exposed via CloudControl

### Which Resources Use Which API?

| Resource Type | API Used | Reason |
|--------------|----------|--------|
| S3 Buckets | S3 API | Better tag/property handling |
| Lambda, DynamoDB, VPC, etc. | CloudControl | Generic interface works well |
| Custom Resources | CloudControl by default | Falls back to service API if needed |

### Supported Resources

CloudControl API supports 1000+ AWS resource types including:
- **Compute**: Lambda, ECS, EKS, Batch
- **Storage**: S3 (via S3 API), EFS, FSx
- **Database**: DynamoDB, RDS, Aurora, DocumentDB
- **Networking**: VPC, Subnet, Security Group, ALB, NLB
- **Security**: IAM, Secrets Manager, KMS
- **Monitoring**: CloudWatch, SNS, SQS
- **And many more...**

Check supported resources: `aws cloudcontrol list-resource-types`

## Adding New Resource Types

With CloudControl API, you **don't need to add service-specific code**. Just use the generic functions:

### Example: VPC Drift
```go
func TestVPCTagDrift(t *testing.T) {
    ctx := context.Background()
    testStack := CreateTestStack(t, filepath.Join("..", "..", "examples", "vpc-example"))
    defer testStack.Destroy(t)

    vpcID := testStack.Resources["vpcId"].(string)
    driftHelper := NewAWSResourceDrift("us-west-2")

    // Create drift - no VPC-specific function needed!
    err := driftHelper.CreateResourceDrift("AWS::EC2::VPC", vpcID, map[string]interface{}{
        "Tags": []map[string]string{
            {"Key": "Environment", "Value": "production"},
            {"Key": "Team", "Value": "platform"},
        },
    })
    require.NoError(t, err)

    // Run Claude to adopt drift
    metrics, err := RunDriftAdoptionWithClaude(ctx, testStack, 10, t)
    require.NoError(t, err)

    testStack.VerifyNoDrift(t)
}
```

### Example: Lambda Function Drift
```go
func TestLambdaConfigDrift(t *testing.T) {
    ctx := context.Background()
    testStack := CreateTestStack(t, filepath.Join("..", "..", "examples", "lambda-example"))
    defer testStack.Destroy(t)

    functionName := testStack.Resources["functionName"].(string)
    driftHelper := NewAWSResourceDrift("us-west-2")

    // Update Lambda timeout and memory - generic function!
    err := driftHelper.CreateResourceDrift("AWS::Lambda::Function", functionName, map[string]interface{}{
        "Timeout":    60,  // Changed from 30
        "MemorySize": 512, // Changed from 256
    })
    require.NoError(t, err)

    metrics, err := RunDriftAdoptionWithClaude(ctx, testStack, 10, t)
    require.NoError(t, err)

    t.Logf("Claude fixed Lambda drift using %d iterations", metrics.IterationsCount)
}
```

### Example: DynamoDB Table Drift
```go
func TestDynamoDBTableDrift(t *testing.T) {
    ctx := context.Background()
    testStack := CreateTestStack(t, filepath.Join("..", "..", "examples", "dynamodb-example"))
    defer testStack.Destroy(t)

    tableName := testStack.Resources["tableName"].(string)
    driftHelper := NewAWSResourceDrift("us-west-2")

    // Enable point-in-time recovery - no DynamoDB-specific code!
    err := driftHelper.CreateResourceDrift("AWS::DynamoDB::Table", tableName, map[string]interface{}{
        "PointInTimeRecoverySpecification": map[string]interface{}{
            "PointInTimeRecoveryEnabled": true,
        },
    })
    require.NoError(t, err)

    metrics, err := RunDriftAdoptionWithClaude(ctx, testStack, 10, t)
    require.NoError(t, err)
}
```

### Optional: Adding Convenience Functions

If you use a resource type frequently, add a convenience function:

```go
// In drift_helpers.go

func (ard *AWSResourceDrift) CreateLambdaConfigDrift(functionName string, config map[string]interface{}) error {
    return ard.CreateResourceDrift("AWS::Lambda::Function", functionName, config)
}

func (ard *AWSResourceDrift) CreateVPCTagDrift(vpcID string, tags map[string]string) error {
    var tagSet []map[string]string
    for key, value := range tags {
        tagSet = append(tagSet, map[string]string{
            "Key":   key,
            "Value": value,
        })
    }
    return ard.CreateResourceDrift("AWS::EC2::VPC", vpcID, map[string]interface{}{
        "Tags": tagSet,
    })
}
```

Then use it in tests:
```go
err := driftHelper.CreateLambdaConfigDrift(functionName, map[string]interface{}{
    "Timeout":    60,
    "MemorySize": 512,
})
```

## Example Test Scenarios

### 1. S3 Bucket with Tag Drift
```go
func TestS3TagDrift(t *testing.T) {
    testDriftAdoptionWithConfig(t, DriftTestConfig{
        ExampleDir:    filepath.Join("..", "..", "examples", "simple-s3"),
        MaxIterations: 10,
        AWSRegion:     "us-west-2",
    })
}
```

### 2. Multiple Resources with Different Drift Types
```go
func TestMultipleResourceDrift(t *testing.T) {
    ctx := context.Background()
    testStack := CreateTestStack(t, filepath.Join("..", "..", "examples", "multi-resource"))
    defer testStack.Destroy(t)

    driftHelper := NewAWSResourceDrift("us-west-2")

    // Create drift on bucket 1 - add tags
    bucket1 := testStack.Resources["bucket1Name"].(string)
    driftHelper.CreateS3BucketTagDrift(bucket1, map[string]string{
        "Environment": "prod",
    })

    // Create drift on bucket 2 - enable versioning
    bucket2 := testStack.Resources["bucket2Name"].(string)
    driftHelper.UpdateS3BucketProperty(bucket2, "versioning", "Enabled")

    metrics, err := RunDriftAdoptionWithClaude(ctx, testStack, 20, t)
    require.NoError(t, err)

    // Should have fixed drift on 2 resources
    assert.Equal(t, 2, metrics.ResourcesWithDrift)
}
```

### 3. Resource Deletion Drift
```go
func TestResourceDeletionDrift(t *testing.T) {
    ctx := context.Background()
    testStack := CreateTestStack(t, filepath.Join("..", "..", "examples", "deletable-bucket"))
    defer testStack.Destroy(t)

    bucketName := testStack.Resources["bucketName"].(string)

    // Delete the bucket manually to simulate deletion drift
    driftHelper := NewAWSResourceDrift("us-west-2")
    err := driftHelper.DeleteS3Bucket(bucketName)
    require.NoError(t, err)

    // Claude should detect and add the resource back to code
    metrics, err := RunDriftAdoptionWithClaude(ctx, testStack, 15, t)
    require.NoError(t, err)

    t.Logf("Fixed deletion drift with %d iterations", metrics.IterationsCount)
}
```

## Benchmarking

The metrics returned by `RunDriftAdoptionWithClaude` can be used for performance benchmarking:

```go
func BenchmarkDriftAdoption(b *testing.B) {
    for i := 0; i < b.N; i++ {
        t := &testing.T{} // Create testing context
        ctx := context.Background()

        testStack := CreateTestStack(t, exampleDir)
        // ... create drift ...

        start := time.Now()
        metrics, _ := RunDriftAdoptionWithClaude(ctx, testStack, 10, t)
        elapsed := time.Since(start)

        b.ReportMetric(float64(metrics.TotalTokens), "tokens")
        b.ReportMetric(float64(metrics.IterationsCount), "iterations")
        b.ReportMetric(elapsed.Seconds(), "seconds")

        testStack.Destroy(t)
    }
}
```

## Tips

1. **Always use defer** for stack cleanup to ensure resources are destroyed even if test fails
2. **Set appropriate maxIterations** - Simple scenarios: 10, Complex: 15-20
3. **Check metrics** to identify performance bottlenecks or inefficient prompts
4. **Run tests in parallel** carefully - they create real AWS resources
5. **Use descriptive test names** that indicate the drift type being tested
6. **Log metrics** for benchmark tracking over time

## Troubleshooting

### Test times out
- Increase `maxIterations` parameter
- Check if drift-adopt skill instructions are clear
- Verify AWS credentials are valid

### Metrics show high tool calls
- May indicate unclear drift-adopt output format
- Consider refining the skill prompt
- Check if Claude is exploring unnecessary files

### Drift not detected
- Ensure `VerifyDriftExists` is called after creating drift
- Verify AWS CLI commands succeeded
- Check that refresh is run before preview

## Contributing

To add new helper functions:
1. Add function to `drift_helpers.go`
2. Document in this README
3. Add example test in `drift_adoption_test.go`
4. Run tests to verify: `go test -v -tags=e2e ./test/e2e/`
