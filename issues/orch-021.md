---
type: issue
id: orch-021
title: Show merge conflict status in orch ps
summary: Indicate if a run's branch has merge conflicts with main in orch ps
status: open
priority: medium
---

# Show merge conflict status in orch ps

In addition to knowing if a branch is merged, it's crucial to know if it *can* be merged cleanly. `orch ps` should indicate if there are merge conflicts.

## Requirements

1.  **Detection**: Check if the run's branch has conflicts with the target branch (usually `main`).
2.  **Display**:
    *   Update the `MERGED` column (or new column) to show "conflict" if applicable.
    *   Priority: Merged > Conflict > Clean (no conflict) > Fresh?
    *   Maybe the column should be `GIT` or `STATE`?
    *   Current `MERGED` column values: `yes`, `-`.
    *   New values: `yes`, `conflict`, `clean` (or just `-` for clean).

## Implementation Plan

1.  **Git Helper**:
    *   Add `CheckMergeConflict(repoRoot, branch, target string) (bool, error)` in `internal/git`.
    *   Technique:
        *   `git merge-tree --write-tree branch target` (fast, does in-memory merge).
        *   If it fails or returns conflict info, then conflict exists.
        *   Actually `git merge-tree <base-commit> <branch> <target>` (old style) or just `git merge-tree <branch> <target>` (new style in git 2.38+).
        *   If `git merge-tree` isn't available/reliable, try `git format-patch` + `git apply --check`? No, too slow.
        *   Standard way: `git merge-base` to find common ancestor, then check?
        *   **Best approach for modern git**: `git merge-tree --write-tree HEAD target`. If exit code is non-zero? No, it writes tree object.
        *   Better: `git merge-tree <branch1> <branch2>` outputs the diff.
        *   Even better: `git merge-file`? No.
    *   **Simple approach**:
        *   `git merge --no-commit --no-ff branch` in a temporary index? Too risky/slow.
        *   **`git merge-tree` is the way if git version allows**.
        *   Fallback: Just don't show it if checking is expensive.

    *   **Alternative**: The `orch-020` issue handles "merged vs fresh". Conflict checking is "next level".

2.  **Performance Check**:
    *   Running conflict check for 50 runs might be slow (1-2s).
    *   Should be optional? Or only for "active" runs?
    *   Let's keep it efficient.

## Proposed Display in `orch ps`
*   Column `MERGED`:
    *   `yes` (Merged)
    *   `conflict` (Has conflicts)
    *   `-` (Not merged, clean)

## Note
This depends on `git merge-tree` availability (Git 2.22+ for basic, 2.38+ for `--write-tree`). We should check git version or handle error gracefully.
