# Analysis: pulumi-terraform-migrate Repository

## Executive Summary

The `pulumi-terraform-migrate` tool is a **guided state machine CLI** designed for LLM-assisted iterative workflows. It orchestrates complex, long-running migrations by breaking them into discrete steps with clear validation gates.

## Architecture Overview

### Core Design Principles

1. **Iterative LLM Workflow**: LLM calls `next` repeatedly, follows suggestions, calls `next` again
2. **Stateful Checkpointing**: All progress stored in `migration.json` for resumability
3. **Fail-Fast Validation**: Sequential gates prevent invalid states
4. **Concrete Guidance**: Outputs exact commands with pre-filled arguments
5. **Progressive Disclosure**: Shows only the immediate next step

### Architecture Layers

```
┌─────────────────────────────────────┐
│   CLI Commands (cmd/)               │
│   - next: orchestrator              │
│   - init-migration, diff, check     │
│   - set-urn, skip, untrack          │
│   - resolve-import-stubs            │
└─────────────┬───────────────────────┘
              │
┌─────────────▼───────────────────────┐
│   Core Logic (pkg/tfmig/)           │
│   - Migration state management      │
│   - Type mapping (TF→Pulumi)        │
│   - Diff computation                │
│   - Import ID inference             │
└─────────────┬───────────────────────┘
              │
┌─────────────▼───────────────────────┐
│   Pulumi Integration (pkg/pulumix/) │
│   - Automation API wrappers         │
│   - Provider gRPC communication     │
│   - Enhanced preview with tracking  │
└─────────────────────────────────────┘
```

## Detailed Analysis

### 1. Codebase Structure

**Command Layer** (`/cmd/pulumi-terraform-migrate/`):
- `main.go` - Entry point
- `root.go` - Cobra CLI root command setup
- `next.go` - Core iterative workflow orchestrator (697 lines - the heart of the system)
- `init_migration.go` - Initializes migration.json from Terraform project
- `diff.go` - Computes and displays migration status
- `check.go` - Validates migration.json integrity
- `resolve_import_stubs.go` - Resolves import IDs for resources
- `skip.go` - Marks resources to skip
- `set_urn.go` - Updates Terraform-to-Pulumi resource mappings
- `suggest_provider.go` - Suggests Pulumi provider for TF provider
- `suggest_resource.go` - Suggests Pulumi resource type for TF resource
- `translate_state.go` - Direct state translation for resources
- `untrack.go` - Removes resources from tracking

**Core Package** (`/pkg/tfmig/`):
- `types.go` - TypeMapper for TF→Pulumi type conversion with caching
- `migration.go` - Migration data structures and initialization (318 lines)
- `check.go` - Migration file integrity validation (217 lines)
- `diff.go` - Resource status computation (164 lines)
- `imports.go` - Import ID inference logic (156 lines)
- `import_stubs.go` - Import stub resolution (381 lines)
- `providers.go` - Hardcoded TF→Pulumi provider mappings
- `tfextras.go` - Terraform state loading utilities

**Pulumi Extensions** (`/pkg/pulumix/`):
- `mapping.go` - Provider binary mapping extraction via gRPC (200 lines)
- `install.go` - Provider installation utilities
- `previewx.go` - Enhanced preview with resource status tracking
- `autox.go` - Temporary stack creation utilities
- `importer.go` - Import ID validation
- `loader.go` - Provider plugin loading

### 2. The `next` Command Workflow

The heart of the tool is `/cmd/pulumi-terraform-migrate/next.go` which implements a **sequential gate pattern**:

```go
func next() {
    if !gate1_ensureMigrationFileExists() { return }
    if !gate2_ensureMigrationFileIntegrity() { return }
    if !gate3_ensurePulumiStacksExist() { return }
    if !gate4_ensureSourceCodeMapped() { return }
    if !gate5_ensureImportStubsExist() { return }
    if !gate6_ensureImportStubsResolved() { return }
    if !gate7_ensureEmptyDiff() { return }

    println("STOP")  // LLM termination signal
}
```

#### The Gates (in order):

1. **ensureMigrationFileExists** (lines 117-143)
   - Checks if migration.json exists
   - Suggests: `pulumi-terraform-migrate init-migration`

2. **ensureMigrationFileIntegrity** (lines 89-115)
   - Validates migration.json structure
   - Suggests: `pulumi-terraform-migrate check`

3. **ensurePulumiStacksExist** (lines 145-172)
   - Checks all stacks are created
   - Suggests: `pulumi stack init <name>`

4. **ensureSourceCodeMapped** (lines 419-574)
   - Validates all TF resources have Pulumi source code
   - Most complex gate - runs preview, compares with migration.json
   - Suggests three options:
     - Translate missing resources
     - Fix URN mappings with `set-urn`
     - Skip resources with `skip`

5. **ensureImportStubsExist** (lines 213-312)
   - Generates import-stub.json via `pulumi preview --import-file`
   - Automatically creates temporary files
   - Updates migration.json with stub file paths

6. **ensureImportStubsResolved** (lines 330-417)
   - Resolves placeholder IDs to actual import IDs
   - Calls `ResolveImportStubs` function
   - Creates import.json files

7. **ensureEmptyDiff** (lines 624-696)
   - Runs diff check on all stacks
   - Ensures all resources are migrated (no diffs)
   - Suggests: `pulumi-terraform-migrate diff --details`

8. **STOP** (line 86)
   - Signals migration complete
   - LLM should terminate

Each gate:
- Validates a precondition
- Returns suggestions if failed (formatted for LLM consumption)
- Continues to next gate if passed

### 3. Key Data Structures

**migration.json** (central state file):
```go
type MigrationFile struct {
    Migration {
        TFSources     string  // Path to Terraform sources
        PulumiSources string  // Path to Pulumi project
        Stacks []Stack {
            TFState            string      // Path to tfstate.json
            PulumiStack        string      // Stack name
            Resources []Resource {
                TFAddr  string      // "module.foo.aws_instance.bar[0]"
                URN     string      // "urn:pulumi:dev::proj::aws:ec2/instance:Instance::name"
                Migrate MigrateMode // "", "skip", "ignore-no-state", etc.
            }
            ImportStubFile     string  // Generated stub file path
            ImportResolvedFile string  // Resolved import file path
        }
    }
}
```

### 4. Design Patterns Used

1. **Three-Level Caching** (`TypeMapper`): Avoids repeated provider installations
   - Cache level 1: Type mappings
   - Cache level 2: Provider info
   - Cache level 3: Binary paths

2. **Visitor Pattern** (`VisitResources`): Walks Terraform state tree recursively through modules

3. **Status Enum with Discriminated Unions** (`ResourceStatus` interface):
   - `ResourceSkipped`
   - `ResourceNotTracked`
   - `ResourceNotTranslated`
   - `ResourceTranslated` (with substatus)

4. **Validation Pattern**: Pre/post validation with `--force` override

5. **Rich Error Context**: Custom error types with actionable suggestions
   - `noMatchingResourceError` provides JSON examples
   - `ambiguousMappingError` shows all matches

6. **Result Pattern**: Functions return rich result objects
   - Example: `ResolveImportStubsResult` with resolved/unresolved/skipped counts

### 5. LLM Guidance Mechanism

Structured text output with:
- **Context**: Current state, counts, file paths
- **Concrete Commands**: Exact CLI examples
- **Options**: Multiple paths forward when applicable
- **Examples**: Shows expected resource mappings

Example output:
```
The next step is to ensure that every Terraform resource is translated to Pulumi...

Missing resources: 5

If you have not yet started translating... do it now.

Otherwise the next step is to iterate on the missing resources...

The first of the 5 missing resources:

  Terraform address: aws_instance.example
  Expected Pulumi URN: urn:pulumi:...

There are three options.

Option 1. Translate it...
Option 2. Fix the association:
  pulumi-terraform-migrate set-urn --migration "..." --tf-addr "..." --urn $urn
Option 3. Skip it:
  pulumi-terraform-migrate skip --migration "..." "..."
```

### 6. How It Determines Next Steps and When to Stop

**Decision Logic in `next()` function:**

The function executes **sequential gates** (if/return pattern):
```go
func next(migrationFile string) {
    if !ensureMigrationFileExists(migrationFile) {
        return  // Printed suggestions, exit
    }
    if !ensureMigrationFileIntegrity(migrationFile) {
        return  // Printed suggestions, exit
    }
    if !ensurePulumiStacksExist(ctx, mf) {
        return  // Printed suggestions, exit
    }
    if !ensureSourceCodeMapped(ctx, mf, migrationFile) {
        return  // Printed suggestions, exit
    }
    if !ensureImportStubsExist(ctx, mf, migrationFile) {
        return  // Printed suggestions, exit
    }
    if !ensureImportStubsResolved(mf, migrationFile) {
        return  // Printed suggestions, exit
    }
    if !ensureEmptyDiff(ctx, mf, migrationFile) {
        return  // Printed suggestions, exit
    }

    fmt.Println("STOP")  // All gates passed
}
```

**Stop Condition:**
- All Terraform resources either:
  - Fully migrated (have Pulumi state, no diffs)
  - Explicitly skipped
- The tool prints "STOP" literal string
- LLM watches for "STOP" output

**State Tracking:**
- Migration progress stored in migration.json
- Import stub/resolved files tracked
- Each run is idempotent (can safely rerun)

### 7. Testing Approach

**Test Coverage:**
Only 2 test files found:
- `/pkg/pulumix/mapping_test.go` - Tests provider mapping extraction
- `/pkg/pulumix/install_test.go` - Tests provider installation

**Testing Strategy:**
1. **Integration-style tests**:
   - Tests use real provider binaries (random provider)
   - Install providers to temp directories
   - Test gRPC communication

2. **Example-driven validation**:
   - `examples/13-DNS-to-DB/` - Terraform project
   - `examples/13-DNS-to-DB-pulumi/` - Completed Pulumi migration
   - `examples/13-DNS-to-DB-pulumi-wip/` - Work-in-progress migration

3. **Manual validation**:
   - Relies on running against real Terraform projects
   - No extensive unit test coverage for workflow logic
   - Command validation is runtime-based

**Testing Gaps:**
- Limited unit test coverage for core logic
- No tests for `next` command workflow
- No tests for diff computation
- No tests for import stub resolution
- Relies heavily on integration testing

### 8. Key Design Insights

1. **Stateful Workflow**: Migration state is checkpointed in migration.json, allowing interrupted migrations to resume

2. **Fail-Fast Validation**: Each gate validates before proceeding, preventing invalid states

3. **LLM-Friendly Output**: Rich, structured text with concrete commands makes it easy for LLMs to parse and execute

4. **Provider Metadata Extraction**: Uses Pulumi's GetMapping gRPC call to extract TF→Pulumi type mappings directly from provider binaries

5. **Import ID Inference**: Attempts to discover valid import IDs by trying common patterns (id, arn) and validating with `pulumi import --preview-only`

6. **Selective Skipping**: Fine-grained skip modes allow progressive migration (can skip resources that need updates but keep migrating others)

7. **Zero Manual JSON Editing**: All migration.json updates happen via commands, reducing errors

This architecture creates a **guided state machine** that an LLM can navigate by repeatedly calling `next`, following suggestions, and checking for "STOP".

---

# Drift Adoption Tool Design

## Overview

Based on the patterns from `pulumi-terraform-migrate`, here's the design for `pulumi-drift-adopt`:

### Tool Purpose
Iteratively incorporate infrastructure drift (changes made outside Pulumi IaC) back into Pulumi source code through LLM-assisted code generation.

### Drift Adoption Process

The drift adoption process involves:

1. **Drift Detection**: Recognizing that infrastructure has drifted from IaC
   - Configure deployment settings on stacks for drift monitoring
   - Run drift detection in Pulumi deployment
   - Execute `pulumi refresh` to update stack state with current cloud configurations

