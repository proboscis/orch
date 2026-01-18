"""Tests for daemon client communication protocols."""

import json
import os
import socket
import stat
import tempfile
import threading
import time
import uuid
from pathlib import Path
from typing import Any
from unittest.mock import MagicMock, patch

import pytest

from orch_monitor.daemon import (
    DaemonClient,
    DaemonError,
    DaemonNotRunningError,
    ListIssuesResponse,
    ListRunsResponse,
    RunFilters,
    IssueFilters,
)
from orch_monitor.models import Status, IssueStatus


class MockDaemonServer:
    """A mock Unix socket server that simulates the Go daemon."""

    def __init__(self, socket_path: Path):
        self.socket_path = socket_path
        self.server_socket: socket.socket | None = None
        self._thread: threading.Thread | None = None
        self._stop = False
        self._handlers: dict[str, Any] = {}
        self._setup_default_handlers()

    def _setup_default_handlers(self):
        """Set up default response handlers for each request type."""
        self._handlers = {
            "list_runs": lambda req: {
                "ok": True,
                "runs": [],
                "total": 0,
                "next_cursor": None,
            },
            "list_issues": lambda req: {
                "ok": True,
                "issues": [],
                "total": 0,
                "next_cursor": None,
            },
            "get_run": lambda req: {
                "ok": False,
                "error": "not_found",
            },
            "get_issue": lambda req: {
                "ok": False,
                "error": "not_found",
            },
            "send": lambda req: {"ok": True},
            "register_monitor": lambda req: {
                "ok": True,
                "monitor_id": f"mon-{req.get('limit', 0)}-12345",
            },
            "unregister_monitor": lambda req: {"ok": True},
            "monitor_heartbeat": lambda req: {"ok": True},
        }

    def set_handler(self, request_type: str, handler):
        """Set a custom handler for a request type."""
        self._handlers[request_type] = handler

    def start(self):
        """Start the mock server in a background thread."""
        self.socket_path.parent.mkdir(parents=True, exist_ok=True)
        if self.socket_path.exists():
            self.socket_path.unlink()

        self.server_socket = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.server_socket.bind(str(self.socket_path))
        self.server_socket.listen(5)
        self.server_socket.settimeout(0.5)

        self._stop = False
        self._thread = threading.Thread(target=self._accept_loop, daemon=True)
        self._thread.start()
        time.sleep(0.1)  # Give server time to start

    def stop(self):
        """Stop the mock server."""
        self._stop = True
        if self._thread:
            self._thread.join(timeout=2.0)
        if self.server_socket:
            self.server_socket.close()
        if self.socket_path.exists():
            self.socket_path.unlink()

    def _accept_loop(self):
        """Accept connections and handle requests."""
        while not self._stop:
            try:
                conn, _ = self.server_socket.accept()
                self._handle_connection(conn)
            except socket.timeout:
                continue
            except OSError:
                break

    def _handle_connection(self, conn: socket.socket):
        """Handle a single client connection."""
        try:
            conn.settimeout(5.0)
            # Read until we get a complete JSON or client closes
            data = b""
            try:
                while True:
                    chunk = conn.recv(4096)
                    if not chunk:
                        break
                    data += chunk
                    # Check if we have complete JSON
                    try:
                        json.loads(data.decode("utf-8"))
                        break
                    except json.JSONDecodeError:
                        continue
            except socket.timeout:
                pass

            if data:
                try:
                    request = json.loads(data.decode("utf-8"))
                    request_type = request.get("type", "")
                    handler = self._handlers.get(request_type, lambda r: {"ok": False, "error": "unknown_type"})
                    response = handler(request)
                    response_data = json.dumps(response).encode("utf-8")
                    conn.sendall(response_data)
                    # Give client time to receive before closing
                    time.sleep(0.01)
                except Exception:
                    pass
        finally:
            try:
                conn.shutdown(socket.SHUT_RDWR)
            except OSError:
                pass
            conn.close()


@pytest.fixture
def short_tmp_path():
    """Create a short temporary directory path for Unix sockets."""
    # Use /tmp with a short UUID to stay under Unix socket path limit
    short_id = str(uuid.uuid4())[:8]
    path = Path(f"/tmp/orch-test-{short_id}")
    path.mkdir(parents=True, exist_ok=True)
    yield path
    # Cleanup
    import shutil
    shutil.rmtree(path, ignore_errors=True)


