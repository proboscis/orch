---
type: issue
id: orch-003
title: Add filters to orch issue list
status: open
priority: medium
---

# Add filters to orch issue list

Implement filtering options for `orch issue list` as specified in `specs/03-commands.md`.

## Options to Add

- `--status <status>` - Filter by issue status (open/closed/etc)
- `--with-runs` - Include detailed run information

## Current State

Basic issue list is implemented with:
- ID, STATUS, TITLE, RUNS columns
- Active run count summary

## Remaining Work

- Add `--status` flag to filter issues by frontmatter status
- Add `--with-runs` flag for verbose run details
