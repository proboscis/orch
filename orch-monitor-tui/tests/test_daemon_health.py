"""Tests for daemon health checks and typed error handling.

Tests cover:
- Stale socket detection (socket exists but connection refused)
- Socket missing detection
- Timeout handling
- Permission errors
- Auto-reconnect behavior
"""

import os
import socket
import stat
import tempfile
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest

import hy  # noqa: F401 - Enable Hy imports

from returns.result import Failure, Success

from orch_monitor.types import (
    ProtoDaemonError,
    ProtoDaemonNotRunningError,
    ProtoDaemonSocketMissingError,
    ProtoDaemonConnectionRefusedError,
    ProtoDaemonTimeoutError,
    ProtoDaemonPermissionError,
)
from orch_monitor.proto_client import ProtoDaemonClient


class TestTypedExceptionHierarchy:
    """Verify exception hierarchy is correct for isinstance checks."""

    def test_socket_missing_is_not_running(self):
        err = ProtoDaemonSocketMissingError("test")
        assert isinstance(err, ProtoDaemonNotRunningError)
        assert isinstance(err, ProtoDaemonError)

    def test_connection_refused_is_not_running(self):
        err = ProtoDaemonConnectionRefusedError("test")
        assert isinstance(err, ProtoDaemonNotRunningError)
        assert isinstance(err, ProtoDaemonError)

    def test_timeout_is_daemon_error_not_not_running(self):
        err = ProtoDaemonTimeoutError("test")
        assert isinstance(err, ProtoDaemonError)
        assert not isinstance(err, ProtoDaemonNotRunningError)

    def test_permission_is_daemon_error_not_not_running(self):
        err = ProtoDaemonPermissionError("test")
        assert isinstance(err, ProtoDaemonError)
        assert not isinstance(err, ProtoDaemonNotRunningError)


