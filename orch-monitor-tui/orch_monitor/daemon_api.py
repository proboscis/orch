"""DaemonOrchAPI implementation using DaemonClient and subprocess fallbacks."""

import logging
import subprocess
import threading
from datetime import datetime
from functools import lru_cache
from pathlib import Path
from time import time as _time
from typing import Optional

from returns.result import Failure, Result, Success

from .daemon import DaemonClient, DaemonError, DaemonNotRunningError
from .daemon import ListIssuesResponse as DaemonListIssuesResponse
from .daemon import ListRunsResponse as DaemonListRunsResponse
from .daemon import RunFilters as DaemonRunFilters
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
    """Convert orch_api.RunStatus to models.Status."""
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


# TTL cache for git diff stats (30 second TTL)
_diff_stats_cache_time: dict[str, float] = {}
_DIFF_STATS_TTL = 30.0

# TTL cache for branch state
_branch_state_cache: dict[str, tuple[str, float]] = {}
_BRANCH_STATE_TTL = 30.0


@lru_cache(maxsize=256)
def _get_git_diff_stats_cached(
    worktree_path: str, branch: str, base_branch: str
) -> Optional[DiffStats]:
    """Internal cached implementation of git diff stats retrieval."""
    try:
        result = subprocess.run(
            ["git", "diff", "--numstat", f"{base_branch}...{branch}"],
            capture_output=True,
            text=True,
            timeout=5,
            cwd=worktree_path,
            encoding="utf-8",
            errors="replace",
        )

        if result.returncode != 0:
            result = subprocess.run(
                ["git", "diff", "--numstat", f"{base_branch}..{branch}"],
                capture_output=True,
                text=True,
                timeout=5,
                cwd=worktree_path,
                encoding="utf-8",
                errors="replace",
            )

        if result.returncode != 0:
            _logger.debug(
                f"git diff failed for {branch} vs {base_branch}: {result.stderr}"
            )
            return None

        file_list: list[str] = []
        total_additions = 0
        total_deletions = 0

        for line in result.stdout.strip().split("\n"):
            if not line:
                continue
            parts = line.split("\t")
            if len(parts) != 3:
                continue

            add_str, del_str, path = parts
            additions = int(add_str) if add_str != "-" else 0
            deletions = int(del_str) if del_str != "-" else 0

            file_list.append(path)
            total_additions += additions
            total_deletions += deletions

        return DiffStats(
            additions=total_additions,
            deletions=total_deletions,
            files_changed=len(file_list),
            file_list=file_list,
        )

    except (
        subprocess.TimeoutExpired,
        subprocess.SubprocessError,
        OSError,
        ValueError,
    ) as e:
        _logger.debug(f"git diff exception for {branch}: {e}")
        return None


def _compute_branch_state(
    worktree_path: str, branch: str, base_branch: str
) -> BranchState:
    """Compute the branch state by running git commands."""
    try:
        dirty_result = subprocess.run(
            ["git", "status", "--porcelain"],
            capture_output=True,
            text=True,
            timeout=5,
            cwd=worktree_path,
        )
        if dirty_result.returncode == 0 and dirty_result.stdout.strip():
            return BranchState.DIRTY

        merged_result = subprocess.run(
            ["git", "branch", "--merged", base_branch, "--format=%(refname:short)"],
            capture_output=True,
            text=True,
            timeout=5,
            cwd=worktree_path,
        )
        if merged_result.returncode == 0:
            merged_branches = set(merged_result.stdout.strip().split("\n"))
            if branch in merged_branches:
                return BranchState.MERGED

        conflict_result = subprocess.run(
            ["git", "merge-tree", "--write-tree", "--messages", branch, base_branch],
            capture_output=True,
            text=True,
            timeout=10,
            cwd=worktree_path,
        )
        if conflict_result.returncode != 0:
            output = conflict_result.stdout + conflict_result.stderr
            if "CONFLICT" in output or "<<<<<<<" in output:
                return BranchState.CONFLICT

        return BranchState.CLEAN
    except (subprocess.TimeoutExpired, subprocess.SubprocessError, OSError) as e:
        _logger.debug(f"git state check failed for {branch}: {e}")
        return BranchState.UNKNOWN


