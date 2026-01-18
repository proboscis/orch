"""Tests for the main TUI application using Textual's Pilot API.

These tests verify core interactions and screen transitions in the orch-monitor TUI.
"""

import pytest
from unittest.mock import patch, MagicMock

from orch_monitor.app import (
    OrchMonitorApp,
    RunsDashboard,
    IssuesDashboard,
    RunFilterScreen,
    IssueFilterScreen,
    KillConfirmScreen,
)
from orch_monitor.models import Status, IssueStatus
from orch_monitor.widgets import RunTable, IssueTable


# ============================================================================
# OrchMonitorApp Tests (Tabbed View)
# ============================================================================


class TestOrchMonitorApp:
    """Tests for the main tabbed OrchMonitorApp."""

    async def test_app_starts_and_shows_runs_tab(self, app_with_mock_daemon):
        """Test that the app starts and displays the runs tab by default."""
        app = app_with_mock_daemon(auto_refresh=False)

        async with app.run_test() as pilot:
            # App should start with runs tab active
            assert app.current_focus == "runs"

            # Verify RunTable widget exists
            run_table = app.query_one("#runs-table", RunTable)
            assert run_table is not None

    async def test_tab_switch_to_issues(self, app_with_mock_daemon):
        """Test switching from Runs to Issues tab via action."""
        app = app_with_mock_daemon(auto_refresh=False)

        async with app.run_test() as pilot:
            # Initially on runs
            assert app.current_focus == "runs"

            # Use action directly since tab key might conflict with widget focus
            app.action_switch_focus()
            await pilot.pause()

            # Should now be on issues
            assert app.current_focus == "issues"

    async def test_tab_switch_back_to_runs(self, app_with_mock_daemon):
        """Test switching back to Runs tab from Issues."""
        app = app_with_mock_daemon(auto_refresh=False)

        async with app.run_test() as pilot:
            # Switch to issues first
            app.action_switch_focus()
            await pilot.pause()
            assert app.current_focus == "issues"

            # Switch back to runs
            app.action_switch_focus()
            await pilot.pause()
            assert app.current_focus == "runs"

    async def test_refresh_action(self, app_with_mock_daemon):
        """Test that refresh action triggers data reload."""
        app = app_with_mock_daemon(auto_refresh=False)

        async with app.run_test() as pilot:
            # Refresh should work without error
            await pilot.press("r")
            await pilot.pause()

            # App should still be running
            assert app.is_running

    async def test_quit_action(self, app_with_mock_daemon):
        """Test that quit action closes the app."""
        app = app_with_mock_daemon(auto_refresh=False)

        async with app.run_test() as pilot:
            await pilot.press("q")
            # App should exit


# ============================================================================
# RunsDashboard Tests
# ============================================================================


