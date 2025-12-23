---
name: install-orch-skill
description: |
  Install the orch-toolset skill to ~/.claude/skills/ by creating a symlink.
  Use when user asks to "install orch skill", "add orch skill to claude", "setup orch skill", or wants to make orch-toolset available permanently.
allowed-tools: Bash
---

# Install Orch Skill

This skill installs the orch-toolset skill to `~/.claude/skills/` by creating a symlink.

## Installation Steps

Execute these commands:

```bash
# 1. Find the orch repository (check common locations)
ORCH_REPO=""
if [ -d "$HOME/repos/orch/claude-plugins/orch-toolset/skills/orch-toolset" ]; then
  ORCH_REPO="$HOME/repos/orch"
elif [ -d "./claude-plugins/orch-toolset/skills/orch-toolset" ]; then
  ORCH_REPO="$(pwd)"
elif [ -d "../claude-plugins/orch-toolset/skills/orch-toolset" ]; then
  ORCH_REPO="$(cd .. && pwd)"
fi

# 2. Create ~/.claude/skills/ if it doesn't exist
mkdir -p ~/.claude/skills

# 3. Create symlink (use -sf to overwrite if exists)
ln -sf "$ORCH_REPO/claude-plugins/orch-toolset/skills/orch-toolset" ~/.claude/skills/orch-toolset

# 4. Verify
ls -la ~/.claude/skills/orch-toolset/SKILL.md
```

## What This Does

1. Locates the orch repository
2. Creates `~/.claude/skills/` directory if needed
3. Creates a symlink: `~/.claude/skills/orch-toolset` -> `<orch-repo>/claude-plugins/orch-toolset/skills/orch-toolset`
4. Verifies the symlink points to valid SKILL.md

## After Installation

Tell the user:
1. **Restart Claude Code** for the skill to be loaded
2. The orch-toolset skill will then be available in all sessions
3. Test by asking: "How do I start a new run with orch?"

## If Orch Repo Not Found

Ask the user for the path to their orch repository, then run:
```bash
ln -sf /path/to/orch/claude-plugins/orch-toolset/skills/orch-toolset ~/.claude/skills/orch-toolset
```
