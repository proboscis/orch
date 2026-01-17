"""Daemon client for communicating with the orch daemon.

This module provides a DaemonClient class that connects to the orch daemon
via Unix socket and provides query and mutation APIs for runs and issues.

The daemon is the single source of truth for all data.
"""

import json
import socket
import stat
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
    """Client for communicating with the orch daemon.

    Connects to the daemon's Unix socket and provides query/mutation APIs.
    The daemon is the single source of truth for all run and issue data.
    """

    def __init__(self, socket_path: Path, timeout: float = 30.0):
        self.socket_path = socket_path
        self._timeout = timeout

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
        page_count = 0

        while True:
            request = {
                "type": "list_runs",
                "issue_id": filters.issue_id or "",
                "status": [s.value for s in filters.status],
                "limit": MAX_PAGE_SIZE,
                "cursor": cursor or "",
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

            page_count += 1
            if page_count >= MAX_PAGES:
                raise DaemonError(f"Exceeded maximum pages ({MAX_PAGES})")

        return ListRunsResponse(runs=all_runs, next_cursor=None, total=total)

    def list_issues(self, filters: Optional[IssueFilters] = None) -> ListIssuesResponse:
        """List all issues from the daemon, fetching all pages."""
        if filters is None:
            filters = IssueFilters()

        all_issues: list[Issue] = []
        cursor: Optional[str] = None
        seen_cursors: set[str] = set()
        total = 0
        page_count = 0

        while True:
            request = {
                "type": "list_issues",
                "status": [s.value for s in filters.status],
                "limit": MAX_PAGE_SIZE,
                "cursor": cursor or "",
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

            page_count += 1
            if page_count >= MAX_PAGES:
                raise DaemonError(f"Exceeded maximum pages ({MAX_PAGES})")

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
        }

        response = self._send_request(request)

        if not response.get("ok", False):
            raise DaemonError(response.get("error", "Unknown error"))

    def close(self) -> None:
        """Close the client (no-op for socket-based client)."""
        pass


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
        branch=data.get("branch", ""),
        worktree_path=data.get("worktree_path", ""),
        tmux_session=data.get("tmux_session", ""),
        pr_url=data.get("pr_url", ""),
        started_at=_parse_timestamp(data.get("started_at", "")),
        updated_at=_parse_timestamp(data.get("updated_at", "")),
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
        body="",  # Summary doesn't include body
        path=Path(data.get("uri", "").replace("file://", ""))
        if data.get("uri")
        else Path(),
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
        body=data.get("body", ""),
        path=Path(data.get("uri", "").replace("file://", ""))
        if data.get("uri")
        else Path(),
        frontmatter=data.get("frontmatter") or {},
    )
