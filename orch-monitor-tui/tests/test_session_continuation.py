"""Tests for session continuation behavior in control agent launchers.

These tests verify that orch-monitor uses the daemon's get_control_agent_launch API
to get the correct command for launching the control agent, which includes:
- Writing the control prompt file
- Resolving agent configuration
- Building the appropriate command with session IDs
"""

from pathlib import Path
from unittest.mock import MagicMock, patch

import pytest
from returns.result import Success, Failure

from orch_monitor.types import ControlAgentLaunch


def mock_daemon_client_with_launch(
    command, port=0, session_id=None, agent="opencode", resumed=False
):
    """Create a mock daemon client that returns a specific launch command."""
    mock_daemon = MagicMock()
    mock_daemon.is_available.return_value = True
    mock_daemon.get_control_agent_launch.return_value = Success(
        ControlAgentLaunch(
            command=command,
            prompt_file="/tmp/ORCH_CONTROL_PROMPT.md",
            port=port,
            session_id=session_id or "",
            agent=agent,
            resumed=resumed,
        )
    )
    return mock_daemon


def mock_daemon_client_unavailable():
    """Create a mock daemon client that is unavailable."""
    mock_daemon = MagicMock()
    mock_daemon.is_available.return_value = False
    return mock_daemon


class TestOpenCodeSessionContinuation:
    """Test OpenCode session continuation behavior with daemon API."""

    def test_tmux_launcher_uses_daemon_command_with_session(self):
        """When daemon returns a command with session ID, it should be used."""
        from orch_monitor.__main__ import TmuxLayoutLauncher

        launcher = TmuxLayoutLauncher()
        commands_sent = []

        def mock_run(args, **kwargs):
            commands_sent.append(args)
            mock_result = MagicMock()
            mock_result.returncode = 0
            return mock_result

        # Daemon returns command with session ID
        mock_daemon = mock_daemon_client_with_launch(
            command="opencode attach http://127.0.0.1:4096 --session session-123",
            port=4096,
            session_id="session-123",
            agent="opencode",
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

        # Find the send-keys command for the agent pane
        agent_cmd = None
        for cmd in commands_sent:
            if "send-keys" in cmd and "opencode" in str(cmd):
                agent_cmd = cmd
                break

        assert agent_cmd is not None, "Should have sent opencode command"
        cmd_str = " ".join(str(c) for c in agent_cmd)
        assert "--session session-123" in cmd_str, (
            f"Should use explicit --session flag, got: {cmd_str}"
        )
        assert "--continue" not in cmd_str, (
            f"Should NOT use --continue flag, got: {cmd_str}"
        )

    def test_tmux_launcher_uses_daemon_command_without_session(self):
        """When daemon returns a command without session ID, it should be used."""
        from orch_monitor.__main__ import TmuxLayoutLauncher

        launcher = TmuxLayoutLauncher()
        commands_sent = []

        def mock_run(args, **kwargs):
            commands_sent.append(args)
            mock_result = MagicMock()
            mock_result.returncode = 0
            return mock_result

        # Daemon returns command without session (new server)
        mock_daemon = mock_daemon_client_with_launch(
            command="opencode attach http://127.0.0.1:4096",
            port=4096,
            session_id=None,
            agent="opencode",
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

        # Find the send-keys command for the agent pane
        agent_cmd = None
        for cmd in commands_sent:
            if "send-keys" in cmd and "opencode" in str(cmd):
                agent_cmd = cmd
                break

        assert agent_cmd is not None, "Should have sent opencode command"
        cmd_str = " ".join(str(c) for c in agent_cmd)
        assert "opencode attach" in cmd_str, (
            f"Should use attach command, got: {cmd_str}"
        )
        assert "--continue" not in cmd_str, (
            f"Should NOT use --continue flag, got: {cmd_str}"
        )


class TestClaudeSessionContinuation:
    """Test Claude session continuation behavior with daemon API."""

    def test_tmux_launcher_uses_daemon_command_with_resume(self):
        """When daemon returns a claude --resume command, it should be used."""
        from orch_monitor.__main__ import TmuxLayoutLauncher

        launcher = TmuxLayoutLauncher()
        commands_sent = []

        def mock_run(args, **kwargs):
            commands_sent.append(args)
            mock_result = MagicMock()
            mock_result.returncode = 0
            return mock_result

        # Daemon returns claude command with resume
        mock_daemon = mock_daemon_client_with_launch(
            command="claude --dangerously-skip-permissions --resume claude-session-456",
            port=0,
            session_id="claude-session-456",
            agent="claude",
        )

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

        # Find the send-keys command for the agent pane
        agent_cmd = None
        for cmd in commands_sent:
            if "send-keys" in cmd and "claude" in str(cmd):
                agent_cmd = cmd
                break

        assert agent_cmd is not None, "Should have sent claude command"
        cmd_str = " ".join(str(c) for c in agent_cmd)
        assert "--resume claude-session-456" in cmd_str, (
            f"Should use --resume with session ID, got: {cmd_str}"
        )
        assert "--continue" not in cmd_str, (
            f"Should NOT use --continue flag, got: {cmd_str}"
        )

    def test_tmux_launcher_uses_daemon_fresh_claude_command(self):
        """When daemon returns a fresh claude command, it should be used."""
        from orch_monitor.__main__ import TmuxLayoutLauncher

        launcher = TmuxLayoutLauncher()
        commands_sent = []

        def mock_run(args, **kwargs):
            commands_sent.append(args)
            mock_result = MagicMock()
            mock_result.returncode = 0
            return mock_result

        # Daemon returns fresh claude command (no session)
        mock_daemon = mock_daemon_client_with_launch(
            command="claude --dangerously-skip-permissions \"ultrathink Please read 'ORCH_CONTROL_PROMPT.md'\"",
            port=0,
            session_id=None,
            agent="claude",
        )

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

        # Find the send-keys command for the agent pane
        agent_cmd = None
        for cmd in commands_sent:
            if "send-keys" in cmd and "claude" in str(cmd):
                agent_cmd = cmd
                break

        assert agent_cmd is not None, "Should have sent claude command"
        cmd_str = " ".join(str(c) for c in agent_cmd)
        assert "--continue" not in cmd_str, (
            f"Should NOT use --continue flag, got: {cmd_str}"
        )


class TestNewControlAgentFlag:
    """Test that --new-control-agent properly starts fresh sessions."""

    def test_new_control_agent_passes_flag_to_daemon(self):
        """When new_control_agent=True, should pass new_session=True to daemon."""
        from orch_monitor.__main__ import TmuxLayoutLauncher

        launcher = TmuxLayoutLauncher()
        commands_sent = []

        def mock_run(args, **kwargs):
            commands_sent.append(args)
            mock_result = MagicMock()
            mock_result.returncode = 0
            return mock_result

        # Create a mock daemon that records the new_session flag
        mock_daemon = MagicMock()
        mock_daemon.is_available.return_value = True
        new_session_values = []

        def capture_get_control_agent_launch(
            project_str, agent_type="", new_session=False
        ):
            new_session_values.append(new_session)
            return Success(
                ControlAgentLaunch(
                    command="opencode attach http://127.0.0.1:4096",
                    prompt_file="/tmp/ORCH_CONTROL_PROMPT.md",
                    port=4096,
                    session_id="",
                    agent="opencode",
                )
            )

        mock_daemon.get_control_agent_launch.side_effect = (
            capture_get_control_agent_launch
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
                new_control_agent=True,  # Force new session
            )

        assert len(new_session_values) == 1, (
            "Should have called get_control_agent_launch once"
        )
        assert new_session_values[0] is True, (
            "Should have passed new_session=True to daemon"
        )

        # Find the send-keys command for the agent pane
        agent_cmd = None
        for cmd in commands_sent:
            if "send-keys" in cmd and "opencode" in str(cmd):
                agent_cmd = cmd
                break

        assert agent_cmd is not None, "Should have sent opencode command"
        cmd_str = " ".join(str(c) for c in agent_cmd)
        assert "--continue" not in cmd_str, (
            f"Should NOT use --continue flag, got: {cmd_str}"
        )

    def test_empty_agent_override_lets_daemon_resolve_agent(self):
        """When no explicit --agent is provided, daemon should receive empty agent_type."""
        from orch_monitor.__main__ import TmuxLayoutLauncher

        launcher = TmuxLayoutLauncher()
        commands_sent = []

        def mock_run(args, **kwargs):
            commands_sent.append(args)
            mock_result = MagicMock()
            mock_result.returncode = 0
            return mock_result

        mock_daemon = MagicMock()
        mock_daemon.is_available.return_value = True
        captured_agent_types = []

        def capture_get_control_agent_launch(
            project_str, agent_type="", new_session=False
        ):
            captured_agent_types.append(agent_type)
            return Success(
                ControlAgentLaunch(
                    command="claude --resume test-session-id",
                    prompt_file="/tmp/ORCH_CONTROL_PROMPT.md",
                    port=0,
                    session_id="test-session-id",
                    agent="claude",
                )
            )

        mock_daemon.get_control_agent_launch.side_effect = (
            capture_get_control_agent_launch
        )

        with (
            patch("subprocess.run", side_effect=mock_run),
            patch("orch_monitor.__main__._get_daemon_client", return_value=mock_daemon),
        ):
            launcher.launch_layout(
                session_name="test-session",
                project_root=Path("/tmp/test"),
                vault_path=Path("/tmp/vault"),
                agent="opencode",  # fallback command only
                agent_override="",  # no explicit --agent
                cwd="/tmp/test",
                new_control_agent=False,
            )

        assert captured_agent_types == [""], (
            f"Expected empty agent_type override, got: {captured_agent_types}"
        )


class TestFallbackControlAgentCommand:
    def test_builder_uses_claude_positional_prompt(self):
        from orch_monitor.__main__ import _build_fallback_control_agent_command

        cmd = _build_fallback_control_agent_command("claude")
        assert cmd.startswith("claude --dangerously-skip-permissions ")
        assert "--prompt" not in cmd

    def test_tmux_fallback_from_daemon_failure_uses_claude_positional_prompt(self):
        from orch_monitor.__main__ import TmuxLayoutLauncher

        launcher = TmuxLayoutLauncher()
        commands_sent = []

        def mock_run(args, **kwargs):
            commands_sent.append(args)
            mock_result = MagicMock()
            mock_result.returncode = 0
            return mock_result

        mock_daemon = MagicMock()
        mock_daemon.is_available.return_value = True
        mock_daemon.get_control_agent_launch.return_value = Failure("daemon failure")

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
                agent_cmd = cmd
                break

        assert agent_cmd is not None, "Should have sent fallback claude command"
        cmd_str = " ".join(str(c) for c in agent_cmd)
        assert "--dangerously-skip-permissions" in cmd_str
        assert "--prompt" not in cmd_str


class TestNewLayoutPreflightGuard:
    """Test that --new (layout restart) fails when control agent session is not recoverable."""

    def test_new_flag_exits_when_session_not_resumed(self):
        """--new should exit(1) when daemon reports resumed=False."""
        from orch_monitor.__main__ import launch_monitor_layout

        mock_launcher = MagicMock()
        mock_launcher.has_session.return_value = True

        mock_daemon = mock_daemon_client_with_launch(
            command="opencode attach http://127.0.0.1:4096 --session new-ses",
            port=4096,
            session_id="new-ses",
            agent="opencode",
            resumed=False,
        )

        with (
            patch("orch_monitor.__main__.get_default_multiplexer_type"),
            patch("orch_monitor.__main__.validate_multiplexer_config"),
            patch(
                "orch_monitor.__main__.get_layout_launcher", return_value=mock_launcher
            ),
            patch("orch_monitor.__main__.get_session_name", return_value="test-ses"),
            patch("orch_monitor.__main__._get_daemon_client", return_value=mock_daemon),
            pytest.raises(SystemExit) as exc_info,
        ):
            launch_monitor_layout(
                project_root=Path("/tmp/test"),
                vault_path=Path("/tmp/vault"),
                new=True,
                new_control_agent=False,
                show_spinner=False,
            )

        assert exc_info.value.code == 1
        mock_launcher.kill_session.assert_not_called()

    def test_new_flag_proceeds_when_session_resumed(self):
        """--new should proceed normally when daemon reports resumed=True."""
        from orch_monitor.__main__ import launch_monitor_layout

        mock_launcher = MagicMock()
        mock_launcher.has_session.return_value = True

        mock_daemon = mock_daemon_client_with_launch(
            command="opencode attach http://127.0.0.1:4096 --session old-ses",
            port=4096,
            session_id="old-ses",
            agent="opencode",
            resumed=True,
        )

        with (
            patch("orch_monitor.__main__.get_default_multiplexer_type"),
            patch("orch_monitor.__main__.validate_multiplexer_config"),
            patch(
                "orch_monitor.__main__.get_layout_launcher", return_value=mock_launcher
            ),
            patch("orch_monitor.__main__.get_session_name", return_value="test-ses"),
            patch("orch_monitor.__main__._get_daemon_client", return_value=mock_daemon),
        ):
            launch_monitor_layout(
                project_root=Path("/tmp/test"),
                vault_path=Path("/tmp/vault"),
                new=True,
                new_control_agent=False,
                show_spinner=False,
            )

        mock_launcher.kill_session.assert_called_once()
        mock_launcher.launch_layout.assert_called_once()

    def test_new_control_agent_skips_preflight(self):
        """--new-control-agent should skip the preflight check entirely."""
        from orch_monitor.__main__ import launch_monitor_layout

        mock_launcher = MagicMock()
        mock_launcher.has_session.return_value = True

        with (
            patch("orch_monitor.__main__.get_default_multiplexer_type"),
            patch("orch_monitor.__main__.validate_multiplexer_config"),
            patch(
                "orch_monitor.__main__.get_layout_launcher", return_value=mock_launcher
            ),
            patch("orch_monitor.__main__.get_session_name", return_value="test-ses"),
            patch("orch_monitor.__main__._get_daemon_client") as mock_get_daemon,
        ):
            launch_monitor_layout(
                project_root=Path("/tmp/test"),
                vault_path=Path("/tmp/vault"),
                new=True,
                new_control_agent=True,
                show_spinner=False,
            )

        mock_launcher.kill_session.assert_called_once()
        mock_launcher.launch_layout.assert_called_once()
        # Pre-flight daemon call should NOT have been made
        # (the launcher.launch_layout makes its own daemon call internally)
        mock_get_daemon.assert_not_called()