2. **Drift Adoption**: Incorporating drift back into IaC
   - Generate an adoption plan by analyzing the dependency tree
   - Process drift in chunks, starting with leaf resources (no dependents)
   - LLM generates code updates for each chunk
   - Validate compilation and preview
   - Iterate until all drift is adopted

## Architecture

```
┌──────────────────────────────────────────────┐
│   CLI Commands                               │
│   - next: orchestrator (like terraform-mig) │
│   - generate-plan: creates adoption plan     │
│   - adopt-chunk: processes one drift chunk   │
│   - status: shows drift summary              │
│   - skip: marks chunk to skip                │
│   - reset-chunk: retries failed chunk        │
└──────────────┬───────────────────────────────┘
               │
┌──────────────▼───────────────────────────────┐
│   Core Logic (pkg/driftadopt/)              │
│   - DriftPlan: dependency-ordered chunks     │
│   - CodeGenerator: LLM-based updates         │
│   - DiffMatcher: validates preview matches   │
│   - CompilationValidator: syntax checking    │
│   - DependencyGraph: topology analysis       │
└──────────────┬───────────────────────────────┘
               │
┌──────────────▼───────────────────────────────┐
│   Pulumi Integration (pkg/pulumix/)         │
│   - Stack drift detection configuration      │
│   - Refresh operations                       │
│   - Preview with diff analysis               │
│   - Dependency graph traversal               │
│   - State file parsing                       │
└──────────────────────────────────────────────┘
```

## Workflow Gates (Sequential)

The `next` command implements these sequential gates:

1. **ensureStackExists**: Verify stack is initialized
2. **ensureDriftDetectionConfigured**: Verify stack has drift detection settings
3. **ensureRefreshCompleted**: Run `pulumi refresh` to update state
4. **ensureDriftPlanExists**: Create `drift-plan.json` via dependency analysis
5. **ensurePlanIntegrity**: Validate drift-plan.json structure
6. **ensureChunksAdopted**: Iteratively process drift chunks (main loop)
7. **ensurePreviewClean**: Verify no remaining drift
8. **STOP**: Signal completion, suggest creating PR

## Core Data Structures

### drift-plan.json

```go
type DriftPlan struct {
    Stack       string        `json:"stack"`
    GeneratedAt time.Time     `json:"generatedAt"`
    TotalChunks int           `json:"totalChunks"`
    Chunks      []DriftChunk  `json:"chunks"`
}

type DriftChunk struct {
    ID           string         `json:"id"`           // "chunk-001"
    Order        int            `json:"order"`        // Processing order (leaves first)
    Resources    []ResourceDiff `json:"resources"`    // Resources to fix together
    Status       ChunkStatus    `json:"status"`       // pending/in_progress/completed/failed
    Dependencies []string       `json:"dependencies"` // IDs of chunks that depend on this
    Attempt      int            `json:"attempt"`      // Retry counter
    LastError    string         `json:"lastError"`    // Error message from last attempt
}

type ResourceDiff struct {
    URN          string       `json:"urn"`
    Type         string       `json:"type"`          // "aws:s3/bucket:Bucket"
    Name         string       `json:"name"`          // Resource logical name
    DiffType     DiffType     `json:"diffType"`      // "update", "delete", "replace"
    PropertyDiff []PropChange `json:"propertyDiff"`
    SourceFile   string       `json:"sourceFile"`    // Where resource is defined
    SourceLine   int          `json:"sourceLine"`    // Line number in source
}

type DiffType string
const (
    DiffTypeUpdate  DiffType = "update"   // Property changes
    DiffTypeDelete  DiffType = "delete"   // Resource deleted in cloud
    DiffTypeReplace DiffType = "replace"  // Resource needs recreation
)

type PropChange struct {
    Path     string      `json:"path"`        // "tags.Environment"
    OldValue interface{} `json:"oldValue"`    // Current IaC value
    NewValue interface{} `json:"newValue"`    // Actual cloud value
    DiffKind string      `json:"diffKind"`    // "add", "delete", "update"
}

type ChunkStatus string
const (
    ChunkPending    ChunkStatus = "pending"
    ChunkInProgress ChunkStatus = "in_progress"
    ChunkCompleted  ChunkStatus = "completed"
    ChunkFailed     ChunkStatus = "failed"
    ChunkSkipped    ChunkStatus = "skipped"
)
```

## Key Components

### 1. DriftPlanGenerator

```go
type DriftPlanGenerator interface {
    // Analyzes preview output and state to create ordered adoption plan
    GeneratePlan(ctx context.Context, stack string) (*DriftPlan, error)
}

type driftPlanGenerator struct {
    previewParser  PreviewParser
    graphBuilder   DependencyGraphBuilder
    chunkGrouper   ChunkGrouper
}
```

**Implementation Steps**:
1. Run `pulumi preview --diff --json` to get drift
2. Parse preview output to identify drifted resources
3. Load state file and extract dependency graph
4. Topologically sort resources (process leaves first - resources with no dependents)
5. Group related changes into chunks
6. Serialize to `drift-plan.json`

**Chunking Strategy**:
- Group resources by dependency level
- Keep chunks small (1-5 resources)
- Resources with same dependencies can be in same chunk
- Related property changes grouped together

### 2. ChunkAdopter

```go
type ChunkAdopter interface {
    // Processes a single drift chunk
    AdoptChunk(ctx context.Context, plan *DriftPlan, chunkID string) (*AdoptionResult, error)
}

type AdoptionResult struct {
    ChunkID         string
    Status          ChunkStatus
    CodeChanges     []FileChange
    PreviewOutput   string
    CompileSuccess  bool
    DiffMatches     bool
    ErrorMessage    string
    Suggestions     []string
}

type FileChange struct {
    FilePath string
    OldCode  string
    NewCode  string
}
```

**Implementation Steps**:
1. Extract chunk from plan
2. Read current source code for affected resources
3. Prepare LLM prompt with:
   - Current code
   - Expected property changes
   - Resource type schema
   - Language-specific context
4. Call LLM to generate code changes
5. Apply LLM-generated code changes to files
6. Validate compilation (language-specific)
7. Run `pulumi preview --diff`
8. Compare preview with expected chunk diff
9. Update chunk status in plan
10. Return result

### 3. CompilationValidator

```go
type CompilationValidator interface {
    // Validates code compiles/is syntactically correct
    Validate(ctx context.Context, projectPath string) (*ValidationResult, error)
}

type ValidationResult struct {
    Success bool
    Errors  []CompilationError
}

type CompilationError struct {
    File    string
    Line    int
    Column  int
    Message string
}
```

**Language-Specific Implementations**:

**TypeScript**:
```go
type TypeScriptValidator struct{}

func (v *TypeScriptValidator) Validate(ctx context.Context, projectPath string) (*ValidationResult, error) {
    // Run: tsc --noEmit
    // Parse error output
    // Return structured errors
}
```

**Python**:
```go
type PythonValidator struct{}

func (v *PythonValidator) Validate(ctx context.Context, projectPath string) (*ValidationResult, error) {
    // Run: python -m py_compile or mypy
    // Parse error output
    // Return structured errors
}
```

**Go**:
```go
type GoValidator struct{}

func (v *GoValidator) Validate(ctx context.Context, projectPath string) (*ValidationResult, error) {
    // Run: go build
    // Parse error output
    // Return structured errors
}
```

### 4. DiffMatcher

```go
type DiffMatcher interface {
    // Compares actual preview diff with expected chunk diff
    Matches(expected []ResourceDiff, actual string) (*MatchResult, error)
}

type MatchResult struct {
    Matches           bool
    MissingChanges    []PropChange  // Expected but not in actual
    UnexpectedChanges []PropChange  // In actual but not expected
    MatchedResources  []string      // URNs that matched
}
```

**Implementation**:
- Parse `pulumi preview --diff` output
- Extract resource URNs and property changes
- Compare with expected chunk diff
- Validate:
  - Expected resources show changes
  - Property paths match
  - Change directions match (add/delete/update)
  - Values match (with tolerance for minor variations)

**Fuzzy Matching**:
- Allow minor formatting differences
- Handle type coercion (string "true" vs bool true)
- Tolerance for floating point comparisons

### 5. DependencyGraph

```go
type DependencyGraph interface {
    // Builds graph from Pulumi state
    FromState(state *apitype.UntypedDeployment) (*Graph, error)
}

type Graph struct {
    Nodes map[string]*Node
    Edges map[string][]string  // URN -> []dependent URNs
}

type Node struct {
    URN          string
    Type         string
    Dependencies []string  // URNs this resource depends on
    Dependents   []string  // URNs that depend on this resource
}

func (g *Graph) TopologicalSort() ([]*Node, error) {
    // Returns nodes in dependency order (leaves first)
}
```

## Command Implementations

### `pulumi-drift-adopt next`

Sequential gate pattern similar to terraform-migrate:

```go
func next(planFile string) error {
    if !ensureStackExists() {
        return nil  // Printed suggestions, exit
    }

    if !ensureDriftDetectionConfigured() {
        return nil  // Suggests: pulumi config set ...
    }

    if !ensureRefreshCompleted() {
        return nil  // Suggests: pulumi refresh
    }

    if !ensureDriftPlanExists(planFile) {
        return nil  // Suggests: pulumi-drift-adopt generate-plan
    }

    if !ensurePlanIntegrity(planFile) {
        return nil  // Suggests: verify drift-plan.json
    }

    if !ensureChunksAdopted(planFile) {
        return nil  // Main loop - suggests adopt-chunk for next pending chunk
    }

    if !ensurePreviewClean() {
        return nil  // Suggests: pulumi preview --diff
    }

    fmt.Println("STOP - Drift adoption complete")
    fmt.Println("\nNext steps:")
    fmt.Println("  1. Review changes: git diff")
    fmt.Println("  2. Test: pulumi preview")
    fmt.Println("  3. Create PR: gh pr create")
    return nil
}
```

### `pulumi-drift-adopt generate-plan`

```go
func generatePlan(stack string, output string) error {
    // 1. Verify stack exists
    // 2. Check drift detection configured
    // 3. Run pulumi refresh (if needed)
    // 4. Run pulumi preview --diff --json
    // 5. Parse drift from preview
    // 6. Load state file, extract dependency graph
    // 7. Topologically sort resources
    // 8. Group into chunks
    // 9. Write drift-plan.json

    fmt.Printf("Generated drift adoption plan: %s\n", output)
    fmt.Printf("Total chunks: %d\n", plan.TotalChunks)
    fmt.Printf("\nNext: pulumi-drift-adopt next\n")
    return nil
}
```

### `pulumi-drift-adopt adopt-chunk`

```go
func adoptChunk(planFile, chunkID string) error {
    plan := loadPlan(planFile)
    chunk := plan.GetChunk(chunkID)

    // 1. Read source files for resources in chunk
    sourceCode := readSourceFiles(chunk.Resources)

    // 2. Prepare LLM prompt with diff context
    prompt := buildPrompt(chunk, sourceCode)

    // 3. Call LLM to generate code changes
    codeChanges := callLLM(prompt)

    // 4. Apply changes to files
    applyChanges(codeChanges)

    // 5. Validate compilation
    validationResult := validator.Validate(ctx, projectPath)
    if !validationResult.Success {
        chunk.Status = ChunkFailed
        chunk.LastError = formatErrors(validationResult.Errors)
        savePlan(plan)
        return fmt.Errorf("compilation failed")
    }

    // 6. Run pulumi preview
    previewOutput := runPreview(ctx)

    // 7. Validate preview matches expected diff
    matchResult := diffMatcher.Matches(chunk.Resources, previewOutput)
    if !matchResult.Matches {
        chunk.Status = ChunkFailed
        chunk.LastError = formatMatchErrors(matchResult)
        savePlan(plan)
        return fmt.Errorf("preview diff mismatch")
    }

    // 8. Update chunk status
    chunk.Status = ChunkCompleted
    savePlan(plan)

    fmt.Printf("✓ Chunk %s adopted successfully\n", chunkID)
    fmt.Printf("\nNext: pulumi-drift-adopt next\n")
    return nil
}
```

