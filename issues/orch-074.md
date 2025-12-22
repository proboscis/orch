---
type: issue
id: orch-074
title: Fix missing MERGED status in 'orch ps' (showing all '-')
status: open
---

# Fix missing MERGED status in 'orch ps' (showing all '-')

In some repositories (e.g., manga/placement), the 'MERGED' column in 'orch ps' output shows '-' for all runs, even when merge status should be available. This indicates a failure in detecting or reporting the branch merge status.

Symptoms:
- 'orch ps' output shows '-' in the MERGED column for all entries.

Possible causes:
- Git command failure when checking merge status.
- Incorrect base branch detection.
- Issue with how merge status is derived in the state model.
