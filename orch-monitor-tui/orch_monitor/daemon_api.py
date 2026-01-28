"""DaemonOrchAPI implementation using ProtoDaemonClient (Hy version).

The Hy proto_client returns Result types instead of raising exceptions,
enabling more explicit error handling throughout the API layer.
"""

import logging
import subprocess
import threading
from datetime import datetime
from pathlib import Path
from typing import Optional

import hy  # noqa: F401 - Enable Hy imports

from returns.result import Failure, Result, Success

# Import client from Hy module (returns Result types)
from .proto_client import ProtoDaemonClient

# Import types/exceptions from types module
from .types import (
    IssueFilters as ProtoIssueFilters,
    ListIssuesResponse as ProtoListIssuesResponse,
    ListRunsResponse as ProtoListRunsResponse,
    ProtoDaemonError,
    ProtoDaemonNotRunningError,
    RunFilters as ProtoRunFilters,
)
from .models import Issue as ModelIssue
from .models import IssueStatus as ModelIssueStatus
from .models import Run as ModelRun
from .models import Status as ModelStatus
from .orch_api import (
    AttachInfo,
    BranchState,
    ControlAgentLaunch,
    DiffStats,
    Issue,
    IssueFilters,
    IssueStatus,
    ListIssuesResponse,
    ListRunsResponse,
    NotFoundError,
    OrchError,
    Run,
    RunFilters,
    RunStatus,
    SessionCapture,
    StartRunResult,
)
from .orch_api import DaemonNotRunningError as ApiDaemonNotRunningError

_logger = logging.getLogger("orch_monitor.daemon_api")


def _log_and_map_error(operation: str, err: Exception) -> Failure:
    """Log error with full context and map to appropriate API error type."""
    import traceback

    err_type = type(err).__name__
    _logger.error(f"{operation}: {err_type}: {err}\n{traceback.format_exc()}")

    if isinstance(err, ProtoDaemonNotRunningError):
        return Failure(ApiDaemonNotRunningError("Daemon not running"))
    if isinstance(err, ProtoDaemonError):
        return Failure(OrchError(str(err)))
    return Failure(OrchError(f"{err_type}: {err}"))


def _map_daemon_error(err: ProtoDaemonError) -> Failure:
    """Map ProtoDaemonError to appropriate API error type (legacy, use _log_and_map_error)."""
    if isinstance(err, ProtoDaemonNotRunningError):
        return Failure(ApiDaemonNotRunningError("Daemon not running"))
    return Failure(OrchError(str(err)))


def _model_status_to_api(status: ModelStatus) -> RunStatus:
    """Convert models.Status to orch_api.RunStatus."""
    mapping = {
        ModelStatus.QUEUED: RunStatus.QUEUED,
        ModelStatus.BOOTING: RunStatus.BOOTING,
        ModelStatus.RUNNING: RunStatus.RUNNING,
        ModelStatus.BLOCKED: RunStatus.BLOCKED,
        ModelStatus.BLOCKED_API: RunStatus.BLOCKED_API,
        ModelStatus.PR_OPEN: RunStatus.PR_OPEN,
        ModelStatus.DONE: RunStatus.DONE,
        ModelStatus.FAILED: RunStatus.FAILED,
        ModelStatus.CANCELED: RunStatus.CANCELED,
        ModelStatus.UNKNOWN: RunStatus.UNKNOWN,
    }
    return mapping.get(status, RunStatus.UNKNOWN)


def _model_issue_status_to_api(status: ModelIssueStatus) -> IssueStatus:
    """Convert models.IssueStatus to orch_api.IssueStatus."""
    mapping = {
        ModelIssueStatus.OPEN: IssueStatus.OPEN,
        ModelIssueStatus.RESOLVED: IssueStatus.RESOLVED,
        ModelIssueStatus.CLOSED: IssueStatus.CLOSED,
    }
    return mapping.get(status, IssueStatus.OPEN)


