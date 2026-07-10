# CLI Command Reference

Complete reference for all orch commands.

## Global Flags

These flags work with all commands:

| Flag | Description |
|------|-------------|
| `--project <id-or-url>` | Project identity (repo ID or git remote URL, or `ORCH_PROJECT`) |
| `--remote <addr>` | Connect to remote daemon address (or `ORCH_REMOTE`) |
| `--json` | Output in JSON format |
| `--tsv` | Output in TSV format (for fzf integration) |
| `--quiet` | Suppress human-readable output |
| `--log-level <level>` | Log level: `error`, `warn`, `info`, `debug` (default: `warn`) |
| `-h, --help` | Show help |

## Exit Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Issue not found |
| 3 | Worktree error |
| 4 | Tmux error |
| 5 | Agent launch error |
| 6 | Run not found |
| 7 | Question not found |
| 8 | Run ended |
| 10 | Internal error |

---
## orch agent

Manage the standalone control agent for interactive issue management.

```bash
orch agent [flags]
```

The control agent is a persistent AI agent that helps you manage issues and runs through conversation. It can create issues, start runs, check status, and coordinate multiple agents.

### Flags

| Flag | Description |
|------|-------------|
| `--new` | Force start a new control agent session |
| `--kill` | Terminate the running control agent |
| `--backend <type>` | Agent type: `claude`, `opencode`, etc. |
| `--dry-run` | Validate agent backend resolution without launching |

### Examples

```bash
# Attach to existing or create new control agent
orch agent

# Force a new session (terminate existing if any)
orch agent --new

# Terminate the control agent
orch agent --kill

# Use a specific agent backend
orch agent --backend opencode
```

### How it works

The control agent runs in a persistent tmux session and can:
- Create and manage issues through natural language
- Start runs for issues
- Monitor run progress
- Help coordinate parallel work

---

## orch diff

Show git diff for a run's worktree changes.

```bash
orch diff RUN_REF [flags]
```

View what changes an agent has made in a run's worktree compared to the base branch. Useful for reviewing agent work before merging or creating a PR.

### Flags

