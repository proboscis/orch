---
name: install-orch-plugin
description: |
  Install the orch-toolset plugin to user's Claude Code settings.
  Use when the user asks to "install orch plugin", "add orch skill", "setup orch for claude", or wants to make the orch toolset available in Claude Code.
allowed-tools: Read, Write, Bash, Glob
---

# Install Orch Plugin

This skill installs the orch-toolset Claude Code plugin to the user's settings.

## Installation Process

1. **Find the orch repository** - Locate where orch is installed
2. **Verify plugin exists** - Check that `claude-plugins/orch-toolset` exists
3. **Update Claude Code settings** - Add the plugin path to settings

## Steps to Execute

### Step 1: Find the orch repository

Look for the orch repository. Common locations:
- Current working directory or parent
- `~/repos/orch`
- Check if `orch` command is available and trace its location

```bash
# Check if orch is in PATH and find its location
which orch 2>/dev/null || echo "orch not in PATH"

# Or find by searching common locations
ls -d ~/repos/orch 2>/dev/null || ls -d ~/orch 2>/dev/null || echo "Check current directory"
```

### Step 2: Verify plugin directory exists

```bash
# Replace ORCH_PATH with actual path
ls -la ORCH_PATH/claude-plugins/orch-toolset/.claude-plugin/plugin.json
```

### Step 3: Update Claude Code settings

The plugin can be added to either:
- **User settings**: `~/.claude/settings.json` (applies to all projects)
- **Project settings**: `.claude/settings.json` (applies to current project only)

Read the existing settings file, add the plugin path to the `plugins` array, and write back.

Example settings structure:
```json
{
  "plugins": [
    "/absolute/path/to/orch/claude-plugins/orch-toolset"
  ]
}
```

### Step 4: Verify installation

After updating settings, inform the user to restart Claude Code for changes to take effect.

## Important Notes

- Always use **absolute paths** for plugin directories
- Create the settings file if it doesn't exist
- Preserve existing settings when adding the plugin
- Don't add duplicate entries if plugin is already installed

## Example Installation Flow

```bash
# 1. Find orch path (example)
ORCH_PATH="$HOME/repos/orch"

# 2. Verify plugin exists
test -f "$ORCH_PATH/claude-plugins/orch-toolset/.claude-plugin/plugin.json" && echo "Plugin found"

# 3. Get absolute path
PLUGIN_PATH="$(cd "$ORCH_PATH/claude-plugins/orch-toolset" && pwd)"

# 4. Add to settings (user level)
# Read ~/.claude/settings.json, add PLUGIN_PATH to plugins array, write back
```

## After Installation

Tell the user:
1. Restart Claude Code (or start a new session)
2. The orch-toolset skill will be automatically available
3. Ask questions like "How do I start a new run?" to verify it works
