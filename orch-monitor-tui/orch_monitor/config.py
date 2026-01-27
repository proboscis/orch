"""Configuration management for orch monitor."""

import os
from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path
from typing import Optional

import yaml

from . import xdg


# Monitor-specific files (stored in project's .orch/ directory)
MONITOR_FILTERS_FILE = "monitor-filters.yaml"
MONITOR_LOG_FILE = "monitor-tui.log"
ORCH_DIR = ".orch"


def _log_config_error(operation: str, error: str, orch_dir: Optional[Path]) -> None:
    if orch_dir is None:
        return
    log_path = orch_dir / MONITOR_LOG_FILE
    try:
        log_path.parent.mkdir(parents=True, exist_ok=True)
        timestamp = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
        with open(log_path, "a") as f:
            f.write(f"{timestamp} [config:{operation}] {error}\n")
    except OSError:
        pass


DEFAULT_RUN_STATUSES = [
    "queued",
    "booting",
    "running",
    "blocked",
    "blocked_api",
    "pr_open",
    "failed",
]


@dataclass
class RunFilterState:
    """Persisted run filter state."""

    statuses: list[str] = field(default_factory=lambda: DEFAULT_RUN_STATUSES.copy())
    agents: list[str] = field(default_factory=list)
    text_search: str = ""
    time_range: str = "all"


@dataclass
class IssueFilterState:
    """Persisted issue filter state."""

    statuses: list[str] = field(default_factory=list)
    priorities: list[str] = field(default_factory=list)
    tags: list[str] = field(default_factory=list)
    tag_mode: str = "any"  # "all" (AND) or "any" (OR)
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
                "tags": self.issue_filters.tags,
                "tag_mode": self.issue_filters.tag_mode,
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
                tags=issue_data.get("tags", []),
                tag_mode=issue_data.get("tag_mode", "any"),
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
        if self.issue_filters.tags:
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
class ConfigurationState:
    """State of orch configuration for onboarding detection."""

    has_orch_dir: bool  # .orch directory exists
    has_config_file: bool  # .orch/config.yaml exists
    has_issues_path: bool  # issues path configured (env or config)
    orch_dir_path: Optional[Path]  # Where .orch was found
    issues_path: Optional[Path]  # Configured issues path
    project_root: Path  # Current working directory


def detect_configuration_state() -> ConfigurationState:
    """Detect current orch configuration state for onboarding."""
    cwd = Path.cwd()

    # Search upward for .orch directory
    orch_dir_path = None
    current = cwd
    while True:
        candidate = current / ORCH_DIR
        if candidate.is_dir():
            orch_dir_path = candidate
            break
        parent = current.parent
        if parent == current:
            break
        current = parent

    has_orch_dir = orch_dir_path is not None
    has_config_file = has_orch_dir and (orch_dir_path / "config.yaml").exists()

    # Check for issues path from environment
    issues_path_str = os.getenv("ORCH_ISSUES_ROOT") or os.getenv("ORCH_VAULT")
    issues_path = Path(issues_path_str).expanduser() if issues_path_str else None

    # Also check config file for issues.path
    if has_config_file and not issues_path and orch_dir_path:
        try:
            with open(orch_dir_path / "config.yaml") as f:
                data = yaml.safe_load(f) or {}
            path_val = data.get("issues", {}).get("path") or data.get("vault")
            if path_val:
                issues_path = Path(path_val).expanduser()
        except Exception as e:
            _log_config_error("load_issues_path", str(e), orch_dir_path)

    return ConfigurationState(
        has_orch_dir=has_orch_dir,
        has_config_file=has_config_file,
        has_issues_path=issues_path is not None,
        orch_dir_path=orch_dir_path,
        issues_path=issues_path,
        project_root=cwd,
    )


@dataclass
class IssueDefaultFilter:
    """Default filter configuration for issues."""

    tags: list[str] = field(default_factory=list)
    tag_mode: str = "any"


