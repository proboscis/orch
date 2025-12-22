---
type: issue
id: orch-051
title: Control agent inherits parent ORCH_VAULT instead of respecting local .orch/config.yaml
status: resolved
---

# Control agent inherits parent ORCH_VAULT instead of respecting local .orch/config.yaml

## Problem

When running `orch monitor` from a project that has its own `.orch/config.yaml`, the control agent spawned inherits the parent run's `ORCH_VAULT` environment variable instead of respecting the local project's configuration.

### Reproduction Steps

1. Start an orch run in project A (e.g., `~/repos/orch`)
2. From within that run's agent session, navigate to project B (e.g., `~/repos/doeff`) which has its own `.orch/config.yaml` with a different vault path
3. Run `orch issue list` from project B
4. **Expected:** Issues from project B's vault are listed
5. **Actual:** Issues from project A's vault are listed (due to inherited `ORCH_VAULT` env var)

### Environment Variables Set by Parent Run

```
ORCH_BRANCH=issue/orch-005/run-20251221-115704
ORCH_ISSUE_ID=orch-005
ORCH_RUN_ID=20251221-115704
ORCH_RUN_PATH=/Users/s22625/repos/orch/runs/orch-005/20251221-115704.md
ORCH_VAULT=/Users/s22625/repos/orch
ORCH_WORKTREE_PATH=.git-worktrees/orch-005/20251221-115704
```

### Root Cause

The `ORCH_VAULT` environment variable takes precedence over the local `.orch/config.yaml` file.

## Required Work

**This issue requires a comprehensive review of the entire codebase for:**

1. **Environment variable usage consistency** - Audit all places where `ORCH_VAULT` and other `ORCH_*` env vars are read
2. **Config file precedence** - Ensure local `.orch/config.yaml` takes precedence over inherited env vars when present
3. **Config resolution order** - Document and verify the intended precedence:
   - CLI flags (highest)
   - Local `.orch/config.yaml` in current directory
   - Parent directory `.orch/config.yaml` (walking up)
   - Environment variables
   - Global config (lowest)

4. **Affected components to review:**
   - Config loading in all commands
   - Monitor spawn logic
   - Control agent initialization
   - Run/issue/ps commands
   - Any place that reads `ORCH_VAULT` or calls config resolution

## Acceptance Criteria

- [ ] Local `.orch/config.yaml` takes precedence over inherited `ORCH_VAULT` env var
- [ ] All orch commands respect local project config when run from a subdirectory with its own config
- [ ] Config precedence order is documented
- [ ] Tests added for cross-project orch usage scenarios