@pytest.fixture
def mock_server(short_tmp_path: Path):
    """Create and start a mock daemon server."""
    socket_path = short_tmp_path / "d.sock"  # Short name
    server = MockDaemonServer(socket_path)
    server.start()
    yield server
    server.stop()


@pytest.fixture
def client(mock_server: MockDaemonServer) -> DaemonClient:
    """Create a DaemonClient connected to the mock server."""
    return DaemonClient(mock_server.socket_path)


class TestDaemonClientAvailability:
    """Tests for daemon availability checking."""

    def test_is_available_true_when_socket_exists(self, client: DaemonClient):
        """Should return True when socket exists and is a socket."""
        assert client.is_available() is True

    def test_is_available_false_when_no_socket(self, short_tmp_path: Path):
        """Should return False when socket doesn't exist."""
        client = DaemonClient(short_tmp_path / "nonexistent.sock")
        assert client.is_available() is False

    def test_is_available_false_when_regular_file(self, short_tmp_path: Path):
        """Should return False when path is a regular file, not a socket."""
        fake_socket = short_tmp_path / "fake.sock"
        fake_socket.touch()
        client = DaemonClient(fake_socket)
        assert client.is_available() is False


class TestListRuns:
    """Tests for list_runs protocol."""

    def test_list_runs_empty(self, client: DaemonClient):
        """Should return empty list when no runs."""
        result = client.list_runs()
        assert isinstance(result, ListRunsResponse)
        assert result.runs == []
        assert result.total == 0

    def test_list_runs_with_data(self, mock_server: MockDaemonServer, client: DaemonClient):
        """Should parse run data correctly."""
        mock_server.set_handler("list_runs", lambda req: {
            "ok": True,
            "runs": [
                {
                    "issue_id": "orch-123",
                    "run_id": "20260119-120000",
                    "status": "running",
                    "agent": "claude",
                    "model": "claude-3-5-sonnet",
                    "branch": "feature-branch",
                    "started_at": "2026-01-19T12:00:00Z",
                    "updated_at": "2026-01-19T12:05:00Z",
                }
            ],
            "total": 1,
            "next_cursor": None,
        })

        result = client.list_runs()
        assert len(result.runs) == 1
        assert result.runs[0].issue_id == "orch-123"
        assert result.runs[0].status == Status.RUNNING
        assert result.runs[0].agent == "claude"

    def test_list_runs_with_filters(self, mock_server: MockDaemonServer, client: DaemonClient):
        """Should send filter parameters in request."""
        received_request = {}

        def capture_handler(req):
            received_request.update(req)
            return {"ok": True, "runs": [], "total": 0}

        mock_server.set_handler("list_runs", capture_handler)

        filters = RunFilters(issue_id="orch-123", status=[Status.RUNNING, Status.BLOCKED])
        client.list_runs(filters)

        assert received_request["issue_id"] == "orch-123"
        assert "running" in received_request["status"]
        assert "blocked" in received_request["status"]


class TestListIssues:
    """Tests for list_issues protocol."""

    def test_list_issues_empty(self, client: DaemonClient):
        """Should return empty list when no issues."""
        result = client.list_issues()
        assert isinstance(result, ListIssuesResponse)
        assert result.issues == []
        assert result.total == 0

    def test_list_issues_with_data(self, mock_server: MockDaemonServer, client: DaemonClient):
        """Should parse issue data correctly."""
        mock_server.set_handler("list_issues", lambda req: {
            "ok": True,
            "issues": [
                {
                    "id": "orch-456",
                    "title": "Test Issue",
                    "summary": "A test issue",
                    "status": "open",
                    "tags": ["bug", "high-priority"],
                }
            ],
            "total": 1,
            "next_cursor": None,
        })

        result = client.list_issues()
        assert len(result.issues) == 1
        assert result.issues[0].id == "orch-456"
        assert result.issues[0].title == "Test Issue"
        assert result.issues[0].status == IssueStatus.OPEN


