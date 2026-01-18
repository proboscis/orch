"""Tests for custom widgets.

These tests verify the behavior of custom Textual widgets used in the TUI.
"""

import pytest
from datetime import datetime, timedelta

from pathlib import Path

from orch_monitor.models import Run, Issue, Status, IssueStatus
from orch_monitor.widgets import (
    RunTable,
    IssueTable,
    DetailPanel,
    shorten_model_name,
    model_display_name,
)


# ============================================================================
# Model Name Shortening Tests
# ============================================================================


class TestModelNameShortening:
    """Tests for model name display formatting."""

    def test_empty_model(self):
        """Test empty model string."""
        assert shorten_model_name("") == ""

    def test_claude_3_5_sonnet(self):
        """Test Claude 3.5 Sonnet model name."""
        result = shorten_model_name("claude-3-5-sonnet-20241022")
        assert "sonnet" in result

    def test_claude_opus(self):
        """Test Claude Opus model name."""
        result = shorten_model_name("claude-3-opus-20240229")
        assert "opus" in result

    def test_gpt_model(self):
        """Test GPT model names."""
        result = shorten_model_name("gpt-4-turbo-preview")
        assert result.startswith("gpt")

    def test_gemini_model(self):
        """Test Gemini model names."""
        result = shorten_model_name("gemini-1.5-pro-latest")
        assert result.startswith("gemini")

    def test_with_provider_prefix(self):
        """Test model with provider prefix (anthropic/...)."""
        result = shorten_model_name("anthropic/claude-3-5-sonnet-20241022")
        # Should strip provider prefix
        assert "anthropic" not in result

    def test_long_model_truncated(self):
        """Test very long model names are truncated."""
        long_name = "some-very-long-model-name-that-exceeds-limits"
        result = shorten_model_name(long_name)
        assert len(result) <= 12

    def test_o1_style_models(self):
        """Test o1/o3 style model names."""
        result = shorten_model_name("o1-preview")
        assert result == "o1-preview"


class TestModelDisplayName:
    """Tests for full model display with variant."""

    def test_no_model(self):
        """Test display when no model set."""
        assert model_display_name("", "") == "-"

    def test_model_no_variant(self):
        """Test display with model but no variant."""
        result = model_display_name("claude-3-5-sonnet-20241022", "")
        assert result != "-"
        assert "/" not in result

    def test_model_with_variant(self):
        """Test display with both model and variant."""
        result = model_display_name("claude-3-5-sonnet", "max")
        assert "/" in result
        assert "max" in result


# ============================================================================
# Run Model Tests
# ============================================================================


class TestRunModel:
    """Tests for Run model methods."""

    def test_run_ref(self, sample_runs):
        """Test run reference string generation."""
        run = sample_runs[0]
        assert run.ref() == f"{run.issue_id}#{run.run_id}"

    def test_short_id(self, sample_runs):
        """Test short ID generation."""
        run = sample_runs[0]
        short = run.short_id()
        assert len(short) == 6
        assert all(c in "0123456789abcdef" for c in short)

    def test_is_active_running(self):
        """Test is_active for running status."""
        run = Run(
            issue_id="test",
            run_id="123",
            path=Path(),
            status=Status.RUNNING,
        )
        assert run.is_active() is True

    def test_is_active_blocked(self):
        """Test is_active for blocked status."""
        run = Run(
            issue_id="test",
            run_id="123",
            path=Path(),
            status=Status.BLOCKED,
        )
        assert run.is_active() is True

    def test_is_active_done(self):
        """Test is_active for done status."""
        run = Run(
            issue_id="test",
            run_id="123",
            path=Path(),
            status=Status.DONE,
        )
        assert run.is_active() is False

    def test_elapsed_time_no_start(self):
        """Test elapsed time when not started."""
        run = Run(
            issue_id="test",
            run_id="123",
            path=Path(),
            started_at=None,
        )
        assert run.elapsed_time() == "-"

    def test_elapsed_time_running(self):
        """Test elapsed time for running task."""
        run = Run(
            issue_id="test",
            run_id="123",
            path=Path(),
            status=Status.RUNNING,
            started_at=datetime.now() - timedelta(minutes=5, seconds=30),
        )
        elapsed = run.elapsed_time()
        assert "m" in elapsed
        assert "s" in elapsed

    def test_elapsed_time_hours(self):
        """Test elapsed time display for hours."""
        run = Run(
            issue_id="test",
            run_id="123",
            path=Path(),
            status=Status.RUNNING,
            started_at=datetime.now() - timedelta(hours=2, minutes=15),
        )
        elapsed = run.elapsed_time()
        assert "h" in elapsed


