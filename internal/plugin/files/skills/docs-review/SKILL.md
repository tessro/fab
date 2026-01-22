---
name: docs-review
description: Documentation review skill for assessing whether code changes are adequately reflected in documentation. Use this after /review to identify documentation gaps or outdated content.
---

# Documentation Review

Assess whether your code changes are adequately documented. This skill identifies features, configuration, CLI commands, and other user-facing changes that may need documentation updates.

## When to Use

Run `/docs-review` after completing `/review` and before committing, especially when your changes include:
- New features or functionality
- Changes to CLI commands or flags
- Configuration changes
- API modifications
- New entrypoints or user-facing behavior

## How to Review

Use the **Task tool** to spawn a sub-agent for documentation review. This provides a fresh perspective focused specifically on documentation accuracy.

### Step 1: Run the Sub-Agent Review

Use the Task tool with `subagent_type: "general-purpose"` and a prompt like:

```
Review all code changes (committed and uncommitted) between main and the current worktree for documentation impact. Run `git diff main` to see all changes.

Analyze the changes to identify:

1. **User-Facing Changes**: New or modified features, behaviors, or functionality that users interact with
2. **CLI/Entrypoint Changes**: New commands, flags, subcommands, or changes to existing ones
3. **Configuration Changes**: New config options, environment variables, or changes to existing settings
4. **API Changes**: New endpoints, changed request/response formats, or deprecations

For each identified change, check:
- Is there existing documentation that covers this? (README, docs/, inline help, etc.)
- Does the existing documentation accurately reflect the new behavior?
- Is the documentation discoverable where users would look for it?

Then examine any documentation changes in the diff:
- Do doc changes accurately describe the feature?
- Are there inconsistencies between code and docs?
- Is anything documented that doesn't match the implementation?

Report:
- **Gaps**: User-facing changes without adequate documentation
- **Outdated**: Existing docs that no longer match the code
- **Suggestions**: Specific documentation improvements (be concise, not exhaustive)
- **Assessment**: Overall documentation status (adequate / needs updates / critical gaps)

Be pragmatic: not every internal refactor needs docs. Focus on what users need to know.
```

### Step 2: Review the Report

The sub-agent will produce a documentation assessment. Review it and decide:

1. **Adequate**: Documentation is sufficient for the changes made. Proceed to commit.
2. **Needs Updates**: Minor gaps exist. Consider adding brief documentation before committing.
3. **Critical Gaps**: Major user-facing features are undocumented. Add documentation before proceeding.

### Step 3: Address Gaps (If Any)

If the review identifies gaps:

1. For CLI changes: Update help text, README usage sections, or man pages
2. For configuration: Update config documentation or example files
3. For features: Add or update relevant docs/ pages or README sections
4. For APIs: Update API documentation or OpenAPI specs

**Keep documentation honest and proportional** - the goal is accuracy, not comprehensiveness. A brief note is often better than verbose documentation that won't be maintained.

## Output

After completing the review process, summarize:
- What user-facing changes were identified
- Documentation status (adequate / gaps found)
- What documentation updates were made (if any)

**Note**: This review is advisory. Use judgment about what documentation is truly needed. Not every code change requires documentation updates.
