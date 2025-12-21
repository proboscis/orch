---
type: issue
id: github-backend
title: Add GitHub Issues backend
status: open
priority: low
---

# Add GitHub Issues backend

Implement a GitHub backend that uses GitHub Issues as the knowledge store.

## Requirements

- `--backend github` flag support
- Map GitHub Issues to orch Issues
- Store runs as issue comments or separate files
- Support GitHub API authentication

## Implementation Notes

- Use `gh` CLI or GitHub API directly
- Issue ID could be `owner/repo#123` format
- Runs stored as timestamped comments
- Events parsed from comment format

## References

- Spec: `specs/02-store.md` (future backends section)
