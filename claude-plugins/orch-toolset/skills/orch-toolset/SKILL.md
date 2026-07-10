---
name: orch-toolset
description: |
  Use when working with the orch CLI for issue management, agent runs, worker/master execution,
  and host-aware session control. Covers orch issue create/list/open, orch run/ps/show/stop/resolve/
  restart-from, orch worker start/status/stop, orch attach/capture/send/exec, and remote execution
  via ORCH_REMOTE and target_host. Trigger terms: orch, orchestrator, worker, master, ORCH_REMOTE,
  target_host, run management, issue management, agent runs, worktree.
version: 1.5.3
---

# Orch Toolset

Orch is a non-interactive orchestrator for managing LLM agent runs around issues,
worktrees, and append-only run events.

## Current Execution Model

- **Issue**: unit of work specification.
- **Run**: one execution attempt for an issue.
- **RUN_REF**: `ISSUE_ID#RUN_ID`, `ISSUE_ID` for latest run, or short hex ID.
- **Issue hex ID (ADR-0001, v1.5+)**: every issue also has a derived hex ID —
  `sha256(issue name)`, shown 8-chars by `orch issue list`. Any unique 7-64
  char hex prefix works wherever an issue is named (`orch run 3f2a91c8`,
  `orch issue show 3f2a91c8`, `orch capture 3f2a91c8`). 2-6 hex chars remain
  run short IDs — the grammars are disjoint by construction. New issue names
  of 2-64 pure hex chars are rejected at creation.
- **Master**: the daemon endpoint that stores issue/run state.
- **Worker**: a long-lived host-local executor process that registers to a master.
- **Execution host**: the host where the run session actually lives. `orch ps` shows this in
  the `HOST` column, and JSON output exposes it as `target_host`.

## Issue Body = the Worker's Brief (REQUIRED structure)

The issue body is the ONLY context the worker agent receives — it cannot see
your conversation, other issues, or your intent. Author every issue as a
complete, self-contained brief with these sections:

- `## Goal` — what must exist or change when the run is done, and why.
- `## Context` — repo-specific facts the worker cannot infer: relevant files,
  prior decisions, links to specs/ADRs.
- `## Constraints` — what must NOT change; dependency/style/scope limits;
  explicit anti-patterns to avoid.
- `## Acceptance criteria` — enumerated, mechanically checkable conditions
  for "done".
- `## Verification` — the exact commands/tests that must pass, and what
  evidence the final report must show.
- `## Out of scope` (optional) — explicitly deferred work.

Authoring mechanics: humans write the body in $EDITOR
(`orch issue create <id> --title "..." --edit` / `orch issue edit <id>`);
agents and scripts pipe the brief via heredoc (`<<'EOF'`). Never a one-line
`--body`.

Beta scope note: multi-host / remote-master operation is the NEXT MILESTONE —
out of scope for the current beta (beta = single machine, zero process
management). The remote knowledge below is for maintainer/cluster use.

Important remote rule:

- `ORCH_REMOTE=<master>` points the CLI and local worker at that master.
- The default master can also be set durably in `client.yaml` — globally in
  `~/.config/orch/client.yaml` or per-repo in `<repo>/.orch/client.yaml`:

  ```yaml
  remote:
    default: "master-host:7777"
  ```

- It does **not** mean `worker start` happens on the remote host.
- `orch worker start` is local-host scoped. The worker process is started on the machine where
  you run the command, then it connects to the configured master.
- When talking to a remote master, pass `--project <origin URL>` (or set `ORCH_PROJECT`)
  explicitly on every command. CWD-based inference resolves against the master's own context,
  not your repo: observed with daemon @6b2bceb1, `issue list` without `--project` returned the
  master repo's issues, and `issue create` fails with `project identity required` even inside
  a git repo with `origin` set.

## Local Single-Machine Setup (required once per repo — verified 2026-07-08)

Three requirements hold even with NO remote master, and their absence is the
most common first-run failure:

1. **`origin` remote required.** Project identity is derived from the origin
   URL (normalized `<owner>-<repo>`). Without one, every command fails with
   `project identity required`. The URL need not exist on GitHub for local
   testing — it is used as an identity.
2. **Project registration required.** The daemon (local or remote) resolves
   commands through a registry mapping. Register the checkout by path:
   `orch daemon repo register "$(pwd)"` — writes
   `~/.config/orch/projects/<project_id>.yaml`, effective immediately, no
   daemon restart. Missing → `unknown project_id "…" (register daemon project
   mapping)`.