class TestRunsDashboard:
    """Tests for the runs-only dashboard."""

    async def test_dashboard_shows_runs_table(self, runs_dashboard_with_mock):
        """Test that the dashboard displays the RunTable widget."""
        app = runs_dashboard_with_mock(auto_refresh=False)

        async with app.run_test() as pilot:
            run_table = app.query_one("#runs-table", RunTable)
            assert run_table is not None
            # Allow data to populate
            await pilot.pause()

    async def test_run_navigation_down(self, runs_dashboard_with_mock):
        """Test navigating down through runs with arrow keys."""
        app = runs_dashboard_with_mock(auto_refresh=False)

        async with app.run_test() as pilot:
            await pilot.pause()  # Wait for data load

            # Navigate down
            await pilot.press("down")
            await pilot.pause()

            # Verify cursor moved (indirectly through highlighted row tracking)
            # The _highlighted_run_ref should be set after navigation
            assert hasattr(app, "_highlighted_run_ref")

    async def test_run_navigation_vim_style(self, runs_dashboard_with_mock):
        """Test vim-style navigation (j/k) works."""
        app = runs_dashboard_with_mock(auto_refresh=False)

        async with app.run_test() as pilot:
            await pilot.pause()

            # Navigate with j (down)
            await pilot.press("j")
            await pilot.pause()

            # Navigate with k (up)
            await pilot.press("k")
            await pilot.pause()

    async def test_filter_screen_opens(self, runs_dashboard_with_mock):
        """Test that pressing 'f' opens the filter screen."""
        app = runs_dashboard_with_mock(auto_refresh=False)

        async with app.run_test() as pilot:
            await pilot.press("f")
            await pilot.pause()

            # Filter screen should be pushed onto the stack
            # Check if RunFilterScreen is in the screen stack
            assert any(
                isinstance(screen, RunFilterScreen) for screen in app.screen_stack
            )

    async def test_filter_screen_cancel(self, runs_dashboard_with_mock):
        """Test canceling the filter screen with Escape."""
        app = runs_dashboard_with_mock(auto_refresh=False)

        async with app.run_test() as pilot:
            await pilot.press("f")
            await pilot.pause()

            # Cancel with Escape
            await pilot.press("escape")
            await pilot.pause()

            # Should be back to main screen
            assert not any(
                isinstance(screen, RunFilterScreen) for screen in app.screen_stack
            )

    async def test_stop_action_no_selection(self, runs_dashboard_with_mock):
        """Test stop action when no run is selected."""
        app = runs_dashboard_with_mock(auto_refresh=False)

        async with app.run_test() as pilot:
            # Try to stop without selection
            await pilot.press("s")
            await pilot.pause()
            # Should show warning notification (no crash)


# ============================================================================
# IssuesDashboard Tests
# ============================================================================


class TestIssuesDashboard:
    """Tests for the issues-only dashboard."""

    async def test_dashboard_shows_issues_table(self, issues_dashboard_with_mock):
        """Test that the dashboard displays the IssueTable widget."""
        app = issues_dashboard_with_mock(auto_refresh=False)

        async with app.run_test() as pilot:
            issue_table = app.query_one("#issues-table", IssueTable)
            assert issue_table is not None

    async def test_issue_navigation(self, issues_dashboard_with_mock):
        """Test navigating through issues."""
        app = issues_dashboard_with_mock(auto_refresh=False)

        async with app.run_test() as pilot:
            await pilot.pause()

            await pilot.press("down")
            await pilot.pause()

            await pilot.press("up")
            await pilot.pause()

    async def test_issue_filter_screen_opens(self, issues_dashboard_with_mock):
        """Test that pressing 'f' opens the issue filter screen."""
        app = issues_dashboard_with_mock(auto_refresh=False)

        async with app.run_test() as pilot:
            await pilot.press("f")
            await pilot.pause()

            assert any(
                isinstance(screen, IssueFilterScreen) for screen in app.screen_stack
            )


# ============================================================================
# Filter Screen Tests
# ============================================================================


class TestRunFilterScreen:
    """Tests for the run filter modal screen."""

    async def test_filter_screen_composition(self, app_with_mock_daemon):
        """Test that filter screen has all expected widgets."""
        app = app_with_mock_daemon(auto_refresh=False)

        async with app.run_test() as pilot:
            await pilot.press("f")
            await pilot.pause()

            # Verify key widgets exist in the filter screen
            filter_screen = app.screen
            assert isinstance(filter_screen, RunFilterScreen)

            # Should have status list, agent list, time range, and search input
            assert filter_screen.query_one("#status-list")
            assert filter_screen.query_one("#agent-list")
            assert filter_screen.query_one("#time-range")
            assert filter_screen.query_one("#text-search-input")