class MonitorHeartbeat:
    """Handles periodic heartbeat for monitor registration."""

    def __init__(self, client: DaemonClient, project: str, view: str = "dashboard"):
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
        self._monitor_id = self._client.register_monitor(
            pid=os.getpid(),
            monitor_type="python",
            view=self._view,
            project=self._project,
            tmux_session=tmux_session,
        )

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
                success = self._client.monitor_heartbeat(self._monitor_id)
                if not success:
                    new_id = self._client.register_monitor(
                        pid=os.getpid(),
                        monitor_type="python",
                        view=self._view,
                        project=self._project,
                        tmux_session=self._tmux_session,
                    )
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
        self._daemon = DaemonClient(socket_path, issues_root)
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
                daemon_filters = DaemonRunFilters(
                    issue_id=filters.issue_id, status=status_list
                )
            response: DaemonListRunsResponse = self._daemon.list_runs(daemon_filters)
            runs = [_model_run_to_api(r) for r in response.runs]
            return Success(ListRunsResponse(runs=runs, total=response.total))
        except DaemonNotRunningError:
            return Failure(ApiDaemonNotRunningError("Daemon not running"))
        except DaemonError as e:
            return Failure(OrchError(str(e)))

    def get_run(self, issue_id: str, run_id: str) -> Result[Run, OrchError]:
        try:
            run = self._daemon.get_run(issue_id, run_id)
            if run is None:
                return Failure(NotFoundError(f"Run {issue_id}#{run_id} not found"))
            return Success(_model_run_to_api(run))
        except DaemonNotRunningError:
            return Failure(ApiDaemonNotRunningError("Daemon not running"))
        except DaemonError as e:
            return Failure(OrchError(str(e)))

    def start_run(
        self,
        issue_id: str,
        agent: Optional[str] = None,
        model: Optional[str] = None,
    ) -> Result[StartRunResult, OrchError]:
        try:
            result = self._daemon.start_run(issue_id, agent or "", model or "")
            return Success(
                StartRunResult(
                    run_id=result.get("run_id", ""),
                    branch=result.get("branch", ""),
                    worktree_path=result.get("worktree", ""),
                )
            )
        except DaemonNotRunningError:
            return Failure(ApiDaemonNotRunningError("Daemon not running"))
        except DaemonError as e:
            return Failure(OrchError(str(e)))

    def stop_run(self, issue_id: str, run_id: str) -> Result[None, OrchError]:
        try:
            self._daemon.stop_run(issue_id, run_id)
            return Success(None)
        except DaemonNotRunningError:
            return Failure(ApiDaemonNotRunningError("Daemon not running"))
        except DaemonError as e:
            return Failure(OrchError(str(e)))

    def resolve_run(self, issue_id: str, run_id: str) -> Result[None, OrchError]:
        try:
            run_ref = f"{issue_id}#{run_id}" if run_id else issue_id
            cmd = self._build_orch_cmd() + ["resolve", run_ref]
            result = subprocess.run(cmd, capture_output=True, text=True, timeout=30)
            if result.returncode != 0:
                error_msg = (
                    result.stderr.strip() or result.stdout.strip() or "Unknown error"
                )
                return Failure(OrchError(f"Failed to resolve run: {error_msg}"))
            return Success(None)
        except subprocess.TimeoutExpired:
            return Failure(OrchError("Timeout resolving run"))
        except Exception as e:
            return Failure(OrchError(str(e)))

    # === Issues ===

    def list_issues(
        self, filters: Optional[IssueFilters] = None
    ) -> Result[ListIssuesResponse, OrchError]:
        try:
            response: DaemonListIssuesResponse = self._daemon.list_issues()
            issues = [_model_issue_to_api(i) for i in response.issues]
            return Success(ListIssuesResponse(issues=issues, total=response.total))
        except DaemonNotRunningError:
            return Failure(ApiDaemonNotRunningError("Daemon not running"))
        except DaemonError as e:
            return Failure(OrchError(str(e)))

    def get_issue(self, issue_id: str) -> Result[Issue, OrchError]:
        try:
            issue = self._daemon.get_issue(issue_id)
            if issue is None:
                return Failure(NotFoundError(f"Issue {issue_id} not found"))
            return Success(_model_issue_to_api(issue))
        except DaemonNotRunningError:
            return Failure(ApiDaemonNotRunningError("Daemon not running"))
        except DaemonError as e:
            return Failure(OrchError(str(e)))

    def create_issue(
        self,
        issue_id: str,
        title: str,
        body: str,
    ) -> Result[None, OrchError]:
        try:
            cmd = self._build_orch_cmd() + [
                "issue",
                "create",
                issue_id,
                "--title",
                title,
            ]
            result = subprocess.run(
                cmd, input=body, capture_output=True, text=True, timeout=30
            )
            if result.returncode != 0:
                error_msg = (
                    result.stderr.strip() or result.stdout.strip() or "Unknown error"
                )
                return Failure(OrchError(f"Failed to create issue: {error_msg}"))
            return Success(None)
        except subprocess.TimeoutExpired:
            return Failure(OrchError("Timeout creating issue"))
        except Exception as e:
            return Failure(OrchError(str(e)))

    def close_issue(self, issue_id: str) -> Result[None, OrchError]:
        try:
            self._daemon.close_issue(issue_id)
            return Success(None)
        except DaemonNotRunningError:
            return Failure(ApiDaemonNotRunningError("Daemon not running"))
        except DaemonError as e:
            return Failure(OrchError(str(e)))

    # === Control Agent ===

    def get_control_agent_launch(
        self,
        project_root: str,
        agent: str = "",
        new_session: bool = False,
    ) -> Result[ControlAgentLaunch, OrchError]:
        try:
            ok, command, prompt_file, port, session_id, resolved_agent, error = (
                self._daemon.get_control_agent_launch(project_root, agent, new_session)
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
        except DaemonNotRunningError:
            return Failure(ApiDaemonNotRunningError("Daemon not running"))
        except DaemonError as e:
            return Failure(OrchError(str(e)))

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
        run_result = self.get_run(issue_id, run_id)
        if isinstance(run_result, Failure):
            return run_result

        run = run_result.unwrap()
        if not run.tmux_session:
            return Failure(OrchError("Run has no session"))

        try:
            # Detect multiplexer type from run
            if run.multiplexer == "zellij":
                import os

                result = subprocess.run(
                    ["zellij", "action", "dump-screen", "/dev/stdout"],
                    capture_output=True,
                    text=True,
                    timeout=5,
                    env={**os.environ, "ZELLIJ_SESSION_NAME": run.tmux_session},
                )
            else:
                result = subprocess.run(
                    ["tmux", "capture-pane", "-t", run.tmux_session, "-p", "-S", "-50"],
                    capture_output=True,
                    text=True,
                    timeout=5,
                )

            if result.returncode != 0:
                return Failure(OrchError("Failed to capture session"))

            return Success(
                SessionCapture(
                    content=result.stdout,
                    timestamp=datetime.now(),
                    source=run.multiplexer or "tmux",
                )
            )
        except subprocess.TimeoutExpired:
            return Failure(OrchError("Timeout capturing session"))
        except Exception as e:
            return Failure(OrchError(str(e)))

    def send_message(
        self,
        issue_id: str,
        run_id: str,
        message: str,
    ) -> Result[None, OrchError]:
        try:
            self._daemon.send_message(issue_id, run_id, message)
            return Success(None)
        except DaemonNotRunningError:
            return Failure(ApiDaemonNotRunningError("Daemon not running"))
        except DaemonError as e:
            return Failure(OrchError(str(e)))

    # === Git ===

    def get_diff_stats(
        self, issue_id: str, run_id: str
    ) -> Result[DiffStats, OrchError]:
        run_result = self.get_run(issue_id, run_id)
        if isinstance(run_result, Failure):
            return run_result

        run = run_result.unwrap()
        if not run.worktree_path or not run.branch:
            return Failure(OrchError("Run has no worktree or branch"))

        cache_key = f"{run.worktree_path}:{run.branch}:{self._base_branch}"
        now = _time()

        if cache_key in _diff_stats_cache_time:
            if now - _diff_stats_cache_time[cache_key] > _DIFF_STATS_TTL:
                _get_git_diff_stats_cached.cache_clear()
                _diff_stats_cache_time.clear()

        stats = _get_git_diff_stats_cached(
            run.worktree_path, run.branch, self._base_branch
        )
        _diff_stats_cache_time[cache_key] = now

        if stats is None:
            return Failure(OrchError("Failed to get diff stats"))
        return Success(stats)

    def get_branch_state(
        self, issue_id: str, run_id: str
    ) -> Result[BranchState, OrchError]:
        run_result = self.get_run(issue_id, run_id)
        if isinstance(run_result, Failure):
            return run_result

        run = run_result.unwrap()
        if not run.worktree_path or not run.branch:
            return Failure(OrchError("Run has no worktree or branch"))

        cache_key = f"{run.worktree_path}:{run.branch}:{self._base_branch}"
        now = _time()

        if cache_key in _branch_state_cache:
            cached_state, cached_time = _branch_state_cache[cache_key]
            if now - cached_time < _BRANCH_STATE_TTL:
                return Success(BranchState(cached_state))

        state = _compute_branch_state(run.worktree_path, run.branch, self._base_branch)
        _branch_state_cache[cache_key] = (state.value, now)
        return Success(state)

    def get_diff(self, issue_id: str, run_id: str) -> Result[str, OrchError]:
        run_result = self.get_run(issue_id, run_id)
        if isinstance(run_result, Failure):
            return run_result

        run = run_result.unwrap()
        if not run.worktree_path or not run.branch:
            return Failure(OrchError("Run has no worktree or branch"))

        try:
            result = subprocess.run(
                ["git", "diff", f"{self._base_branch}...{run.branch}"],
                capture_output=True,
                text=True,
                timeout=30,
                cwd=run.worktree_path,
            )
            if result.returncode != 0:
                # Try without merge-base syntax
                result = subprocess.run(
                    ["git", "diff", f"{self._base_branch}..{run.branch}"],
                    capture_output=True,
                    text=True,
                    timeout=30,
                    cwd=run.worktree_path,
                )
            return Success(result.stdout)
        except subprocess.TimeoutExpired:
            return Failure(OrchError("Timeout getting diff"))
        except Exception as e:
            return Failure(OrchError(str(e)))

    # === Monitor ===

    def register_monitor(
        self,
        pid: int,
        monitor_type: str,
        view: str,
        project: str,
        session_name: str = "",
    ) -> Result[str, OrchError]:
        try:
            monitor_id = self._daemon.register_monitor(
                pid=pid,
                monitor_type=monitor_type,
                view=view,
                project=project,
                tmux_session=session_name,
            )
            if monitor_id is None:
                return Failure(OrchError("Failed to register monitor"))

            # Start heartbeat
            self._monitor_heartbeat = MonitorHeartbeat(self._daemon, project, view)
            self._monitor_heartbeat.start(session_name)

            return Success(monitor_id)
        except DaemonNotRunningError:
            return Failure(ApiDaemonNotRunningError("Daemon not running"))
        except DaemonError as e:
            return Failure(OrchError(str(e)))

    def unregister_monitor(self, monitor_id: str) -> Result[None, OrchError]:
        try:
            if self._monitor_heartbeat:
                self._monitor_heartbeat.stop()
                self._monitor_heartbeat = None

            success = self._daemon.unregister_monitor(monitor_id)
            if not success:
                return Failure(OrchError("Failed to unregister monitor"))
            return Success(None)
        except DaemonNotRunningError:
            return Failure(ApiDaemonNotRunningError("Daemon not running"))
        except DaemonError as e:
            return Failure(OrchError(str(e)))

    def heartbeat(self, monitor_id: str) -> Result[None, OrchError]:
        try:
            success = self._daemon.monitor_heartbeat(monitor_id)
            if not success:
                return Failure(OrchError("Heartbeat failed"))
            return Success(None)
        except DaemonNotRunningError:
            return Failure(ApiDaemonNotRunningError("Daemon not running"))
        except DaemonError as e:
            return Failure(OrchError(str(e)))

    # === Additional methods for backward compatibility ===

    def get_control_session(
        self, project_root: str, agent_type: str = ""
    ) -> tuple[Optional[str], Optional[str]]:
        """Get control session info (backward compatibility)."""
        return self._daemon.get_control_session(project_root, agent_type)

    def set_control_session(
        self, project_root: str, session_id: str, agent_type: str = ""
    ) -> bool:
        """Set control session (backward compatibility)."""
        return self._daemon.set_control_session(project_root, session_id, agent_type)

    def clear_control_session(self, project_root: str) -> bool:
        """Clear control session (backward compatibility)."""
        return self._daemon.clear_control_session(project_root)

    def ensure_opencode_server(
        self, project_root: str
    ) -> tuple[bool, int, Optional[str], Optional[str]]:
        """Ensure opencode server is running (backward compatibility)."""
        return self._daemon.ensure_opencode_server(project_root)
