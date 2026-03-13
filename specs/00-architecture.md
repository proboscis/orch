# Architecture

## Key Concepts

### Project Root vs Issues Path

| Concept | Description | Location |
|---------|-------------|----------|
| **Project Root** | The git repository where orch is configured | Contains `.orch/config.yaml` |
| **Issues Path** | Directory for local markdown issues (optional) | Configured via `issues.path` in `.orch/config.yaml` |

### Issue Backends

| Backend | Vault Required | Issue Source |
|---------|----------------|--------------|
| **File-based** | Yes | Local `.md` files in `issues.path/issues/` |
| **GitHub** | No | GitHub Issues API (`gh-123` format) |

### Directory Structure

**Project root (always present):**
```
project/
└── .orch/
    ├── config.yaml      # Configuration
    ├── daemon.sock      # Daemon socket
    ├── daemon.log       # Daemon logs
    └── monitor.log      # Monitor TUI logs
```

**Issues path (only for file-based issues):**
```
issues-path/
├── issues/
│   └── <ISSUE_ID>.md    # Issue specification
└── runs/
    └── <ISSUE_ID>/
        └── <RUN_ID>.md  # Run log with events
```

### GitHub Issues Backend

When using GitHub as the issue backend:
- Issues are identified by `gh-<number>` (e.g., `gh-123`)
- No local issues directory needed
- Issue content fetched from GitHub API
- Runs stored in project root's `.orch/runs/`
- Configuration:

```yaml
# .orch/config.yaml
github:
  owner: your-org
  repo: your-repo
  label_filter: orch  # Optional: only sync issues with this label
```

### Configuration Precedence

**For project identity:**
1. `--project` flag (highest)
2. `ORCH_PROJECT` environment variable
3. Git remote-derived `project_id` for the repo discovered from cwd / `.orch/config.yaml`
4. none

Note: upward `.orch/config.yaml` discovery and cwd lookup determine the
operational project root. They do not define project identity by themselves.
Identity must come from explicit `--project` / `ORCH_PROJECT` input or from the
repo's remote metadata.

**For issues path (file-based issues only):**
1. `issues.path` in project `.orch/config.yaml` (highest)
2. `issues.path` in `~/.config/orch/config.yaml` (lowest)

### Log Files

All logs are in the **project root's** `.orch/` directory:
- `daemon.log` - Background daemon logs
- `monitor.log` - Monitor TUI logs

```bash
tail -f .orch/daemon.log
tail -f .orch/monitor.log
```
