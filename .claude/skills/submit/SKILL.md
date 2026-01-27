---
name: submit
description: Commit all changes and push to the current branch
disable-model-invocation: true
---

Commit all outstanding changes and push to the current branch.

1. Run `git status` (never use `-uall`) and `git diff` to see all changes, and `git log --oneline -5` to see recent commit style.
2. Stage all modified and untracked files relevant to the current work (prefer naming files explicitly over `git add -A`). Do not stage files that contain secrets.
3. Write a concise commit message summarizing the changes. End with `Co-Authored-By: Claude Opus 4.5 <noreply@anthropic.com>`.
4. Push to the current branch with `git push`.
