# Orch Toolset - Claude Code Skill Plugin

Agent Skills for Claude Code that teach the orch CLI workflow: issue management, run orchestration, monitoring, and multi-agent coordination.

## Overview

This plugin provides Claude Code with comprehensive knowledge about using the **orch** orchestrator CLI to:

- Create and manage issues
- Start, stop, and monitor LLM agent runs
- Coordinate multiple concurrent agent sessions
- Execute commands in isolated worktrees
- Implement control agent workflows

## Installation

### Option 1: Add as Local Marketplace (Recommended)

Add the plugin as a local marketplace in your Claude Code settings (`~/.claude/settings.json`):

```json
{
  "enabledPlugins": {
    "orch-toolset@orch-toolset-marketplace": true
  },
  "extraKnownMarketplaces": {
    "orch-toolset-marketplace": {
      "source": {
        "source": "directory",
        "path": "/absolute/path/to/orch/claude-plugins/orch-toolset"
      }
    }
  }
}
```

Replace `/absolute/path/to/orch` with your actual orch repository path.

### Option 2: Load Per-Session

Load the plugin for a single session:

```bash
claude --plugin-dir /path/to/orch/claude-plugins/orch-toolset
```

### Option 3: GitHub Marketplace (Future)

After publishing to a public GitHub marketplace:

```bash
claude plugin install proboscis/orch-toolset
```

## Usage

Skills are model-invoked - Claude will automatically use this skill when you ask about orch commands or orchestration workflows. Example queries:

- "How do I start a new run for issue orch-055?"
- "Show me how to monitor multiple runs and send guidance."
- "What is the control agent workflow for orch?"
- "How do I run tests in an agent's worktree?"
- "What commands can I use to check on blocked runs?"

### Slash Commands

- `/install` - Install this plugin to your Claude Code settings (when loaded temporarily)

## Plugin Contents

```
orch-toolset/
├── .claude-plugin/
│   ├── plugin.json           # Plugin metadata
│   └── marketplace.json      # Local marketplace definition
├── skills/
│   ├── orch-toolset/
│   │   ├── SKILL.md          # Core orchestration guidance
│   │   └── reference.md      # Detailed command reference
│   └── install-orch-plugin/
│       └── SKILL.md          # Installation helper skill
├── commands/
│   └── install.md            # /install command
└── README.md                 # This file
```

### Skills

**orch-toolset**: Main skill covering:
- Core workflow and design philosophy
- Command categories (issues, runs, monitoring, communication)
- Run lifecycle states
- Best practices for multi-run orchestration
- Control agent workflow

**install-orch-plugin**: Helper skill for installing the plugin to user settings.

### reference.md

Comprehensive command reference including:
- All orch commands with full syntax
- Flags and options for each command
- Usage examples by scenario
- Exit codes and error handling

## Key Commands Covered

| Category | Commands |
|----------|----------|
| Issue Management | `orch issue create`, `orch issue list`, `orch open` |
| Run Management | `orch run`, `orch continue`, `orch ps`, `orch show`, `orch stop`, `orch resolve` |
| Monitoring | `orch monitor`, `orch attach`, `orch capture` |
| Agent Communication | `orch send`, `orch exec` |
| Maintenance | `orch repair`, `orch tick` |

## Quick Start Examples

```bash
# Create an issue and start a run
orch issue create my-task --title "Implement feature X"
orch run my-task

# Monitor progress
orch ps --status running,blocked
orch capture my-task --lines 200

# Send guidance to blocked agent
orch send my-task "Focus on the auth module"

# Run tests in isolation
orch exec my-task -- pytest tests/

# Clean up
orch stop my-task
orch resolve my-task
```

## Requirements

- Claude Code CLI
- orch CLI installed and configured
- tmux (for agent session management)

## License

MIT

## Contributing

Issues and PRs welcome at https://github.com/proboscis/orch
