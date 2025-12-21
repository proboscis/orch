---
type: issue
id: orch-008
title: Show issue status in orch ps output
status: open
priority: medium
---

# Show issue status in orch ps output

Add the parent issue's status to `orch ps` output so users can see both run status and issue status at a glance.

## Current Output

```
ID      ISSUE        STATUS    PHASE  UPDATED      BRANCH
31909e  sample-task  running          12-21 10:54  issue/sample-task/run-...
```

## Desired Output

```
ID      ISSUE        ISSUE-ST  STATUS    PHASE  UPDATED      BRANCH
31909e  sample-task  open      running          12-21 10:54  issue/sample-task/run-...
6928a3  other-task   closed    done             12-21 09:00  issue/other-task/run-...
```

## Implementation

1. In `orch ps`, resolve each run's parent issue
2. Read issue's `status` from frontmatter
3. Add `ISSUE-ST` (or `I-STATUS`) column between ISSUE and STATUS
4. Cache issue lookups to avoid repeated file reads

## Benefits

- Quickly identify runs on closed/resolved issues
- Filter runs by issue status: `orch ps --issue-status open`
- Spot orphaned runs (issue status empty or missing)
