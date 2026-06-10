---
name: orch-toolset
description: |
  Use when working with the orch CLI for issue management, agent runs, worker/master execution,
  and host-aware session control. Covers orch issue create/list/open, orch run/ps/show/stop/resolve/
  restart-from, orch worker start/status/stop, orch attach/capture/send/exec, and remote execution
  via ORCH_REMOTE and target_host. Trigger terms: orch, orchestrator, worker, master, ORCH_REMOTE,
  target_host, run management, issue management, agent runs, worktree.
version: 1.2.0
---

# Orch Toolset

Orch is a non-interactive orchestrator for managing LLM agent runs around issues,
worktrees, and append-only run events.

## Current Execution Model

- **Issue**: unit of work specification.
- **Run**: one execution attempt for an issue.
- **RUN_REF**: `ISSUE_ID#RUN_ID`, `ISSUE_ID` for latest run, or short hex ID.
- **Master**: the daemon endpoint that stores issue/run state.
- **Worker**: a long-lived host-local executor process that registers to a master.
- **Execution host**: the host where the run session actually lives. `orch ps` shows this in
  the `HOST` column, and JSON output exposes it as `target_host`.

Important remote rule:

- `ORCH_REMOTE=<master>` points the CLI and local worker at that master.
- The default master can also be set durably in `client.yaml` — globally in
  `~/.config/orch/client.yaml` or per-repo in `<repo>/.orch/client.yaml`:

  ```yaml
  remote:
    default: "zeus:7777"
  ```

- It does **not** mean `worker start` happens on the remote host.
- `orch worker start` is local-host scoped. The worker process is started on the machine where
  you run the command, then it connects to the configured master.
- When talking to a remote master, pass `--project <origin URL>` (or set `ORCH_PROJECT`)
  explicitly on every command. CWD-based inference resolves against the master's own context,
  not your repo: observed with daemon @6b2bceb1, `issue list` without `--project` returned the
  master repo's issues, and `issue create` fails with `project identity required` even inside
  a git repo with `origin` set.

## Core Workflow

1. Create or inspect the issue with `orch issue create`, `orch issue list`, or `orch open`.
2. If using a remote master, start or verify the **local** worker first:
   `ORCH_REMOTE=<master> orch worker start`
3. Start work with `orch run <issue-id>`.
4. Track state with `orch ps` and inspect details with `orch show`.
5. Interact with the live run via `orch capture`, `orch send`, and `orch attach`.
6. Use `orch stop` only for actually stale or canceled work. Use `orch restart-from` only for
   failed, canceled, or unknown runs. Mark completed work with `orch resolve`.

## Run States

Use the current run states only:

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

Operational guidance:

- `waiting`: run is alive and waiting for user input. Use `orch send`.
- `rate_limited`: run is alive but blocked on provider/API pacing. Do not restart it blindly.
- `running`, `waiting`, and `rate_limited` are all live states. Do not use `orch restart-from`
  on them.
- `restart-from` is for `failed`, `canceled`, or `unknown` runs.

## Host-Aware Session Control

`attach`, `capture`, and `send` are execution-host aware.

- For local runs, they operate on the local host.
- For remote runs, they route to the run's execution host.
- Remote `attach` / `capture` / `send` require SSH reachability from the operator host.
- `tmux` and `zellij` runs use multiplexer control.
- `opencode` runs use OpenCode HTTP session control.

`attach` notes:

- Prefer `attach` when you need true interactive handoff.
- In headless environments, `attach` may only prove that the attach path reaches the interactive
  boundary; it may not stay attached to the UI.

## Worker / Master Plane

Use the worker commands when operating across hosts:

- `orch worker start`
- `orch worker status`
- `orch worker stop`

What to expect:

- `worker status` shows both:
  - local managed-process state
  - master registration state
- `ps` and `show --json` surface the execution host via `HOST` / `target_host`.
- Repeating `worker start` on the same host/profile should reuse the same managed worker rather
  than creating duplicates.

Remote example:

```bash
export ORCH_REMOTE=zeus:7777
orch worker start
orch worker status --json
orch run plc-123
orch ps --json
```

Interpretation:

- the worker process started on **this** machine
- the worker registered to `zeus:7777`
- the run's `target_host` / `HOST` tells you where the session actually runs

## Remote-Master Pitfalls (verified behaviors)

- **Target resolution happens on the master.** `--on <target>` names are mapped to a host by
  `config.targets` as loaded by the **master daemon**, and the resolved host is **baked into
  the run at creation**. Fixing a client-side config does not change what new runs resolve to —
  the master's config (on the master host) is what counts, and the daemon reads it at startup.
- **Stale target host after a machine rename/migration**: `orch run` appears to succeed
  (worktree created), but the session never launches, and `capture`/`send`/`stop` all fail with
  `no active worker available for target "host-<old-hostname>"`. Fix: update `targets:` in the
  config on the **master host**, then restart the master daemon.
- **Runs created before the fix keep the stale baked host** and cannot even be stopped
  normally. Recovery: temporarily register a worker under the old ID, stop, then remove it:

  ```bash
  orch worker start --worker-id host-<old-hostname>
  orch stop <ISSUE> --force
  orch worker stop --worker-id host-<old-hostname>
  ```

