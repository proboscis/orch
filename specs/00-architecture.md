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

1. Command-line flags (highest)
2. Environment variables (`ORCH_VAULT`, etc.)
3. `.orch/config.yaml` in current directory
4. `.orch/config.yaml` in parent directories
5. `~/.config/orch/config.yaml` (lowest)

### Log Files

All logs are in the **project root's** `.orch/` directory:
- `daemon.log` - Background daemon logs
- `monitor.log` - Monitor TUI logs

```bash
tail -f .orch/daemon.log
tail -f .orch/monitor.log
```
