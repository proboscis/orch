# Orch Command Reference

## Run References

Commands that accept a run reference understand:

- `ISSUE_ID#RUN_ID` for a specific run
- `ISSUE_ID` for the latest run of an issue
- short hex ID when unambiguous

## Global Flags

Common flags across `orch` commands:

| Flag | Description |
|------|-------------|
| `--project` | Project identity / normalized repo ID |
| `--remote` | Remote daemon address (same as `ORCH_REMOTE`) |
| `--json` | JSON output |
| `--tsv` | TSV output |
| `--quiet` | Suppress human-oriented output |
| `--log-level` | `error`, `warn`, `info`, `debug` |

## Client Config (`client.yaml`)

The default master and named master aliases live in `client.yaml` — global at
`~/.config/orch/client.yaml`, or per-repo at `<repo>/.orch/client.yaml`:

```yaml
remote:
  default: "master-host:7777"
  hosts:
    primary:
      addr: "master-host:7777"
    mac:
      addr: "laptop-host:7777"
```

Keep `hosts:` addresses in sync with actual hostnames — stale entries from a machine
migration silently break routing. Execution-target name→host mapping is separate: that
lives in `config.targets` of the **master's** config (see Remote-Master Pitfalls in
SKILL.md).

---

## Issue Management

### `orch issue create [ISSUE_ID]`

Create a new issue.

For multi-line issue bodies, prefer redirected stdin/heredoc over a long escaped
`--body` string.

```bash
# Local backend
orch issue create plc-123 --title "Fix login timeout"

# Add summary/tags with a multi-line body
orch issue create plc-123 \
  --title "Fix login timeout" \
  --summary "Login requests can hang for 30s" \
  --tag bug --tag auth <<'EOF'
Repro steps...
Collect the auth logs.
Confirm the timeout path.
EOF

# Short single-line body
orch issue create plc-123 --title "Fix login timeout" --body "Repro steps..."

# Open editor after creation
orch issue create plc-123 --title "Draft issue" --edit
```

Notes:

- Local backend requires `ISSUE_ID`.
- GitHub backend can omit `ISSUE_ID` because GitHub assigns it.
- **Requires explicit project identity**: pass `--project <origin URL>` (e.g.
  `--project git@github.com:your-org/your-repo.git`) or set `ORCH_PROJECT`. Running
  inside a git repo with `origin` set is NOT sufficient — observed failure
  `project identity required` against daemon @6b2bceb1.
- With a remote master and the file backend, the issue file is created on the **master's**
  checkout of the project (e.g. `~/repos/<repo>/VAULT/Issues/ISSUE-X.md` on the master host).
- `no store available for project_id "X" (register daemon project mapping)` means the repo
  is not registered on the master — see **Project Mapping** in SKILL.md for the 3-file
  registration procedure (checkout + `.orch/config.yaml` + `~/.config/orch/projects/<id>.yaml`;
  no daemon restart needed).

### `orch issue list`

List issues, optionally filtering by status or tags.

```bash
orch issue list
orch issue list --status open
orch issue list --tag bug --tag urgent
orch issue list --tag-any bug,enhancement
orch issue list --json
```

### `orch open ISSUE_ID|RUN_REF`

Open an issue or run document in your editor or another app.

```bash
orch open plc-123
orch open plc-123#20260312-101500
orch open a3b4c5
orch open plc-123 --print-path
```

---

## Run Management

### `orch run ISSUE_ID`

Create a run, create its worktree, and launch the selected agent.

```bash
orch run plc-123
orch run plc-123 --agent codex
orch run plc-123 --agent opencode --model anthropic/claude-opus-4-6 --model-variant max
orch run plc-123 --on remote
orch run plc-123 --reuse
orch run plc-123 --json
```

Useful flags:

| Flag | Description |
|------|-------------|
| `--agent` | `claude`, `codex`, `gemini`, `opencode`, `custom` |
| `--agent-cmd` | Command for `custom` agent |
| `--base-branch` | Base branch for worktree. ⚠ resolves the ORIGIN ref when the name exists on the remote, even if the local branch is ahead — for local-only bases use an alias branch that exists only locally (see SKILL.md「Branch Resolution」) |
| `--branch` | Explicit branch name |
| `--model` | OpenCode model (`provider/model`) |
| `--model-variant` | OpenCode variant (`high`, `max`, etc.) |
| `--multiplexer` | `tmux` or `zellij` |
| `--no-pr` | Skip PR instructions in prompt |
| `--on` | Target name from `config.targets` |
| `--preset` | Named model preset from config |
| `--profile` | Agent profile |
| `--prompt-template` | Custom prompt template |
| `--reuse` | Reuse latest `waiting` or `rate_limited` run |
| `--run-id` | Explicit run ID |
| `--session-name` | Explicit session name |
| `--dry-run` | Show plan without executing |

Remote note:

- `--on <target>` chooses a configured execution target.
- `ORCH_REMOTE=<master>` chooses which master daemon the CLI talks to.
- Those are separate concerns.