def _api_run_status_to_model(status: RunStatus) -> ModelStatus:
    mapping = {
        RunStatus.QUEUED: ModelStatus.QUEUED,
        RunStatus.BOOTING: ModelStatus.BOOTING,
        RunStatus.RUNNING: ModelStatus.RUNNING,
        RunStatus.BLOCKED: ModelStatus.BLOCKED,
        RunStatus.BLOCKED_API: ModelStatus.BLOCKED_API,
        RunStatus.PR_OPEN: ModelStatus.PR_OPEN,
        RunStatus.DONE: ModelStatus.DONE,
        RunStatus.FAILED: ModelStatus.FAILED,
        RunStatus.CANCELED: ModelStatus.CANCELED,
        RunStatus.UNKNOWN: ModelStatus.UNKNOWN,
    }
    return mapping.get(status, ModelStatus.UNKNOWN)


def _api_issue_status_to_model(status: IssueStatus) -> ModelIssueStatus:
    mapping = {
        IssueStatus.OPEN: ModelIssueStatus.OPEN,
        IssueStatus.RESOLVED: ModelIssueStatus.RESOLVED,
        IssueStatus.CLOSED: ModelIssueStatus.CLOSED,
    }
    return mapping.get(status, ModelIssueStatus.OPEN)


def _model_run_to_api(run: ModelRun) -> Run:
    """Convert models.Run to orch_api.Run."""
    return Run(
        issue_id=run.issue_id,
        run_id=run.run_id,
        status=_model_status_to_api(run.status),
        agent=run.agent,
        model=run.model,
        branch=run.branch,
        worktree_path=run.worktree_path,
        pr_url=run.pr_url,
        started_at=run.started_at,
        updated_at=run.updated_at,
        tmux_session=run.tmux_session,
        multiplexer=run.multiplexer,
        server_port=run.server_port,
        opencode_session_id=run.opencode_session_id,
        diff_stats=DiffStats(
            additions=run.additions,
            deletions=run.deletions,
            files_changed=0,
            file_list=[],
        )
        if run.additions > 0 or run.deletions > 0
        else None,
    )


def _model_issue_to_api(issue: ModelIssue) -> Issue:
    """Convert models.Issue to orch_api.Issue."""
    return Issue(
        id=issue.id,
        title=issue.title,
        summary=issue.summary,
        status=_model_issue_status_to_api(issue.status),
        tags=issue.tags,
        body=issue.body,
        path=issue.path,
        modified_at=issue.modified_at,
    )


class MonitorHeartbeat:
    """Handles periodic heartbeat for monitor registration."""

    def __init__(
        self, client: ProtoDaemonClient, project: str, view: str = "dashboard"
    ):
        self._client = client
        self._project = project
        self._view = view
        self._monitor_id: Optional[str] = None
        self._tmux_session: str = ""
        self._heartbeat_thread: Optional[threading.Thread] = None
        self._stop_event = threading.Event()

    def start(self, tmux_session: str = "") -> Optional[str]:
        import os

        if not self._client.is_available():
            return None

        self._tmux_session = tmux_session
        result = self._client.register_monitor(
            pid=os.getpid(),
            monitor_type="python",
            view=self._view,
            project=self._project,
            tmux_session=tmux_session,
        )
        if isinstance(result, Success):
            self._monitor_id = result.unwrap()

        if self._monitor_id:
            self._start_heartbeat_thread()

        return self._monitor_id

    def stop(self) -> None:
        self._stop_heartbeat_thread()

        if self._monitor_id and self._client.is_available():
            self._client.unregister_monitor(self._monitor_id)
            self._monitor_id = None

    def _start_heartbeat_thread(self) -> None:
        if self._heartbeat_thread is not None:
            return

        self._stop_event.clear()
        self._heartbeat_thread = threading.Thread(
            target=self._heartbeat_loop, daemon=True
        )
        self._heartbeat_thread.start()

    def _stop_heartbeat_thread(self) -> None:
        if self._heartbeat_thread is None:
            return

        self._stop_event.set()
        self._heartbeat_thread.join(timeout=2.0)
        self._heartbeat_thread = None

    def _heartbeat_loop(self) -> None:
        import os

        while not self._stop_event.wait(timeout=30.0):
            if self._monitor_id and self._client.is_available():
                result = self._client.monitor_heartbeat(self._monitor_id)
                success = isinstance(result, Success) and result.unwrap()
                if not success:
                    reg_result = self._client.register_monitor(
                        pid=os.getpid(),
                        monitor_type="python",
                        view=self._view,
                        project=self._project,
                        tmux_session=self._tmux_session,
                    )
                    if isinstance(reg_result, Success):
                        new_id = reg_result.unwrap()
                        if new_id:
                            self._monitor_id = new_id


