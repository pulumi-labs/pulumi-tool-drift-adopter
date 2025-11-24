#!/bin/bash
set -e

REGION="us-west-2"
BUCKET="timing-test-$(date +%s)"

echo "Creating bucket: $BUCKET"
aws s3api create-bucket \
  --bucket "$BUCKET" \
  --region "$REGION" \
  --create-bucket-configuration "LocationConstraint=$REGION"

echo ""
echo "=== Test 1: CloudControl + immediate check ==="
echo "Adding tags via CloudControl..."
START=$(date +%s)

# Use CloudControl to add tags
aws cloudcontrol update-resource \
  --type-name "AWS::S3::Bucket" \
  --identifier "$BUCKET" \
  --region "$REGION" \
  --patch-document '[{"op":"add","path":"/Tags","value":[{"Key":"Test","Value":"CloudControl"}]}]' \
  > /dev/null

END=$(date +%s)
echo "CloudControl command took: $((END - START)) seconds"

echo "Immediately checking tags with S3 API..."
aws s3api get-bucket-tagging --bucket "$BUCKET" --region "$REGION" 2>&1 || echo "Tags not found yet"

echo ""
echo "Waiting 2 seconds..."
sleep 2

echo "Checking tags again..."
aws s3api get-bucket-tagging --bucket "$BUCKET" --region "$REGION" 2>&1 || echo "Tags still not found"

echo ""
echo "Waiting 5 more seconds (7 total)..."
sleep 5

echo "Checking tags again..."
aws s3api get-bucket-tagging --bucket "$BUCKET" --region "$REGION" 2>&1 || echo "Tags still not found"

# Cleanup
echo ""
echo "Cleaning up..."
aws s3api delete-bucket --bucket "$BUCKET" --region "$REGION"

echo ""
echo "=== Conclusion ==="
echo "If tags weren't immediately available, CloudControl is eventually consistent"
