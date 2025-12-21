---
type: issue
id: orch-010
title: Show issue summary in orch ps output
summary: Add summary column to ps for quick issue context
status: resolved
priority: high
---

# Show issue summary in orch ps output

Add a `summary` frontmatter field to issues and display it in `orch ps` so users can quickly understand what each run is about.

## Frontmatter Field

```yaml
---
type: issue
id: orch-010
title: Show issue summary in orch ps output
summary: Add summary column to ps for quick issue context
status: open
---
```

The `summary` field should be:
- One line, ~50 chars max
- Displayed in `orch ps` output
- Optional (fall back to truncated title if missing)

## Updated orch ps Output

```
ID      ISSUE     STATUS   UPDATED  SUMMARY
3f68c8  orch-008  running  5m ago   Add issue status column to ps
f94c3e  orch-009  running  3m ago   Show relative time like "5m ago"
101abd  orch-007  blocked  15m ago  Instruct agent to create PR
```

## Implementation

1. Add `Summary` field to `model.Issue` struct
2. Parse `summary` from frontmatter in `parseIssueFile()`
3. Update `orch ps` to fetch and display issue summary
4. Truncate long summaries with `...` if needed (e.g., 40 char limit)

## Also Update

- `orch issue list` - show summary column
- `orch issue create` - add `--summary` flag
- Issue template in `runIssueCreate()` - include summary field
