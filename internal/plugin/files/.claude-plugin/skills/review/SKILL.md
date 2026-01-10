---
name: review
description: Thorough code review skill for checking your work before completion. Use this after implementing a feature or fix to verify code quality, completeness, testing, and documentation.
---

# Code Review

Perform a thorough code review of your changes before marking your task as complete.

## When to Use

Run `/review` after you have finished implementing a feature, bug fix, or any code changes, but BEFORE running `fab issue close` or `fab agent done`.

## Review Checklist

Systematically check each of these areas:

### 1. Correctness

- Does the code actually solve the problem described in the issue?
- Are there any logic errors or edge cases not handled?
- Do all code paths work correctly?
- Are error conditions handled appropriately?

### 2. Completeness

- Is the implementation fully complete, or are there TODOs left behind?
- Are all acceptance criteria from the issue satisfied?
- Have you forgotten any files that need to be modified?
- Are all necessary imports/dependencies added?

### 3. Testing

- Are there tests for the new functionality?
- Do existing tests still pass?
- Are edge cases covered in tests?
- If tests were not added, is there a good reason (e.g., trivial change, no testable behavior)?

### 4. Code Quality

- Is the code readable and well-organized?
- Are variable/function names clear and descriptive?
- Is there unnecessary duplication that should be refactored?
- Does the code follow the project's existing patterns and conventions?

### 5. Documentation

- Are public APIs documented?
- Do complex algorithms have explanatory comments?
- Does the README need updating for new features?
- Are configuration changes documented?

### 6. Security

- Are there any potential security vulnerabilities (injection, XSS, etc.)?
- Is user input properly validated?
- Are secrets/credentials handled safely?

### 7. Performance

- Are there any obvious performance issues (N+1 queries, unnecessary loops)?
- Could any operations block or cause timeouts?

## Review Process

1. **Run `git diff`** to see all changes you've made
2. **Read through each changed file** carefully
3. **Check the issue requirements** against your implementation
4. **Run the test suite** if available
5. **Run any linters or type checkers** configured for the project
6. **Fix any issues found** before proceeding

## Output

After completing the review, summarize:
- What you checked
- Any issues found and fixed
- Confidence level that the implementation is complete and correct

If you find issues during review, fix them before proceeding to close the issue.
