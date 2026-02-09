"""Tests for monitor heartbeat reconnect behavior."""

from unittest.mock import MagicMock

from returns.result import Failure, Success

from orch_monitor.daemon_api import MonitorHeartbeat
from orch_monitor.types import ProtoDaemonConnectionRefusedError


class _FakeStopEvent:
    def __init__(self, responses: list[bool]):
        self._responses = iter(responses)

    def wait(self, timeout: float = 0.0) -> bool:
        return next(self._responses)


class TestMonitorHeartbeatReconnect:
    def test_reconnects_once_daemon_becomes_healthy(self):
        client = MagicMock()
        client.is_available.side_effect = [False, True]
        client.monitor_heartbeat.return_value = Failure(
            ProtoDaemonConnectionRefusedError(
                "Daemon connection refused at /tmp/orch/daemon.sock"
            )
        )
        client.register_monitor.return_value = Success("monitor-new")

        heartbeat = MonitorHeartbeat(client, project="test-project", view="runs")
        heartbeat._monitor_id = "monitor-old"
        heartbeat._session_name = "orch-monitor-test"
        heartbeat._stop_event = _FakeStopEvent([False, False, True])

        heartbeat._heartbeat_loop()

        client.monitor_heartbeat.assert_called_once_with("monitor-old")
        client.register_monitor.assert_called_once()
        assert heartbeat._monitor_id == "monitor-new"
