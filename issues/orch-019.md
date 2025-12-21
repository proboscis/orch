---
type: issue
id: orch-019
title: Mark runs with missing worktrees in ps
summary: Identify runs whose worktrees have been deleted in orch ps
status: resolved
priority: medium
---

# Mark runs with missing worktrees in ps

When a run's worktree has been deleted (but the run metadata persists), `orch ps` should indicate this state, as these runs are effectively archived or broken.

## Requirements

1.  **Detection**: Check if the worktree directory exists for each run listed in `ps`.
2.  **Display**:
    *   Add a visual indicator in the `ID` or `STATUS` column, or a new column?
    *   Preferred: Append `(deleted)` or `(missing)` to the status, or use a specific color/flag.
    *   Alternative: A new column `WORKTREE` with "ok" or "missing".
    *   Let's go with: **If worktree is missing, append `*` to the Short ID**. This is subtle but standard (like modified files in `ls`). Or maybe make the ID red?
    *   Better yet: A dedicated column might be too much. Let's create a new visual state or modify the `MERGED` column to be `STATE`?
    *   Simple approach: If worktree is missing, change `STATUS` to have a `(no-wt)` suffix or similar, OR color the ID column differently (e.g. gray/dim).

    **Decision**:
    *   Check `os.Stat(run.WorktreePath)`.
    *   If missing, append `*` to the `ID` column (e.g., `66ff6*`).
    *   Add a legend or help text if needed? No, just keep it simple.

## Implementation Plan

1.  **Update `internal/cli/ps.go`**:
    *   In `outputTable` loop:
        ```go
        _, err := os.Stat(r.WorktreePath)
        worktreeMissing := os.IsNotExist(err)
        
        displayID := r.ShortID()
        if worktreeMissing {
            displayID += "*"
        }
        ```
    *   Pass `displayID` to the table printer.

## Performance
*   `os.Stat` is fast, doing it for 50 runs is negligible.

## Example Output
```
ID      ISSUE     STATUS    PHASE   MERGED  UPDATED
66ff6*  orch-001  done      -       yes     2h ago
```
*   `66ff6*` implies the run record exists but the worktree is gone.
