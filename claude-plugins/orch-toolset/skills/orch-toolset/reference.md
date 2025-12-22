# Orch Command Reference

## Run references

- `ISSUE_ID#RUN_ID` targets a specific run.
- `ISSUE_ID` alone targets the latest run for that issue (where supported).
- Short IDs (2-6 hex chars) can be used in many commands (e.g. `orch capture a3b4c5`).

## Global flags

- `--vault <path>` (or `ORCH_VAULT`): path to the vault.
- `--backend file|github|linear`: backend selection (file is default).
- `--json`: machine-readable output.
- `--tsv`: TSV output (useful for fzf).
- `--quiet`: suppress human output.
- `--log-level error|warn|info|debug`.

## Issue management

### Create an issue

```bash
orch issue create orch-055 --title "Create Claude Code plugin" --summary "Claude Code skill plugin" --body "Details..."
orch issue create orch-055 --edit
```

Flags:
- `--title`, `--summary`, `--body`, `--edit`

### List issues

```bash
orch issue list
orch issue list --json
```

### Open issue or run docs

```bash
orch open orch-055
orch open orch-055#20251222-173250 --app editor
orch open a3b4c5 --print-path
```

Flags:
- `--app obsidian|editor|default`
- `--print-path`

## Run management

### Start a run

```bash
orch run orch-055
orch run orch-055 --agent codex
orch run orch-055 --reuse
orch run orch-055 --branch "issue/orch-055/run-custom" --worktree-root .git-worktrees
```

Useful flags:
- `--agent claude|codex|gemini|custom`
- `--agent-cmd <cmd>` (when using `--agent=custom`)
- `--profile <profile>`
- `--run-id <id>`, `--branch <name>`, `--base-branch <name>`
- `--worktree-root <dir>`, `--repo-root <dir>`
- `--tmux` or `--no-tmux`, `--tmux-session <name>`
- `--dry-run`

### List runs

```bash
orch ps
orch ps --status running,blocked
orch ps --issue orch-055 --all
```

Useful flags:
- `--status`, `--issue-status`, `--issue`, `--limit`, `--sort`, `--since`, `--absolute-time`, `--all`

### Stop runs

```bash
orch stop orch-055
orch stop orch-055#20251222-173250
orch stop --all
```

Flags:
- `--all`, `--force`

### Resolve an issue

```bash
orch resolve orch-055
orch resolve orch-055 --force
```

Note: `orch resolve` updates the issue status, not the run status.

### Inspect a run

```bash
orch show orch-055#20251222-173250
orch show a3b4c5
```

## Monitoring

### Interactive monitor

```bash
orch monitor
orch monitor --issue orch-055
orch monitor --status running,blocked --attach
```

### Attach to a run session

```bash
orch attach orch-055#20251222-173250
```

### Capture output

```bash
orch capture orch-055#20251222-173250 --lines 200
orch capture a3b4c5 --json
```

## Agent communication

### Send a message to an agent

```bash
orch send orch-055#20251222-173250 "Please focus on tests first"
orch send a3b4c5 "partial input" --no-enter
```

Combine `orch capture` + `orch send` for programmatic coordination.
