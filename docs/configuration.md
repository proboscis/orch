# Configuration

orch uses a layered configuration system with sensible defaults. Configuration can come from multiple sources, with more specific settings overriding general ones.

## Configuration Hierarchy

Settings are resolved in this order (highest priority first):

1. **Command-line flags** (`--project`, `--remote`, etc.)
2. **Project config** (`.orch/config.yaml` in current or parent directory)
3. **Environment variables** (`ORCH_AGENT`, etc.)
4. **Global config** (`~/.config/orch/config.yaml`)
5. **Built-in defaults**

## Remote Configuration

When using a remote daemon, configure the client target in
`~/.config/orch/client.yaml` (global), or per repository in
`.orch/client.yaml` (discovered by walking up from the current directory,
like `.orch/config.yaml`).

```yaml
# ~/.config/orch/client.yaml or <repo>/.orch/client.yaml
remote:
  default: primary
  hosts:
    primary:
      addr: master-host:7777
    cloud:
      addr: 10.0.0.5:7777
```

Per-repo values win field-wise: a non-empty `remote.default` overrides the
global one, and host aliases are merged with per-repo entries taking
precedence. The full resolution order for the remote address is `--remote`
flag > `ORCH_REMOTE` > per-repo `client.yaml` > global `client.yaml`.

A committed `.orch/client.yaml` redirects every orch command run inside that
checkout, so review it like code when cloning; for machine-specific values,
keep the file untracked (gitignored) instead of committing it.

You can then run remote commands without passing `--remote` each time:

```bash
# Uses remote.default from client.yaml
orch ps

# Override default with alias
orch --remote cloud ps

# Bypass remote.default for one command (use local daemon)
orch --remote "" ps
```

The daemon's TCP listener binds loopback (`127.0.0.1:7777`) by default —
remote access is an explicit opt-in: start the master with
`--listen tcp://0.0.0.0:7777` (ADR-0003). Register the repository URL on
the server-side daemon:

```bash
# From client machine
orch --remote master-host:7777 daemon repo register https://github.com/your-org/your-project.git
orch --remote master-host:7777 daemon repo list
```

Opting into `0.0.0.0` exposes the TCP API on every network interface — limit
access with a firewall, or keep the loopback default and let clients connect
through an SSH tunnel instead:

```bash
ssh -L 7777:127.0.0.1:7777 master-host   # then: orch --remote localhost:7777 ps
```

In remote mode, orch resolves project identity from `--project`/`ORCH_PROJECT`
and daemon repo mappings.

Note: repo registration is not remote-specific — the **local** daemon needs it
too. For a local checkout, register by path from the repository root:

```bash
orch daemon repo register "$(pwd)"
```

The mapping lands in `~/.config/orch/projects/<project_id>.yaml` (project_id =
origin URL normalized to `<owner>-<repo>`) and takes effect immediately.

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

# Multiplexer for agent sessions: tmux or zellij (default: tmux)
agent_multiplexer: tmux

# `multiplexer` is a deprecated compatibility key. Use `agent_multiplexer`.

# Default model / reasoning-effort variant for runs (per-agent defaults below
# take precedence; --model / --model-variant flags override everything)
model: claude-opus-4-5
model_variant: max

# Skip PR creation by default (equivalent to always passing --no-pr)
no_pr: false

# Default preset name from `presets:` applied when `orch run` has no
# agent/model flags
default_preset: main

# CLI log level: debug, info, warn, error
log_level: info

# External diff tool for `orch diff --tool` (e.g. "cursor --diff")
diff_tool: "cursor --diff"

# =============================================================================
# EXECUTION TARGETS
# =============================================================================

# Named worker hosts used by `orch run --on <name>`
targets:
  - name: mac
    host: mac-host
  - name: linux
    host: linux-host

# =============================================================================
# ISSUES BACKEND
# =============================================================================