- **Workers do not reconnect forever.** A master restart or a network blip can leave the local
  worker `exited` (see `Last Error` in `orch worker status`). Verify the worker at the start of
  every orch session and run `orch worker start` again when needed.
- **File-backend issue files live on the master's checkout** (e.g.
  `~/repos/<repo>/VAULT/Issues/ISSUE-X.md` on the master host). From another host,
  `orch open <ISSUE> --print-path` reports `not found` — read the issue via `orch issue list`
  / `orch show` instead.
- **Updating the master binary while its daemon runs** fails with `Text file busy` on `cp`;
  use `rm <bin> && cp <new> <bin>` (the running process keeps its inode), then restart the
  daemon deliberately. `orch repair`'s daemon restart operates on the operator host — restart a
  remote master on its own host (`kill <pid>`, then
  `nohup orch daemon run --listen 0.0.0.0:<port> ...` from the original working directory).

## Model Routing for Runs

`--agent codex` vs `--agent claude` is a quality decision, not a default:

- **codex (cheap/fast tier)**: scope-bounded mechanical or test-verifiable
  implementation — rename sweeps, fixture ports, wiring an explicitly
  specified contract. ~2x faster; satisfies done conditions to the letter.
- **claude frontier (Fable/Opus)**: design-seam issues — architecture
  boundaries, contract migrations, invariant machinery, anything where the
  issue's checklist is a projection of a deeper design. Empirical A/B on
  the same issue (2026-06-11): codex met every done condition; claude
  additionally found and fixed a latent bug the work exposed and migrated
  without leaving dual surfaces. Blind codex-by-default yields
  letter-satisfying, seam-blind results that cost more in review.
- Whoever dispatches owns this call per issue; when unsure, ask "does this
  issue touch a contract or just implement inside one?"

## Control-Agent Patterns

- **Waiting for a run: `orch wait <RUN_REF> [--timeout N]` is the canonical way —
  do NOT poll `orch ps` in a loop or hand-roll tmux watchers.** It blocks until
  any specified run needs attention (waiting/pr_open/done/failed). Pair with a
  background shell invocation to get a single completion notification.
- Use `orch ps --status running,waiting,rate_limited` to focus on live work.
- Use `orch capture` before `orch send`.
- Use `orch show --json` when you need artifacts like `target_host`, `server_port`, or
  `opencode_session_id`.
- Use short IDs for speed once the run is unambiguous.
- Keep live runs alive; prefer guidance over restart.

## Fail Fast, Fail Clearly

Treat silent success as suspicious.

- Missing sessions should fail with an explicit error, not empty output.
- `orch capture` returning nothing on failure is a bug; expect a concrete host/session/server
  error instead.
- If `send` fails, treat it as a transport/runtime problem first, not an invitation to restart.
- OpenCode bootstrap or session-creation failures are infrastructure/runtime failures.
- Provider/auth failures *inside* Claude/Codex/OpenCode are different from orch transport failures.

Recommended triage order:

1. `orch capture <RUN_REF>`
2. `orch ps`
3. `orch show <RUN_REF> --json`
4. inspect the host-specific runtime (`tmux`, `zellij`, worker status, OpenCode logs)

## Workflow Tips

- When issue bodies span multiple lines, prefer `orch issue create ... <<'EOF'` over a long escaped
  `--body` string.
- Use `orch send` to answer `waiting` runs.
- Do not `stop` + `restart-from` a live run just because it asked a question.
- **NEVER stop or restart an agent because its context percentage is low.** Codex agents
  have automatic context compaction — when context fills up, it compacts and the agent
  continues working. Low context % (even 0-2%) does NOT mean the agent is dead. Only stop
  if: user asks, `orch ps` shows `fail`/`done`/`cancel`, or the tmux process has exited.
- Use `orch exec` for isolated test or git commands inside a run worktree.
- Use `worker status --json` when debugging remote execution, host placement, or stale workers.

## Quick Reference

| Goal | Command |
|------|---------|
| Create issue | `orch issue create <ID> --title "..."` (`<<'EOF'` for multi-line body) |
| Start local worker to remote master | `ORCH_REMOTE=<master> orch worker start` |
| Inspect worker | `ORCH_REMOTE=<master> orch worker status --json` |
| Start run | `orch run <ISSUE>` |
| List live runs | `orch ps --status running,waiting,rate_limited` |
| Inspect run metadata | `orch show <RUN> --json` |
| Block until a run needs attention | `orch wait <RUN> [--timeout N]` (canonical; never poll) |
| Get output | `orch capture <RUN>` |
| Send guidance | `orch send <RUN> [message]` |
| Attach interactively | `orch attach <RUN>` |
| Run tests in worktree | `orch exec <RUN> -- <command>` |
| Retry terminal run | `orch restart-from <RUN>` |
| Stop stale run | `orch stop <RUN>` |
| Resolve issue | `orch resolve <ISSUE>` |

For command syntax, flags, and richer examples, see [reference.md](reference.md).
