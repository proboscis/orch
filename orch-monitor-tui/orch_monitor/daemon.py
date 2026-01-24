"""Daemon client for communicating with the orch daemon."""

import json
import os
import socket
import stat
import threading
import time
from dataclasses import dataclass, field
from datetime import datetime
from pathlib import Path
from typing import Optional

from .models import Event, EventType, Issue, IssueStatus, Phase, Run, Status


class DaemonError(Exception):
    """Error from the daemon."""

    pass


class DaemonNotRunningError(DaemonError):
    """Daemon is not running or socket is unavailable."""

    pass


MAX_PAGE_SIZE = 200  # Go daemon's maxLimit
MAX_PAGES = 100  # Safety cap to prevent infinite pagination loops


@dataclass
class RunFilters:
    """Filters for listing runs."""

    issue_id: Optional[str] = None
    status: list[Status] = field(default_factory=list)


@dataclass
class IssueFilters:
    """Filters for listing issues."""

    status: list[IssueStatus] = field(default_factory=list)


@dataclass
class ListRunsResponse:
    """Response from list_runs."""

    runs: list[Run]
    next_cursor: Optional[str]
    total: int


@dataclass
class ListIssuesResponse:
    """Response from list_issues."""

    issues: list[Issue]
    next_cursor: Optional[str]
    total: int