### `pulumi-drift-adopt status`

```go
func status(planFile string) error {
    plan := loadPlan(planFile)

    // Display summary
    fmt.Printf("Drift Adoption Status\n")
    fmt.Printf("Stack: %s\n", plan.Stack)
    fmt.Printf("Generated: %s\n\n", plan.GeneratedAt.Format(time.RFC3339))

    // Count by status
    counts := plan.CountByStatus()
    fmt.Printf("Progress: %d/%d chunks completed\n", counts[ChunkCompleted], plan.TotalChunks)
    fmt.Printf("  Completed: %d\n", counts[ChunkCompleted])
    fmt.Printf("  Pending:   %d\n", counts[ChunkPending])
    fmt.Printf("  Failed:    %d\n", counts[ChunkFailed])
    fmt.Printf("  Skipped:   %d\n", counts[ChunkSkipped])

    // Show next chunk to process
    nextChunk := plan.GetNextPendingChunk()
    if nextChunk != nil {
        fmt.Printf("\nNext chunk: %s\n", nextChunk.ID)
        fmt.Printf("  Resources: %d\n", len(nextChunk.Resources))
        for _, r := range nextChunk.Resources {
            fmt.Printf("    - %s (%s)\n", r.Name, r.DiffType)
        }
        fmt.Printf("\nRun: pulumi-drift-adopt adopt-chunk %s %s\n", planFile, nextChunk.ID)
    }

    // Show recent failures
    failed := plan.GetFailedChunks()
    if len(failed) > 0 {
        fmt.Printf("\nFailed chunks:\n")
        for _, chunk := range failed {
            fmt.Printf("  %s: %s\n", chunk.ID, chunk.LastError)
        }
    }

    return nil
}
```

### `pulumi-drift-adopt skip`

```go
func skip(planFile, chunkID string, reason string) error {
    plan := loadPlan(planFile)
    chunk := plan.GetChunk(chunkID)

    chunk.Status = ChunkSkipped
    chunk.LastError = fmt.Sprintf("Skipped by user: %s", reason)
    savePlan(plan)

    fmt.Printf("Chunk %s marked as skipped\n", chunkID)
    fmt.Printf("\nNext: pulumi-drift-adopt next\n")
    return nil
}
```

### `pulumi-drift-adopt reset-chunk`

```go
func resetChunk(planFile, chunkID string) error {
    plan := loadPlan(planFile)
    chunk := plan.GetChunk(chunkID)

    chunk.Status = ChunkPending
    chunk.Attempt = 0
    chunk.LastError = ""
    savePlan(plan)

    fmt.Printf("Chunk %s reset to pending\n", chunkID)
    fmt.Printf("\nNext: pulumi-drift-adopt adopt-chunk %s %s\n", planFile, chunkID)
    return nil
}
```

## LLM Integration

The tool integrates with LLMs at two levels:

### 1. Orchestration (External LLM)

The LLM user repeatedly calls `next` and follows suggestions:

```bash
# LLM workflow:
$ pulumi-drift-adopt next

> Next step: Generate drift adoption plan
> Run: pulumi-drift-adopt generate-plan --stack dev --output drift-plan.json

$ pulumi-drift-adopt generate-plan --stack dev --output drift-plan.json

> Generated drift adoption plan: drift-plan.json
> Total chunks: 5
> Next: pulumi-drift-adopt next

$ pulumi-drift-adopt next

> Next step: Adopt chunk-001
> Resources: aws:s3/bucket:Bucket (update)
> Run: pulumi-drift-adopt adopt-chunk drift-plan.json chunk-001

# ... and so on until STOP
```

### 2. Code Generation (Internal LLM)

Inside `adopt-chunk`, the tool calls an LLM (Claude API) to generate code:

```go
type CodeGenerator struct {
    client *anthropic.Client
}

func (g *CodeGenerator) GenerateCode(prompt string) (string, error) {
    response, err := g.client.Messages.Create(ctx, anthropic.MessageCreateParams{
        Model: "claude-sonnet-4-5-20250929",
        Messages: []anthropic.Message{{
            Role: "user",
            Content: prompt,
        }},
        MaxTokens: 4096,
    })
    // Extract and return code
}

func buildPrompt(chunk *DriftChunk, sourceCode string) string {
    return fmt.Sprintf(`You are helping adopt infrastructure drift into Pulumi IaC.

Current source code:
%s

Required changes:
%s

Generate updated code that incorporates these changes.
Preserve existing structure and formatting.
Only modify the specified properties.

Return the complete updated code.`,
        sourceCode,
        formatChanges(chunk.Resources),
    )
}
```

## Error Handling

### Error Types

```go
type AdoptionError struct {
    ChunkID      string
    Phase        string  // "compilation", "preview", "diff-match", "llm"
    Message      string
    Suggestion   string  // Human-readable next step
    Recoverable  bool
}

func (e *AdoptionError) Error() string {
    return fmt.Sprintf("%s error in chunk %s: %s", e.Phase, e.ChunkID, e.Message)
}
```

### Error Handling Strategy

**Compilation Errors**:
```
Error: Compilation failed for chunk-003

  File: index.ts:42
  Error: Type 'string' is not assignable to type 'number'

Suggestions:
  1. Review generated code changes
  2. Fix manually and retry:
     pulumi-drift-adopt reset-chunk drift-plan.json chunk-003
     pulumi-drift-adopt adopt-chunk drift-plan.json chunk-003
  3. Skip this chunk:
     pulumi-drift-adopt skip drift-plan.json chunk-003 "manual fix needed"
```

**Preview Errors**:
```
Error: Preview failed for chunk-004

  error: Preview failed: missing required property 'region'

Suggestions:
  1. Check Pulumi state is up to date: pulumi refresh
  2. Verify resource configuration
  3. Skip and handle manually:
     pulumi-drift-adopt skip drift-plan.json chunk-004
```

**Diff Mismatch**:
```
Error: Preview diff doesn't match expected changes for chunk-002

Expected:
  ~ tags.Environment: "dev" => "production"

Actual:
  ~ tags.Environment: "dev" => "production"
  ~ tags.Owner: "alice" => "bob"  [UNEXPECTED]

Suggestions:
  1. Review unexpected changes
  2. If correct, regenerate plan:
     pulumi-drift-adopt generate-plan --stack dev --output drift-plan.json
  3. If incorrect, reset and retry:
     pulumi-drift-adopt reset-chunk drift-plan.json chunk-002
```

### Recovery Strategies

1. **Automatic Retry**: For transient errors (network, LLM timeout)
   - Track attempt counter
   - Exponential backoff
   - Max 3 attempts

2. **Manual Intervention**: For persistent errors
   - Mark chunk as failed
   - Provide detailed error message
   - Suggest manual fix or skip

3. **Rollback**: If changes break compilation
   - Revert code changes
   - Keep chunk as pending
   - Suggest different approach

## Testing Strategy

See TDD Development Plan section below for comprehensive testing approach.

---

# TDD Development Plan for pulumi-drift-adopt

## Overview

This plan follows strict Test-Driven Development principles:
- **Red**: Write failing test for next feature
- **Green**: Implement minimal code to pass
- **Refactor**: Clean up, optimize, improve design
- **Repeat**: Move to next feature

## Timeline: 9 Weeks

| Week | Phase | Deliverable |
|------|-------|-------------|
| 1 | Core types | DriftPlan data structures with tests |
| 2 | Dependency analysis | Graph builder, topological sort |
| 3 | Code generation | LLM integration, prompt templates |
| 4 | Compilation | Language validators (TS, Py, Go) |
| 5 | Diff matching | Preview comparison logic |
| 6 | Chunk adopter | Full adoption orchestration |
| 7 | CLI commands | User-facing commands |
| 8 | E2E testing | Integration tests with fixtures |
| 9 | Polish | Error messages, docs, examples |

---

## Phase 1: Core Data Structures (Week 1)

### Day 1-2: DriftPlan Types

**Test First**:
```go
// pkg/driftadopt/plan_test.go

func TestDriftPlan_Serialization(t *testing.T) {
    // Arrange
    plan := &DriftPlan{
        Stack:       "dev",
        GeneratedAt: time.Now(),
        TotalChunks: 2,
        Chunks: []DriftChunk{
            {ID: "chunk-001", Order: 0, Status: ChunkPending},
            {ID: "chunk-002", Order: 1, Status: ChunkPending},
        },
    }

    // Act
    data, err := json.Marshal(plan)
    require.NoError(t, err)

    var unmarshaled DriftPlan
    err = json.Unmarshal(data, &unmarshaled)
    require.NoError(t, err)

    // Assert
    assert.Equal(t, plan.Stack, unmarshaled.Stack)
    assert.Equal(t, plan.TotalChunks, unmarshaled.TotalChunks)
    assert.Len(t, unmarshaled.Chunks, 2)
}

func TestDriftChunk_Ordering(t *testing.T) {
    // Arrange
    chunks := []DriftChunk{
        {ID: "chunk-003", Order: 2},
        {ID: "chunk-001", Order: 0},
        {ID: "chunk-002", Order: 1},
    }

    // Act
    sort.Slice(chunks, func(i, j int) bool {
        return chunks[i].Order < chunks[j].Order
    })

    // Assert
    assert.Equal(t, "chunk-001", chunks[0].ID)
    assert.Equal(t, "chunk-002", chunks[1].ID)
    assert.Equal(t, "chunk-003", chunks[2].ID)
}

func TestResourceDiff_PropertyPaths(t *testing.T) {
    // Test nested property path parsing: "tags.Environment"
    diff := ResourceDiff{
        URN:  "urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket",
        Type: "aws:s3/bucket:Bucket",
        PropertyDiff: []PropChange{
            {
                Path:     "tags.Environment",
                OldValue: "dev",
                NewValue: "production",
                DiffKind: "update",
            },
        },
    }

    // Verify path parsing
    parts := strings.Split(diff.PropertyDiff[0].Path, ".")
    assert.Equal(t, []string{"tags", "Environment"}, parts)
}

func TestPropChange_Types(t *testing.T) {
    // Test different value types
    tests := []struct {
        name     string
        change   PropChange
        wantType string
    }{
        {
            name: "string",
            change: PropChange{
                Path:     "name",
                OldValue: "old",
                NewValue: "new",
            },
            wantType: "string",
        },
        {
            name: "number",
            change: PropChange{
                Path:     "count",
                OldValue: 1,
                NewValue: 2,
            },
            wantType: "int",
        },
        {
            name: "bool",
            change: PropChange{
                Path:     "enabled",
                OldValue: false,
                NewValue: true,
            },
            wantType: "bool",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Verify type handling in serialization
            data, err := json.Marshal(tt.change)
            require.NoError(t, err)

            var unmarshaled PropChange
            err = json.Unmarshal(data, &unmarshaled)
            require.NoError(t, err)

            assert.Equal(t, tt.change.OldValue, unmarshaled.OldValue)
            assert.Equal(t, tt.change.NewValue, unmarshaled.NewValue)
        })
    }
}
```

