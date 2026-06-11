"""Tests for local control-agent launch behavior in monitor layout launchers."""

import json
from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest
from returns.result import Failure, Success

from orch_monitor.multiplexer import MultiplexerType
from orch_monitor.orch_api import ControlAgentConfig


def _agent_pane_command(commands_sent: list) -> str | None:
    """Extract the control-agent pane command (tmux pane :0.2) from send-keys.

    Matching on the pane target (rather than a substring like "claude") is robust
    against worktree paths that happen to contain agent names (e.g. a ".claude"
    path segment leaking into the monitor panes' python path).
    """
    for cmd in commands_sent:
        if "send-keys" not in cmd:
            continue
        parts = [str(c) for c in cmd]
        # tmux send-keys -t <session>:0.2 <command> Enter
        if any(p.endswith(":0.2") for p in parts):
            try:
                target_idx = next(
                    i for i, p in enumerate(parts) if p.endswith(":0.2")
                )
            except StopIteration:
                continue
            # The command string follows the target argument.
            if target_idx + 1 < len(parts):
                return parts[target_idx + 1]
    return None


def mock_daemon_client_with_config(
    *,
    agent: str = "opencode",
    model: str = "",
    model_variant: str = "",
    extra_args: list[str] | None = None,
    prompt_content: str = "",
    codex_home: str = "",
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
            codex_home=codex_home,
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

        agent_cmd = _agent_pane_command(commands_sent)

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

        agent_cmd = _agent_pane_command(commands_sent)

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

        agent_cmd = _agent_pane_command(commands_sent)

        assert agent_cmd is not None
        assert "--dangerously-skip-permissions" in agent_cmd
        assert "--prompt" not in agent_cmd


class TestControlAgentCodexHome:
    def test_tmux_codex_control_command_exports_codex_home(self):
        from orch_monitor.__main__ import TmuxLayoutLauncher

        launcher = TmuxLayoutLauncher()
        commands_sent = []

        def mock_run(args, **kwargs):
            commands_sent.append(args)
            result = MagicMock()
            result.returncode = 0
            return result

        mock_daemon = mock_daemon_client_with_config(
            agent="codex",
            model="openai/gpt-5.3-codex",
            extra_args=["--yolo"],
            codex_home="/home/tester/.codex-company",
        )

        with (
            patch("subprocess.run", side_effect=mock_run),
            patch("orch_monitor.__main__._get_daemon_client", return_value=mock_daemon),
        ):
            launcher.launch_layout(
                session_name="test-session",
                project_root=Path("/tmp/test"),
                vault_path=Path("/tmp/vault"),
                agent="codex",
                cwd="/tmp/test",
                new_control_agent=False,
            )

        agent_cmd = _agent_pane_command(commands_sent)

        assert agent_cmd is not None
        assert "export CODEX_HOME=/home/tester/.codex-company;" in agent_cmd
        assert "codex" in agent_cmd

    def test_build_command_no_codex_home_when_unset(self):
        from orch_monitor.__main__ import _build_local_control_agent_command

        cmd = _build_local_control_agent_command(agent="codex", codex_home="")
        assert "CODEX_HOME" not in cmd
        assert cmd.startswith("codex")


class TestControlAgentHostConstraintFailFast:
    """Daemon-enforced codex profile allowed_targets denial must surface, not launch."""

    def _denial_daemon(self):
        mock_daemon = MagicMock()
        mock_daemon.is_available.return_value = True
        mock_daemon.get_control_agent_config.return_value = Failure(
            'codex profile "company" may only run on targets [mac], '
            'not local host (target "zeus"); the control agent runs locally'
        )
        return mock_daemon

    def test_resolve_raises_on_policy_denial(self):
        from orch_monitor.__main__ import (
            ControlAgentLaunchError,
            _resolve_local_control_agent_command,
        )

        with pytest.raises(ControlAgentLaunchError) as exc:
            _resolve_local_control_agent_command(
                daemon=self._denial_daemon(),
                project_root=Path("/tmp/test"),
                cwd="/tmp/test",
                fallback_agent="codex",
                agent_override="",
            )
        assert "company" in str(exc.value)
        assert "zeus" in str(exc.value)

    def test_tmux_launch_does_not_send_agent_command_on_denial(self):
        from orch_monitor.__main__ import (
            ControlAgentLaunchError,
            TmuxLayoutLauncher,
        )

        launcher = TmuxLayoutLauncher()
        commands_sent = []

        def mock_run(args, **kwargs):
            commands_sent.append(args)
            result = MagicMock()
            result.returncode = 0
            return result

        with (
            patch("subprocess.run", side_effect=mock_run),
            patch(
                "orch_monitor.__main__._get_daemon_client",
                return_value=self._denial_daemon(),
            ),
        ):
            with pytest.raises(ControlAgentLaunchError):
                launcher.launch_layout(
                    session_name="test-session",
                    project_root=Path("/tmp/test"),
                    vault_path=Path("/tmp/vault"),
                    agent="codex",
                    cwd="/tmp/test",
                    new_control_agent=False,
                )

        # Fail-fast happens before any tmux session/pane is created or any agent
        # command is sent — the company codex account must not launch on zeus.
        assert not any("new-session" in cmd for cmd in commands_sent)
        assert not any("send-keys" in cmd for cmd in commands_sent)

    def test_launch_monitor_layout_exits_on_denial(self):
        from orch_monitor import __main__ as m

        denial = self._denial_daemon()

        class _DenyingLauncher:
            def has_session(self, session_name):
                return False

            def launch_layout(self, *args, **kwargs):
                from orch_monitor.__main__ import _resolve_local_control_agent_command

                _resolve_local_control_agent_command(
                    daemon=denial,
                    project_root=Path("/tmp/test"),
                    cwd="/tmp/test",
                    fallback_agent="codex",
                    agent_override="",
                )

        with (
            patch.object(m, "get_layout_launcher", return_value=_DenyingLauncher()),
            patch.object(m, "validate_multiplexer_config"),
            patch.object(
                m,
                "get_default_multiplexer_type",
                return_value=MultiplexerType.TMUX,
            ),
        ):
            with pytest.raises(SystemExit) as exc:
                m.launch_monitor_layout(
                    project_root=Path("/tmp/test"),
                    monitor_session_name="test-session",
                    agent="codex",
                    multiplexer=MultiplexerType.TMUX,
                    show_spinner=False,
                )
        assert exc.value.code == 1


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