class TestIssueFilterScreen:
    """Tests for the issue filter modal screen."""

    async def test_filter_screen_composition(self, issues_dashboard_with_mock):
        """Test that issue filter screen has expected widgets."""
        app = issues_dashboard_with_mock(auto_refresh=False)

        async with app.run_test() as pilot:
            await pilot.press("f")
            await pilot.pause()

            filter_screen = app.screen
            assert isinstance(filter_screen, IssueFilterScreen)

            # Should have status list and search input
            assert filter_screen.query_one("#issue-status-list")
            assert filter_screen.query_one("#text-search-input")


# ============================================================================
# Kill Confirm Screen Tests
# ============================================================================


class TestKillConfirmScreen:
    """Tests for the kill confirmation dialog."""

    async def test_kill_confirm_cancel(self, runs_dashboard_with_mock, sample_runs):
        """Test canceling the kill confirmation."""
        app = runs_dashboard_with_mock(auto_refresh=False)
        test_run = sample_runs[0]

        async with app.run_test() as pilot:
            await pilot.pause()

            # Simulate opening kill screen (normally triggered by X key with selection)
            # We'll test the screen directly
            result = []

            def on_result(confirmed):
                result.append(confirmed)

            app.push_screen(KillConfirmScreen(test_run), on_result)
            await pilot.pause()

            # Cancel with 'n' key
            await pilot.press("n")
            await pilot.pause()

            # Should have received False
            assert result == [False]

    async def test_kill_confirm_yes(self, runs_dashboard_with_mock, sample_runs):
        """Test confirming the kill action."""
        app = runs_dashboard_with_mock(auto_refresh=False)
        test_run = sample_runs[0]

        async with app.run_test() as pilot:
            await pilot.pause()

            result = []

            def on_result(confirmed):
                result.append(confirmed)

            app.push_screen(KillConfirmScreen(test_run), on_result)
            await pilot.pause()

            # Confirm with 'y' key
            await pilot.press("y")
            await pilot.pause()

            # Should have received True
            assert result == [True]


# ============================================================================
# Data Population Tests
# ============================================================================


class TestDataPopulation:
    """Tests for data population in tables."""

    async def test_runs_table_populates_from_daemon(
        self, runs_dashboard_with_mock, sample_runs
    ):
        """Test that runs from daemon are displayed in the table."""
        app = runs_dashboard_with_mock(auto_refresh=False)

        async with app.run_test() as pilot:
            await pilot.pause()  # Wait for initial data load

            run_table = app.query_one("#runs-table", RunTable)

            # Table should have rows for each run
            assert run_table.row_count == len(sample_runs)

    async def test_issues_table_populates_from_daemon(
        self, issues_dashboard_with_mock, sample_issues
    ):
        """Test that issues from daemon are displayed (may be filtered)."""
        app = issues_dashboard_with_mock(auto_refresh=False)

        async with app.run_test() as pilot:
            await pilot.pause()

            issue_table = app.query_one("#issues-table", IssueTable)
            # Table populates with available issues (may have default filters)
            assert issue_table.row_count > 0

    async def test_empty_daemon_shows_empty_table(self, mock_config, empty_mock_daemon):
        """Test that empty daemon data shows empty table."""
        from orch_monitor.app import RunsDashboard

        app = RunsDashboard(vault_path=mock_config.vault_path, auto_refresh=False)
        app.daemon = empty_mock_daemon
        app.config = mock_config

        async with app.run_test() as pilot:
            await pilot.pause()

            run_table = app.query_one("#runs-table", RunTable)
            assert run_table.row_count == 0


# ============================================================================
# Agent Select Screen Tests
# ============================================================================