class DaemonClient:
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
        """Check if the daemon socket is available and is actually a socket."""
        try:
            mode = self.socket_path.stat().st_mode
            return stat.S_ISSOCK(mode)
        except (OSError, FileNotFoundError):
            return False

    def _send_request(self, request: dict) -> dict:
        """Send a request to the daemon and return the response.

        Args:
            request: Dictionary to send as JSON.

        Returns:
            Response dictionary from the daemon.

        Raises:
            DaemonNotRunningError: If the daemon is not available.
            DaemonError: If there's an error communicating with the daemon.
        """
        if not self.is_available():
            raise DaemonNotRunningError(
                f"Daemon socket not found at {self.socket_path}"
            )

        try:
            sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            sock.settimeout(self._timeout)
            sock.connect(str(self.socket_path))

            try:
                # Send request
                data = json.dumps(request).encode("utf-8")
                sock.sendall(data)
                sock.shutdown(socket.SHUT_WR)

                # Receive response
                chunks = []
                while True:
                    chunk = sock.recv(4096)
                    if not chunk:
                        break
                    chunks.append(chunk)

                response_data = b"".join(chunks).decode("utf-8")
                return json.loads(response_data)

            finally:
                sock.close()

        except socket.timeout:
            raise DaemonError("Timeout communicating with daemon")
        except ConnectionRefusedError:
            raise DaemonNotRunningError("Daemon is not running")
        except FileNotFoundError:
            raise DaemonNotRunningError(
                f"Daemon socket not found at {self.socket_path}"
            )
        except json.JSONDecodeError as e:
            raise DaemonError(f"Invalid response from daemon: {e}")
        except OSError as e:
            raise DaemonError(f"Socket error: {e}")

    def list_runs(self, filters: Optional[RunFilters] = None) -> ListRunsResponse:
        """List all runs from the daemon, fetching all pages."""
        if filters is None:
            filters = RunFilters()

        all_runs: list[Run] = []
        cursor: Optional[str] = None
        seen_cursors: set[str] = set()
        total = 0

        for page_num in range(MAX_PAGES):
            request = {
                "type": "list_runs",
                "issue_id": filters.issue_id or "",
                "status": [s.value for s in filters.status],
                "limit": MAX_PAGE_SIZE,
                "cursor": cursor or "",
                "issues_root": self._issues_root_str(),
            }

            response = self._send_request(request)

            if not response.get("ok", False):
                raise DaemonError(response.get("error", "Unknown error"))

            all_runs.extend(_json_to_run(r) for r in response.get("runs", []))
            total = response.get("total", 0)
            cursor = response.get("next_cursor")

            if not cursor:
                break

            if cursor in seen_cursors:
                raise DaemonError("Pagination cursor repeated - possible infinite loop")
            seen_cursors.add(cursor)
        else:
            raise DaemonError(
                f"Exceeded maximum pages ({MAX_PAGES}) - try adding filters"
            )

        return ListRunsResponse(runs=all_runs, next_cursor=None, total=total)

    def list_issues(self, filters: Optional[IssueFilters] = None) -> ListIssuesResponse:
        """List all issues from the daemon, fetching all pages."""
        if filters is None:
            filters = IssueFilters()

        all_issues: list[Issue] = []
        cursor: Optional[str] = None
        seen_cursors: set[str] = set()
        total = 0

        for page_num in range(MAX_PAGES):
            request = {
                "type": "list_issues",
                "status": [s.value for s in filters.status],
                "limit": MAX_PAGE_SIZE,
                "cursor": cursor or "",
                "issues_root": self._issues_root_str(),
            }

            response = self._send_request(request)

            if not response.get("ok", False):
                raise DaemonError(response.get("error", "Unknown error"))

            all_issues.extend(
                _json_to_issue_summary(i) for i in response.get("issues", [])
            )
            total = response.get("total", 0)
            cursor = response.get("next_cursor")

            if not cursor:
                break

            if cursor in seen_cursors:
                raise DaemonError("Pagination cursor repeated - possible infinite loop")
            seen_cursors.add(cursor)
        else:
            raise DaemonError(
                f"Exceeded maximum pages ({MAX_PAGES}) - try adding filters"
            )

        return ListIssuesResponse(issues=all_issues, next_cursor=None, total=total)

    def get_run(self, issue_id: str, run_id: str) -> Optional[Run]:
        """Get a specific run by issue ID and run ID.

        Args:
            issue_id: The issue ID.
            run_id: The run ID (optional, gets latest if empty).

        Returns:
            The Run if found, None otherwise.

        Raises:
            DaemonError: If there's an error from the daemon.
        """
        request = {
            "type": "get_run",
            "issue_id": issue_id,
            "run_id": run_id,
            "issues_root": self._issues_root_str(),
        }

        response = self._send_request(request)

        if not response.get("ok", False):
            error = response.get("error", "Unknown error")
            if error == "not_found":
                return None
            raise DaemonError(error)

        run_data = response.get("run")
        if not run_data:
            return None

        return _json_to_run_full(run_data)

    def get_issue(self, issue_id: str) -> Optional[Issue]:
        """Get a specific issue by ID.

        Args:
            issue_id: The issue ID.

        Returns:
            The Issue if found, None otherwise.

        Raises:
            DaemonError: If there's an error from the daemon.
        """
        request = {
            "type": "get_issue",
            "issue_id": issue_id,
            "issues_root": self._issues_root_str(),
        }

        response = self._send_request(request)

        if not response.get("ok", False):
            error = response.get("error", "Unknown error")
            if error == "not_found":
                return None
            raise DaemonError(error)

        issue_data = response.get("issue")
        if not issue_data:
            return None

        return _json_to_issue_full(issue_data)

    def stop_run(self, issue_id: str, run_id: str) -> None:
        """Stop a running run.

        Note: This uses the orch CLI as the daemon doesn't have a stop API.
        The app.py will call orch stop directly via subprocess.

        Args:
            issue_id: The issue ID.
            run_id: The run ID.
        """
        # The daemon doesn't have a stop API - app.py calls orch stop directly
        pass

    def send_message(self, issue_id: str, run_id: str, message: str) -> None:
        """Send a message to a running agent (fire-and-forget).

        Note: The daemon queues the message asynchronously. A successful return
        means the message was accepted for delivery, not that it was delivered.
        Actual delivery may fail if the agent is not available.
        """
        request = {
            "type": "send",
            "issue_id": issue_id,
            "run_id": run_id,
            "message": message,
            "issues_root": self._issues_root_str(),
        }

        response = self._send_request(request)

        if not response.get("ok", False):
            raise DaemonError(response.get("error", "Unknown error"))

    def close(self) -> None:
        pass

    def get_control_session(
        self, project_root: str, agent_type: str = ""
    ) -> tuple[Optional[str], Optional[str]]:
        """Get control session info for the given agent type.

        The daemon will:
        - Return stored session if agent_type matches
        - Clear stored session and discover new one if agent_type changed
        - Discover session automatically for 'claude' agent type

        Returns (session_id, agent_type).
        """
        request = {
            "type": "get_control_session",
            "project_root": project_root,
            "agent_type": agent_type,
        }

        try:
            response = self._send_request(request)
            if response.get("ok", False):
                session_id = response.get("session_id") or None
                resp_agent_type = response.get("agent_type") or None
                return session_id, resp_agent_type
        except DaemonError:
            pass
        return None, None

    def set_control_session(
        self, project_root: str, session_id: str, agent_type: str = ""
    ) -> bool:
        request = {
            "type": "set_control_session",
            "project_root": project_root,
            "session_id": session_id,
            "agent_type": agent_type,
        }

        try:
            response = self._send_request(request)
            return response.get("ok", False)
        except DaemonError:
            return False

    def clear_control_session(self, project_root: str) -> bool:
        request = {
            "type": "clear_control_session",
            "project_root": project_root,
        }

        try:
            response = self._send_request(request)
            return response.get("ok", False)
        except DaemonError:
            return False

    def ensure_opencode_server(
        self, project_root: str
    ) -> tuple[bool, int, Optional[str], Optional[str]]:
        request = {
            "type": "ensure_opencode_server",
            "project_root": project_root,
        }

        try:
            response = self._send_request(request)
            if response.get("ok", False):
                return (
                    True,
                    response.get("port", 0),
                    response.get("session_id"),
                    None,
                )
            return False, 0, None, response.get("error", "Unknown error")
        except DaemonError as e:
            return False, 0, None, str(e)

    def register_monitor(
        self,
        pid: int,
        monitor_type: str,
        view: str,
        project: str,
        tmux_session: str = "",
    ) -> Optional[str]:
        request = {
            "type": "register_monitor",
            "limit": pid,
            "title": monitor_type,
            "summary": view,
            "project_root": project,
            "body": tmux_session,
        }

        try:
            response = self._send_request(request)
            if response.get("ok", False):
                return response.get("monitor_id")
        except DaemonError:
            pass
        return None

    def unregister_monitor(self, monitor_id: str) -> bool:
        request = {
            "type": "unregister_monitor",
            "short_id": monitor_id,
        }

        try:
            response = self._send_request(request)
            return response.get("ok", False)
        except DaemonError:
            return False

    def monitor_heartbeat(self, monitor_id: str) -> bool:
        request = {
            "type": "monitor_heartbeat",
            "short_id": monitor_id,
        }

        try:
            response = self._send_request(request)
            return response.get("ok", False)
        except DaemonError:
            return False


