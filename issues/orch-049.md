---
type: issue
id: orch-049
title: "orch issue create ignores project VAULT path"
status: resolved
---

# orch issue create ignores project VAULT path

## Problem

`orch issue create` creates issues in `~/repos/orch/issues/` instead of the project's configured VAULT directory.

For example, when running from `/Users/s22625/repos/manga/placement` which has:

```
Vault: /Users/s22625/repos/manga/placement/VAULT/Issues
```

The command still creates the issue at `/Users/s22625/repos/orch/issues/ISSUE-PLC-142.md` instead of the project's VAULT.

## Expected Behavior

`orch issue create` should respect the project's VAULT path and create issues in `<project>/VAULT/Issues/`.

## Actual Behavior

Issues are always created in the orch repo's own `issues/` directory regardless of the current working directory or project configuration.

## Reproduction

```bash
cd ~/repos/manga/placement
orch issue create ISSUE-PLC-142 --title "Test" --body "Test body"
# Creates at ~/repos/orch/issues/ISSUE-PLC-142.md
# Should create at ~/repos/manga/placement/VAULT/Issues/ISSUE-PLC-142.md
```
