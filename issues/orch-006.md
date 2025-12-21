---
type: issue
id: orch-006
title: Support partial short IDs for run references
status: resolved
priority: high
---

# Support partial short IDs for run references

Allow users to specify partial short IDs (2-6 hex chars) when referencing runs, similar to how git handles commit hashes.

## Current Behavior

```bash
orch attach 31909e   # Works - full 6-char short ID
orch attach 319      # Fails - not recognized
```

## Desired Behavior

```bash
orch attach 31909e   # Works - full match
orch attach 3190     # Works - if unambiguous
orch attach 319      # Works - if unambiguous
orch attach 31       # Works - if unambiguous
orch attach 3        # Error - too short (minimum 2 chars)
orch attach 31       # Error - ambiguous (if multiple matches)
```

## Implementation

Update `resolveRun()` and `GetRunByShortID()` to:

1. Accept 2-6 character hex strings
2. Find all runs where ShortID starts with the given prefix
3. If exactly one match → return it
4. If zero matches → error "run not found: xxx"
5. If multiple matches → error "ambiguous short ID xxx: matches N runs (31909e, 3190ab, ...)"

## Affected Commands

All commands using `resolveRun()`:
- `orch attach`
- `orch show`
- `orch stop`
- `orch tick`
- `orch answer`
- `orch open`
- `orch delete` (future)

## Error Messages

```
# Not found
Error: run not found: 3xy

# Ambiguous
Error: ambiguous run ID '31': matches 3 runs
  31909e  sample-task#20251221-014644
  3190ab  other-task#20251221-020000
  31f2c8  another#20251221-030000
Hint: use more characters to disambiguate
```