3. **Workers auto-start on demand (ADR-0002, v1.5+).** The daemon auto-starts
   on the first command, and when a run targets the master's own host the
   master auto-starts its colocated worker (reboots included). `orch run`
   against a remote master also auto-starts the local worker for that master.
   Only OTHER hosts still need a manual `orch worker start`. If
   `no active workers available` appears anyway, check `ORCH_WORKER_AUTOSTART`
   (0 = disabled) and the daemon log. On pre-1.5 binaries all workers are
   manual.

Also expect the agent CLI's one-time trust dialog on a fresh
machine/directory: the run parks at `waiting` right after boot; read it with
`orch capture <run>` and accept with `orch send <run> ""` (empty message =
Enter).

## Core Workflow

1. Create or inspect the issue with `orch issue create`, `orch issue list`, or `orch open`.
2. Start work with `orch run <issue-id>` — workers auto-start on demand
   (v1.5+); `orch worker status` shows them. Hosts other than the master and
   your own machine still need `ORCH_REMOTE=<master> orch worker start` there.
3. Track state with `orch ps` and inspect details with `orch show`.
4. Interact with the live run via `orch capture`, `orch send`, and `orch attach`.
5. Use `orch stop` only for actually stale or canceled work. Use `orch restart-from` only for
   failed, canceled, or unknown runs. Mark completed work with `orch resolve`.

## Branch Resolution for `orch run` (verified 2026-07-05)

`orch run` creates the run's worktree branch (`issue/<ID>/run-<RUN_ID>`) from
`--base-branch` (default: `base_branch` in `.orch/config.yaml`). The critical,
verified behavior:

- **orch fetches origin and prefers the ORIGIN ref when a branch of that name
  exists on the remote — even when the local branch in `workspace.root` is
  ahead.** Observed: local `adr0035-byte-faithful-transport` was at a newer tip
  with local-only commits; origin had the same branch name at a stale tip; the
  run's worktree silently came up at the stale origin tip, missing every
  local-only commit. There is no warning — the run boots and the agent simply
  finds the files missing. `orch ps` shows `branch_status: diverged`, which is
  easy to misread as normal.
- **To base a run on local-only commits, create a local alias branch whose
  name does NOT exist on origin** and pass that:

  ```bash
  git -C <workspace.root> branch -f run-base-<topic> <local-tip>
  orch run <ISSUE> --base-branch run-base-<topic> ...
  ```

  With no matching origin ref, orch resolves the local ref. Pushing the real
  branch is NOT the fix when it carries commits that must not be published.
- **ALWAYS verify the worktree base immediately after dispatch** — before the
  agent burns tokens on a wrong tree:

  ```bash
  git -C <worktree_path> log --oneline -1   # must equal the intended base tip
  ```

