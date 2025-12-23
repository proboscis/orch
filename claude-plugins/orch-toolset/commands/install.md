---
name: install
description: Install the orch-toolset skill to ~/.claude/skills/
allowed-tools: Bash
---

# Install Orch-Toolset Skill

Install this skill to the user's Claude Code skills directory so it's available in all sessions.

## Instructions

1. **Find the orch repository path** by checking current directory or asking user

2. **Create symlink** to `~/.claude/skills/orch-toolset`:
   ```bash
   ln -sf /path/to/orch/claude-plugins/orch-toolset/skills/orch-toolset ~/.claude/skills/orch-toolset
   ```

3. **Verify installation**:
   ```bash
   ls -la ~/.claude/skills/orch-toolset
   ```

4. **Report success** and tell user to restart Claude Code

## Execute Now

Create a symlink from the orch repo's skill to ~/.claude/skills/orch-toolset.

The orch repo is likely at one of:
- Current working directory or parent
- ~/repos/orch
- The directory containing the currently loaded plugin

After creating the symlink, verify it points to a directory containing SKILL.md.
