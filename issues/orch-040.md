---
type: issue
id: orch-040
title: Add agent kind selection when starting run from monitor issues panel
status: resolved
---

# Add agent kind selection when starting run from monitor issues panel

## Description

When using the 'start run' feature (likely 'r' keybinding) from the orch issues panel in orch monitor, add the ability to select which agent kind to use for the run.

## Current Behavior

Starting a run from the issues panel uses a default agent without prompting for selection.

## Desired Behavior

- When starting a run from the issues panel, display a selection UI for available agent kinds
- Show options like: claude, codex, gemini, etc.
- Allow user to select the desired agent before the run starts

## Acceptance Criteria

- [ ] Agent selection UI appears when starting a run from issues panel
- [ ] All configured/available agents are listed as options
- [ ] Selected agent is used for the new run
