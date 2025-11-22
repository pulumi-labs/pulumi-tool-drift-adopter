# Drift Adoption Tool - Design

## Overview

The `pulumi-drift-adopt` tool is a stateless CLI that helps AI agents adopt infrastructure drift back into Pulumi code. It runs `pulumi preview`, interprets the output with inverted logic, and returns structured JSON telling the agent what code changes are needed.

## Architecture

### Core Principle: Stateless Simplicity

The tool has one job: run `pulumi preview --json`, parse the output, invert the logic, and return what needs to change in code.

No plan files. No state management. No orchestration. Just preview → parse → return.

### Why Stateless?

The tool runs `pulumi preview --refresh` which automatically updates state to match actual infrastructure, then compares it to code. The state represents the actual infrastructure (what we want), and the code represents what's currently written (what needs updating).

The tool simply reformats this information for agent consumption.

## The Inverted Logic

Pulumi preview shows operations from the perspective of "what would change if we applied this code":

- **Create** = code defines resource not in state → **Agent should DELETE from code**
- **Delete** = state has resource not in code → **Agent should ADD to code**
- **Update** = resource differs → **Agent should UPDATE code**

For property changes:
- **LHS (old value)** = what's in state = **desired value** (what we want)
- **RHS (new value)** = what's in code = **current value** (what needs changing)

The tool inverts this perspective and tells the agent: "here's what your code should look like".

## Command: `next`

Single command that does everything:

```bash
pulumi-drift-adopt next [--project .] [--stack <stack-name>]
```

### Process

1. Run `pulumi preview --json --non-interactive --refresh` in the project directory
2. Optionally specify a stack with `--stack` flag
3. Parse newline-delimited JSON events
3. For each `resource-step` event:
   - Extract operation (`create`, `delete`, `update`, `replace`)
   - Extract resource metadata (URN, type, name)
   - Extract `detailedDiff` for property changes
4. Invert the logic:
   - `create` → `delete_from_code`
   - `delete` → `add_to_code`
   - `update`/`replace` → `update_code`
   - For properties: swap LHS/RHS to show current→desired
5. Output JSON

### Output Format

```json
{
  "status": "changes_needed",
  "resources": [
    {
      "action": "update_code",
      "urn": "urn:pulumi:dev::app::aws:s3/bucket:Bucket::my-bucket",
      "type": "aws:s3/bucket:Bucket",
      "name": "my-bucket",
      "properties": [
        {
          "path": "tags.Environment",
          "currentValue": "dev",
          "desiredValue": "production",
          "kind": "update"
        }
      ]
    }
  ]
}
```

Status can be:
- `changes_needed` - drift detected, code changes required
- `clean` - no drift, code matches state
- `error` - preview failed (syntax error, missing dependency, etc.)

## Workflow

```
1. Agent runs: pulumi-drift-adopt next
   (Tool automatically refreshes state and runs preview)

2. Agent reads output and updates code files
   (Changes properties, adds/removes resources)

3. Agent runs: pulumi-drift-adopt next
   (Verify changes)

4. Repeat until status = "clean"
```

## Design Decisions

### Why not store state?

`pulumi preview` already has all the information. Storing it in a plan file adds complexity without benefit.

### Why not dependency ordering?

The agent can read all changes at once and update code in any order. After each update, running `next` again shows remaining drift. The iterative nature handles dependencies naturally.

### Why not validation?

The tool doesn't apply changes - the agent does. If the agent makes a mistake, `next` will show errors or remaining drift. This keeps the tool simple and safe.

### Why JSON output?

Structured output is easy for agents to parse. Human-readable prose would require LLM parsing, adding latency and unreliability.

### Why single command?

Each command adds complexity. One command that does one thing well is easier to use, test, and maintain.

## Implementation

### Key Files

- `cmd/pulumi-drift-adopt/main.go` - Entry point
- `cmd/pulumi-drift-adopt/root.go` - Root command setup
- `cmd/pulumi-drift-adopt/next.go` - Main command (all logic and types defined inline)

### Dependencies

- Go 1.21+
- Pulumi CLI (must be in PATH)
- Cobra CLI framework

### Testing

The tool is simple enough that manual testing with real Pulumi projects is sufficient. The logic is straightforward preview parsing with minimal complexity.

## Future Enhancements

Possible improvements:
- Source location detection (which file defines each resource)
- Suggested code snippets for additions
- Diff output formatting options
- Support for more Pulumi features (stacks, config, etc.)