### `orch restart-from [RUN_REF|ISSUE_ID]`

Restart work from an existing run by reusing its worktree and branch.

Use this only for terminal runs:

- `failed`
- `canceled`
- `unknown`

Do **not** use it for live runs:

- `running`
- `waiting`
- `rate_limited`
- `booting`
- `queued`
- `pr_open`

Use `orch send` for `waiting` runs.

```bash
orch restart-from plc-123
orch restart-from plc-123#20260312-101500
orch restart-from a3b4c5 --agent codex
orch restart-from --branch "issue/plc-123/run-20260312-101500" --issue plc-123
```

### `orch wait`

Block until a run reaches a "needs attention" state (`waiting`, `pr_open`,
`failed`, etc). This is the **canonical way to wait for an agent** — do not poll
`orch ps` in a loop.

```bash
orch wait <RUN_REF>                          # block forever (no timeout)
orch wait <RUN_REF> --timeout 300            # block up to 5 minutes
orch wait <RUN_REF1> <RUN_REF2> ...          # wait on any of N runs
```

`<RUN_REF>` is the **short hash** (e.g. `1a6e1e`) — same id used by `orch send`.

Output is a single JSON line on the run that triggered:

```json
{"run_id":"32c5b6","status":"waiting","issue":"my-issue","pr_url":"https://github.com/your-org/your-repo/pull/399"}
```

Returns exit `0` immediately if the run is already in an attention state. Returns
non-zero on timeout or invalid ref. **Use this instead of polling** — `orch ps`
output contains ANSI color escapes even when piped, which silently breaks
text-matching loops.

### `orch ps`

List runs with current status and execution-host information.

```bash
orch ps
orch ps --status running,waiting,rate_limited
orch ps --issue plc-123
orch ps --all
orch ps --json
orch ps --tsv
```

Current run statuses:

- `queued`
- `booting`
- `running`
- `waiting`
- `rate_limited`
- `pr_open`
- `done`
- `failed`
- `canceled`
- `unknown`

Important output semantics:

- table output includes a `HOST` column for the actual execution host
- JSON output includes `target_host`
- `target_host` may be populated even when logical `target` is empty
- **default table output emits ANSI color escape codes even when stdout is not a TTY**
  — text matching against words like `wait`/`run`/`done` will not work. Use
  `orch ps --json` (no escapes) for parsing, or `orch wait` for blocking on state
  changes. This bit a poll loop in `orch-review-loop`; the lesson is "don't parse
  the table output, use JSON or `orch wait`".

Example JSON fragment:

```json
{
  "issue_id": "plc-123",
  "run_id": "20260312-101500-codex",
  "target": "",
  "target_host": "mac-host",
  "status": "running"
}
```

TSV ordering begins with:

```text
issue_id  issue_status  run_id  short_id  agent  model  model_variant  target  target_host  status ...
```

### `orch show RUN_REF`

Inspect run details, events, and artifacts.

```bash
orch show plc-123
orch show plc-123#20260312-101500
orch show a3b4c5
orch show plc-123 --tail 100
orch show plc-123 --events-only
orch show plc-123 --json
```

Use `show --json` when you need:

- `target_host`
- `session_name`
- `server_port`
- `opencode_session_id`
- artifact history

### `orch stop [ISSUE_ID|ISSUE_ID#RUN_ID]`

Stop runs and mark them canceled.

```bash
orch stop plc-123
orch stop plc-123#20260312-101500
orch stop a3b4c5
orch stop --all
orch stop plc-123 --force
```

Behavior:

- `ISSUE_ID` stops all active runs for that issue
- `--force` can mark canceled even if the session is already gone

### `orch resolve ISSUE_ID`

Mark the issue itself as resolved.

```bash
orch resolve plc-123
orch resolve plc-123 --force
```

---

## Worker Execution Plane

### `orch worker start`

Start the managed local worker process. Since v1.5 (ADR-0002) this is usually
automatic: the master auto-starts its colocated worker on demand, and
`orch run` against a remote master auto-starts the local worker for it.
Manual start remains for other hosts and for `ORCH_WORKER_AUTOSTART=0`.

```bash
# Start a local worker against the local daemon/master
orch worker start

# Start a local worker that registers to a remote master
ORCH_REMOTE=master-host:7777 orch worker start

# Start a specific worker ID
ORCH_REMOTE=master-host:7777 orch worker start --worker-id host-mac
```

Important behavior:

- `worker start` is local-host scoped
- `ORCH_REMOTE=<master>` changes which master the local worker registers to
- it does not start a worker on the remote host

### `orch worker status`

Inspect the managed local worker and its master registration.

```bash
ORCH_REMOTE=master-host:7777 orch worker status
ORCH_REMOTE=master-host:7777 orch worker status --json
```

Interpretation:

- `local`: local managed process state, PID, log path, last error
- `master`: whether the worker is registered and active on the selected master

Example JSON shape:

