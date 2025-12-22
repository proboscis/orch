---
type: issue
id: orch-041
title: Add filter dialog to toggle resolved issues visibility in orch issue list
status: resolved
---

# Add filter dialog to toggle resolved issues visibility in orch issue list

## Description

Add a filter option in orch monitor's issue panel to toggle the visibility of resolved issues. Users should be able to easily show/hide resolved issues to focus on active work.

## Current Behavior

All issues (open and resolved) are shown in the issue list without filtering options.

## Desired Behavior

- Add a filter settings dialog accessible from the issues panel
- Allow toggling visibility of resolved issues (show/hide)
- Persist filter settings during the session
- Show visual indicator of active filters

## Possible Implementation

- Add a keybinding (e.g., 'f' for filter) to open filter dialog
- Filter dialog with checkboxes or toggles for:
  - Show resolved issues (on/off)
  - Potentially other status filters in the future
- Display current filter state in the panel header

## Acceptance Criteria

- [ ] Filter dialog accessible via keybinding from issues panel
- [ ] Toggle to show/hide resolved issues
- [ ] Filter state reflected in displayed issue list
- [ ] Visual indicator showing when filters are active
