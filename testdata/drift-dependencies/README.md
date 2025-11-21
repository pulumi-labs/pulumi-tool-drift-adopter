# Drift Dependencies Test Fixture

## Scenario
Multi-resource AWS infrastructure with dependencies. Tests that drift adoption respects dependency order.

## Architecture
```
VPC (main-vpc)
├── Subnet (main-subnet)
└── SecurityGroup (main-sg)
```

## Drift Description
All three resources have drift:
- **VPC**: `tags.Environment` changed from "dev" to "production"
- **Subnet**: `tags.Tier` added with value "private"
- **SecurityGroup**: `description` changed to include "- updated"

## Expected Behavior
1. Tool detects drift on all 3 resources
2. Builds dependency graph from state
3. Creates 2 chunks ordered by dependencies:
   - **Chunk 1 (order 0)**: VPC (leaf node, no dependencies)
   - **Chunk 2 (order 1)**: Subnet + SecurityGroup (both depend on VPC)
4. Agent must process chunks in order
5. Cannot process Chunk 2 until Chunk 1 is complete

## Files
- `index.ts` - Pulumi program with VPC, Subnet, and SecurityGroup
- `Pulumi.yaml` - Project configuration
- `state.json` - Pulumi state with dependency information
- `preview.json` - Simulated preview output showing drift
- `expected-plan.json` - Expected 2-chunk plan with correct ordering
