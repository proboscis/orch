---
type: issue
id: orch-009
title: Show elapsed time instead of absolute time in orch ps
status: resolved
priority: medium
---

# Show elapsed time instead of absolute time in orch ps

Change the UPDATED column in `orch ps` to show relative/elapsed time instead of absolute timestamps.

## Current Output

```
ID      ISSUE        STATUS    UPDATED       BRANCH
31909e  sample-task  running   12-21 10:54   issue/sample-task/run-...
```

## Desired Output

```
ID      ISSUE        STATUS    UPDATED    BRANCH
31909e  sample-task  running   5m ago     issue/sample-task/run-...
6928a3  other-task   blocked   2h ago     issue/sample-task/run-...
abc123  old-task     done      3d ago     issue/old-task/run-...
```

## Format Rules

| Duration | Format |
|----------|--------|
| < 1 minute | `just now` or `<N>s ago` |
| < 1 hour | `<N>m ago` |
| < 24 hours | `<N>h ago` |
| < 7 days | `<N>d ago` |
| >= 7 days | `<N>w ago` or absolute date |

## Options

- `--absolute-time` - Show absolute timestamps instead of relative
- JSON output should include both `updated_at` (ISO8601) and `updated_ago` (human string)

## Benefits

- Quickly identify stale/stuck runs
- More intuitive at a glance
- Matches common CLI patterns (git, docker, kubectl)
