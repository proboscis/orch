"""Tests for orch-monitor monitor-management commands."""

import argparse
import os
from pathlib import Path

import pytest
from returns.result import Failure, Result, Success

from orch_monitor.__main__ import (
    _build_parser,
    _print_monitor_table,
    _run_monitor_action,
    main,
)
from orch_monitor.client_bootstrap import ClientBootstrap
from orch_monitor.orch_api import MonitorInfo, OrchError


class _FakeMonitorAPI:
    def __init__(self) -> None:
        self.list_result: Result[list[MonitorInfo], OrchError] = Success([])
        self.kill_result: Result[int, OrchError] = Success(0)
        self.list_calls: list[tuple[str, bool]] = []
        self.kill_calls: list[tuple[str, bool, str]] = []

    def list_monitors(self, project: str, list_all: bool = False):
        self.list_calls.append((project, list_all))
        return self.list_result

    def kill_monitor(self, monitor_id: str, kill_all: bool, project: str):
        self.kill_calls.append((monitor_id, kill_all, project))
        return self.kill_result


def _args(**overrides: object) -> argparse.Namespace:
    values: dict[str, object] = {
        "list_monitors": False,
        "kill_monitor": None,
        "kill_all": False,
    }
    values.update(overrides)
    return argparse.Namespace(**values)


def test_list_prints_required_monitor_fields_in_daemon_order(capsys) -> None:
    api = _FakeMonitorAPI()
    api.list_result = Success(
        [
            MonitorInfo(
                id="mon-older",
                pid=101,
                project="proboscis-orch",
                view="runs",
                session_name="orch-monitor-one",
                last_seen_unix=1_700_000_000,
            ),
            MonitorInfo(
                id="mon-newer",
                pid=202,
                project="proboscis-orch",
                view="issues",
                session_name="orch-monitor-two",
                last_seen_unix=1_700_000_100,
            ),
        ]
    )

    exit_code: int = _run_monitor_action(
        api, _args(list_monitors=True), "proboscis-orch"
    )

    assert exit_code == 0
    assert api.list_calls == [("proboscis-orch", False)]
    output: str = capsys.readouterr().out
    assert "ID" in output
    assert "PID" in output
    assert "PROJECT" in output
    assert "VIEW" in output
    assert "SESSION" in output
    assert "LAST_SEEN" in output
    assert output.index("mon-older") < output.index("mon-newer")
    assert "2023-11-14T22:13:20Z" in output


def test_print_monitor_table_reports_empty_registry(capsys) -> None:
    _print_monitor_table([])

    assert capsys.readouterr().out == "No monitors running\n"


def test_kill_targets_one_monitor_in_project_scope(capsys) -> None:
    api = _FakeMonitorAPI()
    api.kill_result = Success(1)

    exit_code: int = _run_monitor_action(
        api, _args(kill_monitor="mon-123"), "proboscis-orch"
    )

    assert exit_code == 0
    assert api.kill_calls == [("mon-123", False, "proboscis-orch")]
    assert capsys.readouterr().out == "Killed monitor: mon-123\n"


def test_kill_all_is_project_scoped(capsys) -> None:
    api = _FakeMonitorAPI()
    api.kill_result = Success(2)

    exit_code: int = _run_monitor_action(
        api, _args(kill_all=True), "proboscis-orch"
    )

    assert exit_code == 0
    assert api.kill_calls == [("", True, "proboscis-orch")]
    assert capsys.readouterr().out == (
        "Killed 2 monitors for project proboscis-orch\n"
    )


def test_monitor_action_surfaces_daemon_failure(capsys) -> None:
    api = _FakeMonitorAPI()
    api.list_result = Failure(OrchError("remote daemon unavailable"))

    exit_code: int = _run_monitor_action(
        api, _args(list_monitors=True), "proboscis-orch"
    )

    assert exit_code == 1
    assert "remote daemon unavailable" in capsys.readouterr().err


def test_parser_rejects_multiple_monitor_actions() -> None:
    parser = _build_parser()

    with pytest.raises(SystemExit):
        parser.parse_args(["--list", "--kill", "mon-123"])


def test_main_routes_list_through_remote_bootstrap(monkeypatch, tmp_path: Path) -> None:
    monkeypatch.delenv("ORCH_PROJECT", raising=False)
    monkeypatch.delenv("ORCH_REMOTE", raising=False)
    bootstrap = ClientBootstrap(
        project_root=tmp_path,
        project_id="proboscis-orch",
        remote_addr="master.example:7777",
        socket_path=tmp_path / "daemon.sock",
        monitor_session_name="orch-monitor-proboscis-orch",
    )
    api = _FakeMonitorAPI()
    api.list_result = Success([])

    def fake_load_bootstrap() -> ClientBootstrap:
        assert os.environ["ORCH_REMOTE"] == "master.example:7777"
        return bootstrap

    monkeypatch.setattr(
        "sys.argv",
        ["orch-monitor", "--list", "--remote", "master.example:7777"],
    )
    monkeypatch.setattr(
        "orch_monitor.__main__.load_client_bootstrap", fake_load_bootstrap
    )
    monkeypatch.setattr(
        "orch_monitor.__main__.ensure_daemon", lambda *_: (True, "")
    )
    monkeypatch.setattr(
        "orch_monitor.__main__.create_orch_api",
        lambda *_args, **_kwargs: api,
    )

    main()

    assert api.list_calls == [("proboscis-orch", False)]