| Flag | Description |
|------|-------------|
| `--stat` | Show diffstat summary only |
| `--base <branch>` | Compare against specific base branch (default: run's base branch) |

### Examples

```bash
# Show full diff for latest run of an issue
orch diff my-issue

# Show diff for specific run
orch diff my-issue#20260120-163045

# Show just the diffstat summary
orch diff --stat my-issue

# Compare against specific base branch
orch diff --base main my-issue

# Combine flags
orch diff --stat --base develop my-issue
```

---

## orch run

Create and start a new run for an issue.

```bash
orch run ISSUE_ID [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--agent <type>` | Agent: `claude`, `codex`, `gemini`, `opencode`, `custom` |
| `--agent-cmd <cmd>` | Custom agent command (with `--agent custom`) |
| `--base-branch <branch>` | Explicit base branch for the worktree; when omitted, the daemon checks issue `base_branch`, config `base_branch`, then `main` |
| `--branch <name>` | Branch name (default: `issue/<ID>/run-<RUN_ID>`) |
| `--codex-profile <name>` | Codex execution profile from config (`codex.profiles`) |
| `--dry-run` | Show what would be done without doing it |
| `--model <model>` | Model for opencode (e.g., `anthropic/claude-opus-4-5`) |
| `--model-variant <variant>` | Model variant (e.g., `max`) |
| `--multiplexer <type>` | Terminal multiplexer: `tmux`, `zellij` |
| `--new` | Always create a new run (default) |
| `--no-pr` | Skip PR creation instructions in prompt |
| `--on <target>` | Target name from config targets for remote execution |
| `--preset <name>` | Use named preset from config |
| `--profile <name>` | Agent profile |
| `--prompt-template <file>` | Custom prompt template file |
| `--reuse` | Reuse the latest run if it is `waiting` or `rate_limited` |
| `--run-id <id>` | Manually specify run ID |
| `--session-name <name>` | Session name (default: `run-<ISSUE>-<RUN>`) |
| `--tmux` | Run in tmux session (default: true) |
| `-v, --verbose` | Enable debug output |

### Examples

```bash
# Basic run
orch run my-issue

# With specific agent and model
orch run --agent opencode --model anthropic/claude-opus-4-5 my-issue

# Dry run to see what would happen
orch run --dry-run my-issue

# Using a preset
orch run --preset thorough my-issue
```

---

## orch restart-from

Restart work from an existing run, reusing its worktree and branch.
Use this recovery command only for failed/canceled/unknown runs.

```bash
orch restart-from RUN_REF|ISSUE_ID [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--agent <type>` | Agent type |
| `--agent-cmd <cmd>` | Custom agent command |
| `--branch <name>` | Restart from existing branch |
| `--codex-profile <name>` | Codex execution profile from config (`codex.profiles`; default: prior run's profile) |
| `--issue <id>` | Issue ID (with `--branch`) |
| `--multiplexer <type>` | Terminal multiplexer: `tmux`, `zellij` |
| `--no-pr` | Skip PR creation instructions |
| `--profile <name>` | Agent profile |
| `--prompt-template <file>` | Custom prompt template |
| `--session-name <name>` | Session name (default: `run-<ISSUE>-<RUN>`) |
| `--tmux` | Run in tmux (default: true) |

### Examples

```bash
# Restart specific run
orch restart-from my-issue#20260120-163045

# Restart using short run ID
orch restart-from a3b4c5

# Restart from existing branch
orch restart-from --branch feature/my-work --issue my-issue
```

---

## orch ps

List runs with their status.

```bash
orch ps [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--status <statuses>` | Filter by status (comma-separated); overrides `ps.default_statuses` |
| `--issue-status <statuses>` | Filter by issue status |
| `--issue <id>` | Show runs for specific issue only |
| `--limit <n>` | Max runs to show (default: 50) |
| `--sort <field>` | Sort by: `updated`, `started` |
| `--since <timestamp>` | Show runs since timestamp |
| `--absolute-time` | Show absolute timestamps |
| `-a, --all` | Include resolved runs |
| `--no-alive` | Skip agent alive checks for faster listing |
| `--no-git` | Skip git merge state checks for faster listing |
| `-v, --verbose` | Show additional debug info (daemon log location) |

When `ps.default_statuses` is active, plain table output ends with status counts for
runs excluded by that default filter. `--all` and explicit `--status` bypass the
default and do not show the excluded summary.

### Examples

```bash
# List all active runs
orch ps

# Configure the default statuses shown by plain `orch ps`
cat > .orch/config.yaml <<'YAML'
ps:
  default_statuses:
    - queued
    - booting
    - running
    - waiting
    - rate_limited
    - pr_open
YAML

# Filter by status
orch ps --status running,waiting

# Show runs for specific issue
orch ps --issue my-issue

# JSON output for scripting
orch ps --json
```

---

## orch show

Show details of a specific run.

```bash
orch show RUN_REF [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--tail <n>` | Number of events to show (default: 80) |
| `--events-only` | Show only events |

### Examples

```bash
# Show latest run for issue
orch show my-issue

# Show specific run
orch show my-issue#20260120-163045

# Show only events
orch show --events-only my-issue
```

---

## orch attach

Attach to a run's tmux/zellij session for direct interaction.

```bash
orch attach RUN_REF [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--pane <name>` | Pane to attach to: `log`, `shell` |
| `--window <name>` | Window to attach to |

### Examples

```bash
# Attach to latest run
orch attach my-issue

# Attach using short ID
orch attach abc123
```

**Detach**: `Ctrl+B D` (tmux) or `Ctrl+O D` (zellij)

---

## orch stop

Stop running runs.

```bash
orch stop ISSUE_ID|RUN_REF [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--all` | Stop all active runs |
| `--force` | Force stop even if session not found |

### Examples

```bash
# Stop all runs for an issue
orch stop my-issue

# Stop specific run
orch stop my-issue#20260120-163045

# Stop all runs globally
orch stop --all
```

---

## orch send

Send a message to a running/waiting agent.
This is the primary way to interact with waiting runs.
You can pass the message inline or via redirected stdin/heredoc for multi-line input.

If `orch send` fails, do not assume the run is dead:
1. `orch capture <RUN_REF>`
2. `orch ps`
3. Check multiplexer sessions (`tmux list-sessions` / `zellij list-sessions`)
4. Write feedback into `ORCH_PROMPT.md` in the run worktree
5. Use native multiplexer send (`tmux send-keys` / `zellij action write-chars`)

Do NOT use `orch restart-from` for send failures - the run is likely still alive.

```bash
orch send RUN_REF [MESSAGE] [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--no-enter` | Do not press Enter after sending (ignored for opencode agents) |
| `--dry-run` | Validate the run and control path without sending a message |

### Examples

```bash
# Send instruction
orch send my-issue "Please also add tests"

# Send to specific run
orch send my-issue#20260120-163045 "Focus on error handling"

# Send multi-line feedback via heredoc
orch send my-issue <<'EOF'
Please fix the auth regression first.
Then rerun the focused session tests.
EOF
```

---

## orch wait

Block until any specified run needs attention.

```bash
orch wait RUN_REF [RUN_REF...] [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--timeout <seconds>` | Timeout in seconds (0 = unlimited) |

### Examples

```bash
# Wait for the latest run of an issue
orch wait my-issue

# Wait for several runs with a 10-minute timeout
orch wait abc123 def456 --timeout 600
```

---

## orch capture

Capture current output from a running agent.

```bash
orch capture RUN_REF [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--lines <n>` | Number of lines to capture (default: `100`) |

---

## orch capture-all

Capture output from all running agents.

```bash
orch capture-all [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--lines <n>` | Number of lines to capture per agent (default: `100`) |

---

## orch issue

Manage issues.

### orch issue create

Create a new issue.

You can pass the body with `--body` or via redirected stdin/heredoc for
multi-line input.

```bash
orch issue create ISSUE_ID [flags]
```

#### Flags

| Flag | Description |
|------|-------------|
| `-t, --title <title>` | Issue title |
| `-b, --body <body>` | Issue body |
| `-s, --summary <text>` | Short summary for display (~50 chars) |
| `--base-branch <branch>` | Base branch new runs for this issue branch off of |
| `--tag <tags>` | Tags for the issue (repeatable, comma-separated) |
| `-e, --edit` | Open in editor after creation |

#### Examples

```bash
orch issue create fix-login-bug --title "Fix login timeout"
orch issue create my-issue --title "Add dark mode" <<'EOF'
Users want dark mode support.
Include settings persistence.
EOF
```

### orch issue list

List all issues.

```bash
orch issue list [flags]
```

#### Flags

| Flag | Description |
|------|-------------|
| `-s, --status <status>` | Filter by status (open, closed, resolved) |
| `--tag <tags>` | Filter by tag (AND logic, repeatable) |
| `--tag-any <tags>` | Filter by any tag (OR logic, comma-separated) |
| `--no-path` | Hide the PATH column |

### orch issue show

Show issue details.

```bash
orch issue show ISSUE_ID [flags]
```

#### Flags

| Flag | Description |
|------|-------------|
| `-w, --web` | Open in browser |

### orch issue edit

Edit an issue.

```bash
orch issue edit ISSUE_ID [flags]
```

#### Flags

| Flag | Description |
|------|-------------|
| `-t, --title <title>` | Update title directly without opening editor |

### orch issue close

Close an issue.

```bash
orch issue close ISSUE_ID [flags]
```

#### Flags

| Flag | Description |
|------|-------------|
| `-c, --comment <text>` | Add a closing comment |

### orch issue open

Open issue in browser.

```bash
orch issue open ISSUE_ID
```

### orch issue sync

Sync issues from GitHub.

```bash
orch issue sync
```

---

## orch validate-issue-files

Validate all issue files or a specific issue for proper formatting.

```bash
orch validate-issue-files [ISSUE_ID] [flags]
```

Errors (blocking): invalid YAML frontmatter, missing `id` or `title`,
invalid status, duplicate issue IDs, file/ID mismatch.
Warnings (non-blocking): missing status, empty body, very long title,
missing type, unusual characters in ID.

### Examples

```bash
# Validate all issues
orch validate-issue-files

# Validate a specific issue
orch validate-issue-files orch-123

# JSON output for tooling
orch validate-issue-files --json
```

---

## orch open

Open issue or run in editor.

```bash
orch open ISSUE_ID|RUN_REF [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--app <app>` | App to use: `obsidian`, `editor`, `default` |
| `--print-path` | Print path instead of opening |

---

## orch repair

Repair system state (fix stale runs, restart daemon).

```bash
orch repair [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--dry-run` | Show what would be fixed |
| `--force` | Fix without confirmation |

---

## orch resolve

Mark an issue as resolved.

```bash
orch resolve ISSUE_ID [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--force` | Resolve even if the issue has no completed runs |

---

## orch tick

Resume waiting runs.

```bash
orch tick [RUN_REF] [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--all` | Process all waiting runs |
| `--only-waiting` | Only process waiting or rate-limited runs (default: `true`) |
| `--agent <type>` | Agent for resumption |
| `--max <n>` | Max runs to process |

---

## orch query

Query issues and runs using SQL.

```bash
orch query [sql] [flags]
orch q [sql] [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `-f, --format <fmt>` | Output: `table`, `json`, `tsv` |
| `--with-events` | Include events table (slower) |

### Examples

```bash
# Query open issues
orch q "SELECT * FROM issues WHERE status = 'open'"

# Query running runs
orch q "SELECT * FROM runs WHERE status = 'running'"

# Get stats
orch q "SELECT status, COUNT(*) FROM runs GROUP BY status"
```

See [Query Reference](./query.md) for detailed SQL examples.

---

## orch schema

Show database schema for SQL queries.

```bash
orch schema [table] [flags]
```

Omit `table` to list all tables and views, or provide a table/view name to
show its columns.

### Flags

| Flag | Description |
|------|-------------|
| `-f, --format <format>` | Output format: `table`, `json`, or `tsv` (default: `table`) |

---

## Monitor TUI migration

The former Go monitor subcommand, including its `list` and `kill` variants,
has been removed. Use the standalone Python `orch-monitor` binary instead; see
the [orch-monitor TUI guide](../orch-monitor.md) for installation and usage.

---

## orch exec

Execute a command in a run's worktree.

```bash
orch exec RUN_REF -- COMMAND [args...]
```

### Flags

| Flag | Description |
|------|-------------|
| `--env <KEY=VALUE>` | Add an environment variable; may be repeated |
| `--no-orch-env` | Do not inject the run's `ORCH_*` environment variables |
| `--shell` | Run the command through `sh -c` |
| `--quiet` | Suppress human-readable output |

### Examples

```bash
# Run tests in worktree
orch exec my-issue -- npm test

# Check git status
orch exec my-issue -- git status
```

---

## orch delete

Delete runs and their resources. **This command is destructive:** run records
are removed, and worktrees or branches are also removed when their respective
flags are supplied. Use `--dry-run` to inspect the selection first.

```bash
orch delete [RUN_REF | ISSUE_ID] [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--all` | Delete all matching runs for the specified issue |
| `--force` | Skip the confirmation prompt |
| `--dry-run` | Show what would be deleted without deleting it |
| `--with-worktree` | Also remove each run's git worktree |
| `--with-branch` | Also remove each run's git branch |
| `--older-than <duration>` | Delete runs older than a duration such as `7d`, `2w`, or `1m` |
| `--status <status>` | Only delete runs in `done`, `failed`, or `canceled` status |

---

## orch clean

Remove run worktrees while preserving run history.

```bash
orch clean [RUN_REF | ISSUE_ID] [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--all` | Clean all matching runs, globally or for the specified issue |
| `--older-than <duration>` | Clean runs older than a duration such as `7d`, `2w`, or `1m` |
| `--status <statuses>` | Comma-separated statuses to clean: `failed`, `canceled`, or `done` |
| `--force` | Skip the confirmation prompt |
| `--dry-run` | Show what would be cleaned without removing worktrees |

Examples:

```bash
# Clean the latest failed/canceled run worktree for an issue
orch clean my-issue

# Clean all failed/canceled worktrees across all issues
orch clean --all

# Clean all failed/canceled worktrees for an issue
orch clean my-issue --all

# Include done runs explicitly
orch clean my-issue --all --status failed,canceled,done

# Preview cleanup before removing anything
orch clean --older-than 7d --dry-run
```

By default, bulk cleanup targets `failed` and `canceled` runs only.

---

## orch daemon

Manage the background monitoring daemon.

### orch daemon start

Start the daemon.

```bash
orch daemon start
```

#### Flags

| Flag | Description |
|------|-------------|
| `--listen <addr>` | TCP listen address for remote clients (default: `127.0.0.1:7777` — loopback only; multi-host requires an explicit non-loopback address, e.g. `tcp://0.0.0.0:7777`, ADR-0003) |

### orch daemon kill

Kill running daemon(s).

```bash
orch daemon kill
```

#### Flags

| Flag | Description |
|------|-------------|
| `--all` | Kill all running daemons (an alias for the global-daemon behavior) |

### orch daemon list

List all running daemons.

```bash
orch daemon list
```

### orch daemon status

Check daemon status.

```bash
orch daemon status
```

### orch daemon repo register

Register a local Git checkout path for project identity mapping. The path must
exist on the daemon host.

```bash
orch daemon repo register "$(pwd)"
```

### orch daemon repo list

List daemon repo identity mappings used for remote resolution.

```bash
orch daemon repo list
```

---

## orch worker

Manage the orch-worker execution plane.

Workers run as long-lived host managers and execute work assigned by
orch-master via worker protocol APIs. Single-host mode is implemented as
co-located daemon+worker with the same semantics as distributed mode.

### orch worker run

Run the long-lived orch-worker host loop.

```bash
orch worker run [flags]
```

| Flag | Description |
|------|-------------|
| `--heartbeat-interval <dur>` | Worker heartbeat interval (default: `5s`) |
| `--once` | Process at most one lease before exiting |
| `--poll-interval <dur>` | Lease poll interval (default: `200ms`) |
| `--worker-id <id>` | Worker ID for registration |

### orch worker start

Start a managed orch-worker host process. Usually automatic since v1.5
(ADR-0002): the master auto-starts its colocated worker on demand, and run
dispatch to a remote master auto-starts the local worker for it. Manual start
remains for other hosts and for `ORCH_WORKER_AUTOSTART=0`.

```bash
orch worker start [flags]
```

| Flag | Description |
|------|-------------|
| `--worker-id <id>` | Worker ID to start (default: local host worker) |

### orch worker status

Show orch-worker status.

```bash
orch worker status [flags]
```

| Flag | Description |
|------|-------------|
| `--worker-id <id>` | Worker ID to inspect (default: local host worker) |

### orch worker stop

Stop a managed orch-worker host process.

```bash
orch worker stop [flags]
```

| Flag | Description |
|------|-------------|
| `--all` | Stop all managed workers |
| `--worker-id <id>` | Worker ID to stop (default: local host worker) |

---

## orch debug

Debug a run by showing daemon perspective.

```bash
orch debug RUN_REF [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--json` | Request JSON output |

---

## orch events

Stream run state transition events emitted by the daemon.

Each event is printed as a single JSON line on stdout. The stream stays
open until interrupted (Ctrl-C) or the daemon disconnects. Useful for
building external integrations (status mirrors, custom notifiers) that
react to run state changes without polling.

```bash
orch events -f [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `-f, --follow` | Stream events as they occur (required) |
| `--issue <id>` | Only emit events for this issue ID |
| `--run <id>` | Only emit events for this run ID |

### Examples

```bash
# Stream all run state transitions
orch events -f

# Only events for one issue
orch events -f --issue my-issue
```

---

## orch log

View orch logs. The `daemon` subcommand reads the global daemon log.

```bash
orch log daemon [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `-f, --follow` | Follow daemon log output |
| `-n, --lines <n>` | Number of lines to show (default: `100`) |

---

## orch models

List opencode provider models.

```bash
orch models [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--port <port>` | OpenCode server port (default: `4096`) |
| `--timeout <seconds>` | Request timeout in seconds (default: `5`) |

---

## orch notify

Notification management commands.

```bash
orch notify [subcommand]
```

### orch notify test

Send a test notification using the configured Slack notifier.

```bash
orch notify test [flags]
```

| Flag | Description |
|------|-------------|
| `-m, --message <text>` | Custom test message |

---

## orch tutorial

Show setup guide and usage reference.

```bash
orch tutorial
```

---

## orch version

Print orch version information.

```bash
orch version
```

---

## orch completion

Generate shell autocompletion scripts.

```bash
orch completion [bash|zsh|fish|powershell]
```

### Examples

```bash
# Bash
orch completion bash > /etc/bash_completion.d/orch

# Zsh
orch completion zsh > "${fpath[1]}/_orch"

# Fish
orch completion fish > ~/.config/fish/completions/orch.fish
```
