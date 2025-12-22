---
type: issue
id: orch-023
title: Integrated TUI for runs, issues, and agent chat control
summary: Combine run/issue dashboards and an agent chat panel into a single interactive UI
status: resolved
priority: high
---

# Integrated TUI for runs, issues, and agent chat control

We want a more integrated terminal UI that replaces ad-hoc commands with a cohesive, multi-panel experience.

## Goals

1. **orch ps dashboard**
   * Live run list with filters, status counts, and quick actions.
   * Ability to jump into run sessions, stop runs, answer questions, and refresh.

2. **orch issue dashboard**
   * List issues with summary/status, show details, and allow quick actions (open, start run, create issue).
   * Provide run context per issue (latest run status, count of active runs).

3. **Agent chat panel**
   * Open a dedicated agent window (claude/codex/gemini) for creating issues or driving `orch` commands via chat.
   * The chat should have enough context to execute common workflows (create issue, start run, resolve run, etc.).

## Requirements

1. **Unified layout**
   * Single tmux session with consistent navigation.
   * Dedicated windows or panes for: runs dashboard, issues dashboard, agent chat.

2. **Navigation**
   * Keyboard shortcuts to switch between dashboards and agent chat.
   * Consistent footer with key hints.

3. **Live updates**
   * Dashboards update automatically (polling or daemon notifications).
   * Visual indicator when data is stale or syncing.

4. **Agent integration**
   * The agent can run `orch` commands or generate issue drafts.
   * Provide templates or presets for issue creation and run management.

## Implementation Sketch

1. **Dashboards**
   * Extend the existing monitor dashboard into a multi-screen TUI.
   * Add a shared model for run/issue data and selection.

2. **Issue view**
   * Reuse `orch issue list` data and map it to a table view.
   * Add a details pane for the selected issue.

3. **Agent chat window**
   * Launch agent in a tmux window with a pre-filled prompt for orchestrating tasks.
   * Add a minimal protocol for the agent to request `orch` commands.

## Success Criteria

* From the UI, a user can:
  * View active runs and jump into them.
  * Browse issues and start a new run.
  * Create or modify issues via the agent chat.
* The dashboards stay reasonably fresh without manual refresh.
