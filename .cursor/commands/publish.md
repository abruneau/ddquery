# Create PR

## Overview

Create a well-structured pull request with proper description, labels, and
reviewers.

## Steps

1. **Create Commit**
    - Check if we are not on the `main` branch. If this is the case, create a branch
    - 1. **Review changes**
        - Check the diff: `git diff --cached` (if changes are staged) or `git diff` (if unstaged)
        - Understand what changed and why
    - 2. **Stage changes (if not already staged)**
        - `git add -A`
    - 3. **Create short commit message**
        - Base the message on the actual changes in the diff
        - Example: `git commit -m "fix(auth): handle expired token refresh"`
        - Example with issue key: `git commit -m "PROJ-123: fix(auth): handle expired token refresh"`
        - Template
            - `git commit -m "<type>(<scope>): <short summary>"`
            - With issue key: `git commit -m "<issue-key>: <type>(<scope>): <short summary>"`
        - Rules
            - **Length:** <= 72 characters
            - **Imperative mood:** Use "fix", "add", "update" (not "fixed", "added", "updated")
            - **Capitalize:** First letter of summary should be capitalized
            - **No period:** Don't end the subject line with a period
            - **Describe why:** Not just what - "fix stuff" is meaningless

1. **Prepare branch**
    - Ensure all changes are committed
    - Push branch to remote
    - Verify branch is up to date with main
2. **Write PR description**
    - Summarize changes clearly
    - Include context and motivation
    - List any breaking changes
    - Add screenshots if UI changes
3. **Set up PR**
    - Create PR with descriptive title
    - Add appropriate labels
    - Assign reviewers
    - Link related issues

## PR Template

- [ ] Feature A
- [ ] Bug fix B
- [ ] Unit tests pass
- [ ] Manual testing completed