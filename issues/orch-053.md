---
type: issue
id: orch-053
title: Fix Obsidian vault URL in orch monitor open feature
status: open
---

# Fix Obsidian vault URL in orch monitor open feature

## Problem

When using the [o] open feature in orch monitor, Obsidian fails to open the file with the following error:

```
Unable to find a vault for the URL obsidian://open?vault=orch&file=issues/orch-028.md
```

## Cause

The Obsidian URL is using a hardcoded or incorrect vault name ('orch') that doesn't match the user's actual Obsidian vault configuration.

## Expected Behavior

The [o] open feature should either:
1. Use the correct vault name from configuration
2. Use a file path approach instead of vault name
3. Allow users to configure their Obsidian vault name

## Steps to Reproduce

1. Run `orch monitor`
2. Select an issue
3. Press [o] to open in Obsidian
4. Observe the error message
