"""Configuration management for orch monitor."""

import os
from dataclasses import dataclass, field
from pathlib import Path
from typing import Optional

import yaml


# Daemon socket filename (matches Go daemon)
DAEMON_SOCKET_FILE = "daemon.sock"
MONITOR_FILTERS_FILE = "monitor-filters.yaml"
ORCH_DIR = ".orch"


@dataclass
class RunFilterState:
    """Persisted run filter state."""

    statuses: list[str] = field(default_factory=list)
    agents: list[str] = field(default_factory=list)
    text_search: str = ""
    time_range: str = "all"


@dataclass
class IssueFilterState:
    """Persisted issue filter state."""

    statuses: list[str] = field(default_factory=list)
    priorities: list[str] = field(default_factory=list)
    text_search: str = ""


@dataclass
class FilterState:
    """Combined filter state for persistence."""

    run_filters: RunFilterState = field(default_factory=RunFilterState)
    issue_filters: IssueFilterState = field(default_factory=IssueFilterState)

    def to_dict(self) -> dict:
        """Convert to dictionary for YAML serialization."""
        return {
            "run_filters": {
                "statuses": self.run_filters.statuses,
                "agents": self.run_filters.agents,
                "text_search": self.run_filters.text_search,
                "time_range": self.run_filters.time_range,
            },
            "issue_filters": {
                "statuses": self.issue_filters.statuses,
                "priorities": self.issue_filters.priorities,
                "text_search": self.issue_filters.text_search,
            },
        }

    @classmethod
    def from_dict(cls, data: dict) -> "FilterState":
        """Create from dictionary."""
        run_data = data.get("run_filters", {})
        issue_data = data.get("issue_filters", {})

        return cls(
            run_filters=RunFilterState(
                statuses=run_data.get("statuses", []),
                agents=run_data.get("agents", []),
                text_search=run_data.get("text_search", ""),
                time_range=run_data.get("time_range", "all"),
            ),
            issue_filters=IssueFilterState(
                statuses=issue_data.get("statuses", []),
                priorities=issue_data.get("priorities", []),
                text_search=issue_data.get("text_search", ""),
            ),
        )

    def run_filter_count(self) -> int:
        """Count active run filters."""
        count = 0
        if self.run_filters.statuses:
            count += 1
        if self.run_filters.agents:
            count += 1
        if self.run_filters.text_search:
            count += 1
        if self.run_filters.time_range != "all":
            count += 1
        return count

    def issue_filter_count(self) -> int:
        """Count active issue filters."""
        count = 0
        if self.issue_filters.statuses:
            count += 1
        if self.issue_filters.text_search:
            count += 1
        return count

    def clear_run_filters(self) -> None:
        """Clear all run filters."""
        self.run_filters = RunFilterState()

    def clear_issue_filters(self) -> None:
        """Clear all issue filters."""
        self.issue_filters = IssueFilterState()


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

    @property
    def filters_path(self) -> Path:
        """Return the monitor filters file path."""
        return self.orch_dir / MONITOR_FILTERS_FILE

    def load_filters(self) -> FilterState:
        """Load persisted filter state."""
        if not self.filters_path.exists():
            return FilterState()
        try:
            with open(self.filters_path) as f:
                data = yaml.safe_load(f) or {}
            return FilterState.from_dict(data)
        except (yaml.YAMLError, OSError):
            return FilterState()

    def save_filters(self, filters: FilterState) -> None:
        """Save filter state to file."""
        try:
            self.orch_dir.mkdir(parents=True, exist_ok=True)
            with open(self.filters_path, "w") as f:
                yaml.safe_dump(filters.to_dict(), f, default_flow_style=False)
        except OSError:
            pass

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
        vault_path = vault_path.expanduser().resolve()
        config_file = vault_path / ".orch" / "config.yaml"
        config = cls.load(config_file)
        config.vault_path = vault_path
        return config
