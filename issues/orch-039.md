---
type: issue
id: orch-039
title: Make orch monitor respect project-scoped config
status: resolved
---

# Make orch monitor respect project-scoped config

## Problem

When running `orch monitor` in a project directory that has its own orch config file, the monitor shows issues/runs from all projects instead of only the ones belonging to the current project.

## Expected Behavior

- `orch monitor` should respect the project's orch config file
- Each project should have its own monitor window
- The monitor should only display issues and runs that belong to the current project

## Current Behavior

The monitor shows all issues/runs globally, ignoring the project context.

## Acceptance Criteria

- [ ] Monitor filters issues/runs based on the project's vault/config
- [ ] Running `orch monitor` in different projects shows project-specific views
- [ ] Each project's monitor operates independently