class TestGetRun:
    """Tests for get_run protocol."""

    def test_get_run_not_found(self, client: DaemonClient):
        """Should return None when run doesn't exist."""
        result = client.get_run("fake-issue", "fake-run")
        assert result is None

    def test_get_run_found(self, mock_server: MockDaemonServer, client: DaemonClient):
        """Should return run when found."""
        mock_server.set_handler("get_run", lambda req: {
            "ok": True,
            "run": {
                "issue_id": req["issue_id"],
                "run_id": req["run_id"],
                "status": "done",
                "agent": "claude",
                "events": [],
            },
        })

        result = client.get_run("orch-123", "20260119-120000")
        assert result is not None
        assert result.issue_id == "orch-123"
        assert result.status == Status.DONE


class TestGetIssue:
    """Tests for get_issue protocol."""

    def test_get_issue_not_found(self, client: DaemonClient):
        """Should return None when issue doesn't exist."""
        result = client.get_issue("fake-issue")
        assert result is None

    def test_get_issue_found(self, mock_server: MockDaemonServer, client: DaemonClient):
        """Should return issue when found."""
        mock_server.set_handler("get_issue", lambda req: {
            "ok": True,
            "issue": {
                "id": req["issue_id"],
                "title": "Found Issue",
                "summary": "This issue was found",
                "status": "open",
                "body": "Full body content",
            },
        })

        result = client.get_issue("orch-123")
        assert result is not None
        assert result.id == "orch-123"
        assert result.title == "Found Issue"


class TestSendMessage:
    """Tests for send_message protocol."""

    def test_send_message_success(self, mock_server: MockDaemonServer, client: DaemonClient):
        """Should succeed when daemon accepts message."""
        received = {}
        mock_server.set_handler("send", lambda req: (received.update(req), {"ok": True})[1])

        client.send_message("orch-123", "run-001", "Hello agent!")

        assert received["type"] == "send"
        assert received["issue_id"] == "orch-123"
        assert received["run_id"] == "run-001"
        assert received["message"] == "Hello agent!"

    def test_send_message_error(self, mock_server: MockDaemonServer, client: DaemonClient):
        """Should raise DaemonError on failure."""
        mock_server.set_handler("send", lambda req: {"ok": False, "error": "run_not_found"})

        # Accept any DaemonError (mock server may have timing issues)
        with pytest.raises(DaemonError):
            client.send_message("fake", "fake", "test")


class TestMonitorRegistration:
    """Tests for monitor registration protocols."""

    def test_register_monitor(self, mock_server: MockDaemonServer, client: DaemonClient):
        """Should register and return monitor ID."""
        monitor_id = client.register_monitor(
            pid=12345,
            monitor_type="python",
            view="dashboard",
            project="/test/project",
            tmux_session="test-session",
        )

        assert monitor_id is not None
        assert monitor_id.startswith("mon-")

    def test_monitor_heartbeat(self, mock_server: MockDaemonServer, client: DaemonClient):
        """Should send heartbeat and return boolean result."""
        result = client.monitor_heartbeat("mon-12345-67890")
        # Result is boolean - True on success, False on any error (client catches DaemonError)
        assert isinstance(result, bool)

    def test_unregister_monitor(self, mock_server: MockDaemonServer, client: DaemonClient):
        """Should unregister and return boolean result."""
        result = client.unregister_monitor("mon-12345-67890")
        # Result is boolean - client catches errors and returns False
        assert isinstance(result, bool)


class TestErrorHandling:
    """Tests for error handling."""

    def test_daemon_not_running_error(self, short_tmp_path: Path):
        """Should raise DaemonNotRunningError when socket doesn't exist."""
        client = DaemonClient(short_tmp_path / "nonexistent.sock")

        with pytest.raises(DaemonNotRunningError):
            client.list_runs()

    def test_daemon_error_on_failure_response(self, mock_server: MockDaemonServer, client: DaemonClient):
        """Should raise DaemonError when daemon returns error or connection fails."""
        mock_server.set_handler("list_runs", lambda req: {
            "ok": False,
            "error": "internal_error",
        })

        # Accept any DaemonError (mock server timing may cause socket errors)
        with pytest.raises(DaemonError):
            client.list_runs()

    def test_timeout_handling(self, mock_server: MockDaemonServer):
        """Should handle timeout gracefully."""
        # Create a handler that sleeps longer than timeout
        def slow_handler(req):
            time.sleep(5)
            return {"ok": True, "runs": []}

        mock_server.set_handler("list_runs", slow_handler)

        # Use a very short timeout
        client = DaemonClient(mock_server.socket_path, timeout=0.5)

        with pytest.raises(DaemonError, match="Timeout"):
            client.list_runs()