**Implementation**:
```go
// pkg/driftadopt/types.go

type DriftPlan struct {
    Stack       string       `json:"stack"`
    GeneratedAt time.Time    `json:"generatedAt"`
    TotalChunks int          `json:"totalChunks"`
    Chunks      []DriftChunk `json:"chunks"`
}

type DriftChunk struct {
    ID           string         `json:"id"`
    Order        int            `json:"order"`
    Resources    []ResourceDiff `json:"resources"`
    Status       ChunkStatus    `json:"status"`
    Dependencies []string       `json:"dependencies"`
    Attempt      int            `json:"attempt"`
    LastError    string         `json:"lastError,omitempty"`
}

type ResourceDiff struct {
    URN          string       `json:"urn"`
    Type         string       `json:"type"`
    Name         string       `json:"name"`
    DiffType     DiffType     `json:"diffType"`
    PropertyDiff []PropChange `json:"propertyDiff"`
    SourceFile   string       `json:"sourceFile"`
    SourceLine   int          `json:"sourceLine"`
}

type DiffType string

const (
    DiffTypeUpdate  DiffType = "update"
    DiffTypeDelete  DiffType = "delete"
    DiffTypeReplace DiffType = "replace"
)

type PropChange struct {
    Path     string      `json:"path"`
    OldValue interface{} `json:"oldValue"`
    NewValue interface{} `json:"newValue"`
    DiffKind string      `json:"diffKind"`
}

type ChunkStatus string

const (
    ChunkPending    ChunkStatus = "pending"
    ChunkInProgress ChunkStatus = "in_progress"
    ChunkCompleted  ChunkStatus = "completed"
    ChunkFailed     ChunkStatus = "failed"
    ChunkSkipped    ChunkStatus = "skipped"
)
```

### Day 3-4: Plan File Management

**Test First**:
```go
// pkg/driftadopt/planfile_test.go

func TestPlanFile_ReadWrite(t *testing.T) {
    // Arrange
    tempDir := t.TempDir()
    planPath := filepath.Join(tempDir, "drift-plan.json")

    plan := &DriftPlan{
        Stack:       "dev",
        GeneratedAt: time.Now(),
        TotalChunks: 1,
        Chunks: []DriftChunk{
            {ID: "chunk-001", Status: ChunkPending},
        },
    }

    // Act - Write
    err := WritePlanFile(planPath, plan)
    require.NoError(t, err)

    // Act - Read
    loaded, err := ReadPlanFile(planPath)
    require.NoError(t, err)

    // Assert
    assert.Equal(t, plan.Stack, loaded.Stack)
    assert.Equal(t, plan.TotalChunks, loaded.TotalChunks)
}

func TestPlanFile_UpdateChunkStatus(t *testing.T) {
    // Arrange
    tempDir := t.TempDir()
    planPath := filepath.Join(tempDir, "drift-plan.json")

    plan := &DriftPlan{
        Stack:       "dev",
        TotalChunks: 1,
        Chunks: []DriftChunk{
            {ID: "chunk-001", Status: ChunkPending},
        },
    }
    WritePlanFile(planPath, plan)

    // Act
    plan.Chunks[0].Status = ChunkCompleted
    err := WritePlanFile(planPath, plan)
    require.NoError(t, err)

    // Reload and verify
    loaded, err := ReadPlanFile(planPath)
    require.NoError(t, err)

    // Assert
    assert.Equal(t, ChunkCompleted, loaded.Chunks[0].Status)
}

func TestPlanFile_InvalidJSON(t *testing.T) {
    // Arrange
    tempDir := t.TempDir()
    planPath := filepath.Join(tempDir, "invalid.json")
    os.WriteFile(planPath, []byte("not json"), 0644)

    // Act
    _, err := ReadPlanFile(planPath)

    // Assert
    assert.Error(t, err)
}

func TestPlanFile_NotExists(t *testing.T) {
    // Act
    _, err := ReadPlanFile("/nonexistent/plan.json")

    // Assert
    assert.Error(t, err)
    assert.True(t, os.IsNotExist(err))
}
```

**Implementation**:
```go
// pkg/driftadopt/planfile.go

func ReadPlanFile(path string) (*DriftPlan, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, fmt.Errorf("read plan file: %w", err)
    }

    var plan DriftPlan
    if err := json.Unmarshal(data, &plan); err != nil {
        return nil, fmt.Errorf("unmarshal plan: %w", err)
    }

    return &plan, nil
}

func WritePlanFile(path string, plan *DriftPlan) error {
    data, err := json.MarshalIndent(plan, "", "  ")
    if err != nil {
        return fmt.Errorf("marshal plan: %w", err)
    }

    if err := os.WriteFile(path, data, 0644); err != nil {
        return fmt.Errorf("write plan file: %w", err)
    }

    return nil
}
```

### Day 5: Plan Helper Methods

**Test First**:
```go
// pkg/driftadopt/plan_methods_test.go

func TestDriftPlan_GetChunk(t *testing.T) {
    plan := &DriftPlan{
        Chunks: []DriftChunk{
            {ID: "chunk-001"},
            {ID: "chunk-002"},
        },
    }

    // Found
    chunk := plan.GetChunk("chunk-001")
    require.NotNil(t, chunk)
    assert.Equal(t, "chunk-001", chunk.ID)

    // Not found
    chunk = plan.GetChunk("chunk-999")
    assert.Nil(t, chunk)
}

func TestDriftPlan_GetNextPendingChunk(t *testing.T) {
    plan := &DriftPlan{
        Chunks: []DriftChunk{
            {ID: "chunk-001", Order: 0, Status: ChunkCompleted},
            {ID: "chunk-002", Order: 1, Status: ChunkPending},
            {ID: "chunk-003", Order: 2, Status: ChunkPending},
        },
    }

    chunk := plan.GetNextPendingChunk()
    require.NotNil(t, chunk)
    assert.Equal(t, "chunk-002", chunk.ID)
}

func TestDriftPlan_CountByStatus(t *testing.T) {
    plan := &DriftPlan{
        Chunks: []DriftChunk{
            {Status: ChunkCompleted},
            {Status: ChunkCompleted},
            {Status: ChunkPending},
            {Status: ChunkFailed},
        },
    }

    counts := plan.CountByStatus()
    assert.Equal(t, 2, counts[ChunkCompleted])
    assert.Equal(t, 1, counts[ChunkPending])
    assert.Equal(t, 1, counts[ChunkFailed])
}

func TestDriftPlan_GetFailedChunks(t *testing.T) {
    plan := &DriftPlan{
        Chunks: []DriftChunk{
            {ID: "chunk-001", Status: ChunkCompleted},
            {ID: "chunk-002", Status: ChunkFailed, LastError: "error 1"},
            {ID: "chunk-003", Status: ChunkFailed, LastError: "error 2"},
        },
    }

    failed := plan.GetFailedChunks()
    assert.Len(t, failed, 2)
    assert.Equal(t, "chunk-002", failed[0].ID)
}
```

**Implementation**:
```go
// pkg/driftadopt/plan_methods.go

func (p *DriftPlan) GetChunk(id string) *DriftChunk {
    for i := range p.Chunks {
        if p.Chunks[i].ID == id {
            return &p.Chunks[i]
        }
    }
    return nil
}

func (p *DriftPlan) GetNextPendingChunk() *DriftChunk {
    for i := range p.Chunks {
        if p.Chunks[i].Status == ChunkPending {
            return &p.Chunks[i]
        }
    }
    return nil
}

func (p *DriftPlan) CountByStatus() map[ChunkStatus]int {
    counts := make(map[ChunkStatus]int)
    for _, chunk := range p.Chunks {
        counts[chunk.Status]++
    }
    return counts
}

func (p *DriftPlan) GetFailedChunks() []*DriftChunk {
    var failed []*DriftChunk
    for i := range p.Chunks {
        if p.Chunks[i].Status == ChunkFailed {
            failed = append(failed, &p.Chunks[i])
        }
    }
    return failed
}
```

---

## Phase 2: Dependency Graph Analysis (Week 2)

### Day 1-3: State File Parsing and Graph Building

**Test First**:
```go
// pkg/driftadopt/graph_test.go

func TestGraph_FromStateFile(t *testing.T) {
    // Arrange - Load fixture state file
    stateData := loadFixture(t, "testdata/simple-state.json")

    // Act
    graph, err := BuildGraphFromState(stateData)
    require.NoError(t, err)

    // Assert
    assert.Len(t, graph.Nodes, 3)

    // Verify specific node
    node := graph.Nodes["urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket"]
    require.NotNil(t, node)
    assert.Equal(t, "aws:s3/bucket:Bucket", node.Type)
}

func TestGraph_TopologicalSort(t *testing.T) {
    // Arrange - Create DAG
    // A depends on nothing (leaf)
    // B depends on A
    // C depends on B
    graph := &Graph{
        Nodes: map[string]*Node{
            "urn:A": {URN: "urn:A", Dependencies: []string{}},
            "urn:B": {URN: "urn:B", Dependencies: []string{"urn:A"}},
            "urn:C": {URN: "urn:C", Dependencies: []string{"urn:B"}},
        },
    }
    graph.buildEdges()

    // Act
    sorted, err := graph.TopologicalSort()
    require.NoError(t, err)

    // Assert - Leaves come first
    assert.Len(t, sorted, 3)
    assert.Equal(t, "urn:A", sorted[0].URN) // Leaf first
    assert.Equal(t, "urn:B", sorted[1].URN)
    assert.Equal(t, "urn:C", sorted[2].URN)
}

func TestGraph_CycleDetection(t *testing.T) {
    // Arrange - Create cycle: A -> B -> C -> A
    graph := &Graph{
        Nodes: map[string]*Node{
            "urn:A": {URN: "urn:A", Dependencies: []string{"urn:C"}},
            "urn:B": {URN: "urn:B", Dependencies: []string{"urn:A"}},
            "urn:C": {URN: "urn:C", Dependencies: []string{"urn:B"}},
        },
    }
    graph.buildEdges()

    // Act
    _, err := graph.TopologicalSort()

    // Assert
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "cycle")
}

func TestGraph_MultipleLeaves(t *testing.T) {
    // Arrange - A and B are leaves, C depends on both
    graph := &Graph{
        Nodes: map[string]*Node{
            "urn:A": {URN: "urn:A", Dependencies: []string{}},
            "urn:B": {URN: "urn:B", Dependencies: []string{}},
            "urn:C": {URN: "urn:C", Dependencies: []string{"urn:A", "urn:B"}},
        },
    }
    graph.buildEdges()

    // Act
    sorted, err := graph.TopologicalSort()
    require.NoError(t, err)

    // Assert - Both leaves come before C
    assert.Len(t, sorted, 3)
    leafURNs := []string{sorted[0].URN, sorted[1].URN}
    assert.Contains(t, leafURNs, "urn:A")
    assert.Contains(t, leafURNs, "urn:B")
    assert.Equal(t, "urn:C", sorted[2].URN)
}
```

**Implementation**:
```go
// pkg/driftadopt/graph.go

type Graph struct {
    Nodes map[string]*Node
    Edges map[string][]string // URN -> []dependent URNs
}

type Node struct {
    URN          string
    Type         string
    Dependencies []string // URNs this resource depends on
    Dependents   []string // URNs that depend on this resource
}

func BuildGraphFromState(stateJSON []byte) (*Graph, error) {
    var deployment apitype.UntypedDeployment
    if err := json.Unmarshal(stateJSON, &deployment); err != nil {
        return nil, fmt.Errorf("unmarshal state: %w", err)
    }

    graph := &Graph{
        Nodes: make(map[string]*Node),
        Edges: make(map[string][]string),
    }

    // Build nodes from resources
    for _, res := range deployment.Resources {
        node := &Node{
            URN:          string(res.URN),
            Type:         string(res.Type),
            Dependencies: make([]string, len(res.Dependencies)),
        }
        for i, dep := range res.Dependencies {
            node.Dependencies[i] = string(dep)
        }
        graph.Nodes[node.URN] = node
    }

    // Build edges (dependents)
    graph.buildEdges()

    return graph, nil
}

func (g *Graph) buildEdges() {
    for urn, node := range g.Nodes {
        for _, depURN := range node.Dependencies {
            // depURN is a dependency of urn
            // So urn is a dependent of depURN
            g.Edges[depURN] = append(g.Edges[depURN], urn)

            // Also track in node
            if depNode := g.Nodes[depURN]; depNode != nil {
                depNode.Dependents = append(depNode.Dependents, urn)
            }
        }
    }
}

func (g *Graph) TopologicalSort() ([]*Node, error) {
    // Kahn's algorithm
    inDegree := make(map[string]int)
    for urn, node := range g.Nodes {
        inDegree[urn] = len(node.Dependencies)
    }

    // Find all leaves (no dependencies)
    var queue []string
    for urn, degree := range inDegree {
        if degree == 0 {
            queue = append(queue, urn)
        }
    }

    var sorted []*Node
    for len(queue) > 0 {
        // Dequeue
        urn := queue[0]
        queue = queue[1:]
        sorted = append(sorted, g.Nodes[urn])

        // Reduce in-degree of dependents
        for _, depURN := range g.Edges[urn] {
            inDegree[depURN]--
            if inDegree[depURN] == 0 {
                queue = append(queue, depURN)
            }
        }
    }

    // Check for cycle
    if len(sorted) != len(g.Nodes) {
        return nil, fmt.Errorf("cycle detected in dependency graph")
    }

    return sorted, nil
}
```

