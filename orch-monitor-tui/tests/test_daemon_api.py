"""Tests for daemon API error mapping behavior."""

from returns.result import Failure

from orch_monitor.daemon_api import _map_daemon_error
from orch_monitor.orch_api import DaemonNotRunningError, OrchError
from orch_monitor.types import (
    ProtoDaemonConnectionRefusedError,
    ProtoDaemonTimeoutError,
)


class TestDaemonErrorMapping:
    def test_connection_refused_error_is_preserved(self):
        result = _map_daemon_error(
            ProtoDaemonConnectionRefusedError(
                "Daemon connection refused at /tmp/orch/daemon.sock"
            )
        )

        assert isinstance(result, Failure)
        err = result.failure()
        assert isinstance(err, DaemonNotRunningError)
        assert "connection refused" in str(err).lower()

    def test_timeout_error_is_distinct_from_not_running(self):
        result = _map_daemon_error(
            ProtoDaemonTimeoutError("Daemon request timed out at /tmp/orch/daemon.sock")
        )

        assert isinstance(result, Failure)
        err = result.failure()
        assert isinstance(err, OrchError)
        assert not isinstance(err, DaemonNotRunningError)
        assert "timed out" in str(err).lower()
