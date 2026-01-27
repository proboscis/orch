"""Protobuf-based daemon client for orch daemon communication.

DEPRECATED: The ProtoDaemonClient class in this module is deprecated.
Use proto_client_hy.ProtoDaemonClient instead, which returns Result types.

This module is kept for:
- Exception classes (ProtoDaemonError, ProtoDaemonNotRunningError)
- Dataclass definitions (RunFilters, IssueFilters, ListRunsResponse, etc.)
"""

import socket
import struct
from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path
from typing import Optional

from .api import orch_pb2 as pb
from .models import Event, EventType, Issue, IssueStatus, Phase, Run, Status


class ProtoDaemonError(Exception):
    pass


class ProtoDaemonNotRunningError(ProtoDaemonError):
    pass


MAX_PAGE_SIZE = 200
MAX_PAGES = 100


@dataclass
class RunFilters:
    issue_id: Optional[str] = None
    status: list[Status] = field(default_factory=list)
    agent: Optional[str] = None
    text_search: Optional[str] = None
    time_range: Optional[str] = None


@dataclass
class IssueFilters:
    status: list[IssueStatus] = field(default_factory=list)
    tags: list[str] = field(default_factory=list)
    tags_mode: Optional[str] = None
    text_search: Optional[str] = None


@dataclass
class ListRunsResponse:
    runs: list[Run]
    next_cursor: Optional[str]
    total: int


@dataclass
class ListIssuesResponse:
    issues: list[Issue]
    next_cursor: Optional[str]
    total: int


def _model_status_to_proto(s: Status) -> pb.RunStatus:
    mapping = {
        Status.QUEUED: pb.RUN_STATUS_QUEUED,
        Status.BOOTING: pb.RUN_STATUS_BOOTING,
        Status.RUNNING: pb.RUN_STATUS_RUNNING,
        Status.BLOCKED: pb.RUN_STATUS_BLOCKED,
        Status.BLOCKED_API: pb.RUN_STATUS_BLOCKED_API,
        Status.PR_OPEN: pb.RUN_STATUS_PR_OPEN,
        Status.DONE: pb.RUN_STATUS_DONE,
        Status.FAILED: pb.RUN_STATUS_FAILED,
        Status.CANCELED: pb.RUN_STATUS_CANCELED,
    }
    return mapping.get(s, pb.RUN_STATUS_UNSPECIFIED)


def _proto_status_to_model(s: pb.RunStatus) -> Status:
    mapping = {
        pb.RUN_STATUS_QUEUED: Status.QUEUED,
        pb.RUN_STATUS_BOOTING: Status.BOOTING,
        pb.RUN_STATUS_RUNNING: Status.RUNNING,
        pb.RUN_STATUS_BLOCKED: Status.BLOCKED,
        pb.RUN_STATUS_BLOCKED_API: Status.BLOCKED_API,
        pb.RUN_STATUS_PR_OPEN: Status.PR_OPEN,
        pb.RUN_STATUS_DONE: Status.DONE,
        pb.RUN_STATUS_FAILED: Status.FAILED,
        pb.RUN_STATUS_CANCELED: Status.CANCELED,
    }
    return mapping.get(s, Status.UNKNOWN)


def _model_issue_status_to_proto(s: IssueStatus) -> pb.IssueStatus:
    mapping = {
        IssueStatus.OPEN: pb.ISSUE_STATUS_OPEN,
        IssueStatus.RESOLVED: pb.ISSUE_STATUS_RESOLVED,
        IssueStatus.CLOSED: pb.ISSUE_STATUS_CLOSED,
    }
    return mapping.get(s, pb.ISSUE_STATUS_UNSPECIFIED)


def _proto_issue_status_to_model(s: pb.IssueStatus) -> IssueStatus:
    mapping = {
        pb.ISSUE_STATUS_OPEN: IssueStatus.OPEN,
        pb.ISSUE_STATUS_RESOLVED: IssueStatus.RESOLVED,
        pb.ISSUE_STATUS_CLOSED: IssueStatus.CLOSED,
    }
    return mapping.get(s, IssueStatus.OPEN)


def _proto_multiplexer_to_str(m: pb.Multiplexer) -> str:
    if m == pb.MULTIPLEXER_TMUX:
        return "tmux"
    elif m == pb.MULTIPLEXER_ZELLIJ:
        return "zellij"
    return ""


def _proto_branch_state_to_str(s: pb.BranchState) -> str:
    mapping = {
        pb.BRANCH_STATE_CLEAN: "clean",
        pb.BRANCH_STATE_DIRTY: "dirty",
        pb.BRANCH_STATE_MERGED: "merged",
        pb.BRANCH_STATE_CONFLICT: "conflict",
    }
    return mapping.get(s, "")