### Day 4-5: Preview Parsing

**Test First**:
```go
// pkg/driftadopt/preview_test.go

func TestPreviewParser_ParseDiff(t *testing.T) {
    // Arrange
    previewOutput := loadFixture(t, "testdata/preview-with-drift.txt")
    parser := NewPreviewParser()

    // Act
    diffs, err := parser.ParseDiff(previewOutput)
    require.NoError(t, err)

    // Assert
    assert.Len(t, diffs, 2)

    // Check first diff
    diff := diffs[0]
    assert.Equal(t, "urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket", diff.URN)
    assert.Equal(t, DiffTypeUpdate, diff.DiffType)
    assert.Len(t, diff.PropertyDiff, 1)
}

func TestPreviewParser_PropertyChanges(t *testing.T) {
    // Arrange
    previewOutput := `
    ~ aws:s3/bucket:Bucket: (update)
        [urn=urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket]
        ~ tags: {
            ~ Environment: "dev" => "production"
          }
    `
    parser := NewPreviewParser()

    // Act
    diffs, err := parser.ParseDiff(previewOutput)
    require.NoError(t, err)

    // Assert
    require.Len(t, diffs, 1)
    diff := diffs[0]
    require.Len(t, diff.PropertyDiff, 1)

    prop := diff.PropertyDiff[0]
    assert.Equal(t, "tags.Environment", prop.Path)
    assert.Equal(t, "dev", prop.OldValue)
    assert.Equal(t, "production", prop.NewValue)
    assert.Equal(t, "update", prop.DiffKind)
}

func TestPreviewParser_DeleteResource(t *testing.T) {
    previewOutput := `
    - aws:s3/bucket:Bucket: (delete)
        [urn=urn:pulumi:dev::test::aws:s3/bucket:Bucket::old-bucket]
    `
    parser := NewPreviewParser()

    diffs, err := parser.ParseDiff(previewOutput)
    require.NoError(t, err)

    require.Len(t, diffs, 1)
    assert.Equal(t, DiffTypeDelete, diffs[0].DiffType)
}

func TestPreviewParser_ReplaceResource(t *testing.T) {
    previewOutput := `
    +-aws:s3/bucket:Bucket: (replace)
        [urn=urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket]
    `
    parser := NewPreviewParser()

    diffs, err := parser.ParseDiff(previewOutput)
    require.NoError(t, err)

    require.Len(t, diffs, 1)
    assert.Equal(t, DiffTypeReplace, diffs[0].DiffType)
}
```

**Implementation**:
```go
// pkg/driftadopt/preview.go

type PreviewParser struct{}

func NewPreviewParser() *PreviewParser {
    return &PreviewParser{}
}

func (p *PreviewParser) ParseDiff(output string) ([]ResourceDiff, error) {
    // Implementation would parse pulumi preview output
    // This is simplified - real implementation would be more robust

    var diffs []ResourceDiff
    lines := strings.Split(output, "\n")

    var currentDiff *ResourceDiff
    for _, line := range lines {
        // Detect resource line
        if strings.HasPrefix(line, "    ~") ||
           strings.HasPrefix(line, "    -") ||
           strings.HasPrefix(line, "    +-") {

            // Start new diff
            currentDiff = &ResourceDiff{}

            // Parse diff type
            if strings.HasPrefix(line, "    ~") {
                currentDiff.DiffType = DiffTypeUpdate
            } else if strings.HasPrefix(line, "    -") {
                currentDiff.DiffType = DiffTypeDelete
            } else {
                currentDiff.DiffType = DiffTypeReplace
            }

            // Parse type
            parts := strings.Split(line, ":")
            if len(parts) >= 2 {
                currentDiff.Type = strings.TrimSpace(parts[0])
            }

            diffs = append(diffs, *currentDiff)
        }

        // Parse URN
        if strings.Contains(line, "[urn=") {
            urnStart := strings.Index(line, "urn=")
            urnEnd := strings.Index(line[urnStart:], "]")
            if urnStart >= 0 && urnEnd >= 0 {
                urn := line[urnStart+4 : urnStart+urnEnd]
                if currentDiff != nil {
                    currentDiff.URN = urn
                }
            }
        }

        // Parse property changes
        if strings.Contains(line, "=>") {
            // Parse property path and values
            // Example: "~ Environment: \"dev\" => \"production\""
            // This would need more sophisticated parsing
        }
    }

    return diffs, nil
}
```

---

## Phase 3: Code Generation (Week 3)

### Day 1-2: LLM Integration

**Test First**:
```go
// pkg/driftadopt/codegen_test.go

func TestCodeGenerator_BuildPrompt(t *testing.T) {
    // Arrange
    chunk := &DriftChunk{
        ID: "chunk-001",
        Resources: []ResourceDiff{
            {
                URN:  "urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket",
                Type: "aws:s3/bucket:Bucket",
                Name: "my-bucket",
                PropertyDiff: []PropChange{
                    {
                        Path:     "tags.Environment",
                        OldValue: "dev",
                        NewValue: "production",
                        DiffKind: "update",
                    },
                },
            },
        },
    }

    sourceCode := `new aws.s3.Bucket("my-bucket", {
    tags: { Environment: "dev" }
});`

    gen := NewCodeGenerator(nil) // nil client for testing

    // Act
    prompt := gen.BuildPrompt(chunk, sourceCode)

    // Assert
    assert.Contains(t, prompt, "my-bucket")
    assert.Contains(t, prompt, "tags.Environment")
    assert.Contains(t, prompt, "production")
    assert.Contains(t, prompt, sourceCode)
}

func TestCodeGenerator_MockLLM(t *testing.T) {
    // Arrange
    mockLLM := &MockLLMClient{
        Response: `new aws.s3.Bucket("my-bucket", {
    tags: { Environment: "production" }
});`,
    }

    gen := NewCodeGenerator(mockLLM)
    chunk := &DriftChunk{/* ... */}
    sourceCode := "/* old code */"

    // Act
    newCode, err := gen.GenerateCode(chunk, sourceCode)
    require.NoError(t, err)

    // Assert
    assert.Contains(t, newCode, "production")
    assert.NotContains(t, newCode, "dev")
}

func TestCodeGenerator_LLMError(t *testing.T) {
    // Arrange
    mockLLM := &MockLLMClient{
        Error: fmt.Errorf("API error"),
    }

    gen := NewCodeGenerator(mockLLM)

    // Act
    _, err := gen.GenerateCode(&DriftChunk{}, "code")

    // Assert
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "API error")
}
```

**Implementation**:
```go
// pkg/driftadopt/codegen.go

type LLMClient interface {
    Generate(prompt string) (string, error)
}

type CodeGenerator struct {
    client LLMClient
}

func NewCodeGenerator(client LLMClient) *CodeGenerator {
    return &CodeGenerator{client: client}
}

func (g *CodeGenerator) BuildPrompt(chunk *DriftChunk, sourceCode string) string {
    var sb strings.Builder

    sb.WriteString("You are helping adopt infrastructure drift into Pulumi IaC.\n\n")
    sb.WriteString("Current source code:\n")
    sb.WriteString("```\n")
    sb.WriteString(sourceCode)
    sb.WriteString("\n```\n\n")

    sb.WriteString("Required changes:\n")
    for _, res := range chunk.Resources {
        sb.WriteString(fmt.Sprintf("\nResource: %s (%s)\n", res.Name, res.Type))
        for _, prop := range res.PropertyDiff {
            sb.WriteString(fmt.Sprintf("  - %s: %v => %v (%s)\n",
                prop.Path, prop.OldValue, prop.NewValue, prop.DiffKind))
        }
    }

    sb.WriteString("\nGenerate updated code that incorporates these changes.\n")
    sb.WriteString("Preserve existing structure and formatting.\n")
    sb.WriteString("Only modify the specified properties.\n")
    sb.WriteString("\nReturn the complete updated code.\n")

    return sb.String()
}

func (g *CodeGenerator) GenerateCode(chunk *DriftChunk, sourceCode string) (string, error) {
    prompt := g.BuildPrompt(chunk, sourceCode)

    newCode, err := g.client.Generate(prompt)
    if err != nil {
        return "", fmt.Errorf("LLM generation failed: %w", err)
    }

    return newCode, nil
}

// Mock implementation for testing
type MockLLMClient struct {
    Response string
    Error    error
}

func (m *MockLLMClient) Generate(prompt string) (string, error) {
    if m.Error != nil {
        return "", m.Error
    }
    return m.Response, nil
}
```

### Day 3: Anthropic Claude Integration

**Test First**:
```go
// pkg/driftadopt/claude_test.go

func TestClaudeClient_Generate(t *testing.T) {
    // Skip if no API key
    apiKey := os.Getenv("ANTHROPIC_API_KEY")
    if apiKey == "" {
        t.Skip("ANTHROPIC_API_KEY not set")
    }

    // Arrange
    client := NewClaudeClient(apiKey)
    prompt := "Write a simple TypeScript function that adds two numbers"

    // Act
    response, err := client.Generate(prompt)
    require.NoError(t, err)

    // Assert
    assert.NotEmpty(t, response)
    assert.Contains(t, response, "function")
}

func TestClaudeClient_InvalidAPIKey(t *testing.T) {
    // Arrange
    client := NewClaudeClient("invalid-key")

    // Act
    _, err := client.Generate("test")

    // Assert
    assert.Error(t, err)
}
```

**Implementation**:
```go
// pkg/driftadopt/claude.go

import (
    "github.com/anthropics/anthropic-sdk-go"
)

type ClaudeClient struct {
    client *anthropic.Client
}

func NewClaudeClient(apiKey string) *ClaudeClient {
    client := anthropic.NewClient(
        option.WithAPIKey(apiKey),
    )
    return &ClaudeClient{client: client}
}

func (c *ClaudeClient) Generate(prompt string) (string, error) {
    message, err := c.client.Messages.New(context.TODO(), anthropic.MessageNewParams{
        Model:     anthropic.F(anthropic.ModelClaudeSonnet4_5_20250929),
        MaxTokens: anthropic.F(int64(4096)),
        Messages: anthropic.F([]anthropic.MessageParam{
            anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
        }),
    })

    if err != nil {
        return "", fmt.Errorf("Claude API call failed: %w", err)
    }

    // Extract text from response
    if len(message.Content) == 0 {
        return "", fmt.Errorf("empty response from Claude")
    }

    textBlock := message.Content[0]
    return textBlock.Text, nil
}
```

### Day 4-5: Source Code Manipulation

**Test First**:
```go
// pkg/driftadopt/editor/typescript_test.go

func TestTypeScriptEditor_ApplyChange(t *testing.T) {
    // Arrange
    sourceCode := `const bucket = new aws.s3.Bucket("my-bucket", {
    tags: { Environment: "dev" }
});`

    newCode := `const bucket = new aws.s3.Bucket("my-bucket", {
    tags: { Environment: "production" }
});`

    editor := NewTypeScriptEditor()

    // Act
    result, err := editor.ApplyChange(sourceCode, newCode)
    require.NoError(t, err)

    // Assert
    assert.Contains(t, result, "production")
}