class TestAgentSelectScreen:
    """Tests for the agent selection modal screen."""

    async def test_agent_select_screen_composition(self, issues_dashboard_with_mock, sample_issues):
        """Test that agent select screen has expected widgets."""
        from orch_monitor.app import AgentSelectScreen

        app = issues_dashboard_with_mock(auto_refresh=False)

        async with app.run_test() as pilot:
            await pilot.pause()

            # Push agent select screen
            agents = ["claude", "codex", "opencode"]
            result = []

            def on_result(agent):
                result.append(agent)

            app.push_screen(AgentSelectScreen("test-issue", agents), on_result)
            await pilot.pause()

            # Verify the screen is pushed
            assert any(
                isinstance(screen, AgentSelectScreen) for screen in app.screen_stack
            )

    async def test_agent_select_enter_confirms(self, issues_dashboard_with_mock, sample_issues):
        """Test that Enter key confirms the selection and starts the run."""
        from orch_monitor.app import AgentSelectScreen

        app = issues_dashboard_with_mock(auto_refresh=False)

        async with app.run_test() as pilot:
            await pilot.pause()

            agents = ["claude", "codex", "opencode"]
            result = []

            def on_result(agent):
                result.append(agent)

            app.push_screen(AgentSelectScreen("test-issue", agents), on_result)
            await pilot.pause()

            # Press Enter to confirm (first agent is highlighted by default)
            await pilot.press("enter")
            await pilot.pause()

            # Should have dismissed with the first agent
            assert len(result) == 1
            assert result[0] == "claude"

    async def test_agent_select_escape_cancels(self, issues_dashboard_with_mock, sample_issues):
        """Test that Escape key cancels the selection."""
        from orch_monitor.app import AgentSelectScreen

        app = issues_dashboard_with_mock(auto_refresh=False)

        async with app.run_test() as pilot:
            await pilot.pause()

            agents = ["claude", "codex", "opencode"]
            result = []

            def on_result(agent):
                result.append(agent)

            app.push_screen(AgentSelectScreen("test-issue", agents), on_result)
            await pilot.pause()

            # Press Escape to cancel
            await pilot.press("escape")
            await pilot.pause()

            # Should have dismissed with None
            assert len(result) == 1
            assert result[0] is None

    async def test_agent_select_quick_number(self, issues_dashboard_with_mock, sample_issues):
        """Test that number keys quickly select an agent."""
        from orch_monitor.app import AgentSelectScreen

        app = issues_dashboard_with_mock(auto_refresh=False)

        async with app.run_test() as pilot:
            await pilot.pause()

            agents = ["claude", "codex", "opencode"]
            result = []

            def on_result(agent):
                result.append(agent)

            app.push_screen(AgentSelectScreen("test-issue", agents), on_result)
            await pilot.pause()

            # Press "2" to select the second agent
            await pilot.press("2")
            await pilot.pause()

            # Should have dismissed with codex
            assert len(result) == 1
            assert result[0] == "codex"

    async def test_agent_select_navigation_then_enter(self, issues_dashboard_with_mock, sample_issues):
        """Test navigating with j/k then pressing Enter."""
        from orch_monitor.app import AgentSelectScreen

        app = issues_dashboard_with_mock(auto_refresh=False)

        async with app.run_test() as pilot:
            await pilot.pause()

            agents = ["claude", "codex", "opencode"]
            result = []

            def on_result(agent):
                result.append(agent)

            app.push_screen(AgentSelectScreen("test-issue", agents), on_result)
            await pilot.pause()

            # Navigate down with j
            await pilot.press("j")
            await pilot.pause()

            # Press Enter to confirm
            await pilot.press("enter")
            await pilot.pause()

            # Should have dismissed with codex (second item)
            assert len(result) == 1
            assert result[0] == "codex"

    async def test_agent_select_no_agents(self, issues_dashboard_with_mock):
        """Test agent select screen with no agents available."""
        from orch_monitor.app import AgentSelectScreen

        app = issues_dashboard_with_mock(auto_refresh=False)

        async with app.run_test() as pilot:
            await pilot.pause()

            # Empty agents list
            result = []

            def on_result(agent):
                result.append(agent)

            app.push_screen(AgentSelectScreen("test-issue", []), on_result)
            await pilot.pause()

            # Press Enter - should dismiss with None without crashing
            await pilot.press("enter")
            await pilot.pause()

            assert len(result) == 1
            assert result[0] is None