class DaemonOrchAPI:
    """OrchAPI implementation that wraps DaemonClient with subprocess fallbacks."""

    def __init__(
        self,
        socket_path: Optional[Path] = None,
        issues_root: Optional[Path] = None,
        project_root: Optional[Path] = None,
        base_branch: str = "main",
    ):
        if socket_path is None:
            from .config import Config

            config = Config.load()
            socket_path = config.socket_path
            if issues_root is None:
                issues_root = config.issues_root
            if project_root is None:
                project_root = config.project_root

        self._socket_path = socket_path
        self._issues_root = issues_root
        self._project_root = project_root or Path.cwd()
        self._base_branch = base_branch
        self._daemon = ProtoDaemonClient(socket_path, issues_root, self._project_root)
        self._monitor_heartbeat: Optional[MonitorHeartbeat] = None

    def _build_orch_cmd(self) -> list[str]:
        """Build base orch command with project/issues root args."""
        cmd = ["orch"]
        if self._project_root:
            cmd.extend(["--project-root", str(self._project_root)])
        if self._issues_root:
            cmd.extend(["--issues-root", str(self._issues_root)])
        return cmd

    # === Lifecycle ===

    def is_available(self) -> bool:
        return self._daemon.is_available()

    def ensure_running(self) -> Result[bool, OrchError]:
        if self._daemon.is_available():
            return Success(True)

        try:
            cmd = self._build_orch_cmd() + ["daemon", "start"]
            result = subprocess.run(cmd, capture_output=True, text=True, timeout=10)
            if result.returncode != 0:
                return Failure(
                    OrchError(f"Failed to start daemon: {result.stderr.strip()}")
                )

            import time

            for _ in range(75):  # 15 seconds
                time.sleep(0.2)
                if self._daemon.is_available():
                    return Success(True)

            return Failure(OrchError("Daemon did not become available within timeout"))
        except subprocess.TimeoutExpired:
            return Failure(OrchError("Daemon start command timed out"))
        except FileNotFoundError:
            return Failure(OrchError("'orch' command not found"))
        except Exception as e:
            return Failure(OrchError(str(e)))

    # === Runs ===

    def list_runs(
        self, filters: Optional[RunFilters] = None
    ) -> Result[ListRunsResponse, OrchError]:
        try:
            daemon_filters = None
            if filters:
                status_list = [_api_run_status_to_model(s) for s in filters.status]
                daemon_filters = ProtoRunFilters(
                    issue_id=filters.issue_id,
                    status=status_list,
                    agent=filters.agent,
                    text_search=filters.text_search,
                    time_range=filters.time_range,
                )
            result = self._daemon.list_runs(daemon_filters)
            if isinstance(result, Failure):
                return _log_and_map_error("list_runs", result.failure())
            response: ProtoListRunsResponse = result.unwrap()
            runs = [_model_run_to_api(r) for r in response.runs]
            return Success(ListRunsResponse(runs=runs, total=response.total))
        except Exception as e:
            return _log_and_map_error("list_runs", e)

    def get_run(self, issue_id: str, run_id: str) -> Result[Run, OrchError]:
        result = self._daemon.get_run(issue_id, run_id)
        if isinstance(result, Failure):
            return _map_daemon_error(result.failure())
        run = result.unwrap()
        if run is None:
            return Failure(NotFoundError(f"Run {issue_id}#{run_id} not found"))
        return Success(_model_run_to_api(run))

    def start_run(
        self,
        issue_id: str,
        agent: Optional[str] = None,
        model: Optional[str] = None,
    ) -> Result[StartRunResult, OrchError]:
        result = self._daemon.start_run(issue_id, agent or "", model or "")
        if isinstance(result, Failure):
            return _map_daemon_error(result.failure())
        data = result.unwrap()
        return Success(
            StartRunResult(
                run_id=data.get("run_id", ""),
                branch=data.get("branch", ""),
                worktree_path=data.get("worktree", ""),
            )
        )

    def stop_run(self, issue_id: str, run_id: str) -> Result[None, OrchError]:
        result = self._daemon.stop_run(issue_id, run_id)
        if isinstance(result, Failure):
            return _map_daemon_error(result.failure())
        return Success(None)

    def resolve_run(self, issue_id: str, run_id: str) -> Result[None, OrchError]:
        result = self._daemon.resolve_issue(issue_id)
        if isinstance(result, Failure):
            return _map_daemon_error(result.failure())
        success = result.unwrap()
        if not success:
            return Failure(OrchError("Failed to resolve issue"))
        return Success(None)

    # === Issues ===

    def list_issues(
        self, filters: Optional[IssueFilters] = None
    ) -> Result[ListIssuesResponse, OrchError]:
        try:
            daemon_filters = None
            if filters:
                status_list = [_api_issue_status_to_model(s) for s in filters.status]
                daemon_filters = ProtoIssueFilters(
                    status=status_list,
                    tags=filters.tags,
                    tags_mode=filters.tags_mode,
                    text_search=filters.text_search,
                )
            result = self._daemon.list_issues(daemon_filters)
            if isinstance(result, Failure):
                return _log_and_map_error("list_issues", result.failure())
            response: ProtoListIssuesResponse = result.unwrap()
            issues = [_model_issue_to_api(i) for i in response.issues]
            return Success(ListIssuesResponse(issues=issues, total=response.total))
        except Exception as e:
            return _log_and_map_error("list_issues", e)

    def get_issue(self, issue_id: str) -> Result[Issue, OrchError]:
        result = self._daemon.get_issue(issue_id)
        if isinstance(result, Failure):
            return _map_daemon_error(result.failure())
        issue = result.unwrap()
        if issue is None:
            return Failure(NotFoundError(f"Issue {issue_id} not found"))
        return Success(_model_issue_to_api(issue))

    def create_issue(
        self,
        issue_id: str,
        title: str,
        body: str,
    ) -> Result[None, OrchError]:
        result = self._daemon.create_issue(issue_id, title, body)
        if isinstance(result, Failure):
            return _map_daemon_error(result.failure())
        path = result.unwrap()
        if path is None:
            return Failure(OrchError("Failed to create issue"))
        return Success(None)

    def close_issue(self, issue_id: str) -> Result[None, OrchError]:
        result = self._daemon.close_issue(issue_id)
        if isinstance(result, Failure):
            return _map_daemon_error(result.failure())
        return Success(None)

    # === Control Agent ===

    def get_control_agent_launch(
        self,
        project_root: str,
        agent: str = "",
        new_session: bool = False,
    ) -> Result[ControlAgentLaunch, OrchError]:
        result = self._daemon.get_control_agent_launch(project_root, agent, new_session)
        if isinstance(result, Failure):
            return _map_daemon_error(result.failure())
        ok, command, prompt_file, port, session_id, resolved_agent, error = (
            result.unwrap()
        )
        if not ok:
            return Failure(OrchError(error or "Failed to get control agent launch"))
        return Success(
            ControlAgentLaunch(
                command=command or "",
                prompt_file=prompt_file or "",
                port=port,
                session_id=session_id,
            )
        )

    # === Sessions ===

    def get_attach_info(
        self, issue_id: str, run_id: str
    ) -> Result[AttachInfo, OrchError]:
        # Get run info and build attach command
        run_result = self.get_run(issue_id, run_id)
        if isinstance(run_result, Failure):
            return run_result

        run = run_result.unwrap()
        attach_cmd = self._build_orch_cmd() + ["attach", run.ref()]

        return Success(
            AttachInfo(
                command=attach_cmd,
                multiplexer=run.multiplexer,
                session_name=run.tmux_session,
                worktree_path=run.worktree_path,
            )
        )

    def capture_session(
        self, issue_id: str, run_id: str
    ) -> Result[SessionCapture, OrchError]:
        result = self._daemon.capture_session(issue_id, run_id)
        if isinstance(result, Failure):
            return _map_daemon_error(result.failure())
        data = result.unwrap()
        if data is None:
            return Failure(OrchError("Failed to capture session"))
        content, timestamp_unix, source = data
        return Success(
            SessionCapture(
                content=content,
                timestamp=datetime.fromtimestamp(timestamp_unix)
                if timestamp_unix
                else datetime.now(),
                source=source or "tmux",
            )
        )

    def send_message(
        self,
        issue_id: str,
        run_id: str,
        message: str,
    ) -> Result[None, OrchError]:
        result = self._daemon.send_message(issue_id, run_id, message)
        if isinstance(result, Failure):
            return _map_daemon_error(result.failure())
        return Success(None)

    # === Git ===

    def get_diff_stats(
        self, issue_id: str, run_id: str
    ) -> Result[DiffStats, OrchError]:
        result = self._daemon.get_diff_stats(issue_id, run_id)
        if isinstance(result, Failure):
            return _map_daemon_error(result.failure())
        data = result.unwrap()
        if data is None:
            return Failure(OrchError("Failed to get diff stats"))
        additions, deletions, files_changed, files = data
        return Success(
            DiffStats(
                additions=additions,
                deletions=deletions,
                files_changed=files_changed,
                file_list=files,
            )
        )

    def get_branch_state(
        self, issue_id: str, run_id: str
    ) -> Result[BranchState, OrchError]:
        result = self._daemon.get_branch_state(issue_id, run_id)
        if isinstance(result, Failure):
            return _map_daemon_error(result.failure())
        state_str = result.unwrap()
        if not state_str:
            return Failure(OrchError("Failed to get branch state"))
        try:
            return Success(BranchState(state_str))
        except ValueError:
            return Success(BranchState.UNKNOWN)

    def get_diff(self, issue_id: str, run_id: str) -> Result[str, OrchError]:
        result = self._daemon.get_diff(issue_id, run_id)
        if isinstance(result, Failure):
            return _map_daemon_error(result.failure())
        diff = result.unwrap()
        if diff is None:
            return Failure(OrchError("Failed to get diff"))
        return Success(diff)

    # === Monitor ===

    def register_monitor(
        self,
        pid: int,
        monitor_type: str,
        view: str,
        project: str,
        session_name: str = "",
    ) -> Result[str, OrchError]:
        result = self._daemon.register_monitor(
            pid=pid,
            monitor_type=monitor_type,
            view=view,
            project=project,
            tmux_session=session_name,
        )
        if isinstance(result, Failure):
            return _map_daemon_error(result.failure())
        monitor_id = result.unwrap()
        if monitor_id is None:
            return Failure(OrchError("Failed to register monitor"))

        self._monitor_heartbeat = MonitorHeartbeat(self._daemon, project, view)
        self._monitor_heartbeat.start(session_name)

        return Success(monitor_id)

    def unregister_monitor(self, monitor_id: str) -> Result[None, OrchError]:
        if self._monitor_heartbeat:
            self._monitor_heartbeat.stop()
            self._monitor_heartbeat = None

        result = self._daemon.unregister_monitor(monitor_id)
        if isinstance(result, Failure):
            return _map_daemon_error(result.failure())
        success = result.unwrap()
        if not success:
            return Failure(OrchError("Failed to unregister monitor"))
        return Success(None)

    def heartbeat(self, monitor_id: str) -> Result[None, OrchError]:
        result = self._daemon.monitor_heartbeat(monitor_id)
        if isinstance(result, Failure):
            return _map_daemon_error(result.failure())
        success = result.unwrap()
        if not success:
            return Failure(OrchError("Heartbeat failed"))
        return Success(None)
