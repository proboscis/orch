# Architecture

## Key Concepts

### Project Root vs Vault

| Concept | Description | Location |
|---------|-------------|----------|
| **Project Root** | The git repository where orch is configured | Contains `.orch/config.yaml` |
| **Vault** | Obsidian vault for local markdown issues (optional) | Configured via `vault:` in config or `ORCH_VAULT` env |

### Issue Backends

| Backend | Vault Required | Issue Source |
|---------|----------------|--------------|
| **File-based** | Yes | Local `.md` files in `vault/issues/` |
| **GitHub** | No | GitHub Issues API (`gh#123` format) |

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

**Vault (only for file-based issues):**
```
vault/
├── issues/
│   └── <ISSUE_ID>.md    # Issue specification
└── runs/
    └── <ISSUE_ID>/
        └── <RUN_ID>.md  # Run log with events
```

### GitHub Issues Backend

When using GitHub as the issue backend:
- Issues are identified by `gh#<number>` (e.g., `gh#123`)
- No local vault directory needed
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

**For project root:**
1. `--project-root` flag (highest)
2. `ORCH_PROJECT_ROOT` environment variable
3. Directory containing `.orch/config.yaml` (searched upward from cwd)
4. `ORCH_VAULT` (backward compatibility fallback)

**For vault path (file-based issues only):**
1. `--vault` flag (highest)
2. `vault:` in `.orch/config.yaml`
3. `ORCH_VAULT` environment variable
4. `~/.config/orch/config.yaml` (lowest)

### Log Files

All logs are in the **project root's** `.orch/` directory:
- `daemon.log` - Background daemon logs
- `monitor.log` - Monitor TUI logs

```bash
tail -f .orch/daemon.log
tail -f .orch/monitor.log
```