- When the base carries unpublished commits, pass `--no-pr` AND state
  "never push, never open a PR" in the issue body — the default prompt
  template instructs agents to open PRs, which would publish the base.

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
export ORCH_REMOTE=master-host:7777
orch worker start
orch worker status --json
orch run plc-123
orch ps --json
```

Interpretation:

- the worker process started on **this** machine
- the worker registered to `master-host:7777`
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
- **Per-project `.orch/config.yaml` is ALSO a target source and can shadow the fixed global
  config** (verified 2026-07-05): the global `~/.config/orch/config.yaml` on the master
  had the corrected host, but runs for one project still resolved the old hostname because the
  project checkout's `.orch/config.yaml` `targets:` still carried it. After a host rename, sweep
  `targets:` in EVERY registered project checkout on the master
  (`grep -rn "<old-host>" ~/repos/*/.orch/config.yaml`), not just the global config. Unlike the
  global config (read at daemon startup), the project config is re-read per run — fixing the
  file takes effect immediately, no daemon restart needed.
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
- **Pre-fix workers can inherit a foreign tmux server.** If operating an old binary, start workers
  from a clean env (`env -u TMUX -u TMUX_PANE orch worker start`) so run sessions do not land on
  an unrelated app's tmux socket.
- **File-backend issue files live on the master's checkout** (e.g.
  `~/repos/<repo>/VAULT/Issues/ISSUE-X.md` on the master host). From another host,
  `orch open <ISSUE> --print-path` reports `not found` — read the issue via `orch issue list`
  / `orch show` instead.
- **Updating the master binary while its daemon runs** fails with `Text file busy` on `cp`;
  use `rm <bin> && cp <new> <bin>` (the running process keeps its inode), then restart the
  daemon deliberately. `orch repair`'s daemon restart operates on the operator host — restart a
  remote master on its own host (`kill <pid>`, then
  `nohup orch daemon run --listen 0.0.0.0:<port> ...` from the original working directory).
- **`pkill` self-match footgun when restarting the daemon over ssh** (verified 2026-07-05):
  `ssh <master> 'pkill -f "orch daemon run"; ...start...'` kills the wrapper `bash -c` itself —
  its cmdline contains the pattern — so the shell dies before the start half runs. Use a
  self-safe regex (`pkill -f "orch daemon ru[n]"`) and split kill / start into separate ssh
  invocations; start with `ssh -f <master> 'nohup ... < /dev/null &'` so it detaches cleanly.
- **`agent not available: claude` from the worker** means the worker's PATH lacks the agent
  binary. The worker inherits PATH from the shell that started it; a worker started from a
  non-interactive ssh shell typically misses `~/.local/bin`. Fix:
  `orch worker stop`, then `export PATH="$HOME/.local/bin:$PATH"; orch worker start`.
- **Execution-host toolchain rots silently — verify it before dispatching** (verified
  2026-07-05, remote Linux host): (a) the claude native-install `~/.local/bin/claude` symlink can dangle
  after a version GC (`claude --version` via the absolute path is the check; rerun the
  installer to fix); (b) a missing multiplexer binary (tmux uninstalled) makes the run stick
  at `running`/ALIVE `no` with the worker log ending right after "Preparing worktree" and
  `capture` failing `session ... not found` — no explicit error anywhere. Check
  `tmux -V` / `zellij --version` on the execution host when a run creates a worktree but never
  opens a session.
- **File-backend issue store is fail-loud on ANY malformed issue file** (verified
  2026-07-05): a single file with broken frontmatter YAML (e.g. an unquoted
  colon in `title:`) or a status outside `open|closed|resolved` (seen: `done`,
  `in_progress`, `superseded`) makes EVERY `issue list`/`issue create` fail with the opaque
  `daemon error: store_error`. Diagnosis: the master's `~/Library/Logs/orch/daemon.log`
  (macOS) names the offending file (`failed to parse issue file ...`). Fix the file; there
  may be several — repeat until clean, or pre-scan all frontmatter with a YAML parser.
- **`orch wait` flaps on claude runs**: claude's TUI shows the `❯` input box at every turn
  boundary, which the status detector reads as `waiting`, so `orch wait` returns while the
  agent is actively thinking/working. Before acting on a wait return, `orch capture` — a
  spinner ("thinking…", token counters advancing) means the run is alive; re-arm the wait
  or poll `orch ps --json` (`.items[] | select(.short_id==...)`) for a genuinely terminal
  status (`done`/`failed`/`canceled`).
- **Daemon restart races its own pid lock** (macOS: `~/Library/Caches/orch/run/daemon.lock`):
  `kill <pid>` followed immediately by `orch daemon run` fails with
  "daemon already running (pid=<old>)" and exits. Wait 1-2s after the kill before starting.
  The daemon logs to `~/Library/Logs/orch/daemon.log` regardless of where stdout is
  redirected — an empty nohup log does not mean it isn't logging.
- **Worker registration to the daemon's own host over Tailscale can be RST**
  (verified 2026-07-05, mac master): with the daemon listening on `[::]:7777`,
  `ORCH_REMOTE=<own-hostname>:7777` resolved to the host's Tailscale IP and the hairpin
  connection was reset (`worker start` dies with exit status 10, "connection reset by
  peer"). Register the same-host worker via `ORCH_REMOTE=127.0.0.1:7777` instead — the
  worker id is derived from the hostname either way, so `--on <target>` still matches.

## Project Mapping — register a new repo on the master (verified 2026-07-03, ACP onboarding)

`no store available for project_id "X" (register daemon project mapping)` or
`unknown project_id "X"` means the master has no project config for that repo.
There is **no `orch project` CLI subcommand** — registration is manual file
placement on the **master host**. THREE pieces are required:

1. **A checkout of the repo on the master host** — this becomes `workspace.root`;
   runs create worktrees from it. Verify the master host can reach the remote first
   (`git ls-remote <origin-url> HEAD`).
2. **`.orch/config.yaml` inside that checkout** — supplies the issue store and run
   defaults (this is what `LoadFromProjectRoot(workspace.root)` reads; without a
   loadable config here, store resolution fails with the same "no store" error):

   ```yaml
   targets:
     - name: remote
       host: remote-host
   agent: codex
   base_branch: main
   pr_target_branch: main
   issues:
     backend: local
     path: ~/repos/<repo>-issues     # external issues dir (Obsidian-vault
                                     # pattern); an in-repo vault dir also works
   github:
     owner: <org>
     repo: <repo>
   ```

   If `issues.path` is omitted it defaults to `~/.local/share/orch/<owner>-<repo>`.
   Issue files land under the path's `Issues/` subdir if it already exists
   (Obsidian convention), else `issues/` is used/created. `.orch/` is typically
   gitignored — the config stays local to the master checkout.
3. **`~/.config/orch/projects/<project_id>.yaml` on the master host**:

   ```yaml
   version: 1
   project_id: your-org-your-repo
   display_name: your-repo
   workspace:
       root: /home/user/repos/your-repo
   ```

   `project_id` is the normalized origin URL, `<org>-<repo>`
   (`git@github.com:your-org/your-repo.git` →
   `your-org-your-repo`). `project_id` and `workspace.root` are
   required; a mapping whose root doesn't exist is silently broken (same "no
   store available" error even though the yaml is present).

Verified behaviors:

- **No daemon restart needed** — unlike `config.targets` (read at startup only),
  the project registry is re-read from `~/.config/orch/projects/` on a lookup
  miss (`ensureRepoStoreByID` → `loadRepoRegistry`). Place the files, then verify.
- **Verification oracle**: `orch issue list --project <origin URL>` →
  `No issues found` = mapping works; the "no store available" error = a piece is
  still missing or `workspace.root` / its `.orch/config.yaml` is broken.

## Claude Profiles for Runs (verified 2026-07-05)

`orch run --agent claude --profile <name>` selects a Claude Code account via
`CLAUDE_CONFIG_DIR` (Claude Code has no native profile flag). Verified behaviors:

- **Profiles must be declared in the MASTER config** (`~/.config/orch/config.yaml` on the
  master host) under `claude.profiles`. An undeclared name fails fast:
  `unknown claude profile "X" (configure it under claude.profiles)` — and with no `claude:`
  section at all, EVERY `--profile` fails this way. Declaration shape:

  ```yaml
  claude:
    profiles:
      personal:
        config_dir: ~/.config/claude-personal   # ~ expands on the EXECUTION host
      work:
        config_dir: ~/.config/claude-work
        # optional: target: remote / allowed_targets: [remote]
  ```

- The daemon reads this at startup only — **restart the master daemon after adding profiles**.
- The resolved profile/model are visible in `orch ps` (`PROFILE` / `MODEL` columns) — verify
  them there instead of trusting the launch command.
- **The profile's own hooks run inside the dispatched session.** A `UserPromptSubmit` guard
  hook in the profile's `settings.json` can block the initial ORCH_PROMPT ("Operation stopped
  by hook") and park the run at `waiting` forever. Verified: a model-provenance guard plus
  `"model": "sonnet"` in profile settings deadlocks a fresh session launched with
  `--model claude-fable-5` (fresh transcripts have no assistant message to verify against).
  Before dispatching on a profile, check its `settings.json` for hooks and for a `model` key
  that conflicts with the run's `--model`.
- **Account billing/allowance state gates the model at first prompt**: e.g. Fable 5 on an
  account with exhausted allowance shows an interactive "usage credits" dialog and the run
  parks. This is account state, not an orch failure — resolve on the account (or change
  model/profile) rather than restarting the run.

## Model Routing for Runs

The choice is ALWAYS between exactly two models (never Opus, never Haiku):

- **gpt-5.5 (`--agent codex`)**: mundane mechanical work — rename sweeps,
  fixture ports, wiring an explicitly specified contract. ~2x faster;
  satisfies done conditions to the letter.
- **Fable 5 (`--agent claude`, xhigh)**: architectural / complex /
  invariant-touching issues — architecture boundaries, contract
  migrations, invariant machinery, anything where the issue's checklist is
  a projection of a deeper design. Empirical A/B on the same issue
  (2026-06-11): gpt-5.5 met every done condition; Fable 5 additionally
  found and fixed a latent bug the work exposed and migrated without
  leaving dual surfaces. Blind gpt-5.5-by-default yields letter-satisfying,
  seam-blind results that cost more in review.
- Whoever dispatches owns this call per issue; when unsure, ask "does this
  issue touch a contract or just implement inside one?"
- **Effort enforcement caveat**: orch launches claude WITHOUT
  `CLAUDE_CONFIG_DIR` unless a claude profile with `config_dir` is set in
  the master config — so it reads the bare `~/.claude/settings.json`,
  which the cc multi-profile workflow never touches and which can drift.
  Fable 5 must run at xhigh: verify `effortLevel` in `~/.claude/settings.json`
  (or define an orch claude profile) before relying on a dispatched run's
  effort. Verified incident 2026-06-11: all named profiles said xhigh while
  `~/.claude` still said high; the orch run started at HIGH.

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

- **The issue body is the worker agent's ONLY context.** Write it as a complete
  brief — goal, constraints, acceptance criteria, verification — never a
  one-line `--body`. Humans author bodies in their editor
  (`orch issue create <id> --title "..." --edit` / `orch issue edit <id>`);
  agents author them via heredoc.
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
