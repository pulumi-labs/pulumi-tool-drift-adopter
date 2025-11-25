# Drifted Version - loop-resources

This is a modified version of the parent program used for testing drift adoption with loop-based resource deletion.

## How it's used in tests

1. Deploy the original program (parent directory) → creates 3 buckets from array
2. Export the state file
3. Deploy this drifted program → removes "data-bucket" from array (deletes it)
4. Import the original state file → resets state to include all 3 buckets
5. Refresh from original program → detects data-bucket is missing (drift)
6. Run drift adoption → LLM updates original code to remove "data-bucket" from array

## Differences from original

- **Removed**: "data-bucket" from bucketNames array
- **Removed**: dataBucketId export
