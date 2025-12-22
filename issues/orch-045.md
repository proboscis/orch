---
type: issue
id: orch-045
title: Add continue run feature to resume work from existing runs
status: resolved
---

# Add continue run feature to resume work from existing runs

## Feature Request

Add a `continue run` feature that allows resuming work done by other agents on an existing run/branch.

## Use Case

When an agent run is stopped, blocked, or needs different expertise, users should be able to continue the work with a different agent (or the same agent) without losing progress. The new run should:

- Use the existing worktree (not create a new one)
- Use the existing branch (not create a new branch)
- Reference the original issue
- Optionally allow switching to a different agent type

## Proposed Interface

```bash
# Continue from a specific run
orch continue <issue-id>#<run-id> [--agent <agent-type>]

# Examples
orch continue orch-043#20251222-133847 --agent claude
orch continue orch-043#20251222-133847  # use default agent
```

## Requirements

- Reuse existing worktree from the specified run
- Reuse existing branch from the specified run
- Create a new run record that references the continued-from run
- Allow specifying a different agent type
- Preserve all uncommitted changes in the worktree
- Update run metadata to track continuation history

## Implementation Considerations

- Need to verify the worktree still exists
- Need to handle case where original run is still active (should error or stop it first)
- Consider adding a `continued_from` field in run metadata
