# Agent Instructions for orch

This document contains guidance for AI coding agents working on the orch codebase.

## Architecture Principles

### Daemon as Single Source of Truth

The daemon is the authoritative source for all run/issue state. CLI and TUI clients should:
- **Display** data from daemon APIs without modification
- **Never** compute derived state client-side (sorting, filtering, status classification)
- **Never** shell out to git or make filesystem checks - use daemon APIs instead

### Where Logic Belongs

| Logic Type | Location | Rationale |
|------------|----------|-----------|
| Sorting | Daemon | Consistent across all clients |
| Filtering | Daemon | Single implementation, testable |
| Status computation | Daemon | Has access to all state |
| PR lookup | Daemon | Caching, rate limiting |
| Git operations | Daemon | Centralized git access |
| Display formatting | Client | UI-specific concerns |

## Working with Conflicting PRs

When your PR has conflicts with main after other PRs are merged:

### Preferred Approach: Rebase via Feedback

1. **DO**: Use `orch send <run_id> "rebase message"` to send feedback to the blocked run
2. **DO**: Let the agent resolve conflicts in its existing session context
3. **DO**: Wait for the agent to force push the rebased branch

### Avoid

1. **DON'T**: Close the PR and restart the run from scratch
2. **DON'T**: Cancel the run just because it's blocked
3. **DON'T**: Manually resolve conflicts when the agent can do it

### Why This Matters

- Agent has full session context of what it was doing
- Agent understands its own changes better than a fresh run
- Continuing is faster than restarting
- Preserves conversation history for debugging

### Example

```bash
# Run is blocked with conflicts
orch ps
# Shows: 75731b   orch-383   block   conflict

# Send feedback to resume
orch send 75731b "Your PR has conflicts with main. Please run: git fetch origin main && git rebase origin/main - then resolve conflicts and force push."

# Agent will:
# 1. Fetch latest main
# 2. Rebase onto main
# 3. Resolve conflicts using its context
# 4. Force push
# 5. Continue its work
```

## Run Lifecycle

```
queued → booting → running ⟷ blocked → done
                      ↓         ↓
                    fail     cancel
```

- `blocked`: Run is waiting for user input (use `orch send`)
- `blocked_api`: Run is waiting for API response
- `done`: Run completed successfully
- `fail`: Run encountered an error
- `cancel`: Run was manually cancelled

## Code Style

### Go

- Follow existing patterns in the codebase
- Use the established error handling conventions
- Run `go build ./...` and `go test ./...` before creating PRs
- Proto changes require regeneration: `make proto`

### Python (orch-monitor-tui)

- Follow PEP 8
- Type hints are required
- Use existing patterns in the codebase

## Testing

- Unit tests are required for new daemon functionality
- Integration tests in `internal/e2e/` for end-to-end flows
- TUI changes should be manually tested with `orch monitor`

## Common Issues

### "Connection refused" when using `orch send`

The opencode server may have stopped while the run shows as "blocked". 
- Check if the run is actually alive: look at the ALIVE column in `orch ps`
- If server stopped, use `orch continue <id>` to restart

### Proto changes not reflected

After modifying `api/orch.proto`:
```bash
make proto  # Regenerates Go and Python bindings
```

### Worktree issues

Orch creates git worktrees for parallel runs. If a worktree is corrupted:
```bash
git worktree remove <path> --force
```
