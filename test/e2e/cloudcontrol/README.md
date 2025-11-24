# AWS CloudControl API Investigation

This directory contains tests and documentation from our investigation into using AWS CloudControl API for drift adoption testing.

## Summary

**Conclusion: CloudControl API is not suitable for S3 bucket modifications due to region-handling bugs.**

## Files in This Directory

### Documentation

- **`CLOUDCONTROL_FINDINGS.md`** - Final investigation results showing CloudControl's region-handling issues with S3
- **`CLOUDCONTROL_VALIDATION.md`** - Initial validation testing showing CloudControl works with standalone resources
- **`README.md`** - This file

### Test Files

- **`cloudcontrol_debug_test.go`** - Standalone tests validating CloudControl API functions work with AWS CLI-created resources
- **`cloudcontrol_pulumi_integration_test.go`** - Integration test that revealed CloudControl fails with Pulumi-created resources
- **`test_cloudcontrol_timing.sh`** - Shell script for testing CloudControl eventual consistency behavior

## Key Findings

1. **CloudControl operations are asynchronous** - Must poll `get-resource-request-status` for completion
2. **CloudControl has region-handling bugs with S3** - Fails with "Try the request again using the bucket's region: us-east-1" errors
3. **S3 API works reliably** - No region issues
4. **Async handling is now implemented** in `drift_helpers.go` via `waitForOperation()`

## Running CloudControl Tests

**Important:** These tests are primarily for **reference and documentation purposes**. They document the CloudControl API investigation that led to choosing the S3 API approach.

To run these tests (requires moving them back to parent directory temporarily):

```bash
# Option 1: Move tests to parent directory
cd test/e2e
cp cloudcontrol/*.go .
go test -v -tags=e2e -run TestCloudControl ./

# Option 2: Copy drift_helpers.go to cloudcontrol directory
cp drift_helpers.go cloudcontrol/
cd cloudcontrol
go test -v -tags=e2e -run TestCloudControl ./
```

**Why these tests aren't directly runnable:**
- They depend on `../drift_helpers.go` which Go test doesn't include for subdirectories
- They're kept separate for organization (investigation/reference vs production tests)
- The production E2E tests in the parent directory use the proven S3 API approach

If you need to reproduce the CloudControl investigation, refer to the test code and findings documents in this directory.

## Why These Files Are Separate

These tests are kept separate from the main E2E tests because:

1. They're investigative/diagnostic tests, not production test cases
2. They document why we chose the hybrid approach (S3 API + CloudControl)
3. They serve as reference for future CloudControl usage attempts
4. The main drift adoption tests use the proven S3 API approach

## Lessons Learned

- Always handle async CloudControl operations with request token polling
- Test end-to-end scenarios, not just standalone operations
- CloudControl API documentation doesn't mention region-handling issues
- Service-specific APIs (like S3 API) are more reliable than CloudControl for some services

## For Future Reference

If attempting to use CloudControl API for other AWS resource types:

1. Implement async operation handling (`waitForOperation`)
2. Add detailed debug logging
3. Test with both standalone resources AND IaC-created resources
4. Have a fallback to service-specific APIs
5. Check AWS documentation and support forums for known issues

## Related Code

The CloudControl helper functions with async handling are in:
- `../drift_helpers.go` - `UpdateResourceProperties()`, `waitForOperation()`, `CreateResourceDrift()`