class MonitorRegistration:
    def __init__(self, client: DaemonClient, project: str, view: str = "dashboard"):
        self._client = client
        self._project = project
        self._view = view
        self._monitor_id: Optional[str] = None
        self._tmux_session: str = ""
        self._heartbeat_thread: Optional[threading.Thread] = None
        self._stop_event = threading.Event()

    def register(self, tmux_session: str = "") -> Optional[str]:
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
            self._start_heartbeat()

        return self._monitor_id

    def unregister(self) -> None:
        self._stop_heartbeat()

        if self._monitor_id and self._client.is_available():
            self._client.unregister_monitor(self._monitor_id)
            self._monitor_id = None

    def _start_heartbeat(self) -> None:
        if self._heartbeat_thread is not None:
            return

        self._stop_event.clear()
        self._heartbeat_thread = threading.Thread(
            target=self._heartbeat_loop, daemon=True
        )
        self._heartbeat_thread.start()

    def _stop_heartbeat(self) -> None:
        if self._heartbeat_thread is None:
            return

        self._stop_event.set()
        self._heartbeat_thread.join(timeout=2.0)
        self._heartbeat_thread = None

    def _heartbeat_loop(self) -> None:
        while not self._stop_event.wait(timeout=30.0):
            if self._monitor_id and self._client.is_available():
                success = self._client.monitor_heartbeat(self._monitor_id)
                if not success:
                    self._reregister()

    def _reregister(self) -> None:
        if not self._client.is_available():
            return

        new_id = self._client.register_monitor(
            pid=os.getpid(),
            monitor_type="python",
            view=self._view,
            project=self._project,
            tmux_session=self._tmux_session,
        )
        if new_id:
            self._monitor_id = new_id


def _parse_timestamp(ts_str: str) -> Optional[datetime]:
    """Parse an ISO 8601 timestamp string."""
    if not ts_str:
        return None
    try:
        # Handle various ISO formats
        ts_str = ts_str.replace("Z", "+00:00")
        return datetime.fromisoformat(ts_str)
    except (ValueError, TypeError):
        return None


