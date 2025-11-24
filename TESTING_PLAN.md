# Drift Adoption Testing Plan

## Overview

This document outlines the comprehensive testing strategy for the pulumi-drift-adopt tool and LLM-assisted drift adoption workflow.

## Current State

We have a basic E2E test (`TestDriftAdoptionWorkflow`) that:
- Creates a simple Go Pulumi program with an S3 bucket
- Introduces drift by modifying bucket tags
- Tests that the tool + LLM can detect and adopt the drift

## Testing Expansion Goals

### 1. Resource Deletion Tests

**Objective:** Test that the tool and LLM can work together to remove code for deleted resources.

**Approach:**
- Extend existing example with resource deletion drift
- Deploy stack with multiple resources
- Delete a resource outside of Pulumi (via AWS CLI/SDK)
- Run drift adoption workflow
- Verify LLM correctly removes the resource code

### 2. Multi-Language Support

**Objective:** Ensure drift adoption works across all Pulumi languages.

**Languages to support:**
- Go (✅ current)
- TypeScript/JavaScript
- Python
- C#
- Java
- YAML

**Approach:**
- Translate existing test program to each language
- Create subtests for each language variant
- Use same drift scenarios across all languages

### 3. Drift Complexity Spectrum

**Objective:** Test a range of drift scenarios from simple to complex.

**For each example and language, create 3-5 tests:**

1. **Minimal Drift** - Single property change on one resource
2. **Small Drift** - 2-3 property changes across 1-2 resources
3. **Medium Drift** - Multiple property changes, 1 resource deletion
4. **Large Drift** - Extensive property changes, multiple resource additions/deletions
5. **Complex Drift** - Property changes + resource changes + dependency impacts

### 4. Real-World Examples from pulumi/workshops

**Source:** https://github.com/pulumi/workshops

**Selection Criteria:**
- Examples that use **components** (custom resource groupings)
- Stacks with **resource dependency chains** via resource outputs
- Examples that would benefit from **stack config updates** rather than code changes
- Variety of cloud providers and resource types

**Process:**
1. Download and analyze pulumi/workshops repository
2. Identify 5-10 suitable example programs
3. Copy selected programs into `test/e2e/examples/`
4. Translate each to all supported Pulumi languages
5. Create drift adoption tests for each example

**Additional Considerations:**
- Update skill prompt to handle stack config file updates
- Document common Pulumi patterns (components, outputs, config) in skill guidance
- Test LLM's ability to understand resource dependencies

### 5. Large-Scale Resource Tests

**Objective:** Test LLM context handling with large state files.

**Test Scenarios:**
- 250 resources
- 500 resources
- 750 resources
- 1000 resources

**Implementation:**
- Use YAML Pulumi programs for simplicity
- Use [Command Provider](https://www.pulumi.com/registry/packages/command/api-docs/local/command/) to generate many resources
- Introduce drift across subset of resources

**Tool Enhancement Required:**
- Add `--max-resources` flag to limit resources returned per step (default: 10)
- Implement pagination/batching for large drift sets
- Test LLM's ability to handle partial drift adoption workflow

### 6. Future: External Resource Import (Separate Issue)

**Scenario:**
- New resource created outside of stack (e.g., manual AWS Console creation)
- Stack resource has reference to external resource via ID/ARN
- External resource might replace a deleted stack resource of same type

**Proposed Functionality:**
1. Tool detects resource reference to non-existent stack resource
2. Tool attempts to `pulumi import` the external resource
3. Tool runs preview to show code changes needed
4. LLM generates code to create the imported resource

**This is complex and should be tracked separately.**

## Test Organization

```
test/e2e/
├── drift_adoption_test.go       # Main test orchestrator
├── drift_helpers.go              # Shared test utilities
├── examples/                     # Test programs
│   ├── simple-s3/               # Current example
│   │   ├── go/
│   │   ├── typescript/
│   │   ├── python/
│   │   ├── csharp/
│   │   ├── java/
│   │   └── yaml/
│   ├── component-example/       # From pulumi/workshops
│   │   └── [all languages]
│   ├── dependency-chain/        # Resource outputs example
│   │   └── [all languages]
│   ├── config-driven/           # Stack config example
│   │   └── [all languages]
│   └── large-scale/             # Performance tests
│       └── yaml/
│           ├── 250-resources/
│           ├── 500-resources/
│           ├── 750-resources/
│           └── 1000-resources/
└── cloudcontrol/                # CloudControl investigation (reference)
    └── [markdown docs only]
```

## Success Criteria

### Functional
- [ ] Resource deletion drift handled correctly across all languages
- [ ] All Pulumi languages supported with passing tests
- [ ] 3-5 drift complexity levels tested per example
- [ ] Real-world examples from workshops successfully tested
- [ ] Large-scale tests (up to 1000 resources) pass
- [ ] Tool correctly batches large drift sets

### Quality
- [ ] Test coverage > 80%
- [ ] Clear test failure messages
- [ ] Tests run in CI/CD (GitHub Actions)
- [ ] Performance benchmarks for large-scale tests
- [ ] Documentation for adding new test examples

### LLM Performance
- [ ] Token usage tracked and reasonable
- [ ] Success rate > 90% for simple/small drift
- [ ] Success rate > 75% for medium/large drift
- [ ] Handles component-based programs correctly
- [ ] Correctly identifies when to update stack config vs code

## Implementation Phases

### Phase 1: Foundation (Current Sprint)
- ✅ Basic E2E test infrastructure
- ✅ CloudControl API investigation
- ✅ Test metrics tracking
- 🔄 Resource deletion test
- 🔄 Tool enhancement: max resources per step

### Phase 2: Multi-Language Support
- TypeScript/JavaScript support
- Python support
- C#, Java, YAML support
- Language-specific test runner

### Phase 3: Real-World Examples
- Download and analyze pulumi/workshops
- Select and copy 5-10 examples
- Translate to all languages
- Create comprehensive test suite

### Phase 4: Scale Testing
- Large-scale YAML programs
- Performance benchmarking
- Context window optimization

### Phase 5: Advanced Features (Future)
- External resource import capability
- Complex dependency resolution
- Multi-stack scenarios

## Metrics to Track

For each test run, capture:
- **LLM Metrics:** Token usage, API calls, iterations
- **Tool Metrics:** Resources with drift, tool invocations, success rate
- **Time Metrics:** Total workflow time, per-iteration time
- **Accuracy Metrics:** Correct adoptions, partial adoptions, failures

These metrics should be:
- Logged to test output
- Aggregated across test runs
- Used to identify performance regressions
- Reported in CI/CD
