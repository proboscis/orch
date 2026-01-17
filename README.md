# orch

Orchestrator for managing multiple LLM CLIs (claude/codex/gemini) using a unified vocabulary of **issue/run/event**.

## Overview

orch operates **non-interactively** by default. When human input is needed, use `orch attach` to connect to the terminal session (tmux or zellij) and interact directly with the agent.

## User Interaction Flow

```mermaid
sequenceDiagram
    participant U as User
    participant O as orch
    participant A as Agent (claude/codex)
    participant T as tmux

    Note over U: Start working on an issue
    U->>O: orch run my-issue
    O->>T: create session
    O->>A: start agent
    O-->>U: returns immediately

    Note over U: Check progress anytime
    U->>O: orch ps
    O-->>U: shows status (running/blocked/done)

    alt Agent needs help
        A->>A: stops or shows prompt
        Note over U: See status or attach
        U->>O: orch ps
        O-->>U: status: blocked or running
        U->>O: orch attach my-issue
        O->>T: attach session
        Note over U,T: Direct terminal interaction
        U->>T: (provide input to agent)
        U->>T: Ctrl+B D (detach)
    end

    alt Want to interact directly
        U->>O: orch attach my-issue
        O->>T: attach session
        Note over U,T: Direct terminal interaction
        U->>T: (type commands, paste images)
        U->>T: Ctrl+B D (detach)
    end

    alt Want to stop
        U->>O: orch stop my-issue
        O->>T: kill all sessions for issue
        O->>O: mark all runs canceled
    end

    alt Agent finishes
        A->>O: emits done/pr_open event
        U->>O: orch ps
        O-->>U: status: done or pr_open
    end
```

## State Machine

```mermaid
stateDiagram-v2
    [*] --> queued: orch run

    queued --> booting: agent starting
    booting --> running: agent ready

    running --> blocked: needs human input
    running --> pr_open: PR created
    running --> done: task complete
    running --> failed: error occurred
    running --> canceled: orch stop

    blocked --> running: orch attach (provide input)
    blocked --> canceled: orch stop

    pr_open --> done: PR merged

    done --> [*]
    failed --> [*]
    canceled --> [*]
```

## When to Use Each Command

```mermaid
flowchart TD
    START([Want to work on an issue?]) --> RUN[orch run ISSUE]

    RUN --> WAIT([Wait for agent...])
    WAIT --> CHECK{Check status}
    CHECK --> PS[orch ps]

    PS --> |running| DECIDE{Need to interact?}
    PS --> |blocked| BLOCKED([Agent needs help])
    PS --> |done/pr_open| DONE([Finished!])
    PS --> |failed| FAILED([Something went wrong])

    DECIDE --> |yes| ATTACH[orch attach]
    DECIDE --> |no| WAIT
    ATTACH --> |done interacting| WAIT

    BLOCKED --> ATTACH2[orch attach]
    ATTACH2 --> |provide input| WAIT

    FAILED --> RETRY{Retry?}
    RETRY --> |yes| RUN
    RETRY --> |no| END([End])

    DONE --> END

    style RUN fill:#4CAF50,color:#fff
    style PS fill:#2196F3,color:#fff
    style ATTACH fill:#FF9800,color:#fff
    style ATTACH2 fill:#FF9800,color:#fff
```

## Quick Reference

| Situation | Command |
|-----------|---------|
| Start working on an issue | `orch run ISSUE` |
| Continue from an existing run | `orch continue ISSUE#RUN_ID` |
| Continue from a branch | `orch continue ISSUE --branch BRANCH` |
| Check what's running | `orch ps` |
| Watch agent work / interact | `orch attach RUN` |
| See run details | `orch show RUN` |
| Agent is blocked - interact | `orch attach RUN` |
| Stop all runs for an issue | `orch stop ISSUE` |
| Stop a specific run | `orch stop ISSUE#RUN_ID` |
| Stop all runs globally | `orch stop --all` |
| Fix problems | `orch repair` |

## Statuses

| Status | Meaning | User Action |
|--------|---------|-------------|
| `queued` | Run created, waiting to start | Wait |
| `booting` | Agent is starting up | Wait |
| `running` | Agent is actively working | Wait, or `attach` to watch |
| `blocked` | Agent needs input | `attach` to interact |
| `pr_open` | PR created, awaiting review | Review the PR |
| `done` | Work completed | Nothing - celebrate! |
| `failed` | Run failed | Check logs, maybe retry |
| `canceled` | Manually stopped | Nothing |

## Background Monitoring

orch automatically runs a background daemon that monitors all running agents. You don't need to manage it manually.

**What the daemon does:**
- Monitors tmux sessions for all running runs
- Detects when agents finish (done/failed)
- Detects when agents are stuck or need input (blocked)
- Updates run status automatically

**If something goes wrong:**
```bash
orch repair    # Fixes daemon, stale states, orphaned sessions
```

## Troubleshooting

### Debug mode for `orch run`

If runs are failing silently or not connecting to the agent properly:

```bash
# Enable verbose output
orch run --verbose my-issue
orch run -v my-issue

# Or via environment variable
ORCH_DEBUG=1 orch run my-issue

# Or via log level flag
orch run --log-level debug my-issue
```

