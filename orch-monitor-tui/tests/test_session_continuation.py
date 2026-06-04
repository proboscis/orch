"""Tests for local control-agent launch behavior in monitor layout launchers."""

import json
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest
from returns.result import Failure, Success

from orch_monitor.orch_api import ControlAgentConfig


def mock_daemon_client_with_config(
    *,
    agent: str = "opencode",
    model: str = "",
    model_variant: str = "",
    extra_args: list[str] | None = None,
    prompt_content: str = "",
):
    mock_daemon = MagicMock()
    mock_daemon.is_available.return_value = True
    mock_daemon.get_control_agent_config.return_value = Success(
        ControlAgentConfig(
            prompt_content=prompt_content,
            agent=agent,
            model=model,
            model_variant=model_variant,
            extra_args=extra_args or [],
        )
    )
    return mock_daemon


class TestLocalCommandFromConfig:
    def test_tmux_launcher_builds_opencode_command_from_config(self):
        from orch_monitor.__main__ import TmuxLayoutLauncher

        launcher = TmuxLayoutLauncher()
        commands_sent = []

        def mock_run(args, **kwargs):
            commands_sent.append(args)
            result = MagicMock()
            result.returncode = 0
            return result

        mock_daemon = mock_daemon_client_with_config(
            agent="opencode",
            model="openai/gpt-5.3-codex",
            model_variant="fast",
            extra_args=["--permission-mode", "auto"],
            prompt_content="# control prompt",
        )

        with (
            patch("subprocess.run", side_effect=mock_run),
            patch("orch_monitor.__main__._get_daemon_client", return_value=mock_daemon),
        ):
            launcher.launch_layout(
                session_name="test-session",
                project_root=Path("/tmp/test"),
                vault_path=Path("/tmp/vault"),
                agent="opencode",
                cwd="/tmp/test",
                new_control_agent=False,
            )

        agent_cmd = None
        for cmd in commands_sent:
            if "send-keys" in cmd and "opencode" in str(cmd):
                agent_cmd = " ".join(str(c) for c in cmd)
                break

        assert agent_cmd is not None
        assert "opencode" in agent_cmd
        assert "--permission-mode auto" in agent_cmd
        assert "--model openai/gpt-5.3-codex" in agent_cmd
        assert "--model-variant fast" in agent_cmd
        assert "--prompt" in agent_cmd
        mock_daemon.get_control_agent_launch.assert_not_called()

    def test_tmux_launcher_uses_agent_override_over_config(self):
        from orch_monitor.__main__ import TmuxLayoutLauncher

        launcher = TmuxLayoutLauncher()
        commands_sent = []

        def mock_run(args, **kwargs):
            commands_sent.append(args)
            result = MagicMock()
            result.returncode = 0
            return result

        mock_daemon = mock_daemon_client_with_config(
            agent="opencode",
            extra_args=["--dangerously-skip-permissions"],
        )

        with (
            patch("subprocess.run", side_effect=mock_run),
            patch("orch_monitor.__main__._get_daemon_client", return_value=mock_daemon),
        ):
            launcher.launch_layout(
                session_name="test-session",
                project_root=Path("/tmp/test"),
                vault_path=Path("/tmp/vault"),
                agent="opencode",
                agent_override="claude",
                cwd="/tmp/test",
                new_control_agent=False,
            )

        agent_cmd = None
        for cmd in commands_sent:
            if "send-keys" in cmd and "claude" in str(cmd):
                agent_cmd = " ".join(str(c) for c in cmd)
                break

        assert agent_cmd is not None
        assert "claude" in agent_cmd
        assert "opencode" not in agent_cmd
        mock_daemon.get_control_agent_launch.assert_not_called()


