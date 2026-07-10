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
from .client_bootstrap import load_client_bootstrap
from .proto_client import ProtoDaemonClient

# Import types/exceptions from types module
from .types import (
    IssueFilters as ProtoIssueFilters,
    ListIssuesResponse as ProtoListIssuesResponse,
    ListRunsResponse as ProtoListRunsResponse,
    ProtoDaemonConnectionRefusedError,
    ProtoDaemonError,
    ProtoDaemonNotRunningError,
    ProtoDaemonPermissionError,
    ProtoDaemonSocketMissingError,
    ProtoDaemonTimeoutError,
    RunFilters as ProtoRunFilters,
)
from .models import Issue as ModelIssue
from .models import IssueStatus as ModelIssueStatus
from .models import Run as ModelRun
from .models import Status as ModelStatus
from .orch_api import (
    AttachInfo,
    BranchState,
    ControlAgentConfig,
    ControlAgentLaunch,
    DiffStats,
    Issue,
    IssueFilters,
    IssueStatus,
    ListIssuesResponse,
    ListRunsResponse,
    MonitorInfo,
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


def _map_proto_error(err: Exception) -> Failure:
    """Map typed proto errors to API-level errors while preserving cause text."""
    if isinstance(err, ProtoDaemonSocketMissingError):
        return Failure(ApiDaemonNotRunningError(str(err)))
    if isinstance(err, ProtoDaemonConnectionRefusedError):
        return Failure(ApiDaemonNotRunningError(str(err)))
    if isinstance(err, ProtoDaemonNotRunningError):
        return Failure(ApiDaemonNotRunningError(str(err) or "Daemon not running"))
    if isinstance(err, ProtoDaemonTimeoutError):
        return Failure(OrchError(str(err)))
    if isinstance(err, ProtoDaemonPermissionError):
        return Failure(OrchError(str(err)))
    if isinstance(err, ProtoDaemonError):
        return Failure(OrchError(str(err)))
    err_type = type(err).__name__
    return Failure(OrchError(f"{err_type}: {err}"))


def _log_and_map_error(operation: str, err: Exception) -> Failure:
    """Log error with full context and map to appropriate API error type."""
    import traceback

    err_type = type(err).__name__
    _logger.error(f"{operation}: {err_type}: {err}\n{traceback.format_exc()}")
    return _map_proto_error(err)


def _map_daemon_error(err: ProtoDaemonError) -> Failure:
    """Map ProtoDaemonError to appropriate API error type (legacy, use _log_and_map_error)."""
    return _map_proto_error(err)


def _model_status_to_api(status: ModelStatus) -> RunStatus:
    value = getattr(status, "value", str(status))
    try:
        return RunStatus(value)
    except ValueError:
        return RunStatus.UNKNOWN


def _model_issue_status_to_api(status: ModelIssueStatus) -> IssueStatus:
    value = getattr(status, "value", str(status))
    try:
        return IssueStatus(value)
    except ValueError:
        return IssueStatus.OPEN


def _model_run_to_api(run: ModelRun) -> Run:
    """Convert models.Run to orch_api.Run."""
    return Run(
        issue_id=run.issue_id,
        run_id=run.run_id,
        status=_model_status_to_api(run.status),
        agent=run.agent,
        model=run.model,
        branch=run.branch,
        branch_state=run.branch_state,
        worktree_path=run.worktree_path,
        pr_url=run.pr_url,
        started_at=run.started_at,
        updated_at=run.updated_at,
        elapsed_seconds=run.elapsed_seconds,
        elapsed_display=run.elapsed_display,
        session_name=run.session_name,
        multiplexer=run.multiplexer,
        server_port=run.server_port,
        opencode_session_id=run.opencode_session_id,
        diff_stats=DiffStats(
            additions=run.additions,
            deletions=run.deletions,
            files_changed=run.files_changed,
            file_list=run.files,
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
        self._pid: Optional[int] = None
        self._monitor_id: Optional[str] = None
        self._session_name: str = ""
        self._heartbeat_thread: Optional[threading.Thread] = None
        self._stop_event = threading.Event()

    def start(
        self,
        session_name: str = "",
        pid: Optional[int] = None,
        monitor_id: Optional[str] = None,
    ) -> Optional[str]:
        import os

        if monitor_id is None and not self._client.is_available():
            return None

        self._pid = pid if pid is not None else os.getpid()
        self._session_name = session_name
        self._monitor_id = monitor_id
        if self._monitor_id is None:
            result = self._client.register_monitor(
                pid=self._pid,
                monitor_type="python",
                view=self._view,
                project=self._project,
                session_name=session_name,
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
                        pid=self._pid if self._pid is not None else os.getpid(),
                        monitor_type="python",
                        view=self._view,
                        project=self._project,
                        session_name=self._session_name,
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
        project_root: Optional[Path] = None,
        base_branch: str = "main",
    ):
        if socket_path is None:
            from .config import Config

            config = Config.load()
            socket_path = config.socket_path
            if project_root is None:
                project_root = config.project_root

        bootstrap = load_client_bootstrap()

        self._socket_path = socket_path
        self._project_root = project_root or bootstrap.project_root or Path.cwd()
        self._remote_addr = bootstrap.remote_addr
        self._project_scope = bootstrap.project_id
        self._project_id = bootstrap.project_id or None
        self._monitor_session_name = bootstrap.monitor_session_name
        self._base_branch = base_branch
        self._daemon = ProtoDaemonClient(
            socket_path,
            self._project_root,
            self._remote_addr,
            project_id=self._project_id,
        )
        self._monitor_heartbeat: Optional[MonitorHeartbeat] = None

    def _build_orch_cmd(self) -> list[str]:
        """Build base orch command with project scope args."""
        cmd = ["orch"]
        if self._project_scope:
            cmd.extend(["--project", self._project_scope])
        return cmd

    # === Lifecycle ===

    def is_available(self) -> bool:
        return self._daemon.is_available()

    def ensure_running(self) -> Result[bool, OrchError]:
        if self._daemon.is_available():
            return Success(True)

        if self._remote_addr:
            # A remote master cannot be started locally; just report unreachable.
            return Failure(
                OrchError(
                    f"Remote orch daemon not reachable at {self._remote_addr} "
                    "(set via --remote/ORCH_REMOTE/client.yaml remote.default)"
                )
            )

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
                daemon_filters = ProtoRunFilters(
                    issue_id=filters.issue_id,
                    status=[getattr(s, "value", str(s)) for s in filters.status],
                    agent=filters.agent,
                    agents=list(filters.agents or []),
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
                daemon_filters = ProtoIssueFilters(
                    status=[getattr(s, "value", str(s)) for s in filters.status],
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
        launch = result.unwrap()
        return Success(
            ControlAgentLaunch(
                command=launch.command or "",
                prompt_file=launch.prompt_file or "",
                port=launch.port,
                session_id=launch.session_id,
                resumed=launch.resumed,
            )
        )

    def get_control_agent_config(
        self,
        project_root: str,
    ) -> Result[ControlAgentConfig, OrchError]:
        result = self._daemon.get_control_agent_config(project_root)
        if isinstance(result, Failure):
            return _map_daemon_error(result.failure())
        cfg = result.unwrap()
        return Success(
            ControlAgentConfig(
                prompt_content=cfg.prompt_content or "",
                agent=cfg.agent or "",
                model=cfg.model or "",
                model_variant=cfg.model_variant or "",
                extra_args=list(cfg.extra_args or []),
                codex_home=getattr(cfg, "codex_home", "") or "",
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
                session_name=run.session_name,
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
        project_scope = self._project_scope or project
        resolved_session_name = session_name or self._monitor_session_name
        result = self._daemon.register_monitor(
            pid=pid,
            monitor_type=monitor_type,
            view=view,
            project=project_scope,
            session_name=resolved_session_name,
        )
        if isinstance(result, Failure):
            return _map_daemon_error(result.failure())
        monitor_id = result.unwrap()
        if monitor_id is None:
            return Failure(OrchError("Failed to register monitor"))

        self._monitor_heartbeat = MonitorHeartbeat(
            self._daemon, project_scope, view
        )
        self._monitor_heartbeat.start(resolved_session_name, pid, monitor_id)

        return Success(monitor_id)

    def unregister_monitor(self, monitor_id: str) -> Result[None, OrchError]:
        if self._monitor_heartbeat:
            self._monitor_heartbeat.stop()
            self._monitor_heartbeat = None
            return Success(None)

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

    def list_monitors(
        self, project: str, list_all: bool = False
    ) -> Result[list[MonitorInfo], OrchError]:
        result = self._daemon.list_monitors(project, list_all)
        if isinstance(result, Failure):
            return _map_daemon_error(result.failure())
        monitors = [
            MonitorInfo(
                id=monitor.id,
                pid=monitor.pid,
                project=monitor.project,
                view=monitor.view,
                session_name=monitor.session_name,
                last_seen_unix=monitor.last_heartbeat_unix,
            )
            for monitor in result.unwrap()
        ]
        return Success(monitors)

    def kill_monitor(
        self, monitor_id: str, kill_all: bool, project: str
    ) -> Result[int, OrchError]:
        result = self._daemon.kill_monitor(monitor_id, kill_all, project)
        if isinstance(result, Failure):
            return _map_daemon_error(result.failure())
        return Success(result.unwrap())
