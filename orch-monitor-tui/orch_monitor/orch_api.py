"""Orch API Port - contract between TUI and backend."""

from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum
from pathlib import Path
from typing import Optional, Protocol, runtime_checkable

from returns.result import Result


class OrchError(Exception):
    pass


class NotFoundError(OrchError):
    pass


class DaemonNotRunningError(OrchError):
    pass


class RunStatus(str, Enum):
    QUEUED = "queued"
    BOOTING = "booting"
    RUNNING = "running"
    WAITING = "waiting"
    RATE_LIMITED = "rate_limited"
    PR_OPEN = "pr_open"
    DONE = "done"
    FAILED = "failed"
    CANCELED = "canceled"
    UNKNOWN = "unknown"


class IssueStatus(str, Enum):
    OPEN = "open"
    RESOLVED = "resolved"
    CLOSED = "closed"


class BranchState(str, Enum):
    CLEAN = "clean"
    DIRTY = "dirty"
    MERGED = "merged"
    CONFLICT = "conflict"
    AHEAD = "ahead"
    BEHIND = "behind"
    DIVERGED = "diverged"
    SYNCED = "synced"
    UNKNOWN = "unknown"


@dataclass
class DiffStats:
    additions: int = 0
    deletions: int = 0
    files_changed: int = 0
    file_list: list[str] = field(default_factory=list)


@dataclass
class Run:
    issue_id: str
    run_id: str
    status: RunStatus
    agent: str = ""
    model: str = ""
    branch: str = ""
    worktree_path: str = ""
    pr_url: str = ""
    started_at: Optional[datetime] = None
    updated_at: Optional[datetime] = None
    elapsed_seconds: Optional[int] = None
    elapsed_display: str = ""
    diff_stats: Optional[DiffStats] = None
    branch_state: str = ""
    session_name: str = ""
    multiplexer: str = ""
    server_port: int = 0
    opencode_session_id: str = ""

    def ref(self) -> str:
        return f"{self.issue_id}#{self.run_id}"

    def short_id(self) -> str:
        import hashlib

        return hashlib.sha256(self.ref().encode()).hexdigest()[:6]


@dataclass
class Issue:
    id: str
    title: str = ""
    summary: str = ""
    status: IssueStatus = IssueStatus.OPEN
    tags: list[str] = field(default_factory=list)
    body: str = ""
    path: Path = field(default_factory=Path)
    modified_at: Optional[datetime] = None


@dataclass
class RunFilters:
    issue_id: Optional[str] = None
    status: list[RunStatus] = field(default_factory=list)
    agent: Optional[str] = None
    text_search: Optional[str] = None
    time_range: Optional[str] = None


@dataclass
class IssueFilters:
    status: list[IssueStatus] = field(default_factory=list)
    tags: list[str] = field(default_factory=list)
    tags_mode: str = "or"
    text_search: Optional[str] = None


@dataclass
class ListRunsResponse:
    runs: list[Run]
    total: int


@dataclass
class ListIssuesResponse:
    issues: list[Issue]
    total: int


@dataclass
class ControlAgentLaunch:
    command: str
    prompt_file: str
    port: int = 0
    session_id: Optional[str] = None
    resumed: bool = False


@dataclass
class ControlAgentConfig:
    prompt_content: str
    agent: str
    model: str
    model_variant: str
    extra_args: list[str]


@dataclass
class AttachInfo:
    command: list[str]
    multiplexer: str
    session_name: str
    worktree_path: str


@dataclass
class SessionCapture:
    content: str
    timestamp: datetime
    source: str


@dataclass
class StartRunResult:
    run_id: str
    branch: str
    worktree_path: str


@runtime_checkable
class OrchAPI(Protocol):
    def is_available(self) -> bool: ...
    def ensure_running(self) -> Result[bool, OrchError]: ...

    def list_runs(
        self, filters: Optional[RunFilters] = None
    ) -> Result[ListRunsResponse, OrchError]: ...
    def get_run(self, issue_id: str, run_id: str) -> Result[Run, OrchError]: ...
    def start_run(
        self,
        issue_id: str,
        agent: Optional[str] = None,
        model: Optional[str] = None,
    ) -> Result[StartRunResult, OrchError]: ...
    def stop_run(self, issue_id: str, run_id: str) -> Result[None, OrchError]: ...
    def resolve_run(self, issue_id: str, run_id: str) -> Result[None, OrchError]: ...

    def list_issues(
        self, filters: Optional[IssueFilters] = None
    ) -> Result[ListIssuesResponse, OrchError]: ...
    def get_issue(self, issue_id: str) -> Result[Issue, OrchError]: ...
    def create_issue(
        self,
        issue_id: str,
        title: str,
        body: str,
    ) -> Result[None, OrchError]: ...
    def close_issue(self, issue_id: str) -> Result[None, OrchError]: ...

    def get_control_agent_launch(
        self,
        project_root: str,
        agent: str = "",
        new_session: bool = False,
    ) -> Result[ControlAgentLaunch, OrchError]: ...

    def get_control_agent_config(
        self,
        project_root: str,
    ) -> Result[ControlAgentConfig, OrchError]: ...

    def get_attach_info(
        self, issue_id: str, run_id: str
    ) -> Result[AttachInfo, OrchError]: ...
    def capture_session(
        self, issue_id: str, run_id: str
    ) -> Result[SessionCapture, OrchError]: ...
    def send_message(
        self,
        issue_id: str,
        run_id: str,
        message: str,
    ) -> Result[None, OrchError]: ...

    def get_diff_stats(
        self, issue_id: str, run_id: str
    ) -> Result[DiffStats, OrchError]: ...
    def get_branch_state(
        self, issue_id: str, run_id: str
    ) -> Result[BranchState, OrchError]: ...
    def get_diff(self, issue_id: str, run_id: str) -> Result[str, OrchError]: ...

    def register_monitor(
        self,
        pid: int,
        monitor_type: str,
        view: str,
        project: str,
        session_name: str = "",
    ) -> Result[str, OrchError]: ...
    def unregister_monitor(self, monitor_id: str) -> Result[None, OrchError]: ...
    def heartbeat(self, monitor_id: str) -> Result[None, OrchError]: ...


def create_orch_api(
    socket_path: Optional[Path] = None,
    issues_root: Optional[Path] = None,
    project_root: Optional[Path] = None,
    base_branch: str = "main",
    fallback_to_cli: bool = True,
) -> OrchAPI:
    from .daemon_api import DaemonOrchAPI

    api = DaemonOrchAPI(
        socket_path=socket_path,
        issues_root=issues_root,
        project_root=project_root,
        base_branch=base_branch,
    )

    if api.is_available():
        return api

    if fallback_to_cli:
        pass

    return api
