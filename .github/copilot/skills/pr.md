# PR Implementation Skill

Implement a PR from description, optionally referencing PR_TODO.md, and create a GitHub pull request.

## Instructions

When invoked with a PR description, this skill:

1. **Understand the PR**: Parse the user's description of what needs to be implemented
2. **Check PR_TODO.md**: Search for matching PR in PR_TODO.md to get context about:
   - Current situation and problems
   - Suggested approach
   - Related files and references
3. **Plan implementation**: Based on description and PR_TODO context, create implementation plan
4. **Implement changes**: Make necessary code changes following Goverse conventions
5. **Test changes**: Run relevant tests to verify implementation
6. **Create GitHub PR**: Use `gh pr create` to open pull request

## Process Flow

### 1. Parse PR Description
- Extract what needs to be implemented
- Identify key requirements and scope

### 2. Search PR_TODO.md
```bash
# Search for related PR in PR_TODO.md
grep -i "<search terms from description>" PR_TODO.md
```

If found in PR_TODO.md:
- Use **Current Situation** to understand existing code
- Reference **Problems This Causes** to validate solution addresses issues
- Follow **Suggested Approach** for directional guidance
- Check **Reference** section for relevant files

### 3. Implementation
- Follow project conventions (see custom instructions)
- Make minimal, surgical changes
- Update tests if needed
- Update documentation if directly related
- Run `go fmt ./...` and `go vet ./...`

### 4. Validation
- Compile proto files first if needed (`.\script\win\compile-proto.cmd`)
- Run `go build ./...`
- Run relevant tests with `go test -v -run <TestName> ./...`
- Check no regressions introduced

### 5. Create GitHub PR
```bash
# Create feature branch
git checkout -b <feature-branch-name>

# Stage and commit changes
git add <files>
git commit -m "<commit message>"

# Push branch
git push -u origin <feature-branch-name>

# Create PR with gh
gh pr create --title "<PR title>" --body "<PR description>"
```

## PR Title and Description Format

**Title**: `[Priority] Brief description`
- Example: `[P1] Add reliable call timeout and expiration`

**Description**: Should include:
```markdown
## What
Brief description of changes

## Why
Reference to PR_TODO.md entry or explanation of problem being solved

## Changes
- Bullet list of key changes made
- Files modified/added

## Testing
- How changes were tested
- Test cases added/modified

## Related
- Closes #<issue-number> (if applicable)
- Related to PR_TODO.md: <section name>
```

## Branch Naming Convention
- Feature: `feature/<brief-description>`
- Bugfix: `fix/<brief-description>`
- Chore: `chore/<brief-description>`

Example: `feature/reliable-call-expiration`

## Key Principles

1. **Check PR_TODO.md first** - It contains valuable context about current state and problems
2. **Follow Goverse conventions** - Lock hierarchy, error handling, testing patterns
3. **Minimal changes** - Surgical, focused changes only
4. **Test thoroughly** - Don't break existing functionality
5. **Clean commits** - Clear commit messages, formatted code
6. **Complete PR description** - Help reviewers understand context

## Example Usage

User: "Implement reliable call timeout"

Skill:
1. Searches PR_TODO.md → Finds "[P1] Reliable Call Timeout and Expiration"
2. Reads current situation (no cleanup, unbounded growth)
3. Follows suggested approach (add expires_at, cleanup job, config)
4. Implements changes in util/postgres/, adds cleanup logic
5. Tests with integration tests
6. Creates branch `feature/reliable-call-expiration`
7. Opens PR with gh command

## Required Tools
- `git` for version control
- `gh` CLI for creating pull requests
- `go` toolchain for building and testing
- Proto compilation scripts when proto changes made

