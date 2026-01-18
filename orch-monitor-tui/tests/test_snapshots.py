"""Snapshot tests for visual regression testing.

These tests capture the visual output of the TUI and compare against saved snapshots
to detect unintended visual changes.

To update snapshots when changes are intentional:
    pytest tests/test_snapshots.py --snapshot-update
"""

import pytest
from pathlib import Path
from unittest.mock import patch

# Note: pytest-textual-snapshot must be installed for these tests
# The snap_compare fixture is provided by pytest-textual-snapshot


# ============================================================================
# Runs Dashboard Snapshots
# ============================================================================


class TestRunsDashboardSnapshots:
    """Snapshot tests for the RunsDashboard view."""

    @pytest.fixture
    def app_path(self):
        """Path to the app module for snapshot testing."""
        return Path(__file__).parent.parent / "orch_monitor" / "app.py"

    @pytest.mark.skip(
        reason="Requires pytest-textual-snapshot and running daemon mock setup"
    )
    def test_runs_dashboard_initial_view(self, snap_compare, app_path):
        """Test initial runs dashboard layout matches snapshot."""
        # This would test the visual layout of the runs dashboard
        # Requires proper mock setup during snapshot run

        async def setup(pilot):
            # Wait for data to load
            await pilot.pause()

        assert snap_compare(
            str(app_path),
            terminal_size=(120, 40),
            run_before=setup,
        )

    @pytest.mark.skip(
        reason="Requires pytest-textual-snapshot and running daemon mock setup"
    )
    def test_runs_dashboard_with_filter_dialog(self, snap_compare, app_path):
        """Test runs dashboard with filter dialog open."""

        async def setup(pilot):
            await pilot.pause()
            await pilot.press("f")  # Open filter
            await pilot.pause()

        assert snap_compare(
            str(app_path),
            terminal_size=(120, 40),
            run_before=setup,
        )


# ============================================================================
# Issues Dashboard Snapshots
# ============================================================================


class TestIssuesDashboardSnapshots:
    """Snapshot tests for the IssuesDashboard view."""

    @pytest.fixture
    def app_path(self):
        return Path(__file__).parent.parent / "orch_monitor" / "app.py"

    @pytest.mark.skip(
        reason="Requires pytest-textual-snapshot and running daemon mock setup"
    )
    def test_issues_dashboard_initial_view(self, snap_compare, app_path):
        """Test initial issues dashboard layout matches snapshot."""

        async def setup(pilot):
            await pilot.pause()

        assert snap_compare(
            str(app_path),
            terminal_size=(120, 40),
            run_before=setup,
        )


# ============================================================================
# Main App Snapshots
# ============================================================================


class TestOrchMonitorAppSnapshots:
    """Snapshot tests for the main tabbed OrchMonitorApp."""

    @pytest.fixture
    def app_path(self):
        return Path(__file__).parent.parent / "orch_monitor" / "app.py"

    @pytest.mark.skip(
        reason="Requires pytest-textual-snapshot and running daemon mock setup"
    )
    def test_main_app_runs_tab(self, snap_compare, app_path):
        """Test main app with runs tab active."""

        async def setup(pilot):
            await pilot.pause()

        assert snap_compare(
            str(app_path),
            terminal_size=(140, 50),
            run_before=setup,
        )

    @pytest.mark.skip(
        reason="Requires pytest-textual-snapshot and running daemon mock setup"
    )
    def test_main_app_issues_tab(self, snap_compare, app_path):
        """Test main app with issues tab active."""

        async def setup(pilot):
            await pilot.pause()
            await pilot.press("tab")  # Switch to issues tab
            await pilot.pause()

        assert snap_compare(
            str(app_path),
            terminal_size=(140, 50),
            run_before=setup,
        )


# ============================================================================
# Modal Screen Snapshots
# ============================================================================


class TestModalScreenSnapshots:
    """Snapshot tests for modal screens/dialogs."""

    @pytest.fixture
    def app_path(self):
        return Path(__file__).parent.parent / "orch_monitor" / "app.py"

    @pytest.mark.skip(
        reason="Requires pytest-textual-snapshot and running daemon mock setup"
    )
    def test_run_filter_screen(self, snap_compare, app_path):
        """Test run filter modal screen layout."""

        async def setup(pilot):
            await pilot.pause()
            await pilot.press("f")
            await pilot.pause()

        assert snap_compare(
            str(app_path),
            terminal_size=(120, 40),
            run_before=setup,
        )

    @pytest.mark.skip(
        reason="Requires pytest-textual-snapshot and running daemon mock setup"
    )
    def test_kill_confirm_screen(self, snap_compare, app_path):
        """Test kill confirmation dialog layout."""

        async def setup(pilot):
            await pilot.pause()
            # Would need to select a run and press X
            await pilot.press("down")
            await pilot.pause()
            await pilot.press("X")
            await pilot.pause()

        assert snap_compare(
            str(app_path),
            terminal_size=(120, 40),
            run_before=setup,
        )


# ============================================================================
# Note: How to Enable Snapshot Tests
# ============================================================================
#
# To enable these snapshot tests:
#
# 1. Install pytest-textual-snapshot:
#    uv pip install pytest-textual-snapshot
#
# 2. Create a conftest.py override that provides proper mocking:
#    - Mock the DaemonClient to return test data
#    - Mock Config.load() to return test config
#
# 3. Run tests with snapshot update to create initial baselines:
#    pytest tests/test_snapshots.py --snapshot-update
#
# 4. Commit the generated snapshots to version control
#
# 5. Future test runs will compare against the saved snapshots
#
# See pytest-textual-snapshot documentation for more details:
# https://github.com/Textualize/pytest-textual-snapshot
#
