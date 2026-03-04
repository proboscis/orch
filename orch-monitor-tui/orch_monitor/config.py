"""Configuration management for orch monitor."""

import os
import subprocess
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


def _looks_like_local_path(value: str) -> bool:
    return (
        value.startswith("/")
        or value.startswith("./")
        or value.startswith("../")
        or value.startswith("~")
        or os.sep in value
    )


def resolve_project_root_hint(project_value: Optional[str]) -> Optional[Path]:
    """Resolve a local project root hint from explicit project input.

    The new primary project selector is identity-based (ORCH_PROJECT/--project).
    For local workflows, we still accept path-like values as hints.
    """
    if not project_value:
        return None

    value = project_value.strip()
    if not value:
        return None
    if value.startswith("repoid:"):
        return None
    if not _looks_like_local_path(value):
        return None

    candidate = Path(value).expanduser()
    if not candidate.is_absolute():
        candidate = (Path.cwd() / candidate).resolve()
    else:
        candidate = candidate.resolve()

    if candidate.is_dir():
        return candidate
    return None


def _repo_url_from_git_remote(project_root: Path) -> Optional[str]:
    try:
        result = subprocess.run(
            ["git", "-C", str(project_root), "config", "--get", "remote.origin.url"],
            capture_output=True,
            text=True,
            timeout=5,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired):
        return None

    if result.returncode != 0:
        return None
    repo_url = result.stdout.strip()
    if not repo_url:
        return None
    return repo_url


def resolve_project_identity(
    project_root: Optional[Path] = None,
    explicit_project: Optional[str] = None,
) -> Optional[str]:
    """Resolve identity value for orch `--project`/`ORCH_PROJECT` scope."""

    value = (explicit_project or "").strip()
    if not value:
        value = os.getenv("ORCH_PROJECT", "").strip()

    if value:
        if value.startswith("repoid:"):
            value = value[len("repoid:") :].strip()

        path_hint = resolve_project_root_hint(value)
        if path_hint is not None:
            return _repo_url_from_git_remote(path_hint)

        return value

    if project_root is None:
        return None

    return _repo_url_from_git_remote(project_root)


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
    "waiting",
    "rate_limited",
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
    orch_dir_path: Optional[Path]  # Where .orch was found
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

    return ConfigurationState(
        has_orch_dir=has_orch_dir,
        has_config_file=has_config_file,
        orch_dir_path=orch_dir_path,
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
    """

    project_root: Path
    project: str = ""
    agent: str = "claude"
    control_agent: Optional[str] = None
    control_model: Optional[str] = None
    control_model_variant: Optional[str] = None
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
    def _get_project_root(cls, config_path: Path) -> Path:
        config_dir = config_path.parent
        if config_dir.name == ORCH_DIR:
            return config_dir.parent
        return config_dir

    @classmethod
    def load(cls, config_path: Optional[Path] = None) -> "Config":
        data: dict = {}
        project_root: Optional[Path] = None
        env_project = os.getenv("ORCH_PROJECT")

        hinted_root = resolve_project_root_hint(env_project)
        if hinted_root and (hinted_root / ORCH_DIR).is_dir():
            project_root = hinted_root
            config_file = hinted_root / ORCH_DIR / "config.yaml"
            if config_file.exists():
                with open(config_file) as f:
                    data = yaml.safe_load(f) or {}

        if config_path and config_path.exists():
            with open(config_path) as f:
                data = yaml.safe_load(f) or {}
            if project_root is None:
                project_root = cls._get_project_root(config_path)

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

        if project_root is None:
            project_root = Path(".").resolve()

        return cls(
            project_root=project_root,
            project=resolve_project_identity(
                project_root=project_root, explicit_project=env_project
            )
            or "",
            agent=data.get("agent", "claude"),
            control_agent=data.get("control_agent"),
            control_model=data.get("control_model"),
            control_model_variant=data.get("control_model_variant"),
            worktree_dir=data.get("worktree_dir", ".git-worktrees"),
            base_branch=data.get("base_branch", "main"),
            pr_target_branch=data.get("pr_target_branch", "main"),
            monitor=cls._parse_monitor_config(data.get("monitor", {})),
        )

    @classmethod
    def from_project_root(cls, project_root: Path) -> "Config":
        config_file = project_root / ORCH_DIR / "config.yaml"
        config = cls.load(config_file)
        if not config.project:
            config.project = resolve_project_identity(project_root=project_root) or ""
        return config