@dataclass
class MonitorConfig:
    """Monitor-specific configuration from config.yaml."""

    default_run_statuses: list[str] = field(
        default_factory=lambda: DEFAULT_RUN_STATUSES.copy()
    )
    default_issue_statuses: list[str] = field(default_factory=list)
    default_issue_filter: IssueDefaultFilter = field(default_factory=IssueDefaultFilter)


@dataclass
class Config:
    """Orch configuration.

    project_root: Directory containing .orch/ (always required)
    issues_root: Directory for file-based issues (optional, only for file backend)
    """

    project_root: Path
    issues_root: Optional[Path] = None
    agent: str = "claude"
    worktree_dir: str = ".git-worktrees"
    base_branch: str = "main"
    pr_target_branch: str = "main"
    monitor: MonitorConfig = field(default_factory=MonitorConfig)

    @property
    def orch_dir(self) -> Path:
        """Return the project's .orch directory (for project-specific files)."""
        return self.project_root / ORCH_DIR

    @property
    def socket_path(self) -> Path:
        """Return the global daemon socket path (XDG-compliant)."""
        return xdg.socket_path()

    @property
    def filters_path(self) -> Path:
        """Return the path to monitor filters file (project-specific)."""
        return self.orch_dir / MONITOR_FILTERS_FILE

    @property
    def log_path(self) -> Path:
        return xdg.state_dir() / "orch-monitor.log"

    def load_filters(self) -> FilterState:
        """Load persisted filter state, using config defaults if no saved state."""
        if not self.filters_path.exists():
            return FilterState(
                run_filters=RunFilterState(
                    statuses=self.monitor.default_run_statuses.copy()
                ),
                issue_filters=IssueFilterState(
                    statuses=self.monitor.default_issue_statuses.copy(),
                    tags=self.monitor.default_issue_filter.tags.copy(),
                    tag_mode=self.monitor.default_issue_filter.tag_mode,
                ),
            )
        try:
            with open(self.filters_path) as f:
                data = yaml.safe_load(f) or {}
            return FilterState.from_dict(data)
        except (yaml.YAMLError, OSError) as e:
            _log_config_error("load_filters", str(e), self.orch_dir)
            return FilterState(
                run_filters=RunFilterState(
                    statuses=self.monitor.default_run_statuses.copy()
                ),
                issue_filters=IssueFilterState(
                    statuses=self.monitor.default_issue_statuses.copy(),
                    tags=self.monitor.default_issue_filter.tags.copy(),
                    tag_mode=self.monitor.default_issue_filter.tag_mode,
                ),
            )

    def save_filters(self, filters: FilterState) -> None:
        try:
            self.orch_dir.mkdir(parents=True, exist_ok=True)
            with open(self.filters_path, "w") as f:
                yaml.safe_dump(filters.to_dict(), f, default_flow_style=False)
        except OSError as e:
            _log_config_error("save_filters", str(e), self.orch_dir)

    @classmethod
    def _parse_issue_default_filter(cls, data: dict) -> IssueDefaultFilter:
        """Parse issue default_filter from config."""
        if not data:
            return IssueDefaultFilter()
        return IssueDefaultFilter(
            tags=data.get("tags", []),
            tag_mode=data.get("tag_mode", "any"),
        )

    @classmethod
    def _parse_monitor_config(cls, data: dict) -> MonitorConfig:
        if not data:
            return MonitorConfig()
        return MonitorConfig(
            default_run_statuses=data.get(
                "default_run_statuses", DEFAULT_RUN_STATUSES.copy()
            ),
            default_issue_statuses=data.get("default_issue_statuses", []),
            default_issue_filter=cls._parse_issue_default_filter(
                data.get("default_issue_filter", {})
            ),
        )

    @classmethod
    def _find_repo_configs(cls) -> list[Path]:
        """Search upward from cwd for .orch/config.yaml files (matches Go CLI config.go)."""
        cwd = Path.cwd()
        paths: list[Path] = []

        current = cwd
        while True:
            config_path = current / ORCH_DIR / "config.yaml"
            if config_path.exists():
                paths.append(config_path)

            parent = current.parent
            if parent == current:
                break
            current = parent

        paths.reverse()  # furthest ancestor first, closest last (highest precedence)
        return paths

    @classmethod
    def _resolve_path_from_config(cls, path: str, base_dir: Path) -> Path:
        if not path:
            return Path(".")

        p = Path(path)
        if str(p).startswith("~"):
            p = p.expanduser()
        if not p.is_absolute():
            p = base_dir / p

        return p.resolve()

    @classmethod
    def _get_project_root(cls, config_path: Path) -> Path:
        config_dir = config_path.parent
        if config_dir.name == ORCH_DIR:
            return config_dir.parent
        return config_dir

    @classmethod
    def _get_issues_path_from_data(cls, data: dict, base_dir: Path) -> Optional[str]:
        issues_config = data.get("issues", {})
        if isinstance(issues_config, dict) and issues_config.get("path"):
            return str(cls._resolve_path_from_config(issues_config["path"], base_dir))
        if data.get("vault"):
            return str(cls._resolve_path_from_config(data["vault"], base_dir))
        return None

    @classmethod
    def load(cls, config_path: Optional[Path] = None) -> "Config":
        issues_root_str = os.getenv("ORCH_ISSUES_ROOT") or os.getenv("ORCH_VAULT")
        data: dict = {}
        project_root: Optional[Path] = None

        env_project_root = os.getenv("ORCH_PROJECT_ROOT")
        if env_project_root:
            candidate = Path(env_project_root).expanduser().resolve()
            if (candidate / ORCH_DIR).is_dir():
                project_root = candidate
                config_file = candidate / ORCH_DIR / "config.yaml"
                if config_file.exists():
                    with open(config_file) as f:
                        data = yaml.safe_load(f) or {}
                    if not issues_root_str:
                        issues_root_str = cls._get_issues_path_from_data(
                            data, candidate
                        )

        if config_path and config_path.exists():
            with open(config_path) as f:
                data = yaml.safe_load(f) or {}
            if project_root is None:
                project_root = cls._get_project_root(config_path)

            if not issues_root_str:
                issues_root_str = cls._get_issues_path_from_data(
                    data, cls._get_project_root(config_path)
                )

        elif project_root is None:
            repo_configs = cls._find_repo_configs()
            if repo_configs:
                project_root = cls._get_project_root(repo_configs[-1])

            for repo_config in repo_configs:
                with open(repo_config) as f:
                    file_data = yaml.safe_load(f) or {}

                for key, value in file_data.items():
                    if value:
                        data[key] = value

                if not issues_root_str:
                    base_dir = cls._get_project_root(repo_config)
                    issues_root_str = cls._get_issues_path_from_data(
                        file_data, base_dir
                    )

        if project_root is None:
            project_root = Path(".").resolve()

        env_issues_root = os.getenv("ORCH_ISSUES_ROOT") or os.getenv("ORCH_VAULT")
        if env_issues_root:
            issues_root_str = env_issues_root

        issues_root: Optional[Path] = None
        if issues_root_str:
            issues_root = Path(issues_root_str).expanduser().resolve()

        return cls(
            project_root=project_root,
            issues_root=issues_root,
            agent=data.get("agent", "claude"),
            worktree_dir=data.get("worktree_dir", ".git-worktrees"),
            base_branch=data.get("base_branch", "main"),
            pr_target_branch=data.get("pr_target_branch", "main"),
            monitor=cls._parse_monitor_config(data.get("monitor", {})),
        )

    @classmethod
    def from_project_root(cls, project_root: Path) -> "Config":
        config_file = project_root / ORCH_DIR / "config.yaml"
        return cls.load(config_file)

    @classmethod
    def from_issues_root(cls, issues_root: Path) -> "Config":
        config = cls.load()
        if config.issues_root is None:
            config.issues_root = issues_root
        return config

    @classmethod
    def from_vault(cls, vault_path: Path) -> "Config":
        return cls.from_issues_root(vault_path)