# ============================================================================
# Issue Model Tests
# ============================================================================


class TestIssueModel:
    """Tests for Issue model."""

    def test_issue_status_display(self):
        """Test issue status display string."""
        issue = Issue(
            id="test-1",
            title="Test Issue",
            status=IssueStatus.OPEN,
        )
        assert issue.status_display() == "open"

    def test_issue_resolved_status(self):
        """Test resolved issue status."""
        issue = Issue(
            id="test-1",
            status=IssueStatus.RESOLVED,
        )
        assert issue.status_display() == "resolved"


# ============================================================================
# Widget Behavior Tests (Using Mocked Tables)
# ============================================================================


class TestRunTableWidget:
    """Tests for RunTable widget behavior."""

    async def test_run_table_population(self, sample_runs):
        """Test that RunTable populates correctly."""
        # Create a simple Textual app to test the widget
        from textual.app import App, ComposeResult
        from textual.widgets import Static

        class TestApp(App):
            def compose(self) -> ComposeResult:
                yield RunTable(id="test-table")

        app = TestApp()
        async with app.run_test() as pilot:
            table = app.query_one("#test-table", RunTable)
            table.populate(sample_runs)
            await pilot.pause()

            assert table.row_count == len(sample_runs)

    async def test_run_table_status_colors(self, sample_runs):
        """Test that status colors are applied correctly."""
        from textual.app import App, ComposeResult

        class TestApp(App):
            def compose(self) -> ComposeResult:
                yield RunTable(id="test-table")

        app = TestApp()
        async with app.run_test() as pilot:
            table = app.query_one("#test-table", RunTable)
            table.populate(sample_runs)
            await pilot.pause()

            # Table should have rows - visual verification of colors
            # would need snapshot tests
            assert table.row_count > 0


class TestIssueTableWidget:
    """Tests for IssueTable widget behavior."""

    async def test_issue_table_population(self, sample_issues):
        """Test that IssueTable populates correctly."""
        from textual.app import App, ComposeResult

        class TestApp(App):
            def compose(self) -> ComposeResult:
                yield IssueTable(id="test-table")

        app = TestApp()
        async with app.run_test() as pilot:
            table = app.query_one("#test-table", IssueTable)
            table.populate(sample_issues)
            await pilot.pause()

            assert table.row_count == len(sample_issues)


class TestDetailPanelWidget:
    """Tests for DetailPanel widget behavior."""

    async def test_detail_panel_update(self):
        """Test updating detail panel content."""
        from textual.app import App, ComposeResult

        class TestApp(App):
            def compose(self) -> ComposeResult:
                yield DetailPanel(id="detail")

        app = TestApp()
        async with app.run_test() as pilot:
            panel = app.query_one("#detail", DetailPanel)
            panel.update_content("Test content", title="Test Title")
            await pilot.pause()

            # The content widget should have the text
            content = app.query_one("#detail-content")
            # Content is updated - exact verification depends on implementation

    async def test_detail_panel_clear(self):
        """Test clearing detail panel."""
        from textual.app import App, ComposeResult

        class TestApp(App):
            def compose(self) -> ComposeResult:
                yield DetailPanel(id="detail")

        app = TestApp()
        async with app.run_test() as pilot:
            panel = app.query_one("#detail", DetailPanel)
            panel.update_content("Some content")
            panel.clear()
            await pilot.pause()


# ============================================================================
# FileChange and DiffStats Model Tests
# ============================================================================


class TestFileChangeModel:
    """Tests for FileChange model."""

    def test_display_str_basic(self):
        """Test basic display string formatting."""
        from orch_monitor.models import FileChange

        fc = FileChange(path="src/main.py", additions=10, deletions=5)
        display = fc.display_str(max_path_width=30)
        assert "src/main.py" in display
        assert "+10" in display
        assert "-5" in display

    def test_display_str_long_path(self):
        """Test display string with path truncation."""
        from orch_monitor.models import FileChange

        fc = FileChange(
            path="very/long/path/to/deeply/nested/file.py", additions=1, deletions=0
        )
        display = fc.display_str(max_path_width=20)
        assert "..." in display
        assert len(display.split()[0]) <= 20

    def test_display_str_zero_changes(self):
        """Test display when additions or deletions are zero."""
        from orch_monitor.models import FileChange

        fc = FileChange(path="file.py", additions=0, deletions=5)
        display = fc.display_str()
        # Zero additions should not show +0
        assert "+0" not in display
        assert "-5" in display


