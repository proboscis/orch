"""Tests for daemon API behavior."""

from unittest.mock import MagicMock

from returns.result import Failure, Success

from orch_monitor import daemon_api
from orch_monitor.daemon_api import DaemonOrchAPI, _map_daemon_error
from orch_monitor.multiplexer import MultiplexerType
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


class TestMonitorRegistration:
    def test_uses_verified_project_and_session_without_duplicate_registration(
        self, monkeypatch
    ):
        daemon = MagicMock()
        daemon.is_available.return_value = True
        daemon.register_monitor.return_value = Success("mon-123")
        daemon.unregister_monitor.return_value = Success(True)
        api = DaemonOrchAPI.__new__(DaemonOrchAPI)
        api._daemon = daemon
        api._project_scope = "proboscis-orch"
        api._monitor_session_name = "orch-monitor-proboscis-orch"
        api._monitor_heartbeat = None
        multiplexer = MagicMock()
        multiplexer.get_current_session.return_value = "orch-monitor-proboscis-orch"
        monkeypatch.setattr(
            daemon_api,
            "detect_current_multiplexer",
            lambda: MultiplexerType.TMUX,
        )
        monkeypatch.setattr(
            daemon_api,
            "get_multiplexer",
            lambda _: multiplexer,
        )

        result = api.register_monitor(
            pid=123,
            monitor_type="python",
            view="runs",
            project="/local/path/to/orch",
        )

        assert result == Success("mon-123")
        daemon.register_monitor.assert_called_once_with(
            pid=123,
            monitor_type="python",
            view="runs",
            project="proboscis-orch",
            session_name="orch-monitor-proboscis-orch",
        )

        unregister_result = api.unregister_monitor("mon-123")

        assert unregister_result == Success(None)
        daemon.unregister_monitor.assert_called_once_with("mon-123")

    def test_standalone_pane_does_not_claim_layout_session(self, monkeypatch):
        daemon = MagicMock()
        daemon.register_monitor.return_value = Success("mon-standalone")
        daemon.unregister_monitor.return_value = Success(True)
        api = DaemonOrchAPI.__new__(DaemonOrchAPI)
        api._daemon = daemon
        api._project_scope = "proboscis-orch"
        api._monitor_session_name = "orch-monitor-proboscis-orch"
        api._monitor_heartbeat = None
        multiplexer = MagicMock()
        multiplexer.get_current_session.return_value = "unrelated-tmux-session"
        monkeypatch.setattr(
            daemon_api,
            "detect_current_multiplexer",
            lambda: MultiplexerType.TMUX,
        )
        monkeypatch.setattr(
            daemon_api,
            "get_multiplexer",
            lambda _: multiplexer,
        )

        result = api.register_monitor(
            pid=456,
            monitor_type="python",
            view="runs",
            project="/local/path/to/orch",
        )

        assert result == Success("mon-standalone")
        daemon.register_monitor.assert_called_once_with(
            pid=456,
            monitor_type="python",
            view="runs",
            project="proboscis-orch",
            session_name="",
        )
        assert api.unregister_monitor("mon-standalone") == Success(None)

    def test_unregister_propagates_heartbeat_unregister_failure(self, monkeypatch):
        daemon = MagicMock()
        daemon.register_monitor.return_value = Success("mon-123")
        daemon.unregister_monitor.return_value = Failure(
            ProtoDaemonConnectionRefusedError("daemon connection refused")
        )
        api = DaemonOrchAPI.__new__(DaemonOrchAPI)
        api._daemon = daemon
        api._project_scope = "proboscis-orch"
        api._monitor_session_name = "orch-monitor-proboscis-orch"
        api._monitor_heartbeat = None
        monkeypatch.setattr(
            daemon_api,
            "_verified_monitor_session_name",
            lambda expected_session_name: expected_session_name,
        )

        register_result = api.register_monitor(
            pid=123,
            monitor_type="python",
            view="runs",
            project="proboscis-orch",
        )
        unregister_result = api.unregister_monitor("mon-123")

        assert register_result == Success("mon-123")
        assert isinstance(unregister_result, Failure)
        assert isinstance(unregister_result.failure(), DaemonNotRunningError)
        assert "connection refused" in str(unregister_result.failure())
