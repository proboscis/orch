---
type: issue
id: orch-048
title: Create VSCode extension for orch run and issue management
status: resolved
---

# Create VSCode extension for orch run and issue management

## Summary

Create a VSCode extension that provides panels for monitoring and interacting with orch runs and issues directly from the editor.

## Core Use Case

1. User monitors runs and issues via VSCode sidebar panels
2. User writes issue files with agent assistance in editor
3. User starts runs from issues via panel click (with agent picker)
4. User attaches to running sessions by clicking runs (opens terminal with tmux session)

## Architecture

```
vscode-orch/
├── src/
│   ├── extension.ts           # Entry point
│   ├── providers/
│   │   ├── issuesProvider.ts  # TreeDataProvider for issues
│   │   └── runsProvider.ts    # TreeDataProvider for runs
│   ├── commands/
│   │   ├── startRun.ts        # With agent quick-pick
│   │   ├── stopRun.ts
│   │   ├── resolveRun.ts
│   │   ├── attachRun.ts       # Opens terminal + orch attach
│   │   └── continueRun.ts     # Branch/agent picker (uses orch-047)
│   ├── orch/
│   │   └── client.ts          # CLI wrapper around orch commands
│   └── config.ts              # Settings management
└── package.json
```

## Features

### Issues Panel (Sidebar TreeView)
- Display issues from `orch issue list`
- Show status icons (open/resolved)
- Configurable filters (show/hide resolved, etc.)
- Context menu actions:
  - Open issue file in editor
  - Start run (with agent quick-pick: claude, codex, gemini)
  - Continue run (branch picker + agent picker)

### Runs Panel (Sidebar TreeView)
- Display runs from `orch ps`
- Show status icons (running/blocked/completed)
- Configurable filters (by status)
- Context menu actions:
  - Attach to run (opens VSCode terminal with `orch attach <run-id>`)
  - Stop run
  - Resolve run

### Data Refresh
- Periodic auto-refresh (configurable interval)
- Manual refresh button
- Cache results to avoid excessive CLI calls

### Configuration Settings
- `orch.vaultPath`: Path to orch vault (auto-detect from workspace if not set)
- `orch.refreshInterval`: Auto-refresh interval in seconds
- `orch.issues.showResolved`: Show resolved issues (default: false)
- `orch.runs.statusFilter`: Filter runs by status

## Technical Requirements

### Prerequisites
- Add `--json` output support to `orch ps` and `orch issue list` commands (if not already available)

### Vault Detection
1. Look for `.orch/` directory in workspace root
2. Read vault path from `.orch/config.yaml`
3. Fall back to `orch.vaultPath` setting

### Terminal Management
- Create named terminals for each attached run
- Reuse existing terminal if already attached to same run
- Terminal naming: `orch: <issue-id>#<run-id>`

## Panel UX Mockup

```
ISSUES
├── 📋 orch-047 - Add continue run dialogue...
├── 📋 orch-046 - orch send command doesn't...
├── 📋 orch-045 - Add continue run feature...
└── ✅ orch-044 - Widen issue ID column... (resolved)

RUNS
├── 🟢 orch-047#202512... (running)
├── 🟡 orch-046#7b46f6 (blocked)
├── 🟡 orch-043#3789c6 (blocked)
└── ✅ orch-042#abc123 (completed)
```

## Acceptance Criteria

- [ ] Issues panel displays issues with status icons
- [ ] Runs panel displays runs with status icons
- [ ] Click issue to open in editor
- [ ] Right-click issue → Start Run with agent picker
- [ ] Click run to attach terminal
- [ ] Right-click run → Stop/Resolve
- [ ] Configurable refresh interval
- [ ] Configurable filters for issues and runs
- [ ] Auto-detect vault from workspace

## Future Enhancements (Out of Scope)
- Status bar indicator showing active runs count
- Notifications when runs complete or get blocked
- Inline decorations in issue files showing run status
- Multi-root workspace support with multiple vaults
