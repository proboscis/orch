# Event Reference

Events are append-only records that track everything that happens during a run. They form the complete history and are the source of truth for run state.

## Event Format

Events are stored as markdown list items in run files:

```
- <timestamp> | <type> | <name> | key=value | key=value ...
```

### Components

| Component | Description | Example |
|-----------|-------------|---------|
| timestamp | ISO8601 datetime | `2026-01-20T16:30:45+09:00` |
| type | Event category | `status`, `artifact`, `phase` |
| name | Specific event | `running`, `branch`, `implement` |
| key=value | Additional data | `agent=claude`, `url=...` |

## Event Types

### status

Tracks run state changes. These events determine the current status of a run.

| Name | Description | Example |
|------|-------------|---------|
| `queued` | Run created, waiting to start | `status \| queued \|` |
| `booting` | Agent is starting | `status \| booting \| agent=claude` |
| `running` | Agent is working | `status \| running \|` |
| `waiting` | Needs human input | `status \| waiting \| reason=gate_login` |
| `rate_limited` | API/rate limit issue | `status \| rate_limited \| error=rate_limit` |
| `pr_open` | PR created | `status \| pr_open \|` |
| `done` | Completed successfully | `status \| done \|` |
| `failed` | Error occurred | `status \| failed \| error=...` |
| `canceled` | Manually stopped | `status \| canceled \|` |
| `unknown` | Agent exited unexpectedly | `status \| unknown \| reason=agent_exited` |

#### Status reasons

Status events may carry a machine-readable `reason` attribute
(`model.AttrStatusReason`) explaining why the verdict was reached.
`orch ps` renders it inline, e.g. `unknown(never_alive)` or `waiting(gate_login)`.

| Reason | Attached to | Description |
|--------|-------------|-------------|
| `never_alive` | `unknown` | Agent was never observed alive and the boot grace expired |
| `session_lost` | `unknown` | Agent was alive but the backend lost observability of its session |
| `agent_exited` | `unknown` | Agent process exited without a verdict, shell prompt showing |
| `observer_unverified` | `unknown` | Dead-check threshold reached via an observation channel that never saw this run alive |
| `launch_<step>` | `failed` | Launch failed at the named bootstrap step |
| `gate_<kind>` | `waiting` | Run is stopped at an interactive gate (e.g. `gate_login`, `gate_trust`; trust gates are auto-acknowledged by the daemon once per run) |
| `pr_branch_mismatch` | `waiting` | Agent opened a PR from a branch other than the run's assigned branch; the PR was not attached to the run |

### artifact

Records outputs and resources created during the run. These names are
consumed by the run state fold (`DeriveState`) to populate run fields.

| Name | Description | Example |
|------|-------------|---------|
| `worktree` | Git worktree path | `artifact \| worktree \| path=/path/to/worktree` |
| `branch` | Git branch name | `artifact \| branch \| name=issue/x/run-y` |
| `target` | Execution target (host/worker) | `artifact \| target \| name=remote \| host=remote-host \| worker_id=...` |
| `session` | Multiplexer session | `artifact \| session \| name=run-x-y \| multiplexer=tmux` |
| `window` | Multiplexer window ID | `artifact \| window \| id=@42` |
| `pr` | Pull request URL | `artifact \| pr \| url=https://github.com/.../pull/42` |
| `server` | opencode server port | `artifact \| server \| port=4096` |
| `opencode_session` | opencode session ID | `artifact \| opencode_session \| id=ses_abc123` |
| `agent_model` | Model reported by the agent | `artifact \| agent_model \| model=... \| variant=...` |
| `error` | Persisted error message | `artifact \| error \| message="..."` |

### phase

Tracks workflow stages within a run.

| Name | Description |
|------|-------------|
| `plan` | Agent is analyzing and planning |
| `implement` | Writing code |
| `test` | Running tests |
| `pr` | Creating pull request |
| `review` | Addressing review feedback |

