---
id: orch-038
type: issue
title: Relative vault path in .orch/config.yaml not resolved correctly
status: resolved
created: 2025-12-22
---

# Relative vault path in .orch/config.yaml not resolved correctly

## Symptom

When configuring a repo with `.orch/config.yaml` using a relative vault path, issues are not found.

## Reproduction

1. Create `.orch/config.yaml` in a repo:
```yaml
vault: ./VAULT
agent: claude
worktree_root: .git-worktrees
base_branch: main
```

2. Ensure `./VAULT` exists and contains issues with `type: issue` frontmatter

3. Run `orch issue list` from the repo directory

**Expected:** Issues are listed
**Actual:** "No issues found"

## Workaround

Use absolute path with tilde expansion instead:
```yaml
vault: ~/repos/manga/VAULT
```

This works correctly.

## Relevant Code

- `internal/cli/root.go:121-126` - relative path expansion logic
- `internal/config/config.go:53-60` - `RepoConfigDir()` function