Debug output shows:
- Server discovery (which ports are scanned)
- Health check requests/responses
- Session creation with directory context
- Prompt delivery status

### Daemon logs

The background daemon logs to `.orch/daemon.log` in your vault directory:

```bash
tail -f $ORCH_VAULT/.orch/daemon.log
```

## Configuration

```bash
# Set vault path (required)
export ORCH_VAULT=~/vault

# Or pass per-command
orch --vault ~/vault ps
```

Per-repo defaults can live in `.orch/config.yaml`:

```yaml
vault: ~/vault
agent: claude
worktree_root: .git-worktrees
base_branch: main
pr_target_branch: develop
```

`pr_target_branch` controls the target branch mentioned in agent PR instructions.

### Terminal Multiplexer

orch supports both **tmux** (default) and **zellij** as terminal multiplexers for running agent sessions.

```bash
# Use tmux (default)
orch run my-issue

# Use zellij
orch run --multiplexer zellij my-issue

# Or set via environment variable
export ORCH_MULTIPLEXER=zellij
orch run my-issue
```

**Configuration:**

```yaml
# .orch/config.yaml
multiplexer: zellij  # or "tmux" (default)
```

**Detach keys:**
- tmux: `Ctrl+B D`
- zellij: `Ctrl+O D` (or your configured detach keybind)

**Notes:**
- The multiplexer used for a run is recorded in the run metadata
- `orch attach` automatically uses the correct multiplexer for each run
- Some advanced features (window linking, pane inspection) have limited support in zellij
- **`orch monitor` requires tmux** - it uses tmux's multi-pane layout. For zellij users, use `orch-monitor-tui` (Python TUI) or the basic `orch run/attach/ps` workflow

### Repo config

Create `.orch/config.yaml` in the repo root to set defaults:

```yaml
vault: ~/vault
agent: claude
base_branch: main
pr_target_branch: develop
```

`pr_target_branch` controls the default target branch in the agent PR instructions.

### Slack Notifications

Get notified when runs become blocked and need your attention:

```yaml
# .orch/config.yaml
slack:
  enabled: true
  # Option 1: Incoming Webhook (simpler setup)
  webhook_url: https://hooks.slack.com/services/XXX/YYY/ZZZ
  
  # Option 2: Bot Token (more features)
  # bot_token: xoxb-your-bot-token
  # channel: "#orch-notifications"
  
  # Which events trigger notifications (default: blocked, blocked_api)
  notify_on:
    - blocked
    - blocked_api
    # - done
    # - failed
```

Environment variables are also supported:
- `ORCH_SLACK_WEBHOOK_URL` - Webhook URL (auto-enables if set)
- `ORCH_SLACK_BOT_TOKEN` - Bot token for Slack API
- `ORCH_SLACK_CHANNEL` - Channel for bot messages

Example notification:
```
:no_entry: Run blocked: orch-145#8cd1d7
Issue: Implement feature X
Status: blocked (waiting for user input)
Attach: orch attach orch-145#20260115-161736
```

### Prompt Templates

Customize the initial prompt sent to agents when starting a run. Supports per-backend configuration with global fallback.

```yaml
# .orch/config.yaml

# Global default template (used when no backend-specific template is set)
prompt_template: |
  ultrathink Please read 'ORCH_PROMPT.md' in the current directory.
  
  {{issue}}

# Per-backend templates (override global)
opencode:
  default_model: anthropic/claude-opus-4-5
  default_variant: max
  prompt_template: |
    ultrawork Please read 'ORCH_PROMPT.md' in the current directory.
    
    {{issue}}

claude:
  prompt_template: |
    ultrathink Be thorough and create comprehensive solutions.
    
    {{issue}}

codex:
  prompt_template: |
    Think step by step. Follow best practices.
    
    {{issue}}

gemini:
  prompt_template: "{{issue}}"
```

**Template Variables:**

| Variable | Description |
|----------|-------------|
| `{{issue}}` | Full issue content (title + body) |
| `{{issue_id}}` | Issue ID only (e.g., `orch-149`) |
| `{{issue_title}}` | Issue title only |

**Behavior:**
1. When `orch run` starts, it checks for a backend-specific template (e.g., `opencode.prompt_template`)
2. Falls back to global `prompt_template` if no backend-specific template exists
3. If no template is configured, uses the default: `ultrathink Please read 'ORCH_PROMPT.md'...`
4. Template variables are replaced with actual issue values
5. The rendered prompt is sent as the initial message to the agent

## Vault Structure

```
vault/
├── issues/
│   └── <ISSUE_ID>.md      # Issue specification
└── runs/
    └── <ISSUE_ID>/
        └── <RUN_ID>.md    # Run log with events
```

## Vocabulary

| Term | Description |
|------|-------------|
| **Issue** | A unit of work/specification (e.g., `plc124`) |
| **Run** | A single execution attempt for an issue |
| **Event** | A single append-only record in a run |
| **RUN_REF** | Reference format: `ISSUE_ID#RUN_ID` or just `ISSUE_ID` (latest) |