issues:
  # Backend type: local or github
  backend: local
  
  # Optional path for the local backend. When omitted, defaults to
  # ~/.local/share/orch/<repo-id>.
  # path: ~/orch-issues
  
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
  # Extra CLI args appended to every launch / control-agent launch
  extra_args: ["--dangerously-skip-permissions"]
  control_extra_args: []
  # Multi-account profiles: each maps to a CLAUDE_CONFIG_DIR and may be
  # pinned to targets (see .orch/config.example.yaml for a full example)
  default_profile: personal
  profiles:
    personal:
      config_dir: ~/.claude-personal
      target: mac              # optional: default target for this profile
      allowed_targets: [mac]   # optional: hosts this profile may run on
    work:
      config_dir: ~/.claude-work

# Codex configuration
codex:
  default_model: gpt-5.2-codex   # default model for codex runs
  default_variant: high          # default reasoning effort
  prompt_template: |
    Think step by step. Follow best practices.
    {{issue}}
  extra_args: []
  control_extra_args: []
  # Multi-account profiles: each maps to a CODEX_HOME directory
  default_profile: personal
  profiles:
    personal:
      codex_home: ~/.codex
    company:
      codex_home: ~/.codex-profiles/company
      allowed_targets: [mac]

# Gemini configuration
gemini:
  prompt_template: "{{issue}}"
  extra_args: []
  control_extra_args: []

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
  # Only sync issues carrying this label (also: ORCH_GITHUB_LABEL_FILTER)
  label_filter: orch
  # Map GitHub labels to orch issue statuses (label -> status); when set,
  # resolving an issue applies the matching label
  status_labels:
    "status:open": open
    "status:resolved": resolved

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
  
  # Events that trigger notifications.
  # Valid: waiting, blocked, rate_limited, blocked_api, done, failed.
  # When omitted, defaults to: waiting, blocked, rate_limited, blocked_api.
  notify_on:
    - waiting
    - rate_limited
    # - blocked
    # - blocked_api
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
# MONITOR SETTINGS (for the Python orch-monitor TUI)
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

  # Initial issue tag filter (`any` = OR, `all` = AND)
  default_issue_filter:
    tags:
      - active
    tag_mode: any

# Control agent settings (for interactive control from monitor)
control_agent: opencode
control_model: opus
control_model_variant: default

# =============================================================================
# PRESETS (named configurations for orch run --preset)
# =============================================================================

presets:
  - name: opus-max
    backend: opencode
    model: anthropic/claude-opus-4-5
    variant: max
  - name: sonnet-fast
    backend: opencode
    model: anthropic/claude-sonnet-4
    variant: default
  - name: codex-high
    backend: codex
    model: o3
  - name: claude-work
    backend: claude
    model: claude-opus-4-5
    variant: max
    profile: work   # claude/codex profile name (see profiles above)
