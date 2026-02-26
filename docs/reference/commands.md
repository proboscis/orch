# CLI Command Reference

Complete reference for all orch commands.

## Global Flags

These flags work with all commands:

| Flag | Description |
|------|-------------|
| `--backend <type>` | Backend type: `file`, `github`, `linear` (default: `file`) |
| `--project-root <path>` | Path to project root (or `ORCH_PROJECT_ROOT`) |
| `--issues-root <path>` | Path to issues root (or `ORCH_ISSUES_ROOT`) |
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
| `--base-branch <branch>` | Base branch for worktree (default: `main`) |
| `--branch <name>` | Branch name (default: `issue/<ID>/run-<RUN_ID>`) |
| `--dry-run` | Show what would be done without doing it |
| `--model <model>` | Model for opencode (e.g., `anthropic/claude-opus-4-5`) |
| `--model-variant <variant>` | Model variant (e.g., `max`) |
| `--multiplexer <type>` | Terminal multiplexer: `tmux`, `zellij` |
| `--new` | Always create a new run (default) |
| `--no-pr` | Skip PR creation instructions in prompt |
| `--preset <name>` | Use named preset from config |
| `--profile <name>` | Agent profile |
| `--prompt-template <file>` | Custom prompt template file |
| `--repo-root <path>` | Git repository root |
| `--reuse` | Reuse latest run if waiting |
| `--run-id <id>` | Manually specify run ID |
| `--tmux` | Run in tmux session (default: true) |
| `--tmux-session <name>` | Session name |
| `-v, --verbose` | Enable debug output |
| `--worktree-dir <path>` | Directory for worktrees |

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
| `--issue <id>` | Issue ID (with `--branch`) |
| `--no-pr` | Skip PR creation instructions |
| `--profile <name>` | Agent profile |
| `--prompt-template <file>` | Custom prompt template |
| `--repo-root <path>` | Git repository root |
| `--tmux` | Run in tmux (default: true) |
| `--tmux-session <name>` | Session name |
| `--worktree-dir <path>` | Worktree directory |

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
| `--status <statuses>` | Filter by status (comma-separated) |
| `--issue-status <statuses>` | Filter by issue status |
| `--issue <id>` | Show runs for specific issue only |
| `--limit <n>` | Max runs to show (default: 50) |
| `--sort <field>` | Sort by: `updated`, `started` |
| `--since <timestamp>` | Show runs since timestamp |
| `--absolute-time` | Show absolute timestamps |
| `--all` | Include resolved runs |

### Examples

```bash
# List all active runs
orch ps

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
| `--questions` | Show only unanswered questions |
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

If `orch send` fails, do not assume the run is dead:
1. `orch capture <RUN_REF>`
2. `orch ps`
3. Check multiplexer sessions (`tmux list-sessions` / `zellij list-sessions`)
4. Write feedback into `ORCH_PROMPT.md` in the run worktree
5. Use native multiplexer send (`tmux send-keys` / `zellij action write-chars`)

Do NOT use `orch restart-from` for send failures - the run is likely still alive.

```bash
orch send RUN_REF MESSAGE [flags]
```

### Examples

```bash
# Send instruction
orch send my-issue "Please also add tests"

# Send to specific run
orch send my-issue#20260120-163045 "Focus on error handling"
```

---

## orch capture

Capture current output from a running agent.

```bash
orch capture RUN_REF [flags]
```

---

## orch capture-all

Capture output from all running agents.

```bash
orch capture-all [flags]
```

---

## orch issue

Manage issues.

### orch issue create

Create a new issue.

```bash
orch issue create ISSUE_ID [flags]
```

#### Flags

| Flag | Description |
|------|-------------|
| `--title <title>` | Issue title |
| `--body <body>` | Issue body |
| `--edit` | Open in editor after creation |

### orch issue list

List all issues.

```bash
orch issue list [flags]
```

#### Flags

| Flag | Description |
|------|-------------|
| `--status <status>` | Filter by status |
| `--with-runs` | Show active runs for each issue |

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

---

## orch tick

Resume waiting runs.

```bash
orch tick RUN_REF|--all [flags]
```

### Flags

| Flag | Description |
|------|-------------|
| `--all` | Process all waiting runs |
| `--only-waiting` | Only process waiting runs |
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
orch schema
```

---

## orch monitor

Interactive TUI for monitoring runs.

```bash
orch monitor [flags]
```

---

## orch exec

Execute a command in a run's worktree.

```bash
orch exec RUN_REF -- COMMAND [args...]
```

### Examples

```bash
# Run tests in worktree
orch exec my-issue -- npm test

# Check git status
orch exec my-issue -- git status
```

---

## orch delete

Delete runs and their resources.

```bash
orch delete RUN_REF [flags]
```

---

## orch daemon

Manage the background monitoring daemon.

### orch daemon start

Start the daemon.

```bash
orch daemon start
```

### orch daemon stop

Stop the daemon.

```bash
orch daemon stop
```

### orch daemon status

Check daemon status.

```bash
orch daemon status
```

---

## orch debug

Debug a run by showing daemon perspective.

```bash
orch debug RUN_REF
```

---

## orch log

View orch logs.

```bash
orch log [flags]
```

---

## orch models

List available models for opencode.

```bash
orch models
```

---

## orch notify

Notification management commands.

```bash
orch notify [subcommand]
```

---

## orch tutorial

Show setup guide and usage reference.

```bash
orch tutorial
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
