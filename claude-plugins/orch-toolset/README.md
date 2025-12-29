# Orch Toolset - Claude Code Skill

Agent Skills for Claude Code that teach the orch CLI workflow: issue management, run orchestration, monitoring, and multi-agent coordination.

## Overview

This skill provides Claude Code with comprehensive knowledge about using the **orch** orchestrator CLI to:

- Create and manage issues
- Start, stop, and monitor LLM agent runs
- Coordinate multiple concurrent agent sessions
- Execute commands in isolated worktrees
- Implement control agent workflows

## Installation

### Option 1: Symlink (Recommended)

Create a symlink from the orch repo to your Claude Code skills directory:

```bash
ln -s /path/to/orch/claude-plugins/orch-toolset/skills/orch-toolset ~/.claude/skills/orch-toolset
```

### Option 2: Copy

Copy the skill directory:

```bash
cp -r /path/to/orch/claude-plugins/orch-toolset/skills/orch-toolset ~/.claude/skills/
```

### Option 3: Load as Plugin (Per-Session)

Load the entire plugin for a single session:

```bash
claude --plugin-dir /path/to/orch/claude-plugins/orch-toolset
```

## Verification

After installation, verify the skill is in place:

```bash
ls ~/.claude/skills/orch-toolset/SKILL.md
```

Then **restart Claude Code** for the skill to be loaded.

## Usage

Skills are model-invoked - Claude will automatically use this skill when you ask about orch commands or orchestration workflows. Example queries:

- "How do I start a new run for issue orch-055?"
- "Show me how to monitor multiple runs and send guidance."
- "What is the control agent workflow for orch?"
- "How do I run tests in an agent's worktree?"
- "What commands can I use to check on blocked runs?"

## Skill Contents

```
orch-toolset/
├── SKILL.md          # Core orchestration guidance
└── reference.md      # Detailed command reference
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
| Maintenance | `orch repair` |

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

## Sources

- [Agent Skills - Claude Code Docs](https://code.claude.com/docs/en/skills)
- [How to create custom Skills | Claude Help Center](https://support.claude.com/en/articles/12512198-how-to-create-custom-skills)