func TestTypeScriptEditor_PreserveFormatting(t *testing.T) {
    // Arrange - Code with specific indentation and comments
    sourceCode := `// My bucket
const bucket = new aws.s3.Bucket("my-bucket", {
    // Environment tag
    tags: {
        Environment: "dev",
    },
});`

    editor := NewTypeScriptEditor()

    // Act - Simple property update
    newCode := strings.Replace(sourceCode, "dev", "production", 1)
    result, err := editor.ApplyChange(sourceCode, newCode)
    require.NoError(t, err)

    // Assert - Comments and formatting preserved
    assert.Contains(t, result, "// My bucket")
    assert.Contains(t, result, "// Environment tag")
}
```

**Implementation**:
```go
// pkg/driftadopt/editor/typescript.go

type TypeScriptEditor struct{}

func NewTypeScriptEditor() *TypeScriptEditor {
    return &TypeScriptEditor{}
}

func (e *TypeScriptEditor) ApplyChange(oldCode, newCode string) (string, error) {
    // For now, simple replacement
    // In future, could use AST parsing for more sophisticated edits
    return newCode, nil
}

// TODO: Implement AST-based editing
// - Parse with esprima or similar
// - Apply targeted changes
// - Preserve formatting
```

---

## Phase 4: Compilation Validation (Week 4)

### Day 1-2: TypeScript Validator

**Test First**:
```go
// pkg/driftadopt/validator/typescript_test.go

func TestTypeScriptValidator_ValidCode(t *testing.T) {
    // Arrange
    tempDir := t.TempDir()
    createTypeScriptProject(t, tempDir, `
const x: number = 42;
console.log(x);
`)

    validator := NewTypeScriptValidator()

    // Act
    result, err := validator.Validate(context.Background(), tempDir)
    require.NoError(t, err)

    // Assert
    assert.True(t, result.Success)
    assert.Empty(t, result.Errors)
}

func TestTypeScriptValidator_InvalidCode(t *testing.T) {
    // Arrange
    tempDir := t.TempDir()
    createTypeScriptProject(t, tempDir, `
const x: number = "not a number";  // Type error
`)

    validator := NewTypeScriptValidator()

    // Act
    result, err := validator.Validate(context.Background(), tempDir)
    require.NoError(t, err)

    // Assert
    assert.False(t, result.Success)
    assert.NotEmpty(t, result.Errors)
    assert.Contains(t, result.Errors[0].Message, "Type")
}

func TestTypeScriptValidator_MissingTSC(t *testing.T) {
    // Arrange - Mock environment without tsc
    validator := NewTypeScriptValidator()
    validator.tscPath = "/nonexistent/tsc"

    // Act
    _, err := validator.Validate(context.Background(), t.TempDir())

    // Assert
    assert.Error(t, err)
}

func createTypeScriptProject(t *testing.T, dir string, code string) {
    // Write index.ts
    os.WriteFile(filepath.Join(dir, "index.ts"), []byte(code), 0644)

    // Write tsconfig.json
    tsconfig := `{
  "compilerOptions": {
    "target": "ES2020",
    "module": "commonjs",
    "strict": true
  }
}`
    os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(tsconfig), 0644)
}
```

**Implementation**:
```go
// pkg/driftadopt/validator/typescript.go

type TypeScriptValidator struct {
    tscPath string
}

func NewTypeScriptValidator() *TypeScriptValidator {
    // Find tsc in PATH or node_modules
    tscPath := findTSC()
    return &TypeScriptValidator{tscPath: tscPath}
}

func (v *TypeScriptValidator) Validate(ctx context.Context, projectPath string) (*ValidationResult, error) {
    // Run: tsc --noEmit
    cmd := exec.CommandContext(ctx, v.tscPath, "--noEmit")
    cmd.Dir = projectPath

    output, err := cmd.CombinedOutput()

    // tsc returns non-zero on errors
    if err != nil {
        // Parse errors from output
        errors := v.parseErrors(string(output))
        return &ValidationResult{
            Success: false,
            Errors:  errors,
        }, nil
    }

    return &ValidationResult{Success: true}, nil
}

func (v *TypeScriptValidator) parseErrors(output string) []CompilationError {
    var errors []CompilationError

    // Parse tsc error format:
    // index.ts(2,7): error TS2322: Type 'string' is not assignable to type 'number'.
    lines := strings.Split(output, "\n")
    for _, line := range lines {
        if strings.Contains(line, "error TS") {
            err := v.parseErrorLine(line)
            if err != nil {
                errors = append(errors, *err)
            }
        }
    }

    return errors
}

func (v *TypeScriptValidator) parseErrorLine(line string) *CompilationError {
    // Parse: "index.ts(2,7): error TS2322: message"
    parts := strings.SplitN(line, ":", 3)
    if len(parts) < 3 {
        return nil
    }

    // Extract file and location
    filePart := parts[0]
    message := strings.TrimSpace(parts[2])

    // Parse file(line,col)
    if idx := strings.Index(filePart, "("); idx >= 0 {
        file := filePart[:idx]
        locationStr := filePart[idx+1 : len(filePart)-1]
        locParts := strings.Split(locationStr, ",")

        line := 0
        col := 0
        if len(locParts) >= 1 {
            fmt.Sscanf(locParts[0], "%d", &line)
        }
        if len(locParts) >= 2 {
            fmt.Sscanf(locParts[1], "%d", &col)
        }

        return &CompilationError{
            File:    file,
            Line:    line,
            Column:  col,
            Message: message,
        }
    }

    return nil
}

func findTSC() string {
    // Try npx tsc first
    if path, err := exec.LookPath("npx"); err == nil {
        return path + " tsc"
    }

    // Try global tsc
    if path, err := exec.LookPath("tsc"); err == nil {
        return path
    }

    return "tsc" // Fallback, will error if not found
}
```

### Day 3: Python Validator

**Test First**:
```go
// pkg/driftadopt/validator/python_test.go

func TestPythonValidator_ValidCode(t *testing.T) {
    tempDir := t.TempDir()
    createPythonProject(t, tempDir, `
import pulumi_aws as aws

bucket = aws.s3.Bucket("my-bucket")
`)

    validator := NewPythonValidator()
    result, err := validator.Validate(context.Background(), tempDir)
    require.NoError(t, err)
    assert.True(t, result.Success)
}

func TestPythonValidator_SyntaxError(t *testing.T) {
    tempDir := t.TempDir()
    createPythonProject(t, tempDir, `
def foo(:  # Syntax error
    pass
`)

    validator := NewPythonValidator()
    result, err := validator.Validate(context.Background(), tempDir)
    require.NoError(t, err)
    assert.False(t, result.Success)
}

func createPythonProject(t *testing.T, dir string, code string) {
    os.WriteFile(filepath.Join(dir, "__main__.py"), []byte(code), 0644)
}
```

**Implementation**:
```go
// pkg/driftadopt/validator/python.go

type PythonValidator struct{}

func NewPythonValidator() *PythonValidator {
    return &PythonValidator{}
}

func (v *PythonValidator) Validate(ctx context.Context, projectPath string) (*ValidationResult, error) {
    // Run: python -m py_compile *.py
    cmd := exec.CommandContext(ctx, "python3", "-m", "py_compile", "__main__.py")
    cmd.Dir = projectPath

    output, err := cmd.CombinedOutput()

    if err != nil {
        errors := v.parseErrors(string(output))
        return &ValidationResult{
            Success: false,
            Errors:  errors,
        }, nil
    }

    return &ValidationResult{Success: true}, nil
}

func (v *PythonValidator) parseErrors(output string) []CompilationError {
    // Parse Python syntax errors
    // Similar to TypeScript parser
    var errors []CompilationError
    // Implementation details...
    return errors
}
```

### Day 4: Go Validator

**Test First**:
```go
// pkg/driftadopt/validator/go_test.go

func TestGoValidator_ValidCode(t *testing.T) {
    tempDir := t.TempDir()
    createGoProject(t, tempDir, `
package main

import "github.com/pulumi/pulumi/sdk/v3/go/pulumi"

func main() {
    pulumi.Run(func(ctx *pulumi.Context) error {
        return nil
    })
}
`)

    validator := NewGoValidator()
    result, err := validator.Validate(context.Background(), tempDir)
    require.NoError(t, err)
    assert.True(t, result.Success)
}

func TestGoValidator_CompileError(t *testing.T) {
    tempDir := t.TempDir()
    createGoProject(t, tempDir, `
package main

func main() {
    x := "string"
    var y int = x  // Type error
}
`)

    validator := NewGoValidator()
    result, err := validator.Validate(context.Background(), tempDir)
    require.NoError(t, err)
    assert.False(t, result.Success)
}

func createGoProject(t *testing.T, dir string, code string) {
    os.WriteFile(filepath.Join(dir, "main.go"), []byte(code), 0644)

    // Create go.mod
    gomod := `module test
go 1.21`
    os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0644)
}
```

**Implementation**:
```go
// pkg/driftadopt/validator/go.go

type GoValidator struct{}

func NewGoValidator() *GoValidator {
    return &GoValidator{}
}

func (v *GoValidator) Validate(ctx context.Context, projectPath string) (*ValidationResult, error) {
    // Run: go build
    cmd := exec.CommandContext(ctx, "go", "build", ".")
    cmd.Dir = projectPath

    output, err := cmd.CombinedOutput()

    if err != nil {
        errors := v.parseErrors(string(output))
        return &ValidationResult{
            Success: false,
            Errors:  errors,
        }, nil
    }

    return &ValidationResult{Success: true}, nil
}

func (v *GoValidator) parseErrors(output string) []CompilationError {
    // Parse Go compile errors
    // Format: "./main.go:5:2: cannot use x (variable of type string) as int value in variable declaration"
    var errors []CompilationError
    // Implementation details...
    return errors
}
```

### Day 5: Validator Factory

**Test First**:
```go
// pkg/driftadopt/validator/factory_test.go

func TestValidatorFactory_DetectTypeScript(t *testing.T) {
    tempDir := t.TempDir()
    os.WriteFile(filepath.Join(tempDir, "package.json"), []byte("{}"), 0644)

    validator, err := DetectValidator(tempDir)
    require.NoError(t, err)
    assert.IsType(t, &TypeScriptValidator{}, validator)
}

func TestValidatorFactory_DetectPython(t *testing.T) {
    tempDir := t.TempDir()
    os.WriteFile(filepath.Join(tempDir, "__main__.py"), []byte(""), 0644)

    validator, err := DetectValidator(tempDir)
    require.NoError(t, err)
    assert.IsType(t, &PythonValidator{}, validator)
}

func TestValidatorFactory_DetectGo(t *testing.T) {
    tempDir := t.TempDir()
    os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(""), 0644)

    validator, err := DetectValidator(tempDir)
    require.NoError(t, err)
    assert.IsType(t, &GoValidator{}, validator)
}

func TestValidatorFactory_Unknown(t *testing.T) {
    tempDir := t.TempDir()

    _, err := DetectValidator(tempDir)
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "unknown project type")
}
```

**Implementation**:
```go
// pkg/driftadopt/validator/factory.go

func DetectValidator(projectPath string) (CompilationValidator, error) {
    // Check for TypeScript
    if fileExists(filepath.Join(projectPath, "package.json")) {
        return NewTypeScriptValidator(), nil
    }

    // Check for Python
    if fileExists(filepath.Join(projectPath, "__main__.py")) {
        return NewPythonValidator(), nil
    }

    // Check for Go
    if fileExists(filepath.Join(projectPath, "go.mod")) {
        return NewGoValidator(), nil
    }

    return nil, fmt.Errorf("unknown project type in %s", projectPath)
}

