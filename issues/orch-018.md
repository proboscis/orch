---
type: issue
id: orch-018
title: Add topic field to issue for short summary in ps
summary: Add a dedicated 'topic' frontmatter field for a short summary in orch ps
status: open
priority: medium
---

# Add topic field to issue for short summary in ps

The `summary` field in issue frontmatter is often too long for the `orch ps` table view. We need a dedicated field for a short, 5-word max summary.

## Requirements

1.  **New Field**: Add support for a `topic` field in the issue frontmatter.
2.  **Model Update**: Update `Issue` struct to include `Topic`.
3.  **Parsing**: Update file store to parse `topic` from frontmatter.
4.  **Display**: Update `orch ps` to display `topic` instead of `summary` in the list view.
    *   If `topic` is missing, fall back to `summary` (truncated).
    *   If `topic` is present, display it (potentially truncated to ~5 words or ~30 chars if user provides a long one).

## Implementation Plan

1.  **Update Model (`internal/model/issue.go`)**:
    *   Add `Topic string` to `Issue` struct.

2.  **Update Store (`internal/store/file/file.go`)**:
    *   In `parseIssueFile`, read `topic` from frontmatter map.
    *   `issue.Topic = frontmatter["topic"]`

3.  **Update CLI (`internal/cli/ps.go`)**:
    *   In `outputTable`, prefer `issue.Topic` over `issue.Summary`.
    *   Example logic:
        ```go
        displaySummary := issue.Topic
        if displaySummary == "" {
            displaySummary = issue.Summary
            // truncate summary...
        }
        // potentially also truncate topic if it's abused
        ```

## Example Frontmatter

```yaml
---
type: issue
id: orch-018
title: Add topic field to issue for short summary in ps
summary: Add a dedicated 'topic' frontmatter field for a short summary in orch ps
topic: Short topic field
status: open
---
```