def _proto_run_to_model(r: pb.Run) -> Run:
    additions = 0
    deletions = 0
    files_changed = 0
    files: list[str] = []
    if r.HasField("diff_stats"):
        additions = r.diff_stats.additions
        deletions = r.diff_stats.deletions
        files_changed = r.diff_stats.files_changed
        files = list(r.diff_stats.files)

    return Run(
        issue_id=r.issue_id,
        run_id=r.run_id,
        path=Path(),
        status=_proto_status_to_model(r.status),
        agent=r.agent,
        model=r.model,
        branch=r.branch,
        worktree_path=r.worktree_path,
        tmux_session=r.tmux_session,
        multiplexer=_proto_multiplexer_to_str(r.multiplexer),
        pr_url=r.pr_url,
        server_port=r.server_port,
        opencode_session_id=r.opencode_session_id,
        continued_from=r.continued_from,
        started_at=datetime.fromtimestamp(r.started_at_unix)
        if r.started_at_unix
        else None,
        updated_at=datetime.fromtimestamp(r.updated_at_unix)
        if r.updated_at_unix
        else None,
        elapsed_seconds=r.elapsed_seconds,
        elapsed_display=r.elapsed_display,
        additions=additions,
        deletions=deletions,
        files_changed=files_changed,
        files=files,
        branch_state=_proto_branch_state_to_str(r.branch_state),
    )


def _proto_issue_to_model(i: pb.Issue) -> Issue:
    return Issue(
        id=i.id,
        title=i.title,
        summary=i.summary,
        status=_proto_issue_status_to_model(i.status),
        tags=list(i.tags),
        body=i.body,
        path=Path(i.path) if i.path else Path(),
        modified_at=datetime.fromtimestamp(i.modified_at_unix)
        if i.modified_at_unix
        else None,
    )


def _proto_event_to_model(e: pb.Event) -> Event:
    try:
        event_type = EventType(e.type)
    except ValueError:
        event_type = EventType.NOTE
    return Event(
        timestamp=datetime.fromtimestamp(e.timestamp_unix)
        if e.timestamp_unix
        else datetime.now(),
        type=event_type,
        name=e.name,
        attrs=dict(e.attrs),
    )


