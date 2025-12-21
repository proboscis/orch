---
type: issue
id: orch-004
title: Create orch ui command with fzf integration
status: resolved
priority: low
---

# Create orch ui command with fzf integration

Build an interactive UI using fzf for browsing and managing runs.

## Features

- `orch ui` - Interactive run browser
- `orch ui issues` - Issue picker
- `orch ui runs` - Run picker with preview

## Implementation

Use `orch ps --tsv` output with fzf:
```bash
orch ps --tsv | fzf --preview 'orch show {1}#{2}'
```

## Keybindings

- Enter: Attach to run
- Ctrl-S: Stop run
- Ctrl-O: Open in editor
- Ctrl-R: Refresh

## References

- Spec: `specs/03-commands.md` (TSV output format)
- Spec: `specs/00-overview.md` (fzf support section)
