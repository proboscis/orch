---
name: install
description: Install the orch-toolset plugin to Claude Code settings
allowed-tools: Read, Write, Edit, Bash, Glob
---

# Install Orch-Toolset Plugin

Install this plugin to the user's Claude Code settings so it's available in all sessions.

## Instructions

1. **Find the orch repository path**:
   - Check if we're currently in the orch repo by looking for `claude-plugins/orch-toolset`
   - Search common locations: `~/repos/orch`, `~/orch`, or use `which orch` to find it
   - Ask the user if the path cannot be determined

2. **Get the absolute plugin path**:
   ```bash
   PLUGIN_PATH="$(cd /path/to/orch/claude-plugins/orch-toolset && pwd)"
   ```

3. **Check user's Claude Code settings** at `~/.claude/settings.json`:
   - If file doesn't exist, create it with the plugin
   - If file exists, read it and check if plugin is already installed
   - If not installed, add to the `plugins` array

4. **Update the settings file**:
   - Preserve all existing settings
   - Add the absolute plugin path to `plugins` array
   - Handle case where `plugins` key doesn't exist

5. **Report success** and tell user to restart Claude Code

## Settings File Format

```json
{
  "plugins": [
    "/absolute/path/to/orch/claude-plugins/orch-toolset"
  ]
}
```

## Error Handling

- If orch repo not found: Ask user for the path
- If plugin already installed: Inform user, no changes needed
- If settings file has invalid JSON: Report error, don't overwrite

## Execute Now

Find the orch repository and install the plugin to `~/.claude/settings.json`.