```json
{
  "worker_id": "host-mac",
  "local": {
    "process_exists": true,
    "state": "running"
  },
  "master": {
    "state": "active"
  }
}
```

### `orch worker stop`

Stop the managed local worker process.

```bash
ORCH_REMOTE=master-host:7777 orch worker stop
ORCH_REMOTE=master-host:7777 orch worker stop --all
ORCH_REMOTE=master-host:7777 orch worker stop --worker-id host-mac
```

---

## Monitoring and Session Control

### `orch-monitor`

Standalone Python monitor for issue/run management.

```bash
orch-monitor
orch-monitor --project github.com/owner/repo
orch-monitor --agent codex
orch-monitor --new
orch-monitor --new-control-agent
orch-monitor --multiplexer tmux
```

### `orch attach RUN_REF`

Attach to the run's live session on its execution host.

```bash
orch attach plc-123#20260312-101500
orch attach a3b4c5
```

Current behavior:

- for local runs, attaches locally
- for remote runs, routes to the execution host over SSH
- for `tmux` and `zellij`, attaches to the multiplexer session
- for OpenCode, attaches to the OpenCode session/server

Headless note:

- in non-interactive environments, `attach` may only prove attach-path preflight

### `orch capture RUN_REF`

Capture run output without attaching.

```bash
orch capture plc-123
orch capture plc-123 --lines 200
orch capture plc-123 --json
orch capture a3b4c5
```

Current behavior:

- local runs capture locally
- remote runs capture from the execution host
- `opencode` captures transcript content
- `tmux` / `zellij` captures pane output

Fail-fast expectation:

- missing session/server should return an explicit error
- capture should not silently return empty output on infrastructure failure

### `orch send RUN_REF [MESSAGE]`

Send input to a live run.

```bash
orch send plc-123 "Please focus on the tests first"
orch send a3b4c5 "Continue with the implementation"
orch send plc-123 <<'EOF'
Please fix the auth failure first.
Then rerun the focused tests.
EOF
orch send plc-123 "partial input" --no-enter
```

Behavior:

- `tmux` / `zellij`: sends keys through the multiplexer
- `opencode`: sends via OpenCode HTTP API
- `--no-enter` only matters for multiplexer-backed agents

Best practice:

```bash
orch capture plc-123
orch send plc-123 "Please address the auth failure first"
orch capture plc-123
```

If you omit `MESSAGE`, `orch send` reads from redirected stdin, which is useful
for multi-line feedback via pipes or heredocs.

### `orch exec RUN_REF -- COMMAND [ARGS...]`

Run an arbitrary command inside the run worktree.

```bash
orch exec plc-123 -- uv run pytest
orch exec plc-123 -- git status
orch exec plc-123 --shell -- "echo $ORCH_ISSUE_ID && pwd"
orch exec plc-123 --env DEBUG=1 -- ./script.sh
```

---

## Maintenance

### `orch repair`

Repair known daemon and run-state inconsistencies.

```bash
orch repair --dry-run
orch repair
orch repair --force
```

Typical uses:

- restart unhealthy daemon
- mark stale `running` runs as failed when their session is gone
- report orphaned sessions or worktrees

### `orch tick [RUN_REF]`

Resume waiting runs when their questions are satisfied.

```bash
orch tick plc-123#20260312-101500
orch tick --all
orch tick --all --max 5
orch tick plc-123 --agent claude
```

This is for `waiting` runs, not for general live-run control.

---

## Remote Execution Examples

### Remote master, local worker

```bash
export ORCH_REMOTE=master-host:7777

orch worker start
orch worker status --json
orch run plc-123 --agent codex
orch ps --json
```

Expected interpretation:

- worker process is on the local machine
- master state is on `master-host:7777`
- `ps` / `show --json` tell you the actual execution host via `HOST` / `target_host`

### Operator-host control of remote run

```bash
export ORCH_REMOTE=master-host:7777

orch capture plc-123#20260312-101500
orch send plc-123#20260312-101500 "Please reply with ACK"
orch attach plc-123#20260312-101500
```

Requirements:

- SSH reachability from operator host to execution host
- matching multiplexer or OpenCode runtime on the execution host

---

## Troubleshooting

### Run is `waiting`

This is a live run, not a failed one.

Use:

```bash
orch capture <RUN_REF>
orch send <RUN_REF> "your answer"
```

Do not use:

```bash
orch restart-from <RUN_REF>
```

### `capture` / `send` fails

Treat this as transport or runtime failure first:

1. `orch capture <RUN_REF>`
2. `orch ps`
3. `orch show <RUN_REF> --json`
4. inspect worker status, session host, and host-local runtime

### OpenCode run fails

Distinguish two failure classes:

- **orch/runtime/bootstrap failure**
  - session not created
  - server not reachable
  - explicit bootstrap/session error from orch
- **provider/auth failure inside OpenCode**
  - OpenCode session exists
  - `attach` / `capture` / `send` work
  - the UI shows provider/token/model errors

Only the first class is an orch control-plane problem.