Example:
```
- 2026-01-20T16:30:00+09:00 | phase | plan |
- 2026-01-20T16:35:00+09:00 | phase | implement |
- 2026-01-20T16:50:00+09:00 | phase | test |
- 2026-01-20T16:55:00+09:00 | phase | pr |
```

### test

Records test execution results.

```
- <ts> | test | <test_name> | result=PASS|FAIL | log=...
```

Examples:
```
- 2026-01-20T16:50:00+09:00 | test | unit | result=PASS | count=42
- 2026-01-20T16:51:00+09:00 | test | integration | result=FAIL | error="timeout"
```

### note

Human-added notes or comments, plus daemon-authored notices.

```
- <ts> | note | <title> | text="..."
```

Example:
```
- 2026-01-20T17:00:00+09:00 | note | manual_fix | text="Fixed import manually"
```

The daemon records its own one-time interventions as `daemon_notice` note
events. Current kinds: `gate_ack` (the daemon auto-acknowledged a trust gate
by sending Enter) and `pr_branch_mismatch` (the daemon sent the agent a
corrective message about a PR opened from the wrong branch). These serve as
the once-only ledger — a notice is never re-sent for the same cause.

```
- <ts> | note | daemon_notice | kind=gate_ack gate=trust
```

## Event Flow Example

A typical run produces events like this:

```markdown
## Events

- 2026-01-20T16:30:00+09:00 | status | queued |
- 2026-01-20T16:30:01+09:00 | status | booting | agent=claude
- 2026-01-20T16:30:05+09:00 | artifact | worktree | path=/Users/me/.orch/worktrees/my-issue/abc123
- 2026-01-20T16:30:06+09:00 | artifact | branch | name=issue/my-issue/run-20260120-163000
- 2026-01-20T16:30:10+09:00 | status | running |
- 2026-01-20T16:30:15+09:00 | phase | plan |
- 2026-01-20T16:32:00+09:00 | phase | implement |
- 2026-01-20T16:45:00+09:00 | phase | test |
- 2026-01-20T16:46:00+09:00 | test | unit | result=PASS | count=15
- 2026-01-20T16:47:00+09:00 | phase | pr |
- 2026-01-20T16:48:00+09:00 | artifact | pr | url=https://github.com/org/repo/pull/42
- 2026-01-20T16:48:01+09:00 | status | pr_open |
```

## Reading Events

### CLI

```bash
# Show all events
orch show my-issue --events-only

# Show recent events
orch show my-issue --tail 20
```

### SQL Query

```sql
-- Requires --with-events flag
SELECT * FROM events 
WHERE run_id = '20260120-163000'
ORDER BY timestamp;
```

### Direct file access

Events are stored in the run file at `runs/<issue>/<run>.md`:

```bash
cat ~/orch-issues/runs/my-issue/20260120-163000.md
```

## Creating Events

Events are typically created automatically by:
- orch commands (`run`, `stop`, etc.)
- The monitoring daemon
- Agent completion detection

## Event Storage

Events are stored as part of the run markdown file:

```
runs/
└── my-issue/
    └── 20260120-163000.md
```

The file structure:
```yaml
---
issue_id: my-issue
run_id: 20260120-163000
agent: claude
status: running
# ... other frontmatter
---

# Run: my-issue#20260120-163000

## Events

- 2026-01-20T16:30:00+09:00 | status | queued |
- 2026-01-20T16:30:01+09:00 | status | booting | agent=claude
# ... more events
```

## Best Practices

### For custom agents

If building custom tooling:
1. Append events chronologically
2. Never modify existing events
3. Use ISO8601 timestamps with timezone
4. Include relevant key=value pairs

### For analysis

Events enable powerful queries:
```sql
-- Average time from start to PR
SELECT AVG(
  julianday(pr_time) - julianday(start_time)
) * 24 * 60 as avg_minutes
FROM (
  SELECT 
    run_id,
    MIN(CASE WHEN name = 'booting' THEN timestamp END) as start_time,
    MAX(CASE WHEN name = 'pr_open' THEN timestamp END) as pr_time
  FROM events
  GROUP BY run_id
)
WHERE pr_time IS NOT NULL;
```
