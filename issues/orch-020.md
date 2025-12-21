---
type: issue
id: orch-020
title: Fix false positive merged status for fresh branches
summary: Improve branch merge detection to ignore fresh branches that haven't diverged
status: open
priority: medium
---

# Fix false positive merged status for fresh branches

`orch ps` currently reports a branch as "merged" if it is reachable from `main`. This causes fresh branches (just created from `main`) to be reported as "merged: yes" even though no work has been done or merged.

## Requirements

1.  **Refine Detection**: A branch should only be considered "merged" if it has actually introduced changes that are now in `main`.
    *   This is tricky with Git because "fresh" and "fast-forward merged" look similar.
    *   However, we can check if the branch is *ahead* of the fork point? No, if it's merged, it's not ahead.
2.  **Alternative Approach**:
    *   Check if the branch has any commits? `git rev-list --count main..branch` == 0 implies it's "fully inside main".
    *   If `git rev-list --count branch..main` is also 0 (i.e., branch == main), then it's a fresh branch (or fast-forward merged to tip).
    *   Ideally, we want to know if *commits were made on this branch* and then merged.
    *   Maybe we can simply say: If `branch == main`, it's not "merged" (it's "current").
    *   But if I finish a task and fast-forward merge it, `branch == main`. I want that to show as "merged".

## Heuristic

Maybe we rely on the Run's status or event history?
*   If Run is `queued` or `booting` or `running` (with few events), and branch == main, it's likely "Fresh".
*   If Run is `done` and branch == main, it's "Merged".

But `orch ps` shouldn't rely too heavily on parsing events for every row (performance).

**Proposed Git-only heuristic**:
*   A branch is "merged" if it is reachable from `main` AND it is NOT pointing to the exact same commit as `main`?
    *   No, fast-forward merge results in same commit.
*   Maybe we can't distinguish "Fresh" from "FF-Merged" purely by git topology if we don't know where it started.
*   **Wait**: `orch` knows when the run started (`StartedAt`).
*   Can we check if the branch tip commit is *older* than `StartedAt`?
    *   If branch tip is older than `StartedAt`, it means no new commits have been made on this branch since the run started. -> **Not Merged** (it's just the base branch).
    *   If branch tip is newer than `StartedAt`, and it is reachable from `main` -> **Merged**.

## Implementation Plan

1.  **Update `GetMergedBranches`**:
    *   It currently returns `map[string]bool`.
    *   We might need to fetch the commit date of the branch tip as well.
    *   Or, fetching `git branch -v` might give us enough info?

2.  **New Logic in `ps.go`**:
    *   We have `run.StartedAt`.
    *   We need the commit time of the branch tip.
    *   If `branch_commit_time < run.StartedAt`, then "merged" = NO (it's just a fresh branch pointer).
    *   If `branch_commit_time >= run.StartedAt` AND `git branch --merged` includes it, then "merged" = YES.

3.  **Performance**:
    *   Fetching commit times for all branches might be slow if done one by one.
    *   `git for-each-ref --format='%(refname:short) %(committerdate:unix)' refs/heads` is fast.

4.  **Steps**:
    *   Modify `internal/git/merge.go` to `GetMergedBranchInfo(repoRoot, target string) (map[string]time.Time, error)`?
    *   No, `GetMergedBranches` checks reachability.
    *   We also need a function `GetBranchCommitTimes(repoRoot string) (map[string]time.Time, error)`.
    *   Combine them in `ps.go`.

## Logic check
*   **Fresh branch**: Created at T1. Tip commit is C0 (from T0). T0 < T1. -> **Not Merged**. Correct.
*   **Work done**: Commit C1 at T2. T2 > T1. Not merged yet. -> `git branch --merged` says NO. -> **Not Merged**. Correct.
*   **Merged**: C1 merged into main. `git branch --merged` says YES. Tip is C1. T2 > T1. -> **Merged**. Correct.
*   **Old run**: Run started T0. Branch created. Work done T1. Merged T2. Now it's T10. T1 > T0. -> **Merged**. Correct.

This seems robust enough.

## Tasks
1.  Add `GetBranchCommitTimes` to `internal/git/merge.go` (or `branch.go`).
2.  Update `orch ps` to use this timestamp check.
