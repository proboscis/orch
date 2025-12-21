---
type: issue
id: orch-024
title: Add dedicated PR column to orch ps
summary: Separate PR status from MERGED column in orch ps
status: resolved
priority: medium
---

# Add dedicated PR column to orch ps (orch-024)

Currently, PR existence is mixed into the `MERGED` column status (e.g., "pr (clean)"). It would be clearer to have a dedicated `PR` column.

## Requirements

1.  **New Column**: `PR` in `orch ps` table output.
2.  **Values**:
    *   `yes` (or `✓`): PR exists (based on `run.PRUrl`).
    *   `-`: No PR.
3.  **Update `MERGED` Column**:
    *   Should focus on git status only.
    *   Values: `merged`, `modified`, `conflict`, `clean` (maybe just `-` for clean?), `no change`.
    *   If `modified` and no conflict -> `clean`? Or just `modified`.
    *   Let's keep `modified` vs `conflict` vs `merged` vs `no change`.

## Implementation Plan

1.  **Update `internal/cli/ps.go`**:
    *   Add `PR` to headers.
    *   In row loop:
        ```go
        pr := "-"
        if r.PRUrl != "" {
            pr = "yes"
        }
        ```
    *   Update `gitStatesForRuns` to stop returning "pr" statuses.
        *   It should just return `merged`, `no change`, `modified`, `conflict`.
        *   If ahead > 0:
            *   If conflict -> `conflict`
            *   Else -> `modified` (or `clean` if we want to emphasize mergeability)
            *   "Modified" implies "Clean" implicitly if not "Conflict". "Clean" sounds better for "Ready to merge".
            *   Let's use:
                *   `merged`
                *   `no change`
                *   `conflict`
                *   `clean` (Ahead > 0, no conflict)

2.  **Display Order**:
    *   `ID`, `ISSUE`, `AGENT`, `STATUS`, `PR`, `MERGED`, `UPDATED`, `TOPIC`

## Example Output
```
ID      ISSUE     AGENT   STATUS   PR   MERGED    UPDATED
66ff6   orch-001  claude  done     yes  clean     2h ago
```
*   `clean` means modified locally, mergeable.
*   `merged` means already in main.
