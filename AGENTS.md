# Agent Instructions for orch

This document is the canonical instruction file for AI coding agents working on
the orch codebase. `CLAUDE.md` is kept only as a compatibility alias.

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

### Fail Fast, Fail Clearly

When a command cannot complete, orch should fail immediately with an explicit,
actionable error instead of returning partial, empty, or placeholder output.

Required behavior:
- Do not silently return empty content, `None`, or success-with-missing-data for failed operations.
- Include the concrete failing component in the error when possible:
  host, session, multiplexer, opencode server, worktree, or daemon.
- Prefer a hard error over a fallback that hides the real problem.
- For host-routed run control (`attach`, `capture`, `send`), behavior must be consistent regardless of which host the run is executing on.

Examples:
- Bad: `orch capture` prints nothing when the target session is missing.
- Good: `orch capture` fails with `session not found on host mac-host for run orch-123#...`.
- Bad: `orch send` reports success when the remote control path was never reached.
- Good: `orch send` fails with the exact host/session/opencode error and the next diagnostic step.

### Coupling Cores and Change Routing

Some clusters of design decisions in this codebase are mutually constraining:
changing one silently invalidates the others' premises. These coupling cores
are inventoried in [docs/design/coupling-core-roadmap.md](docs/design/coupling-core-roadmap.md)
(the watchlist table). The run-status transition core is specified in
[docs/design/run-state-machine.md](docs/design/run-state-machine.md).

**Routing rule.** Work that touches a core goes to a frontier model plus a
human reviewer, and ships its law/spec/invariant revision in the same change
set. Work outside the cores can be delegated to sub-frontier agents as a
verified issue. Core surfaces today:

- `internal/daemon/step.go`, `internal/daemon/monitor.go` (transition policy
  and its executor)
- `internal/daemon/socket.go`, `internal/daemon/proto_handler.go` launch
  ladders (frozen status writers, Phase B consolidation)
- `internal/daemon/worker_plane.go` (worker lease ownership and heartbeats)
- `internal/model/event.go`, `internal/model/run.go` (event fold = store of
  record)

**Status write surface.** Run status events are appended in exactly one
sanctioned place: `Daemon.updateStatus`. The semgrep rule
`run-status-write-surface` enforces this; the `nosemgrep` annotations on
existing writers are frozen legacy and may only shrink. Never add a new
writer or a new `nosemgrep` annotation — extend `step()` with a new
observation or effect instead.

**Tripwire checklist for reviewing delegated PRs.** Any single hit means
stop and re-route to frontier + human:

- a new ID type for something that previously had no identity
- a new registry / lookup side-table mapping an id to a live object
- a shared mutable flag synchronizing two copies of state
- a clone/copy of something conceptually move-only or single-owner
- a cache or accumulated field mirroring live state
- a "keep in sync" comment between two representations
- a new `nosemgrep` annotation on any architecture rule

**Choice-space gate.** Do not delegate work near a core until the choice
space is closed: enumerate the options as explicit equations/laws and fill
the spec holes first. A delegate fills any open classification space with
the cheapest compliant answer.

**Burn-in loop.** When the same review comment repeats about three times,
encode it as a semgrep rule or convention here instead of repeating it.

## Working with Conflicting PRs

When your PR has conflicts with main after other PRs are merged:

### Preferred Approach: Rebase via Feedback

1. **DO**: Use `orch send <run_id> "rebase message"` to send feedback to the waiting run
2. **DO**: Let the agent resolve conflicts in its existing session context
3. **DO**: Wait for the agent to force push the rebased branch

### Avoid

1. **DON'T**: Close the PR and restart the run from scratch
2. **DON'T**: Cancel the run just because it's waiting
3. **DON'T**: Manually resolve conflicts when the agent can do it

### Why This Matters

- Agent has full session context of what it was doing
- Agent understands its own changes better than a fresh run
- Continuing is faster than restarting
- Preserves conversation history for debugging

### Example

```bash
# Run is waiting with conflicts
orch ps
# Shows: 75731b   orch-383   wait   conflict

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
queued → booting → running ⟷ waiting → done
                      ↓         ↓
                    fail     cancel
```

- `waiting`: Run is waiting for user input (use `orch send`)
- `rate_limited`: Run is waiting for API response
- `done`: Run completed successfully
- `fail`: Run encountered an error
- `cancel`: Run was manually cancelled

### Architecture Lint (Enforced by Semgrep)

The daemon boundary is mechanically enforced via semgrep rules in `.semgrep/architecture.yaml`.

```bash
make lint          # Strict architecture lint (exits non-zero on violations)
```

**Before creating PRs that touch `internal/cli/` or `internal/monitor/`**, run `make lint` and ensure you are not introducing new violations. The rules enforce:
- CLI must not import `internal/git`, `internal/pr`, `internal/store`
- CLI must not shell out to `git` or call git functions directly
- CLI must use `orchapi.OrchAPI` (via `getAPI()`), not `daemon` package directly
- Monitor must not import `internal/git`, `internal/pr`, or shell out to `git`/`gh`

Existing violations are tracked; do not add new ones.

## Code Style

### Go

- Follow existing patterns in the codebase
- Use the established error handling conventions
- Run `go build ./...` and `go test ./...` before creating PRs
- Run `make lint` to check for architecture boundary violations
- Proto changes require regeneration: `make proto`

### Python (orch-monitor-tui)

- Follow PEP 8
- Type hints are required
- Use existing patterns in the codebase

## Testing

- Unit tests are required for new daemon functionality
- Integration tests in `internal/e2e/` for end-to-end flows
- TUI changes should be manually tested with `orch monitor`

Canonical E2E instruction docs:
- [docs/e2e-master-worker-client.md](/Users/s22625/.codex/worktrees/0b31/orch/docs/e2e-master-worker-client.md)
- [docs/e2e-backend-matrix.md](/Users/s22625/.codex/worktrees/0b31/orch/docs/e2e-backend-matrix.md)
- [docs/e2e-automation-plan.md](/Users/s22625/.codex/worktrees/0b31/orch/docs/e2e-automation-plan.md)

Use those files as the source of truth for E2E validation requirements. In
particular, backend/session-control changes must follow the real-agent matrix in
`docs/e2e-backend-matrix.md`, not a reduced `custom`-only substitute.

## Common Issues

### "Connection refused" when using `orch send`

The opencode server may have stopped while the run shows as "waiting". 
- Check if the run is actually alive: look at the ALIVE column in `orch ps`
- Capture output first: `orch capture <id>`
- Check multiplexer sessions directly (`tmux list-sessions` / `zellij list-sessions`)
- If needed, write feedback into `ORCH_PROMPT.md` in the run worktree and use native multiplexer send
- Do NOT use `orch restart-from` for send failures unless the run is truly `failed`, `canceled`, or `unknown`

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

## Communication Style

### Use ASCII Diagrams When Explaining Context

When passing the turn back to the user (e.g., summarizing what was done, explaining a bug, describing architecture), use ASCII diagrams to make the explanation concrete and visual. This is especially useful for:

- Data/control flow through multiple components
- Timeline-based problems (race conditions, timeouts)
- Before/after comparisons of a fix
- Architecture boundaries and layer relationships
