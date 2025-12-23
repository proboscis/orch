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

### Option 1: Local Development

Load the plugin directly while testing:

```bash
claude --plugin-dir /path/to/orch/claude-plugins/orch-toolset
```

### Option 2: From GitHub Repository

If you have the orch repository cloned:

```bash
claude --plugin-dir ./claude-plugins/orch-toolset
```

### Option 3: Marketplace Install

After publishing to a Claude Code marketplace:

```bash
claude plugin install orch-toolset@<marketplace>
```

For marketplace setup and publishing guidelines, see:
https://code.claude.com/docs/en/plugin-marketplaces

## Usage

Skills are model-invoked - Claude will automatically use this skill when you ask about orch commands or orchestration workflows. Example queries:

- "How do I start a new run for issue orch-055?"
- "Show me how to monitor multiple runs and send guidance."
- "What is the control agent workflow for orch?"
- "How do I run tests in an agent's worktree?"
- "What commands can I use to check on blocked runs?"

## Plugin Contents

```
orch-toolset/
├── .claude-plugin/
│   └── plugin.json           # Plugin metadata
├── skills/
│   └── orch-toolset/
│       ├── SKILL.md          # Core orchestration guidance
│       └── reference.md      # Detailed command reference
└── README.md                 # This file
```

### SKILL.md

Main skill file covering:
- Core workflow and design philosophy
- Command categories (issues, runs, monitoring, communication)
- Run lifecycle states
- Best practices for multi-run orchestration
- Control agent workflow

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