```

## Environment Variables

All settings can be configured via environment variables:

| Variable | Description | Example |
|----------|-------------|---------|
| `ORCH_PROJECT` | Project identity (repo ID or URL) | `your-org-your-repo` |
| `ORCH_REMOTE` | Remote daemon address | `master-host:7777` |
| `ORCH_AGENT` | Default agent | `claude` |
| `ORCH_ISSUES_BACKEND` | Issue store backend (`local` or `github`) | `local` |
| `ORCH_MODEL` | Default model | `anthropic/claude-opus-4-5` |
| `ORCH_MODEL_VARIANT` | Model variant | `max` |
| `ORCH_WORKTREE_DIR` | Directory in which run worktrees are created | `~/.orch/worktrees` |
| `ORCH_BASE_BRANCH` | Default base branch | `main` |
| `ORCH_LOG_LEVEL` | Logging verbosity | `debug` |
| `ORCH_PR_TARGET_BRANCH` | PR target branch | `develop` |
| `ORCH_PROMPT_TEMPLATE` | Global prompt template content | `Read ORCH_PROMPT.md` |
| `ORCH_AGENT_MULTIPLEXER` | Multiplexer for agent sessions | `tmux` |
| `ORCH_MONITOR_MULTIPLEXER` | Multiplexer for Python `orch-monitor` | `zellij` |
| `ORCH_MULTIPLEXER` | Deprecated multiplexer compatibility setting | `tmux` |
| `ORCH_NO_PR` | Omit PR creation instructions (`true`, `1`, or `yes`) | `true` |
| `ORCH_OPENCODE_DEFAULT_MODEL` | Default OpenCode model | `anthropic/claude-opus-4-5` |
| `ORCH_OPENCODE_DEFAULT_VARIANT` | Default OpenCode model variant | `max` |
| `ORCH_CODEX_DEFAULT_MODEL` | Default Codex model | `gpt-5.2-codex` |
| `ORCH_DEFAULT_PRESET` | Preset used when `--preset` is omitted | `opus-max` |
| `ORCH_CONTROL_AGENT` | Agent used by the interactive control agent | `opencode` |
| `ORCH_CONTROL_MODEL` | Model used by the interactive control agent | `opus` |
| `ORCH_CONTROL_MODEL_VARIANT` | Control-agent model variant | `default` |
| `ORCH_WORKER_AUTOSTART` | Set to `0` to disable worker autostart | `0` |
| `ORCH_DEBUG` | Enable debug mode | `1` |
| `ORCH_SLACK_WEBHOOK_URL` | Slack webhook | `https://hooks.slack.com/...` |
| `ORCH_SLACK_BOT_TOKEN` | Slack bot token | `xoxb-...` |
| `ORCH_SLACK_CHANNEL` | Slack channel | `#notifications` |
| `ORCH_GITHUB_OWNER` | GitHub issue-backend repository owner | `your-org` |
| `ORCH_GITHUB_REPO` | GitHub issue-backend repository name | `your-repo` |
| `ORCH_GITHUB_LABEL_FILTER` | Only sync GitHub issues with this label | `orch` |

### Removed Variables

`ORCH_VAULT` and `ORCH_ISSUES_ROOT` are no longer used at runtime.
Configure issue storage with `issues.path` in `.orch/config.yaml`.

## Execution Targets

`targets` is the routing table for multi-host execution. Each entry maps the
stable name used by users and profiles to the host identity of a registered
worker. The daemon resolves this table, records the selected target in the run
event stream, and routes later session-control operations to the same host.

```yaml
targets:
  - name: mac
    host: mac-host
  - name: gpu
    host: gpu-worker-01
```

Select a target when starting a run:

```bash
orch run --on mac my-issue
```

Both `name` and `host` are required and must be non-empty. `local` is reserved
for the daemon's local worker and does not need a `targets` entry. Target names
are also used by agent-profile `target` and `allowed_targets` settings.

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

## Terminal Multiplexers

Agent sessions and the Python `orch-monitor` TUI have separate multiplexer
settings. Agent sessions default to tmux; the Python TUI auto-detects its
environment and otherwise defaults to zellij.

```yaml
agent_multiplexer: tmux
```

The old `multiplexer` key is deprecated; use `agent_multiplexer` for runs.

Or per-command:

```bash
orch run --multiplexer zellij my-issue
```

**Detach keys:**

- tmux: `Ctrl+B D`
- zellij: `Ctrl+O D`

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
  notify_on:
    - waiting
```

Set the webhook through the supported environment override (config-file
`${...}` interpolation is not performed):

```bash
export ORCH_SLACK_WEBHOOK_URL=https://hooks.slack.com/services/XXX/YYY/ZZZ
```

### Multiple models

```yaml
agent: opencode

opencode:
  default_model: anthropic/claude-sonnet-4
  default_variant: default

presets:
  - name: fast
    model: anthropic/claude-sonnet-4
  - name: thorough
    model: anthropic/claude-opus-4-5
    variant: max
```

Use presets:
```bash
orch run --preset thorough complex-issue
orch run --preset fast simple-fix
```
