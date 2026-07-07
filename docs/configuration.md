# Configuration

orch uses a layered configuration system with sensible defaults. Configuration can come from multiple sources, with more specific settings overriding general ones.

## Configuration Hierarchy

Settings are resolved in this order (highest priority first):

1. **Command-line flags** (`--agent`, `--backend`, etc.)
2. **Environment variables** (`ORCH_AGENT`, etc.)
3. **Project config** (`.orch/config.yaml` in current or parent directory)
4. **Global config** (`~/.config/orch/config.yaml`)
5. **Built-in defaults**

## Remote Configuration

When using a remote daemon, configure the client target in
`~/.config/orch/client.yaml`.

```yaml
# ~/.config/orch/client.yaml
remote:
  default: primary
  hosts:
    primary:
      addr: master-host:7777
    cloud:
      addr: 10.0.0.5:7777
```

You can then run remote commands without passing `--remote` each time:

```bash
# Uses remote.default from client.yaml
orch ps

# Override default with alias
orch --remote cloud ps

# Bypass remote.default for one command (use local daemon)
orch --remote "" ps
```

On the server side, expose the daemon over TCP and register the repository URL:

```bash
# On remote server
orch daemon start --listen tcp://0.0.0.0:7777

# From client machine
orch --remote master-host:7777 daemon repo register https://github.com/your-org/your-project.git
orch --remote master-host:7777 daemon repo list
```

In remote mode, orch resolves project identity from `--project`/`ORCH_PROJECT`
and daemon repo mappings.

## Quick Start

Create `.orch/config.yaml` in your project root:

```yaml
# Minimal configuration
agent: claude
base_branch: main
```

## Project Configuration

### Location

Place `.orch/config.yaml` in your git repository root. orch will search upward from your current directory to find it.

```
your-repo/
├── .orch/
│   └── config.yaml
├── src/
└── ...
```

### Full Configuration Reference

```yaml
# =============================================================================
# CORE SETTINGS
# =============================================================================

# Default agent for new runs
# Options: claude, opencode, codex, gemini, custom
agent: claude

# Base branch for creating worktrees (default: main)
base_branch: main

# Target branch for PRs (default: same as base_branch)
pr_target_branch: develop

# Directory for worktrees (default: ~/.orch/worktrees)
worktree_dir: ~/.orch/worktrees

# Terminal multiplexer: tmux or zellij (default: tmux)
multiplexer: tmux

# =============================================================================
# ISSUES BACKEND
# =============================================================================

issues:
  # Backend type: local, github, or linear
  backend: local
  
  # For local backend: path to issues directory
  path: ~/orch-issues
  
  # Alternative: path relative to project root
  # path: ./issues

# =============================================================================
# AGENT-SPECIFIC CONFIGURATION
# =============================================================================

# OpenCode configuration
opencode:
  default_model: anthropic/claude-opus-4-5
  default_variant: max
  prompt_template: |
    ultrawork Please read 'ORCH_PROMPT.md' in the current directory.
    {{issue}}

# Claude configuration
claude:
  prompt_template: |
    ultrathink Please read 'ORCH_PROMPT.md' in the current directory.
    {{issue}}

# Codex configuration
codex:
  prompt_template: |
    Think step by step. Follow best practices.
    {{issue}}

# Gemini configuration
gemini:
  prompt_template: "{{issue}}"

# =============================================================================
# PROMPT TEMPLATES
# =============================================================================

# Global default template (used when no agent-specific template exists)
prompt_template: |
  ultrathink Please read 'ORCH_PROMPT.md' in the current directory.
  {{issue}}

# =============================================================================
# GITHUB BACKEND (when issues.backend: github)
# =============================================================================

github:
  owner: your-org
  repo: your-repo
  poll_interval: 300  # seconds between polling for updates

# =============================================================================
# LINEAR BACKEND (when issues.backend: linear)
# =============================================================================

linear:
  team_id: YOUR_TEAM_ID
  api_key: ${LINEAR_API_KEY}  # Use environment variable

# =============================================================================
# NOTIFICATIONS
# =============================================================================

slack:
  enabled: true
  
  # Option 1: Incoming Webhook (simpler)
  webhook_url: https://hooks.slack.com/services/XXX/YYY/ZZZ
  
  # Option 2: Bot Token (more features)
  # bot_token: xoxb-your-bot-token
  # channel: "#orch-notifications"
  
  # Events that trigger notifications
  notify_on:
    - waiting
    - rate_limited
    # - done
    # - failed

# =============================================================================
# PS SETTINGS (for orch ps command)
# =============================================================================

ps:
  # Default run statuses to show when --status is not specified.
  # Use `orch ps --all` or an explicit `--status ...` to bypass this default.
  # Plain table output shows excluded status counts at the end.
  default_statuses:
    - queued
    - booting
    - running
    - waiting
    - rate_limited
    - pr_open

# =============================================================================
# MONITOR SETTINGS (for orch monitor command)
# =============================================================================

monitor:
  # Default run statuses to show
  default_run_statuses:
    - queued
    - booting
    - running
    - waiting
    - rate_limited
    - pr_open
  
  # Default issue statuses to show
  default_issue_statuses:
    - open

# Control agent settings (for interactive control from monitor)
control_agent: opencode
control_model: opus
control_model_variant: default

# =============================================================================
# PRESETS (named configurations for orch run --preset)
# =============================================================================

presets:
  opus-max:
    agent: opencode
    model: anthropic/claude-opus-4-5
    model_variant: max
  
  sonnet-fast:
    agent: opencode
    model: anthropic/claude-sonnet-4
    model_variant: default
  
  codex-high:
    agent: codex
    model: o3
```

