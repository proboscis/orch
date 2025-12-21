---
type: issue
id: orch-012
title: Implement orch exec command
summary: Run commands in a run's worktree environment
status: open
priority: high
---

# Implement orch exec command

Add `orch exec` command to run arbitrary commands within a specific run's worktree environment.

## Use Case

```bash
# Run tests in a run's isolated worktree
orch exec 66ff6 -- uv run pytest

# Execute a script in run's environment
orch exec 66ff6 -- uv run python new_impl_for_issue.py

# Check git status in worktree
orch exec orch-010 -- git status
```

## Specification

See `specs/09-exec.md` for full specification.

## Implementation Tasks

1. Create `internal/cli/exec.go` with command implementation
2. Add `exec` command to root command in `internal/cli/root.go`
3. Implement options:
   - `--env KEY=VALUE` for additional environment variables
   - `--no-orch-env` to skip ORCH_* variables
   - `--shell` to run through `sh -c`
   - `--quiet` for script-friendly output
4. Handle exit codes properly (pass through command's exit code)
5. Verify worktree exists before execution

## Key Implementation Details

### Run Resolution
Use existing `resolveRun()` helper to support:
- Short ID: `66ff6`
- Full ref: `orch-010#20251221-123456`
- Issue ID (latest run): `orch-010`

### Environment Setup
Reuse pattern from `internal/agent/adapter.go`:
```go
env := []string{
    "ORCH_ISSUE_ID=" + run.IssueID,
    "ORCH_RUN_ID=" + run.RunID,
    "ORCH_RUN_PATH=" + run.Path,
    "ORCH_WORKTREE_PATH=" + run.WorktreePath,
    "ORCH_BRANCH=" + run.Branch,
    "ORCH_VAULT=" + vaultPath,
}
```

### Command Execution
```go
cmd := exec.Command(args[0], args[1:]...)
cmd.Dir = run.WorktreePath
cmd.Env = append(os.Environ(), env...)
cmd.Stdin = os.Stdin
cmd.Stdout = os.Stdout
cmd.Stderr = os.Stderr
return cmd.Run()
```

## Testing

```bash
# Start a run first
orch run orch-001

# Test exec
orch exec orch-001 -- pwd
orch exec orch-001 -- env | grep ORCH
orch exec orch-001 -- git branch
```