class ProtoDaemonClient:
    def __init__(
        self,
        socket_path: Path,
        issues_root: Optional[Path] = None,
        timeout: float = 30.0,
    ):
        self.socket_path = socket_path
        self.issues_root = issues_root
        self._timeout = timeout

    def _issues_root_str(self) -> str:
        return str(self.issues_root) if self.issues_root else ""

    def is_available(self) -> bool:
        try:
            import stat

            mode = self.socket_path.stat().st_mode
            return stat.S_ISSOCK(mode)
        except (OSError, FileNotFoundError):
            return False

    def _send_request(self, request: pb.Request) -> pb.Response:
        if not self.is_available():
            raise ProtoDaemonNotRunningError(
                f"Daemon socket not found at {self.socket_path}"
            )

        try:
            sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            sock.settimeout(self._timeout)
            sock.connect(str(self.socket_path))

            try:
                data = request.SerializeToString()
                length = struct.pack(">I", len(data))
                sock.sendall(length + data)
                sock.shutdown(socket.SHUT_WR)

                len_data = b""
                while len(len_data) < 4:
                    chunk = sock.recv(4 - len(len_data))
                    if not chunk:
                        break
                    len_data += chunk

                if len(len_data) < 4:
                    raise ProtoDaemonError("Incomplete response length")

                resp_len = struct.unpack(">I", len_data)[0]
                resp_data = b""
                while len(resp_data) < resp_len:
                    chunk = sock.recv(resp_len - len(resp_data))
                    if not chunk:
                        break
                    resp_data += chunk

                response = pb.Response()
                response.ParseFromString(resp_data)
                return response

            finally:
                sock.close()

        except socket.timeout:
            raise ProtoDaemonError("Timeout communicating with daemon")
        except ConnectionRefusedError:
            raise ProtoDaemonNotRunningError("Daemon is not running")
        except FileNotFoundError:
            raise ProtoDaemonNotRunningError(
                f"Daemon socket not found at {self.socket_path}"
            )
        except Exception as e:
            raise ProtoDaemonError(f"Socket error: {e}")

    def list_runs(self, filters: Optional[RunFilters] = None) -> ListRunsResponse:
        if filters is None:
            filters = RunFilters()

        req = pb.Request()
        req.list_runs.issues_root = self._issues_root_str()
        req.list_runs.issue_id = filters.issue_id or ""
        for s in filters.status:
            req.list_runs.status.append(_model_status_to_proto(s))
        req.list_runs.agent = filters.agent or ""
        req.list_runs.text_search = filters.text_search or ""
        req.list_runs.time_range = filters.time_range or ""
        req.list_runs.limit = MAX_PAGE_SIZE

        response = self._send_request(req)

        if not response.ok:
            raise ProtoDaemonError(response.error or "Unknown error")

        list_resp = response.list_runs
        runs = [_proto_run_to_model(r) for r in list_resp.runs]
        return ListRunsResponse(runs=runs, next_cursor=None, total=list_resp.total)

    def list_issues(self, filters: Optional[IssueFilters] = None) -> ListIssuesResponse:
        if filters is None:
            filters = IssueFilters()

        req = pb.Request()
        req.list_issues.issues_root = self._issues_root_str()
        for s in filters.status:
            req.list_issues.status.append(_model_issue_status_to_proto(s))
        for tag in filters.tags:
            req.list_issues.tags.append(tag)
        req.list_issues.tags_mode = filters.tags_mode or ""
        req.list_issues.text_search = filters.text_search or ""
        req.list_issues.limit = MAX_PAGE_SIZE

        response = self._send_request(req)

        if not response.ok:
            raise ProtoDaemonError(response.error or "Unknown error")

        list_resp = response.list_issues
        issues = [_proto_issue_to_model(i) for i in list_resp.issues]
        return ListIssuesResponse(
            issues=issues, next_cursor=None, total=list_resp.total
        )

    def get_run(self, issue_id: str, run_id: str) -> Optional[Run]:
        req = pb.Request()
        req.get_run.issues_root = self._issues_root_str()
        req.get_run.issue_id = issue_id
        req.get_run.run_id = run_id

        response = self._send_request(req)

        if not response.ok:
            if response.error == "not_found":
                return None
            raise ProtoDaemonError(response.error or "Unknown error")

        run = response.get_run.run
        result = _proto_run_to_model(run)
        result.events = [_proto_event_to_model(e) for e in response.get_run.events]
        return result

    def get_issue(self, issue_id: str) -> Optional[Issue]:
        req = pb.Request()
        req.get_issue.issues_root = self._issues_root_str()
        req.get_issue.issue_id = issue_id

        response = self._send_request(req)

        if not response.ok:
            if response.error == "not_found":
                return None
            raise ProtoDaemonError(response.error or "Unknown error")

        return _proto_issue_to_model(response.get_issue.issue)

    def start_run(self, issue_id: str, agent: str = "", model: str = "") -> dict:
        req = pb.Request()
        req.start_run.issues_root = self._issues_root_str()
        req.start_run.issue_id = issue_id
        req.start_run.agent = agent
        req.start_run.model = model

        response = self._send_request(req)

        if not response.ok:
            raise ProtoDaemonError(response.error or "Unknown error")

        sr = response.start_run
        return {
            "run_id": sr.run_id,
            "branch": sr.branch,
            "worktree": sr.worktree_path,
            "tmux_session": sr.tmux_session,
        }

    def stop_run(self, issue_id: str, run_id: str = "") -> dict:
        req = pb.Request()
        req.stop_run.issues_root = self._issues_root_str()
        req.stop_run.issue_id = issue_id
        req.stop_run.run_id = run_id

        response = self._send_request(req)

        if not response.ok:
            raise ProtoDaemonError(response.error or "Unknown error")

        return {"stopped": True}

    def close_issue(self, issue_id: str, comment: str = "") -> None:
        req = pb.Request()
        req.close_issue.issues_root = self._issues_root_str()
        req.close_issue.issue_id = issue_id

        response = self._send_request(req)

        if not response.ok:
            raise ProtoDaemonError(response.error or "Unknown error")

    def send_message(self, issue_id: str, run_id: str, message: str) -> None:
        req = pb.Request()
        req.send_message.issues_root = self._issues_root_str()
        req.send_message.issue_id = issue_id
        req.send_message.run_id = run_id
        req.send_message.message = message

        response = self._send_request(req)

        if not response.ok:
            raise ProtoDaemonError(response.error or "Unknown error")

    def ping(self) -> bool:
        req = pb.Request()
        req.ping.CopyFrom(pb.PingRequest())

        try:
            response = self._send_request(req)
            return response.ok and response.ping.ok
        except ProtoDaemonError:
            return False

    def get_control_agent_launch(
        self, project_root: str, agent_type: str = "", new_session: bool = False
    ) -> tuple[
        bool,
        Optional[str],
        Optional[str],
        int,
        Optional[str],
        Optional[str],
        Optional[str],
    ]:
        req = pb.Request()
        req.get_control_agent_launch.project_root = project_root
        req.get_control_agent_launch.agent = agent_type
        req.get_control_agent_launch.new_session = new_session

        try:
            response = self._send_request(req)
            if response.ok:
                r = response.get_control_agent_launch
                return (
                    True,
                    r.command,
                    r.prompt_file,
                    r.port,
                    r.session_id,
                    agent_type,
                    None,
                )
            return (False, None, None, 0, None, None, response.error)
        except ProtoDaemonError as e:
            return (False, None, None, 0, None, None, str(e))

    def register_monitor(
        self,
        pid: int,
        monitor_type: str,
        view: str,
        project: str,
        tmux_session: str = "",
    ) -> Optional[str]:
        req = pb.Request()
        req.register_monitor.pid = pid
        req.register_monitor.monitor_type = monitor_type
        req.register_monitor.view = view
        req.register_monitor.project = project
        req.register_monitor.session_name = tmux_session

        response = self._send_request(req)
        if response.ok:
            return response.register_monitor.monitor_id
        raise ProtoDaemonError(response.error or "Failed to register monitor")

    def unregister_monitor(self, monitor_id: str) -> bool:
        req = pb.Request()
        req.unregister_monitor.monitor_id = monitor_id

        response = self._send_request(req)
        if not response.ok:
            raise ProtoDaemonError(response.error or "Failed to unregister monitor")
        return True

    def monitor_heartbeat(self, monitor_id: str) -> bool:
        req = pb.Request()
        req.heartbeat.monitor_id = monitor_id

        response = self._send_request(req)
        if not response.ok:
            raise ProtoDaemonError(response.error or "Heartbeat failed")
        return True

    def close(self) -> None:
        pass

    def get_diff_stats(
        self, issue_id: str, run_id: str
    ) -> Optional[tuple[int, int, int, list[str]]]:
        """Get diff stats for a run. Returns (additions, deletions, files_changed, files) or None."""
        req = pb.Request()
        req.get_diff_stats.issues_root = self._issues_root_str()
        req.get_diff_stats.issue_id = issue_id
        req.get_diff_stats.run_id = run_id

        response = self._send_request(req)
        if response.ok and response.HasField("get_diff_stats"):
            ds = response.get_diff_stats.diff_stats
            return (ds.additions, ds.deletions, ds.files_changed, list(ds.files))
        if not response.ok:
            raise ProtoDaemonError(response.error or "Failed to get diff stats")
        return None

    def get_branch_state(self, issue_id: str, run_id: str) -> str:
        """Get branch state for a run. Returns 'clean', 'dirty', 'merged', 'conflict', or ''."""
        req = pb.Request()
        req.get_branch_state.issues_root = self._issues_root_str()
        req.get_branch_state.issue_id = issue_id
        req.get_branch_state.run_id = run_id

        response = self._send_request(req)
        if response.ok and response.HasField("get_branch_state"):
            return _proto_branch_state_to_str(response.get_branch_state.state)
        if not response.ok:
            raise ProtoDaemonError(response.error or "Failed to get branch state")
        return ""

    def get_diff(self, issue_id: str, run_id: str) -> Optional[str]:
        """Get diff for a run. Returns the diff string or None."""
        req = pb.Request()
        req.get_diff.issues_root = self._issues_root_str()
        req.get_diff.issue_id = issue_id
        req.get_diff.run_id = run_id

        response = self._send_request(req)
        if response.ok and response.HasField("get_diff"):
            return response.get_diff.diff
        if not response.ok:
            raise ProtoDaemonError(response.error or "Failed to get diff")
        return None

    def capture_session(
        self, issue_id: str, run_id: str
    ) -> Optional[tuple[str, int, str]]:
        """Capture session output. Returns (content, timestamp_unix, source) or None."""
        req = pb.Request()
        req.capture_session.issues_root = self._issues_root_str()
        req.capture_session.issue_id = issue_id
        req.capture_session.run_id = run_id

        response = self._send_request(req)
        if response.ok and response.HasField("capture_session"):
            cs = response.capture_session
            return (cs.content, cs.timestamp_unix, cs.source)
        if not response.ok:
            raise ProtoDaemonError(response.error or "Failed to capture session")
        return None

    def create_issue(self, issue_id: str, title: str, body: str) -> Optional[str]:
        """Create a new issue. Returns the path or None on failure."""
        req = pb.Request()
        req.create_issue.issues_root = self._issues_root_str()
        req.create_issue.issue_id = issue_id
        req.create_issue.title = title
        req.create_issue.body = body

        try:
            response = self._send_request(req)
            if response.ok and response.HasField("create_issue"):
                return response.create_issue.path
            raise ProtoDaemonError(response.error or "Failed to create issue")
        except ProtoDaemonError:
            raise

    def resolve_issue(self, issue_id: str, force: bool = False) -> bool:
        """Resolve an issue. Returns True on success."""
        req = pb.Request()
        req.resolve_issue.issues_root = self._issues_root_str()
        req.resolve_issue.issue_id = issue_id
        req.resolve_issue.force = force

        response = self._send_request(req)
        if not response.ok:
            raise ProtoDaemonError(response.error or "Failed to resolve issue")
        return True
