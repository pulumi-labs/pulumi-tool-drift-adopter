# Drifted Version - simple-s3

This is a modified version of the parent program used for testing drift adoption.

## How it's used in tests

1. Deploy the original program (parent directory) → creates bucket without tags
2. Export the state file
3. Deploy this drifted program → adds tags to the bucket
4. Import the original state file → resets state to pre-drift
5. Refresh from original program → detects tags as drift
6. Run drift adoption → LLM updates original code to include tags

## Differences from original

- **Added**: `tags` property with Environment and ManagedBy tags
