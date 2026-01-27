---
name: pr
description: Resolve a task (issue, feature, bug, etc.) and create a GitHub PR with the changes
---

Resolve a task and open a GitHub pull request.

The user may invoke this skill with an argument describing the task, a GitHub issue number, or a GitHub issue URL. If no argument is provided, ask the user what they want to work on using AskUserQuestion.

## Steps

### 1. Understand the task

- If the argument is a GitHub issue number (e.g. `123`) or URL, fetch the issue details with `gh issue view <number>`.
- If the argument is a free-text description, use it directly.
- If no argument is provided, ask the user: "What task should I resolve? You can provide a description, a GitHub issue number, or a GitHub issue URL."
- Review the task requirements carefully. If anything is ambiguous or under-specified, ask the user for clarification using AskUserQuestion before proceeding. Do not guess at requirements.

### 2. Plan the implementation

- Use EnterPlanMode to design the implementation approach.
- Explore the codebase to understand relevant files, patterns, and conventions.
- Present a clear plan and get user approval before writing code.

### 3. Implement the fix

- Create a new branch from `main` named descriptively (e.g. `fix/issue-123-brief-description` or `feat/brief-description`). Use `fix/` prefix for bugs, `feat/` for features, `chore/` for maintenance.
- Make the necessary code changes following the project's conventions (see CLAUDE.md).
- Run `go fmt ./...` and `go vet ./...` on changed packages.
- Run relevant tests to verify the changes work: `go test -v ./affected/package/...`
- If tests fail, fix the issues and re-run until they pass.

### 4. Commit the changes

- Run `git status` (never use `-uall`) and `git diff` to review all changes.
- Stage files explicitly by name (never use `git add -A` or `git add .`). Do not stage files that contain secrets.
- Write a concise commit message summarizing the change. End with `Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>`.

### 5. Ask for PR description

- If the user provided a PR description or the task description is sufficiently detailed, use it.
- Otherwise, ask the user using AskUserQuestion: "Please provide a PR description, or I can draft one based on the changes. Which do you prefer?" with options:
  - "Draft for me" — generate a description from the changes made
  - "I'll provide one" — wait for user input
- The PR description should include a summary of what changed and why, and a test plan.

### 6. Create the pull request

- Push the branch: `git push -u origin <branch-name>`
- Create the PR with `gh pr create` using this format:
  ```
  gh pr create --title "<short title>" --body "$(cat <<'EOF'
  ## Summary
  <bullet points>

  ## Test plan
  <checklist>

  🤖 Generated with [Claude Code](https://claude.com/claude-code)
  EOF
  )"
  ```
- If the task was a GitHub issue, link the issue in the PR body (e.g. "Resolves #123").
- Return the PR URL to the user.
