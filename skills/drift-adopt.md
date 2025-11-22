# Drift Adoption Skill

You are helping the user adopt infrastructure drift back into their Pulumi code using the `pulumi-drift-adopt` tool.

## Overview

The tool automatically runs `pulumi preview --refresh` to detect drift between actual infrastructure and code. It then provides structured guidance on what code changes are needed.

## Workflow

Follow this iterative process:

### 1. Run the Drift Adoption Tool

Execute the tool in the Pulumi project directory:

```bash
pulumi-drift-adopt next

# Or specify a stack if needed
pulumi-drift-adopt next --stack dev
```

The tool will:
- Automatically refresh state to match actual infrastructure
- Run preview to compare code vs state
- Return JSON with needed code changes

### 2. Parse the JSON Output

The tool returns JSON with one of three statuses:

#### Status: "error"
Preview failed (likely a code error).

**Action:** Read the error message, identify the issue (syntax error, missing import, etc.), fix the code, and run again.

#### Status: "clean"
No drift detected. Code matches state.

**Action:** You're done! Inform the user that drift adoption is complete.

#### Status: "changes_needed"
Drift detected. The tool provides a list of resources and properties to change.

**Action:** Proceed to step 3.

### 3. Process Each Resource Change

For each resource in the output, look at the `action` field:

#### Action: "update_code"
The resource exists in both code and state but has property differences.

**What to do:**
- Find the resource in the code (use the `name` and `type` to locate it)
- For each property change:
  - `path`: The property path (e.g., "tags.Environment")
  - `currentValue`: What's currently in the code (wrong)
  - `desiredValue`: What should be in the code (from state)
- Update the code to change `currentValue` to `desiredValue`

**Example:**
```json
{
  "action": "update_code",
  "name": "my-bucket",
  "type": "aws:s3/bucket:Bucket",
  "properties": [{
    "path": "tags.Environment",
    "currentValue": "dev",
    "desiredValue": "production"
  }]
}
```

Find the bucket resource and change `tags: { Environment: "dev" }` to `tags: { Environment: "production" }`.

#### Action: "delete_from_code"
The resource is in code but not in state (it doesn't exist in actual infrastructure).

**What to do:**
- Find the resource in the code
- Delete it completely
- Also remove any references to this resource from other resources

#### Action: "add_to_code"
The resource is in state but not in code (it exists in infrastructure but isn't defined in code).

**What to do:**
- Add the resource definition to the code
- Use the `type` and `name` from the output
- You may need to look at the `urn` to understand the resource's configuration
- Add appropriate properties based on the resource type

### 4. Run the Tool Again

After making code changes, run the tool again:

```bash
pulumi-drift-adopt next
```

This verifies your changes and shows any remaining drift.

### 5. Iterate

Repeat steps 2-4 until the status is "clean".

## Important Notes

- **Make one change at a time** if you're uncertain. You can always run the tool again to see what's left.
- **Read the entire JSON output** before making changes. Sometimes there are dependencies between resources.
- **The tool is stateless** - it just runs preview and interprets the output. You can run it as many times as needed.
- **If you make a mistake**, the tool will show it on the next run. Just fix it and continue.

## Example Session

**Initial run:**
```bash
$ pulumi-drift-adopt next
{
  "status": "changes_needed",
  "resources": [
    {
      "action": "update_code",
      "name": "my-bucket",
      "type": "aws:s3/bucket:Bucket",
      "properties": [{
        "path": "versioning.enabled",
        "currentValue": false,
        "desiredValue": true
      }]
    }
  ]
}
```

**You update the code to set versioning.enabled to true.**

**Second run:**
```bash
$ pulumi-drift-adopt next
{
  "status": "clean"
}
```

**Done!**

## Troubleshooting

**Tool returns "error" status:**
- Check the error message
- Usually a syntax error or compilation issue in the code
- Fix the code error and run again

**Not sure which file contains a resource:**
- Search the codebase for the resource name
- Use the `type` field to narrow down (e.g., "aws:s3/bucket:Bucket")
- Look for the logical resource name from the `name` field

**Resource has many property changes:**
- Update them all at once
- The tool will verify all changes on the next run

**Confused about what to change:**
- Remember: `desiredValue` is what you want (from state)
- Change your code from `currentValue` to `desiredValue`