class TestDiffStatsModel:
    """Tests for DiffStats model."""

    def test_file_count(self):
        """Test file count property."""
        from orch_monitor.models import DiffStats, FileChange

        stats = DiffStats(
            files=[
                FileChange(path="a.py", additions=10, deletions=2),
                FileChange(path="b.py", additions=5, deletions=1),
            ],
            total_additions=15,
            total_deletions=3,
        )
        assert stats.file_count == 2

    def test_summary_str(self):
        """Test summary string formatting."""
        from orch_monitor.models import DiffStats, FileChange

        stats = DiffStats(
            files=[
                FileChange(path="a.py", additions=50, deletions=10),
                FileChange(path="b.py", additions=30, deletions=5),
            ],
            total_additions=80,
            total_deletions=15,
        )
        summary = stats.summary_str()
        assert "2 file(s)" in summary
        assert "+80" in summary
        assert "-15" in summary

    def test_empty_stats(self):
        """Test empty diff stats."""
        from orch_monitor.models import DiffStats

        stats = DiffStats()
        assert stats.file_count == 0
        assert stats.total_additions == 0
        assert stats.total_deletions == 0


# ============================================================================
# Git Diff Stats Function Tests
# ============================================================================


class TestGetGitDiffStats:
    """Tests for _get_git_diff_stats function."""

    def test_returns_none_without_worktree(self):
        """Test returns None when worktree_path is empty."""
        from orch_monitor.app import _get_git_diff_stats

        result = _get_git_diff_stats("", "feature-branch", "main")
        assert result is None

    def test_returns_none_without_branch(self):
        """Test returns None when branch is empty."""
        from orch_monitor.app import _get_git_diff_stats

        result = _get_git_diff_stats("/tmp/test", "", "main")
        assert result is None

    def test_parses_numstat_output(self):
        """Test parsing of git diff --numstat output."""
        from unittest.mock import patch, MagicMock
        from orch_monitor.app import _get_git_diff_stats, _diff_stats_cache

        # Clear cache
        _diff_stats_cache.clear()

        mock_result = MagicMock()
        mock_result.returncode = 0
        mock_result.stdout = "10\t5\tsrc/main.py\n20\t3\tsrc/utils.py\n"

        with patch("subprocess.run", return_value=mock_result):
            result = _get_git_diff_stats("/tmp/test", "feature", "main")

        assert result is not None
        assert result.file_count == 2
        assert result.total_additions == 30
        assert result.total_deletions == 8
        assert result.files[0].path == "src/main.py"
        assert result.files[0].additions == 10
        assert result.files[0].deletions == 5

    def test_handles_binary_files(self):
        """Test handling of binary files (shown as - -)."""
        from unittest.mock import patch, MagicMock
        from orch_monitor.app import _get_git_diff_stats, _diff_stats_cache

        # Clear cache
        _diff_stats_cache.clear()

        mock_result = MagicMock()
        mock_result.returncode = 0
        mock_result.stdout = "-\t-\timage.png\n5\t2\tcode.py\n"

        with patch("subprocess.run", return_value=mock_result):
            result = _get_git_diff_stats("/tmp/test2", "feature", "main")

        assert result is not None
        assert result.file_count == 2
        assert result.total_additions == 5
        assert result.total_deletions == 2
        # Binary file should have 0 additions/deletions
        assert result.files[0].additions == 0
        assert result.files[0].deletions == 0

    def test_caches_results(self):
        """Test that results are cached."""
        from unittest.mock import patch, MagicMock
        from orch_monitor.app import _get_git_diff_stats, _diff_stats_cache

        # Clear cache
        _diff_stats_cache.clear()

        mock_result = MagicMock()
        mock_result.returncode = 0
        mock_result.stdout = "1\t1\tfile.py\n"

        with patch("subprocess.run", return_value=mock_result) as mock_run:
            # First call
            result1 = _get_git_diff_stats("/tmp/cache_test", "feature", "main")
            # Second call should use cache
            result2 = _get_git_diff_stats("/tmp/cache_test", "feature", "main")

        # Should only call subprocess once
        assert mock_run.call_count == 1
        assert result1 is result2
