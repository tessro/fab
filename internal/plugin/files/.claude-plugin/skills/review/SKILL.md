---
name: review
description: Thorough code review skill for checking your work before completion. Use this after implementing a feature or fix to verify code quality, completeness, testing, and documentation.
---

# Code Review

Perform a thorough code review of your changes before marking your task as complete.

## When to Use

Run `/review` after you have finished implementing a feature, bug fix, or any code changes, but BEFORE running `fab issue close` or `fab agent done`.

## How to Review

Use the **Task tool** to spawn a sub-agent for code review. This provides a fresh perspective without the context of how the code was written, leading to more objective reviews.

### Step 1: Run the Sub-Agent Review

Use the Task tool with `subagent_type: "general-purpose"` and a prompt like:

```
Review the code changes in this branch for a pull request. Run `git diff main...HEAD` to see all changes.

Check each of these areas:

1. **Correctness**: Does the code solve the problem? Are there logic errors or unhandled edge cases?
2. **Completeness**: Is the implementation fully complete? Any TODOs left behind?
3. **Testing**: Are there tests for new functionality? Do existing tests pass?
4. **Code Quality**: Is the code readable and well-organized? Does it follow project conventions?
5. **Documentation**: Are public APIs documented? Do complex algorithms have comments?
6. **Security**: Any potential vulnerabilities (injection, XSS, etc.)? Is user input validated?
7. **Performance**: Any obvious performance issues (N+1 queries, unnecessary loops)?

Run the test suite and any linters configured for the project.

Report:
- Issues found (with file paths and line numbers)
- Suggestions for improvement
- Confidence level that the implementation is complete and correct
```

### Step 2: Address Feedback

After the sub-agent completes its review:

1. Read through all issues and suggestions reported
2. Fix any problems identified
3. If significant changes were made, consider running `/review` again

### Step 3: Verify Fixes

Run the test suite one more time to ensure your fixes didn't introduce new issues.

## Output

After completing the review process, summarize:
- What the sub-agent found
- What you fixed
- Confidence level that the implementation is ready to merge

If you find issues during review, fix them before proceeding to close the issue.
