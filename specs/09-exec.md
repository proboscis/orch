# orch exec - Execute Commands in Run Environment

## Overview

`orch exec` allows running arbitrary commands within a specific run's environment (worktree). This is useful for:

- Running tests in the run's isolated worktree
- Executing scripts that need the run's environment variables
- Debugging or inspecting the run's state
- Running one-off commands without attaching to the tmux session

## Command

```
orch exec RUN_REF [--] COMMAND [ARGS...]
```

### Arguments

| Argument | Description |
|----------|-------------|
| `RUN_REF` | Run reference: short ID (e.g., `66ff6`), `ISSUE_ID#RUN_ID`, or `ISSUE_ID` (latest run) |
| `COMMAND` | Command to execute |
| `ARGS` | Arguments to pass to command |

### Options

| Option | Description |
|--------|-------------|
| `--env KEY=VALUE` | Additional environment variables (repeatable) |
| `--no-orch-env` | Don't set ORCH_* environment variables |
| `--shell` | Run command through shell (`sh -c`) |
| `--quiet` | Suppress status messages, only show command output |

### Environment Variables

By default, the following environment variables are set (same as agent launch):

```
ORCH_ISSUE_ID=<issue_id>
ORCH_RUN_ID=<run_id>
ORCH_RUN_PATH=<path_to_run_doc>
ORCH_WORKTREE_PATH=<worktree_path>
ORCH_BRANCH=<branch_name>
ORCH_VAULT=<vault_path>
```

### Working Directory

Command is executed with working directory set to the run's worktree path.

## Examples

```bash
# Run tests in a run's worktree
orch exec 66ff6 -- uv run pytest

# Run a Python script
orch exec 66ff6 -- uv run python new_impl.py

# Check git status in worktree
orch exec orch-010 -- git status

# Run with shell expansion
orch exec 66ff6 --shell -- 'echo $ORCH_BRANCH'

# Add custom environment
orch exec 66ff6 --env DEBUG=1 -- ./run_tests.sh

# Quiet mode for scripting
orch exec 66ff6 --quiet -- cat README.md
```

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Command succeeded |
| 6 | Run not found |
| 3 | Worktree not found or inaccessible |
| * | Command's exit code (passed through) |

## Behavior

1. Resolve `RUN_REF` to a run using standard resolution (short ID → RUN_REF → ISSUE_ID)
2. Verify run's worktree path exists
3. Set up environment variables
4. Execute command in worktree directory
5. Pass through command's stdout/stderr
6. Return command's exit code

### Error Cases

- **Run not found**: Exit with code 6
- **Worktree missing**: Exit with code 3, suggest using `orch attach` to recreate
- **Command not found**: Pass through shell's exit code (typically 127)

## Implementation Notes

### Command Structure

```go
type execOptions struct {
    Env       []string
    NoOrchEnv bool
    Shell     bool
    Quiet     bool
}

func newExecCmd() *cobra.Command {
    opts := &execOptions{}
    cmd := &cobra.Command{
        Use:   "exec RUN_REF [--] COMMAND [ARGS...]",
        Short: "Execute command in run's worktree environment",
        Args:  cobra.MinimumNArgs(2),
        RunE: func(cmd *cobra.Command, args []string) error {
            return runExec(args[0], args[1:], opts)
        },
    }
    // ... flag registration
    return cmd
}
```

### Key Steps

1. Use `resolveRun(st, refStr)` for run resolution
2. Check `run.WorktreePath` exists with `os.Stat()`
3. Build environment from `LaunchConfig.Env()` pattern
4. Use `exec.Command()` with:
   - `cmd.Dir = run.WorktreePath`
   - `cmd.Env = orchEnv + userEnv + os.Environ()`
   - `cmd.Stdout = os.Stdout`
   - `cmd.Stderr = os.Stderr`
   - `cmd.Stdin = os.Stdin`
5. Return `cmd.Run()` error (preserves exit code)

### Shell Mode

When `--shell` is specified:
```go
shellCmd := strings.Join(args, " ")
cmd := exec.Command("sh", "-c", shellCmd)
```
