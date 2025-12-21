---
type: issue
id: orch-017
title: Show branch merge status in orch ps
summary: Indicate if a run's branch has been merged to main in the process list
status: resolved
priority: medium
---

# Show branch merge status in orch ps

Update `orch ps` to indicate whether the git branch associated with a run has been merged into the main branch.

## Motivation

Currently, `orch ps` shows the status of the run (running, done, failed), but doesn't tell you if the resulting code has been merged. Knowing the merge status helps in cleaning up old runs and tracking overall progress.

## Requirements

1.  **Detection**: Efficiently detect if a run's branch is merged into `main`.
2.  **Display**: specific column or indicator in the `orch ps` output.
    *   Could be a new column "MERGED".
    *   Or a symbol/flag in the "STATUS" column.
3.  **Performance**: The check should not significantly slow down the listing of runs.

## Implementation Plan

1.  **Git Helper**:
    *   Add a function in `internal/git` (e.g., `GetMergedBranches(target string)`) that returns a map or slice of branches merged into `target` (default `main`).
    *   Use `git branch --merged main` to fetch this list in a single batch operation rather than checking each branch individually.

2.  **Update CLI (`internal/cli/ps.go`)**:
    *   In the `ps` command handler, fetch the list of merged branches once.
    *   Iterate through the runs.
    *   If `run.Branch` is in the merged list, mark it.

3.  **UI/UX**:
    *   Decide on the indicator. A "✔" or "M" in a new column, or appending to the status (e.g., `done (merged)`).
    *   Example output:
        ```text
        ID      ISSUE     STATUS    PHASE   MERGED  AGE    TOPIC
        66ff6   orch-001  done      -       ✔       2h     implement basic...
        ```

## Considerations

*   Handle cases where the branch has been deleted but was merged (might be hard if we only track branch name, but usually `git branch --merged` only shows existing branches. If the branch is deleted, we might need to rely on the PR status if we track it).
*   For now, strictly checking if the *local* branch name appears in `git branch --merged` is a good first step.
