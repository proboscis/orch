---
type: issue
id: orch-037
title: Fix 'c' keybinding in monitor to open control agent chat pane
status: resolved
---

# Fix 'c' keybinding in monitor to open control agent chat pane

## Problem

When pressing 'c' in orch monitor or orch issues panel, it should open the main control agent chat pane. However, it currently opens some other run's chat pane instead.

## Expected Behavior

Pressing 'c' should navigate to and focus the control agent's chat pane.

## Actual Behavior

Pressing 'c' opens a different run's chat pane.

## Location

This likely affects the keybinding handler in the monitor TUI code.