## Environment Variables

All settings can be configured via environment variables:

| Variable | Description | Example |
|----------|-------------|---------|
| `ORCH_PROJECT` | Project identity (repo ID or URL) | `your-org-your-repo` |
| `ORCH_REMOTE` | Remote daemon address | `master-host:7777` |
| `ORCH_AGENT` | Default agent | `claude` |
| `ORCH_BACKEND` | Backend type | `file` |
| `ORCH_MODEL` | Default model | `anthropic/claude-opus-4-5` |
| `ORCH_MODEL_VARIANT` | Model variant | `max` |
| `ORCH_MULTIPLEXER` | Terminal multiplexer | `tmux` |
| `ORCH_LOG_LEVEL` | Logging verbosity | `debug` |
| `ORCH_PR_TARGET_BRANCH` | PR target branch | `develop` |
| `ORCH_DEBUG` | Enable debug mode | `1` |
| `ORCH_SLACK_WEBHOOK_URL` | Slack webhook | `https://hooks.slack.com/...` |
| `ORCH_SLACK_BOT_TOKEN` | Slack bot token | `xoxb-...` |
| `ORCH_SLACK_CHANNEL` | Slack channel | `#notifications` |

### Removed Variables

`ORCH_VAULT` and `ORCH_ISSUES_ROOT` are no longer used at runtime.
Configure issue storage with `issues.path` in `.orch/config.yaml`.

## Prompt Templates

Customize the initial prompt sent to agents using template variables:

| Variable | Description |
|----------|-------------|
| `{{issue}}` | Full issue content (frontmatter + body) |
| `{{issue_id}}` | Issue ID only |
| `{{issue_title}}` | Issue title only |

### Example: Custom template

```yaml
prompt_template: |
  You are working on issue {{issue_id}}: {{issue_title}}
  
  Instructions:
  - Follow the existing code style
  - Write tests for new functionality
  - Create a PR when done
  
  Issue details:
  {{issue}}
```

### Per-agent templates

Different agents may need different prompts:

```yaml
claude:
  prompt_template: |
    ultrathink Be thorough and comprehensive.
    {{issue}}

opencode:
  prompt_template: |
    ultrawork Focus on efficiency.
    {{issue}}
```

## Terminal Multiplexer

orch supports both tmux (default) and zellij:

```yaml
multiplexer: zellij
```

Or per-command:

```bash
orch run --multiplexer zellij my-issue
```

**Detach keys:**
- tmux: `Ctrl+B D`
- zellij: `Ctrl+O D`

**Note:** Some features like `orch monitor` require tmux for multi-pane layouts.

## Slack Notifications

Get notified when runs need attention:

### Using Webhooks (simpler)

1. Create an incoming webhook in Slack
2. Add to config:

```yaml
slack:
  enabled: true
  webhook_url: https://hooks.slack.com/services/XXX/YYY/ZZZ
  notify_on:
    - waiting
    - failed
```

### Using Bot Token (more features)

1. Create a Slack app with `chat:write` permission
2. Add to config:

```yaml
slack:
  enabled: true
  bot_token: xoxb-your-token
  channel: "#orch-notifications"
  notify_on:
    - waiting
    - rate_limited
    - done
```

## Common Configurations

### Solo developer

```yaml
agent: claude
base_branch: main
issues:
  backend: local
  path: ./issues
```

### Team with GitHub Issues

```yaml
agent: opencode
base_branch: main
pr_target_branch: develop

issues:
  backend: github

github:
  owner: my-org
  repo: my-repo

slack:
  enabled: true
  webhook_url: ${SLACK_WEBHOOK_URL}
  notify_on:
    - waiting
```

### Multiple models

```yaml
agent: opencode

opencode:
  default_model: anthropic/claude-sonnet-4
  default_variant: default

presets:
  fast:
    model: anthropic/claude-sonnet-4
  thorough:
    model: anthropic/claude-opus-4-5
    model_variant: max
```

Use presets:
```bash
orch run --preset thorough complex-issue
orch run --preset fast simple-fix
```
