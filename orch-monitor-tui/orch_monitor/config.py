"""Configuration management for orch monitor."""

import os
from dataclasses import dataclass
from pathlib import Path
from typing import Optional

import yaml


# Daemon socket filename (matches Go daemon)
DAEMON_SOCKET_FILE = "daemon.sock"
ORCH_DIR = ".orch"


@dataclass
class Config:
    """Orch configuration."""

    vault_path: Path
    agent: str = "claude"
    worktree_dir: str = ".git-worktrees"
    base_branch: str = "main"
    pr_target_branch: str = "main"

    @property
    def orch_dir(self) -> Path:
        """Return the .orch directory path."""
        return self.vault_path / ORCH_DIR

    @property
    def socket_path(self) -> Path:
        """Return the daemon socket path."""
        return self.orch_dir / DAEMON_SOCKET_FILE

    @classmethod
    def load(cls, config_path: Optional[Path] = None) -> "Config":
        """Load configuration from .orch/config.yaml or environment."""
        vault_path = os.getenv("ORCH_VAULT")

        if config_path and config_path.exists():
            with open(config_path) as f:
                data = yaml.safe_load(f) or {}

            if not vault_path:
                vault_path = data.get("vault", ".")

            return cls(
                vault_path=Path(vault_path).expanduser().resolve(),
                agent=data.get("agent", "claude"),
                worktree_dir=data.get("worktree_dir", ".git-worktrees"),
                base_branch=data.get("base_branch", "main"),
                pr_target_branch=data.get("pr_target_branch", "main"),
            )

        if not vault_path:
            vault_path = "."

        return cls(vault_path=Path(vault_path).expanduser().resolve())

    @classmethod
    def from_vault(cls, vault_path: Path) -> "Config":
        """Load configuration from a specific vault path."""
        config_file = vault_path / ".orch" / "config.yaml"
        config = cls.load(config_file)
        config.vault_path = vault_path
        return config