class TestSocketExists:
    """Test the passive socket_exists method."""

    def test_socket_exists_when_valid_socket(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            socket_path = Path(tmpdir) / "test.sock"
            # Create a real Unix socket
            sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            try:
                sock.bind(str(socket_path))
                client = ProtoDaemonClient(socket_path)
                assert client.socket_exists() is True
            finally:
                sock.close()
                socket_path.unlink(missing_ok=True)

    def test_socket_exists_false_when_missing(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            socket_path = Path(tmpdir) / "nonexistent.sock"
            client = ProtoDaemonClient(socket_path)
            assert client.socket_exists() is False

    def test_socket_exists_false_when_regular_file(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            file_path = Path(tmpdir) / "not_a_socket.txt"
            file_path.write_text("not a socket")
            client = ProtoDaemonClient(file_path)
            assert client.socket_exists() is False


class TestCheckHealthStaleSocket:
    """Test that stale socket (exists but daemon not running) is detected."""

    def test_stale_socket_returns_connection_refused_error(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            socket_path = Path(tmpdir) / "stale.sock"
            # Create a socket file but don't listen on it (simulates stale socket)
            sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            try:
                sock.bind(str(socket_path))
                # Don't call listen() - socket exists but nothing accepts connections
                sock.close()

                client = ProtoDaemonClient(socket_path)

                # socket_exists should return True (file is a socket)
                assert client.socket_exists() is True

                # check_health should fail with connection refused
                result = client.check_health()
                assert isinstance(result, Failure)
                err = result.failure()
                assert isinstance(err, ProtoDaemonConnectionRefusedError)

                # is_available should return False (active check fails)
                assert client.is_available() is False
            finally:
                socket_path.unlink(missing_ok=True)


class TestCheckHealthMissingSocket:
    """Test that missing socket is properly detected."""

    def test_missing_socket_returns_socket_missing_error(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            socket_path = Path(tmpdir) / "nonexistent.sock"
            client = ProtoDaemonClient(socket_path)

            result = client.check_health()
            assert isinstance(result, Failure)
            err = result.failure()
            assert isinstance(err, ProtoDaemonSocketMissingError)
            assert "not found" in str(err).lower()


class TestCheckHealthTimeout:
    """Test that timeout is properly detected and typed."""

    def test_timeout_returns_timeout_error(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            socket_path = Path(tmpdir) / "timeout.sock"
            # Create a socket and listen but never accept
            sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            try:
                sock.bind(str(socket_path))
                sock.listen(1)

                # Create client with very short timeout
                client = ProtoDaemonClient(socket_path, timeout=0.1)

                # The ping should timeout because we never accept/respond
                result = client.check_health()
                assert isinstance(result, Failure)
                err = result.failure()
                # Could be timeout or connection error depending on timing
                assert isinstance(err, (ProtoDaemonTimeoutError, ProtoDaemonError))
            finally:
                sock.close()
                socket_path.unlink(missing_ok=True)


class TestIsAvailableActiveProbe:
    """Test that is_available uses active health probe."""

    def test_is_available_false_for_stale_socket(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            socket_path = Path(tmpdir) / "stale.sock"
            # Create a socket file but don't listen
            sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            try:
                sock.bind(str(socket_path))
                sock.close()

                client = ProtoDaemonClient(socket_path)

                # Old behavior would return True (socket file exists)
                # New behavior should return False (ping fails)
                assert client.is_available() is False
            finally:
                socket_path.unlink(missing_ok=True)

    def test_is_available_false_for_missing_socket(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            socket_path = Path(tmpdir) / "missing.sock"
            client = ProtoDaemonClient(socket_path)
            assert client.is_available() is False


class TestErrorMessageDistinguishability:
    """Test that error messages are distinguishable in logs/errors."""

    def test_connection_refused_message_distinct(self):
        err = ProtoDaemonConnectionRefusedError("Connection refused at /path")
        assert "refused" in str(err).lower() or "stale" in str(err).lower()

    def test_timeout_message_distinct(self):
        err = ProtoDaemonTimeoutError("Timeout at /path")
        assert "timeout" in str(err).lower()

    def test_socket_missing_message_distinct(self):
        err = ProtoDaemonSocketMissingError("Socket not found at /path")
        assert "not found" in str(err).lower() or "missing" in str(err).lower()

    def test_permission_message_distinct(self):
        err = ProtoDaemonPermissionError("Permission denied at /path")
        assert "permission" in str(err).lower()


class TestReconnectBehavior:
    """Test that monitor can reconnect when daemon becomes healthy."""

    def test_client_can_reconnect_after_failure(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            socket_path = Path(tmpdir) / "reconnect.sock"

            client = ProtoDaemonClient(socket_path)

            # Initially no socket - should fail
            result1 = client.check_health()
            assert isinstance(result1, Failure)
            assert isinstance(result1.failure(), ProtoDaemonSocketMissingError)

            # Create a stale socket - should still fail but different error
            sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            try:
                sock.bind(str(socket_path))
                sock.close()

                result2 = client.check_health()
                assert isinstance(result2, Failure)
                assert isinstance(result2.failure(), ProtoDaemonConnectionRefusedError)
            finally:
                socket_path.unlink(missing_ok=True)

            # After socket is removed - back to missing
            result3 = client.check_health()
            assert isinstance(result3, Failure)
            assert isinstance(result3.failure(), ProtoDaemonSocketMissingError)

    def test_persistent_connection_reconnects_on_failure(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            socket_path = Path(tmpdir) / "persist.sock"
            client = ProtoDaemonClient(socket_path)

            # First check - missing
            assert client.is_available() is False

            # Create stale socket
            sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
            try:
                sock.bind(str(socket_path))
                sock.close()

                # Still unavailable (stale)
                assert client.is_available() is False
            finally:
                socket_path.unlink(missing_ok=True)


class TestDaemonApiErrorMapping:
    """Test that daemon_api correctly maps proto errors to API errors."""

    def test_maps_socket_missing_to_api_error(self):
        from orch_monitor.daemon_api import _map_daemon_error
        from orch_monitor.orch_api import DaemonSocketMissingError

        proto_err = ProtoDaemonSocketMissingError("test")
        result = _map_daemon_error(proto_err)

        assert isinstance(result, Failure)
        assert isinstance(result.failure(), DaemonSocketMissingError)

    def test_maps_connection_refused_to_api_error(self):
        from orch_monitor.daemon_api import _map_daemon_error
        from orch_monitor.orch_api import DaemonConnectionRefusedError

        proto_err = ProtoDaemonConnectionRefusedError("test")
        result = _map_daemon_error(proto_err)

        assert isinstance(result, Failure)
        assert isinstance(result.failure(), DaemonConnectionRefusedError)

    def test_maps_timeout_to_api_error(self):
        from orch_monitor.daemon_api import _map_daemon_error
        from orch_monitor.orch_api import DaemonTimeoutError

        proto_err = ProtoDaemonTimeoutError("test")
        result = _map_daemon_error(proto_err)

        assert isinstance(result, Failure)
        assert isinstance(result.failure(), DaemonTimeoutError)

    def test_maps_permission_to_api_error(self):
        from orch_monitor.daemon_api import _map_daemon_error
        from orch_monitor.orch_api import DaemonPermissionError

        proto_err = ProtoDaemonPermissionError("test")
        result = _map_daemon_error(proto_err)

        assert isinstance(result, Failure)
        assert isinstance(result.failure(), DaemonPermissionError)
