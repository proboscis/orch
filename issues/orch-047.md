---
type: issue
id: orch-047
title: Add continue run dialogue to monitor issue dashboard
status: resolved
---

# Add continue run dialogue to monitor issue dashboard

## Summary

Add an interactive CLI dialogue in orch monitor's issue dashboard that allows users to continue a run from an existing branch.

## Requirements

When a user selects an issue in the monitor dashboard, provide an option to continue a run with the following interactive flow:

1. **Branch Selection**: Show a list of available branches related to the issue and let the user select which branch to continue from
2. **Agent Selection**: Present a list of available agents (claude, codex, gemini, etc.) for the user to choose
3. **Follow-up Prompt (Optional)**: Allow the user to enter an optional follow-up prompt to guide the continued run

## Context

- The `orch continue` command already exists for programmatic continuation
- This dialogue should integrate with the existing monitor keybindings
- Should be accessible from the issue panel in the dashboard

## Acceptance Criteria

- [ ] Add keybinding in issue dashboard to trigger continue dialogue
- [ ] Implement branch selection list (filtered to branches related to the selected issue)
- [ ] Implement agent selection list
- [ ] Implement optional prompt input
- [ ] Execute the continue command with selected options
- [ ] Handle cancellation gracefully at any step