func fileExists(path string) bool {
    _, err := os.Stat(path)
    return err == nil
}
```

---

## Phase 5: Diff Matching (Week 5)

### Day 1-3: DiffMatcher Implementation

**Test First**:
```go
// pkg/driftadopt/diffmatch_test.go

func TestDiffMatcher_ExactMatch(t *testing.T) {
    // Arrange
    expected := []ResourceDiff{
        {
            URN:      "urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket",
            DiffType: DiffTypeUpdate,
            PropertyDiff: []PropChange{
                {
                    Path:     "tags.Environment",
                    OldValue: "dev",
                    NewValue: "production",
                    DiffKind: "update",
                },
            },
        },
    }

    actual := `
    ~ aws:s3/bucket:Bucket: (update)
        [urn=urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket]
        ~ tags: {
            ~ Environment: "dev" => "production"
          }
    `

    matcher := NewDiffMatcher()

    // Act
    result, err := matcher.Matches(expected, actual)
    require.NoError(t, err)

    // Assert
    assert.True(t, result.Matches)
    assert.Empty(t, result.MissingChanges)
    assert.Empty(t, result.UnexpectedChanges)
}

func TestDiffMatcher_ExtraChanges(t *testing.T) {
    // Arrange
    expected := []ResourceDiff{
        {
            URN:      "urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket",
            DiffType: DiffTypeUpdate,
            PropertyDiff: []PropChange{
                {Path: "tags.Environment", NewValue: "production"},
            },
        },
    }

    actual := `
    ~ aws:s3/bucket:Bucket: (update)
        ~ tags.Environment: "dev" => "production"
        ~ tags.Owner: "alice" => "bob"  # UNEXPECTED
    `

    matcher := NewDiffMatcher()

    // Act
    result, err := matcher.Matches(expected, actual)
    require.NoError(t, err)

    // Assert
    assert.False(t, result.Matches)
    assert.Len(t, result.UnexpectedChanges, 1)
    assert.Equal(t, "tags.Owner", result.UnexpectedChanges[0].Path)
}

func TestDiffMatcher_MissingChanges(t *testing.T) {
    // Arrange
    expected := []ResourceDiff{
        {
            URN: "urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket",
            PropertyDiff: []PropChange{
                {Path: "tags.Environment", NewValue: "production"},
                {Path: "tags.Owner", NewValue: "bob"},  // Missing in actual
            },
        },
    }

    actual := `
    ~ aws:s3/bucket:Bucket: (update)
        ~ tags.Environment: "dev" => "production"
    `

    matcher := NewDiffMatcher()

    // Act
    result, err := matcher.Matches(expected, actual)
    require.NoError(t, err)

    // Assert
    assert.False(t, result.Matches)
    assert.Len(t, result.MissingChanges, 1)
    assert.Equal(t, "tags.Owner", result.MissingChanges[0].Path)
}

func TestDiffMatcher_FuzzyValueMatch(t *testing.T) {
    // Test that minor formatting differences are tolerated
    expected := []ResourceDiff{
        {
            PropertyDiff: []PropChange{
                {Path: "enabled", NewValue: true},
            },
        },
    }

    actual := `~ enabled: false => "true"`  // String instead of bool

    matcher := NewDiffMatcher()
    result, err := matcher.Matches(expected, actual)
    require.NoError(t, err)

    // Should match with fuzzy comparison
    assert.True(t, result.Matches)
}
```

**Implementation**:
```go
// pkg/driftadopt/diffmatch.go

type DiffMatcher struct {
    parser *PreviewParser
}

func NewDiffMatcher() *DiffMatcher {
    return &DiffMatcher{
        parser: NewPreviewParser(),
    }
}

func (m *DiffMatcher) Matches(expected []ResourceDiff, actual string) (*MatchResult, error) {
    // Parse actual preview output
    actualDiffs, err := m.parser.ParseDiff(actual)
    if err != nil {
        return nil, fmt.Errorf("parse actual diff: %w", err)
    }

    result := &MatchResult{
        Matches:          true,
        MatchedResources: []string{},
    }

    // Build maps for easier lookup
    actualByURN := make(map[string]ResourceDiff)
    for _, d := range actualDiffs {
        actualByURN[d.URN] = d
    }

    // Check expected changes are in actual
    for _, exp := range expected {
        act, found := actualByURN[exp.URN]
        if !found {
            result.Matches = false
            result.MissingChanges = append(result.MissingChanges, exp.PropertyDiff...)
            continue
        }

        // Check property diffs
        missing, unexpected := m.comparePropertyDiffs(exp.PropertyDiff, act.PropertyDiff)

        if len(missing) > 0 {
            result.Matches = false
            result.MissingChanges = append(result.MissingChanges, missing...)
        }

        if len(unexpected) > 0 {
            result.Matches = false
            result.UnexpectedChanges = append(result.UnexpectedChanges, unexpected...)
        }

        if len(missing) == 0 && len(unexpected) == 0 {
            result.MatchedResources = append(result.MatchedResources, exp.URN)
        }
    }

    return result, nil
}

func (m *DiffMatcher) comparePropertyDiffs(expected, actual []PropChange) (missing, unexpected []PropChange) {
    actualByPath := make(map[string]PropChange)
    for _, a := range actual {
        actualByPath[a.Path] = a
    }

    // Check expected in actual
    for _, exp := range expected {
        act, found := actualByPath[exp.Path]
        if !found {
            missing = append(missing, exp)
            continue
        }

        // Fuzzy value comparison
        if !m.valuesEqual(exp.NewValue, act.NewValue) {
            missing = append(missing, exp)
        }

        // Mark as matched
        delete(actualByPath, exp.Path)
    }

    // Remaining in actual are unexpected
    for _, act := range actualByPath {
        unexpected = append(unexpected, act)
    }

    return
}

func (m *DiffMatcher) valuesEqual(a, b interface{}) bool {
    // Direct equality
    if a == b {
        return true
    }

    // Fuzzy comparison for type coercion
    aStr := fmt.Sprintf("%v", a)
    bStr := fmt.Sprintf("%v", b)

    return aStr == bStr
}
```

### Day 4-5: Diff Matcher Edge Cases

**Test First**:
```go
func TestDiffMatcher_NestedProperties(t *testing.T) {
    // Test deeply nested property paths
}

func TestDiffMatcher_ArrayChanges(t *testing.T) {
    // Test array property changes
}

func TestDiffMatcher_MapChanges(t *testing.T) {
    // Test map/object property changes
}

func TestDiffMatcher_NullValues(t *testing.T) {
    // Test null/undefined value handling
}
```

---

## Phase 6: Chunk Adopter (Week 6)

### Day 1-3: ChunkAdopter Implementation

**Test First**:
```go
// pkg/driftadopt/adopter_test.go

func TestChunkAdopter_Success(t *testing.T) {
    // Arrange
    tempDir := createTestProject(t)
    plan := &DriftPlan{
        Chunks: []DriftChunk{
            {
                ID:     "chunk-001",
                Status: ChunkPending,
                Resources: []ResourceDiff{
                    {
                        URN:        "urn:test",
                        SourceFile: "index.ts",
                        PropertyDiff: []PropChange{
                            {Path: "tags.Environment", NewValue: "production"},
                        },
                    },
                },
            },
        },
    }

    mockLLM := &MockLLMClient{
        Response: `new aws.s3.Bucket("my-bucket", {
            tags: { Environment: "production" }
        });`,
    }

    adopter := NewChunkAdopter(mockLLM, tempDir)

    // Act
    result, err := adopter.AdoptChunk(context.Background(), plan, "chunk-001")
    require.NoError(t, err)

    // Assert
    assert.Equal(t, ChunkCompleted, result.Status)
    assert.True(t, result.CompileSuccess)
    assert.True(t, result.DiffMatches)
}

func TestChunkAdopter_CompilationFailure(t *testing.T) {
    // Arrange - Mock LLM returns invalid code
    mockLLM := &MockLLMClient{
        Response: `const x: number = "string";`, // Type error
    }

    adopter := NewChunkAdopter(mockLLM, tempDir)

    // Act
    result, err := adopter.AdoptChunk(ctx, plan, "chunk-001")
    require.NoError(t, err)

    // Assert
    assert.Equal(t, ChunkFailed, result.Status)
    assert.False(t, result.CompileSuccess)
    assert.Contains(t, result.ErrorMessage, "compilation")
}

func TestChunkAdopter_DiffMismatch(t *testing.T) {
    // Arrange - Generated code doesn't fix drift
    mockLLM := &MockLLMClient{
        Response: `// Code that doesn't fix the drift`,
    }

    adopter := NewChunkAdopter(mockLLM, tempDir)

    // Act
    result, err := adopter.AdoptChunk(ctx, plan, "chunk-001")
    require.NoError(t, err)

    // Assert
    assert.Equal(t, ChunkFailed, result.Status)
    assert.False(t, result.DiffMatches)
}
```

**Implementation**:
```go
// pkg/driftadopt/adopter.go

type ChunkAdopter struct {
    codeGen   *CodeGenerator
    validator CompilationValidator
    diffMatch *DiffMatcher
    projectPath string
}

func NewChunkAdopter(llmClient LLMClient, projectPath string) *ChunkAdopter {
    validator, _ := DetectValidator(projectPath)

    return &ChunkAdopter{
        codeGen:     NewCodeGenerator(llmClient),
        validator:   validator,
        diffMatch:   NewDiffMatcher(),
        projectPath: projectPath,
    }
}

func (a *ChunkAdopter) AdoptChunk(ctx context.Context, plan *DriftPlan, chunkID string) (*AdoptionResult, error) {
    chunk := plan.GetChunk(chunkID)
    if chunk == nil {
        return nil, fmt.Errorf("chunk not found: %s", chunkID)
    }

    result := &AdoptionResult{
        ChunkID: chunkID,
        Status:  ChunkInProgress,
    }

    // 1. Read source files
    sourceCode, err := a.readSourceFiles(chunk.Resources)
    if err != nil {
        result.Status = ChunkFailed
        result.ErrorMessage = fmt.Sprintf("read source: %v", err)
        return result, nil
    }

    // 2. Generate code changes
    newCode, err := a.codeGen.GenerateCode(chunk, sourceCode)
    if err != nil {
        result.Status = ChunkFailed
        result.ErrorMessage = fmt.Sprintf("code generation: %v", err)
        return result, nil
    }

    // 3. Apply changes
    changes := a.applyChanges(chunk.Resources, newCode)
    result.CodeChanges = changes

    // 4. Validate compilation
    validationResult, err := a.validator.Validate(ctx, a.projectPath)
    if err != nil {
        result.Status = ChunkFailed
        result.ErrorMessage = fmt.Sprintf("validation error: %v", err)
        return result, nil
    }

    if !validationResult.Success {
        result.Status = ChunkFailed
        result.CompileSuccess = false
        result.ErrorMessage = formatValidationErrors(validationResult.Errors)
        // Rollback changes
        a.rollbackChanges(changes)
        return result, nil
    }
    result.CompileSuccess = true

    // 5. Run preview
    previewOutput, err := a.runPreview(ctx)
    if err != nil {
        result.Status = ChunkFailed
        result.ErrorMessage = fmt.Sprintf("preview error: %v", err)
        // Rollback
        a.rollbackChanges(changes)
        return result, nil
    }
    result.PreviewOutput = previewOutput

    // 6. Validate diff matches
    matchResult, err := a.diffMatch.Matches(chunk.Resources, previewOutput)
    if err != nil {
        result.Status = ChunkFailed
        result.ErrorMessage = fmt.Sprintf("diff matching error: %v", err)
        return result, nil
    }

    if !matchResult.Matches {
        result.Status = ChunkFailed
        result.DiffMatches = false
        result.ErrorMessage = formatMatchErrors(matchResult)
        // Rollback
        a.rollbackChanges(changes)
        return result, nil
    }
    result.DiffMatches = true

    // Success!
    result.Status = ChunkCompleted
    chunk.Status = ChunkCompleted

    return result, nil
}