class TestFallbackControlAgentCommand:
    def test_tmux_fallback_when_config_rpc_fails(self):
        from orch_monitor.__main__ import TmuxLayoutLauncher

        launcher = TmuxLayoutLauncher()
        commands_sent = []

        def mock_run(args, **kwargs):
            commands_sent.append(args)
            result = MagicMock()
            result.returncode = 0
            return result

        mock_daemon = MagicMock()
        mock_daemon.is_available.return_value = True
        mock_daemon.get_control_agent_config.return_value = Failure("daemon failure")

        with (
            patch("subprocess.run", side_effect=mock_run),
            patch("orch_monitor.__main__._get_daemon_client", return_value=mock_daemon),
        ):
            launcher.launch_layout(
                session_name="test-session",
                project_root=Path("/tmp/test"),
                vault_path=Path("/tmp/vault"),
                agent="claude",
                cwd="/tmp/test",
                new_control_agent=False,
            )

        agent_cmd = None
        for cmd in commands_sent:
            if "send-keys" in cmd and "claude" in str(cmd):
                agent_cmd = " ".join(str(c) for c in cmd)
                break

        assert agent_cmd is not None
        assert "--dangerously-skip-permissions" in agent_cmd
        assert "--prompt" not in agent_cmd


class TestLocalSessionState:
    def test_save_and_load_control_session(self, tmp_path: Path):
        from orch_monitor.__main__ import load_control_session, save_control_session

        assert save_control_session(tmp_path, "ses-123", "opencode") is True
        assert load_control_session(tmp_path) == "ses-123"
        assert load_control_session(tmp_path, agent_type="opencode") == "ses-123"
        assert load_control_session(tmp_path, agent_type="claude") is None

    def test_new_control_agent_clears_session_file(self, tmp_path: Path):
        from orch_monitor.__main__ import launch_monitor_layout

        session_file = tmp_path / ".orch" / "control-session.json"
        session_file.parent.mkdir(parents=True, exist_ok=True)
        session_file.write_text(
            json.dumps({"session_id": "ses-old", "agent_type": "opencode"})
        )

        mock_launcher = MagicMock()
        mock_launcher.has_session.return_value = True

        with (
            patch("orch_monitor.__main__.get_default_multiplexer_type"),
            patch("orch_monitor.__main__.validate_multiplexer_config"),
            patch(
                "orch_monitor.__main__.get_layout_launcher", return_value=mock_launcher
            ),
        ):
            launch_monitor_layout(
                project_root=tmp_path,
                monitor_session_name="test-ses",
                vault_path=tmp_path,
                new=True,
                new_control_agent=True,
                show_spinner=False,
            )

        assert not session_file.exists()
        mock_launcher.kill_session.assert_called_once()
        mock_launcher.launch_layout.assert_called_once()


class TestNewLayoutPreflightGuard:
    def test_new_flag_exits_without_local_session(self, tmp_path: Path):
        from orch_monitor.__main__ import launch_monitor_layout

        mock_launcher = MagicMock()
        mock_launcher.has_session.return_value = True

        with (
            patch("orch_monitor.__main__.get_default_multiplexer_type"),
            patch("orch_monitor.__main__.validate_multiplexer_config"),
            patch(
                "orch_monitor.__main__.get_layout_launcher", return_value=mock_launcher
            ),
            pytest.raises(SystemExit) as exc_info,
        ):
            launch_monitor_layout(
                project_root=tmp_path,
                monitor_session_name="test-ses",
                vault_path=tmp_path,
                new=True,
                new_control_agent=False,
                show_spinner=False,
            )

        assert exc_info.value.code == 1
        mock_launcher.kill_session.assert_not_called()

    def test_new_flag_proceeds_with_local_session(self, tmp_path: Path):
        from orch_monitor.__main__ import launch_monitor_layout, save_control_session

        mock_launcher = MagicMock()
        mock_launcher.has_session.return_value = True
        assert save_control_session(tmp_path, "ses-keep", "opencode")

        with (
            patch("orch_monitor.__main__.get_default_multiplexer_type"),
            patch("orch_monitor.__main__.validate_multiplexer_config"),
            patch(
                "orch_monitor.__main__.get_layout_launcher", return_value=mock_launcher
            ),
        ):
            launch_monitor_layout(
                project_root=tmp_path,
                monitor_session_name="test-ses",
                vault_path=tmp_path,
                new=True,
                new_control_agent=False,
                show_spinner=False,
            )

        mock_launcher.kill_session.assert_called_once()
        mock_launcher.launch_layout.assert_called_once()
