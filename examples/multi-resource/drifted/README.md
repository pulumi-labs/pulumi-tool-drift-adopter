# Drifted Version - multi-resource

This is a modified version of the parent program used for testing drift adoption with resource deletion.

## How it's used in tests

1. Deploy the original program (parent directory) → creates 3 buckets (a, b, c)
2. Export the state file
3. Deploy this drifted program → deletes bucket-b
4. Import the original state file → resets state to include all 3 buckets
5. Refresh from original program → detects bucket-b is missing (drift)
6. Run drift adoption → LLM updates original code to remove bucket-b

## Differences from original

- **Removed**: bucket-b resource and bucketNameB export