func (a *ChunkAdopter) readSourceFiles(resources []ResourceDiff) (string, error) {
    // Read source files for resources
    // Combine into single string for LLM
    // Implementation details...
    return "", nil
}

func (a *ChunkAdopter) applyChanges(resources []ResourceDiff, newCode string) []FileChange {
    // Apply code changes to files
    // Track old/new for rollback
    // Implementation details...
    return nil
}

func (a *ChunkAdopter) rollbackChanges(changes []FileChange) {
    // Restore old code
    for _, change := range changes {
        os.WriteFile(change.FilePath, []byte(change.OldCode), 0644)
    }
}

func (a *ChunkAdopter) runPreview(ctx context.Context) (string, error) {
    // Run: pulumi preview --diff
    cmd := exec.CommandContext(ctx, "pulumi", "preview", "--diff")
    cmd.Dir = a.projectPath

    output, err := cmd.CombinedOutput()
    return string(output), err
}
```

---

## Phase 7: CLI Commands (Week 7)

### Day 1-2: Command Structure and Root

**Test First**:
```go
// cmd/pulumi-drift-adopt/root_test.go

func TestRootCommand(t *testing.T) {
    cmd := NewRootCommand()
    assert.NotNil(t, cmd)
    assert.Equal(t, "pulumi-drift-adopt", cmd.Use)
}

func TestRootCommand_Version(t *testing.T) {
    cmd := NewRootCommand()
    cmd.SetArgs([]string{"--version"})

    output := captureOutput(func() {
        cmd.Execute()
    })

    assert.Contains(t, output, "pulumi-drift-adopt version")
}
```

**Implementation**:
```go
// cmd/pulumi-drift-adopt/root.go

var rootCmd = &cobra.Command{
    Use:   "pulumi-drift-adopt",
    Short: "Tool for adopting infrastructure drift into Pulumi IaC",
    Long: `pulumi-drift-adopt helps you iteratively incorporate changes made
outside of Pulumi IaC back into your infrastructure as code.`,
}

func Execute() error {
    return rootCmd.Execute()
}

func init() {
    rootCmd.Version = "0.1.0"
}
```

### Day 2-3: `next` Command

**Test First**:
```go
// cmd/pulumi-drift-adopt/next_test.go

func TestNextCommand_NoPlan(t *testing.T) {
    tempDir := t.TempDir()

    cmd := newNextCommand()
    cmd.SetArgs([]string{"--plan", filepath.Join(tempDir, "drift-plan.json")})

    output := captureOutput(func() {
        cmd.Execute()
    })

    assert.Contains(t, output, "generate-plan")
}

func TestNextCommand_PendingChunks(t *testing.T) {
    tempDir := t.TempDir()
    plan := &DriftPlan{
        Chunks: []DriftChunk{
            {ID: "chunk-001", Status: ChunkPending},
        },
    }
    planPath := filepath.Join(tempDir, "drift-plan.json")
    WritePlanFile(planPath, plan)

    cmd := newNextCommand()
    cmd.SetArgs([]string{"--plan", planPath})

    output := captureOutput(func() {
        cmd.Execute()
    })

    assert.Contains(t, output, "adopt-chunk")
    assert.Contains(t, output, "chunk-001")
}

func TestNextCommand_Complete(t *testing.T) {
    tempDir := t.TempDir()
    plan := &DriftPlan{
        Chunks: []DriftChunk{
            {ID: "chunk-001", Status: ChunkCompleted},
        },
    }
    planPath := filepath.Join(tempDir, "drift-plan.json")
    WritePlanFile(planPath, plan)

    cmd := newNextCommand()
    cmd.SetArgs([]string{"--plan", planPath})

    output := captureOutput(func() {
        cmd.Execute()
    })

    assert.Contains(t, output, "STOP")
}
```

**Implementation**:
```go
// cmd/pulumi-drift-adopt/next.go

func newNextCommand() *cobra.Command {
    var planFile string

    cmd := &cobra.Command{
        Use:   "next",
        Short: "Suggests the next step in drift adoption",
        RunE: func(cmd *cobra.Command, args []string) error {
            return runNext(planFile)
        },
    }

    cmd.Flags().StringVar(&planFile, "plan", "drift-plan.json", "Path to drift plan file")

    return cmd
}

func runNext(planFile string) error {
    ctx := context.Background()

    // Gate 1: Ensure plan exists
    if !fileExists(planFile) {
        fmt.Println("Drift plan not found.")
        fmt.Println("\nNext step: Generate adoption plan")
        fmt.Printf("  pulumi-drift-adopt generate-plan --output %s\n", planFile)
        return nil
    }

    // Gate 2: Load and validate plan
    plan, err := ReadPlanFile(planFile)
    if err != nil {
        fmt.Printf("Error reading plan: %v\n", err)
        return nil
    }

    // Gate 3: Check for pending chunks
    nextChunk := plan.GetNextPendingChunk()
    if nextChunk != nil {
        fmt.Printf("Next step: Adopt chunk %s\n\n", nextChunk.ID)
        fmt.Printf("Resources:\n")
        for _, res := range nextChunk.Resources {
            fmt.Printf("  - %s (%s)\n", res.Name, res.DiffType)
        }
        fmt.Printf("\nRun:\n")
        fmt.Printf("  pulumi-drift-adopt adopt-chunk %s %s\n", planFile, nextChunk.ID)
        return nil
    }

    // Gate 4: Check for failed chunks
    failed := plan.GetFailedChunks()
    if len(failed) > 0 {
        fmt.Printf("There are %d failed chunks.\n\n", len(failed))
        fmt.Println("Options:")
        fmt.Println("  1. Reset and retry:")
        fmt.Printf("     pulumi-drift-adopt reset-chunk %s %s\n", planFile, failed[0].ID)
        fmt.Println("  2. Skip:")
        fmt.Printf("     pulumi-drift-adopt skip %s %s\n", planFile, failed[0].ID)
        return nil
    }

    // Gate 5: Verify preview is clean
    previewOutput, err := runPulumiPreview(ctx)
    if err != nil {
        fmt.Printf("Preview error: %v\n", err)
        return nil
    }

    if containsDrift(previewOutput) {
        fmt.Println("Warning: Preview still shows drift")
        fmt.Println("Consider regenerating plan:")
        fmt.Printf("  pulumi-drift-adopt generate-plan --output %s\n", planFile)
        return nil
    }

    // All done!
    fmt.Println("STOP - Drift adoption complete!")
    fmt.Println("\nNext steps:")
    fmt.Println("  1. Review changes: git diff")
    fmt.Println("  2. Test: pulumi preview")
    fmt.Println("  3. Create PR: gh pr create")

    return nil
}
```

### Day 3-4: Other Commands

Implement:
- `generate-plan.go`
- `adopt-chunk.go`
- `status.go`
- `skip.go`
- `reset-chunk.go`

Following similar TDD pattern as above.

---

## Phase 8: End-to-End Testing (Week 8)

### Day 1-3: Test Fixtures

Create comprehensive test fixtures:

```
testdata/
  drift-simple/
    pulumi/
      index.ts
      package.json
      Pulumi.yaml
    terraform/
      main.tf
    expected-plan.json
    expected-fixed/
      index.ts

  drift-dependencies/
    # Multi-resource with dependencies

  drift-deletion/
    # Resource deleted in cloud

  drift-replacement/
    # Resource needs replacement
```

### Day 4-5: E2E Tests

**Test First**:
```go
// e2e/drift_test.go

//go:build e2e

func TestE2E_SimpleDrift(t *testing.T) {
    // Setup
    testDir := setupTestProject(t, "testdata/drift-simple")
    defer cleanup(testDir)

    // 1. Generate plan
    cmd := exec.Command("pulumi-drift-adopt", "generate-plan",
        "--stack", "dev",
        "--output", "drift-plan.json")
    cmd.Dir = testDir
    output, err := cmd.CombinedOutput()
    require.NoError(t, err, string(output))

    // 2. Verify plan created
    plan, err := ReadPlanFile(filepath.Join(testDir, "drift-plan.json"))
    require.NoError(t, err)
    assert.Greater(t, len(plan.Chunks), 0)

    // 3. Run next (should suggest adopt-chunk)
    cmd = exec.Command("pulumi-drift-adopt", "next")
    cmd.Dir = testDir
    output, err = cmd.CombinedOutput()
    require.NoError(t, err)
    assert.Contains(t, string(output), "adopt-chunk")

    // 4. Adopt all chunks
    for {
        // Get next chunk
        plan, _ = ReadPlanFile(filepath.Join(testDir, "drift-plan.json"))
        nextChunk := plan.GetNextPendingChunk()
        if nextChunk == nil {
            break
        }

        // Adopt it
        cmd = exec.Command("pulumi-drift-adopt", "adopt-chunk",
            "drift-plan.json", nextChunk.ID)
        cmd.Dir = testDir
        output, err = cmd.CombinedOutput()
        require.NoError(t, err, string(output))
    }

    // 5. Verify completion
    cmd = exec.Command("pulumi-drift-adopt", "next")
    cmd.Dir = testDir
    output, err = cmd.CombinedOutput()
    require.NoError(t, err)
    assert.Contains(t, string(output), "STOP")

    // 6. Verify code matches expected
    actualCode, _ := os.ReadFile(filepath.Join(testDir, "index.ts"))
    expectedCode, _ := os.ReadFile("testdata/drift-simple/expected-fixed/index.ts")
    assert.Equal(t, string(expectedCode), string(actualCode))
}
```

---

## Phase 9: Polish & Documentation (Week 9)

### Day 1-2: Error Message Refinement

- Improve LLM-friendly output formatting
- Add more concrete examples in error messages
- Test error message quality

### Day 3-4: Documentation

Create:
- `README.md` - Overview, installation, usage
- `ARCHITECTURE.md` - Design decisions, patterns
- `CONTRIBUTING.md` - Development setup, testing
- `examples/` - Step-by-step guides

### Day 5: Final Testing and Release Prep

- Run full test suite
- Fix any issues
- Prepare v0.1.0 release

---

## Testing Infrastructure

### Test Utilities

```go
// pkg/driftadopt/testutil/

// fixtures.go
func LoadFixture(t *testing.T, path string) []byte

// mock_llm.go
type MockLLMClient struct {
    Response string
    Error    error
}

// mock_pulumi.go
type MockPulumiClient struct {
    PreviewOutput string
    RefreshOutput string
}

// assertions.go
func AssertPlanValid(t *testing.T, plan *DriftPlan)
func AssertChunkCompleted(t *testing.T, chunk *DriftChunk)
```

### CI/CD Pipeline

```yaml
# .github/workflows/test.yml
name: Test

on: [push, pull_request]

jobs:
  unit:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      - run: go test -v -tags=unit ./...

  integration:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
      - uses: pulumi/actions@v4
      - run: go test -v -tags=integration ./...

  e2e:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
      - uses: pulumi/actions@v4
      - run: go test -v -tags=e2e ./...

  coverage:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
      - run: go test -v -coverprofile=coverage.out ./...
      - run: go tool cover -func=coverage.out
      - uses: codecov/codecov-action@v3
```

### Test Categories

```go
// Build tags for selective testing

//go:build unit
// Unit tests - fast, no external dependencies

//go:build integration
// Integration tests - real Pulumi, no cloud resources

//go:build e2e
// E2E tests - full workflow, may create cloud resources
```

---

## Summary

This TDD development plan provides:

1. **Comprehensive test coverage** from day one
2. **Incremental development** with clear milestones
3. **Proven patterns** from pulumi-terraform-migrate
4. **Robust error handling** for LLM interactions
5. **End-to-end validation** with realistic scenarios

The tool will enable LLMs to iteratively adopt drift back into Pulumi IaC, following the same guided state machine pattern that makes pulumi-terraform-migrate successful.