def _json_to_run(data: dict) -> Run:
    """Convert a JSON run summary to a Run model."""
    try:
        status = Status(data.get("status", "unknown"))
    except ValueError:
        status = Status.UNKNOWN

    try:
        phase = Phase(data.get("phase", "")) if data.get("phase") else None
    except ValueError:
        phase = None

    return Run(
        issue_id=data.get("issue_id", ""),
        run_id=data.get("run_id", ""),
        path=Path(data.get("uri", "").replace("file://", ""))
        if data.get("uri")
        else Path(),
        status=status,
        phase=phase,
        agent=data.get("agent", ""),
        model=data.get("model", ""),
        model_variant=data.get("model_variant", ""),
        branch=data.get("branch", ""),
        worktree_path=data.get("worktree_path", ""),
        tmux_session=data.get("tmux_session", ""),
        pr_url=data.get("pr_url", ""),
        started_at=_parse_timestamp(data.get("started_at", "")),
        updated_at=_parse_timestamp(data.get("updated_at", "")),
        additions=data.get("additions", 0),
        deletions=data.get("deletions", 0),
    )


def _json_to_run_full(data: dict) -> Run:
    """Convert a JSON run full response to a Run model."""
    try:
        status = Status(data.get("status", "unknown"))
    except ValueError:
        status = Status.UNKNOWN

    try:
        phase = Phase(data.get("phase", "")) if data.get("phase") else None
    except ValueError:
        phase = None

    # Parse events
    events = []
    for event_data in data.get("events", []):
        try:
            event_type = EventType(event_data.get("type", "note"))
        except ValueError:
            event_type = EventType.NOTE

        timestamp = _parse_timestamp(event_data.get("timestamp", ""))
        if timestamp is None:
            timestamp = datetime.now()

        events.append(
            Event(
                timestamp=timestamp,
                type=event_type,
                name=event_data.get("name", ""),
                attrs=event_data.get("attrs") or {},
                raw="",
            )
        )

    return Run(
        issue_id=data.get("issue_id", ""),
        run_id=data.get("run_id", ""),
        path=Path(data.get("uri", "").replace("file://", ""))
        if data.get("uri")
        else Path(),
        status=status,
        phase=phase,
        events=events,
        agent=data.get("agent", ""),
        model=data.get("model", ""),
        model_variant=data.get("model_variant", ""),
        branch=data.get("branch", ""),
        worktree_path=data.get("worktree_path", ""),
        tmux_session=data.get("tmux_session", ""),
        tmux_window_id="",
        pr_url=data.get("pr_url", ""),
        server_port=data.get("server_port", 0),
        opencode_session_id=data.get("opencode_session_id", ""),
        continued_from=data.get("continued_from", ""),
        started_at=_parse_timestamp(data.get("started_at", "")),
        updated_at=_parse_timestamp(data.get("updated_at", "")),
    )


def _json_to_issue_summary(data: dict) -> Issue:
    """Convert a JSON issue summary to an Issue model."""
    try:
        status = IssueStatus(data.get("status", "open"))
    except ValueError:
        status = IssueStatus.OPEN

    return Issue(
        id=data.get("id", ""),
        title=data.get("title", ""),
        topic=data.get("topic", ""),
        summary=data.get("summary", ""),
        status=status,
        tags=data.get("tags") or [],
        body="",  # Summary doesn't include body
        path=Path(data.get("uri", "").replace("file://", ""))
        if data.get("uri")
        else Path(),
        modified_at=_parse_timestamp(data.get("modified_at", "")),
    )


def _json_to_issue_full(data: dict) -> Issue:
    """Convert a JSON issue full response to an Issue model."""
    try:
        status = IssueStatus(data.get("status", "open"))
    except ValueError:
        status = IssueStatus.OPEN

    return Issue(
        id=data.get("id", ""),
        title=data.get("title", ""),
        topic=data.get("topic", ""),
        summary=data.get("summary", ""),
        status=status,
        tags=data.get("tags") or [],
        body=data.get("body", ""),
        path=Path(data.get("uri", "").replace("file://", ""))
        if data.get("uri")
        else Path(),
        frontmatter=data.get("frontmatter") or {},
        modified_at=_parse_timestamp(data.get("modified_at", "")),
    )
