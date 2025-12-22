---
type: issue
id: orch-044
title: Widen issue ID column in orch monitor dashboards
status: resolved
---

# Widen issue ID column in orch monitor dashboards

## Problem

The issue ID column in orch monitor's dashboards (both issues panel and runs panel) is too narrow to display longer issue IDs properly.

## Requirements

- Must support issue IDs up to the format: `ISSUE-PLC-0000` (14 characters)
- Both dashboards in orch monitor need this fix:
  - Issues panel
  - Runs panel

## Expected Behavior

Issue IDs should be fully visible without truncation in both dashboard panels.
