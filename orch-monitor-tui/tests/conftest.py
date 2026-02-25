"""Pytest configuration and fixtures for orch-monitor TUI tests."""

from datetime import datetime, timedelta
from pathlib import Path
from typing import Optional
from unittest.mock import MagicMock, patch

import pytest

from orch_monitor.config import Config, FilterState, MonitorConfig
from orch_monitor.types import (
    ListIssuesResponse,
    ListRunsResponse,
)
from orch_monitor.models import Issue, IssueStatus, Run, Status


# ============================================================================
# Mock Data Factories
# ============================================================================


def create_mock_run(
    issue_id: str = "test-issue-1",
    run_id: str = "20260115-120000",
    status: Status = Status.RUNNING,
    agent: str = "claude",
    model: str = "claude-3-5-sonnet-20241022",
    model_variant: str = "",
    branch: str = "test-branch",
    multiplexer: str = "tmux",
    session_name: str = "orch-test",
    started_at: Optional[datetime] = None,
    updated_at: Optional[datetime] = None,
    pr_url: str = "",
) -> Run:
    """Factory function to create a mock Run object."""
    if started_at is None:
        started_at = datetime.now() - timedelta(minutes=5)
    if updated_at is None:
        updated_at = datetime.now()

    return Run(
        issue_id=issue_id,
        run_id=run_id,
        path=Path(f"/tmp/test/{issue_id}/{run_id}.md"),
        status=status,
        agent=agent,
        model=model,
        model_variant=model_variant,
        branch=branch,
        multiplexer=multiplexer,
        session_name=session_name,
        started_at=started_at,
        updated_at=updated_at,
        pr_url=pr_url,
    )


def create_mock_issue(
    issue_id: str = "test-issue-1",
    title: str = "Test Issue Title",
    summary: str = "This is a test issue summary.",
    status: IssueStatus = IssueStatus.OPEN,
    body: str = "Full issue body content here.",
) -> Issue:
    """Factory function to create a mock Issue object."""
    return Issue(
        id=issue_id,
        title=title,
        summary=summary,
        status=status,
        body=body,
        path=Path(f"/tmp/test/issues/{issue_id}.md"),
    )


# ============================================================================
# Sample Data Fixtures
# ============================================================================


@pytest.fixture
def sample_runs() -> list[Run]:
    """Return a list of sample runs for testing."""
    return [
        create_mock_run(
            issue_id="orch-123",
            run_id="20260115-100000",
            status=Status.RUNNING,
            agent="claude",
        ),
        create_mock_run(
            issue_id="orch-124",
            run_id="20260115-110000",
            status=Status.WAITING,
            agent="opencode",
        ),
        create_mock_run(
            issue_id="orch-125",
            run_id="20260115-120000",
            status=Status.DONE,
            agent="claude",
        ),
        create_mock_run(
            issue_id="orch-126",
            run_id="20260115-130000",
            status=Status.PR_OPEN,
            agent="gemini",
            pr_url="https://github.com/test/repo/pull/42",
        ),
        create_mock_run(
            issue_id="orch-127",
            run_id="20260115-140000",
            status=Status.FAILED,
            agent="codex",
        ),
    ]


@pytest.fixture
def sample_issues() -> list[Issue]:
    """Return a list of sample issues for testing."""
    return [
        create_mock_issue(
            issue_id="orch-123",
            title="Implement feature X",
            status=IssueStatus.OPEN,
        ),
        create_mock_issue(
            issue_id="orch-124",
            title="Fix bug in component Y",
            status=IssueStatus.OPEN,
        ),
        create_mock_issue(
            issue_id="orch-125",
            title="Refactor module Z",
            status=IssueStatus.RESOLVED,
        ),
        create_mock_issue(
            issue_id="orch-126",
            title="Add documentation for API",
            status=IssueStatus.OPEN,
        ),
        create_mock_issue(
            issue_id="orch-127",
            title="Performance optimization",
            status=IssueStatus.CLOSED,
        ),
    ]


# ============================================================================
# Mock Daemon Fixtures
# ============================================================================


class MockDaemonClient:
    """Mock DaemonClient that returns predefined data."""

    def __init__(
        self,
        runs: Optional[list[Run]] = None,
        issues: Optional[list[Issue]] = None,
    ):
        self._runs = runs or []
        self._issues = issues or []

    def is_available(self) -> bool:
        return True

    def list_runs(self, filters=None) -> ListRunsResponse:
        return ListRunsResponse(
            runs=self._runs,
            next_cursor=None,
            total=len(self._runs),
        )

    def list_issues(self, filters=None) -> ListIssuesResponse:
        return ListIssuesResponse(
            issues=self._issues,
            next_cursor=None,
            total=len(self._issues),
        )

    def get_run(self, issue_id: str, run_id: str) -> Optional[Run]:
        for run in self._runs:
            if run.issue_id == issue_id and run.run_id == run_id:
                return run
        return None

    def get_issue(self, issue_id: str) -> Optional[Issue]:
        for issue in self._issues:
            if issue.id == issue_id:
                return issue
        return None

    def close(self) -> None:
        pass


@pytest.fixture
def mock_daemon(sample_runs, sample_issues) -> MockDaemonClient:
    """Return a mock daemon client with sample data."""
    return MockDaemonClient(runs=sample_runs, issues=sample_issues)


@pytest.fixture
def empty_mock_daemon() -> MockDaemonClient:
    """Return a mock daemon client with no data."""
    return MockDaemonClient(runs=[], issues=[])


# ============================================================================
# Mock Config Fixtures
# ============================================================================


@pytest.fixture
def mock_config(tmp_path: Path) -> Config:
    """Return a mock Config object."""
    orch_dir = tmp_path / ".orch"
    orch_dir.mkdir(parents=True, exist_ok=True)

    return Config(
        project_root=tmp_path,
        vault_path=tmp_path / "vault",
        agent="claude",
        monitor=MonitorConfig(),
    )


# ============================================================================
# Mock Multiplexer Fixtures
# ============================================================================


class MockMultiplexer:
    """Mock multiplexer for testing without real terminal sessions."""

    def __init__(self, name: str = "tmux"):
        self._name = name
        self._sessions: set[str] = set()
        self._available = True

    @property
    def name(self) -> str:
        return self._name

    def is_available(self) -> bool:
        return self._available

    def is_inside(self) -> bool:
        return False

    def has_session(self, session_name: str) -> bool:
        return session_name in self._sessions

    def kill_session(self, session_name: str) -> bool:
        if session_name in self._sessions:
            self._sessions.discard(session_name)
            return True
        return False

    def attach_session(self, session_name: str) -> None:
        pass

    def new_session(
        self,
        session_name: str,
        working_dir: str,
        width: int = 180,
        height: int = 50,
    ) -> bool:
        self._sessions.add(session_name)
        return True

    def split_horizontal(self, session_name: str, working_dir: str) -> bool:
        return session_name in self._sessions

    def split_vertical(
        self,
        session_name: str,
        pane_target: str,
        working_dir: str,
        percentage: int = 50,
    ) -> bool:
        return session_name in self._sessions

    def send_keys(self, target: str, keys: str, enter: bool = True) -> bool:
        return True

    def select_pane(self, target: str) -> bool:
        return True

    def new_tab_with_command(
        self, name: str, command: list[str], cwd: str | None = None
    ) -> bool:
        return True


@pytest.fixture
def mock_tmux() -> MockMultiplexer:
    """Return a mock tmux multiplexer."""
    return MockMultiplexer(name="tmux")


@pytest.fixture
def mock_zellij() -> MockMultiplexer:
    """Return a mock zellij multiplexer."""
    return MockMultiplexer(name="zellij")


# ============================================================================
# App Testing Fixtures
# ============================================================================


@pytest.fixture
def app_with_mock_daemon(mock_daemon, mock_config, tmp_path):
    """Create an app with mocked daemon for testing.

    Returns a function that creates the app, allowing tests to customize.
    """
    from orch_monitor.app import OrchMonitorApp

    def _create_app(auto_refresh: bool = False):
        app = OrchMonitorApp(
            vault_path=mock_config.vault_path, auto_refresh=auto_refresh
        )
        app.daemon = mock_daemon
        app.config = mock_config
        return app

    return _create_app


@pytest.fixture
def runs_dashboard_with_mock(mock_daemon, mock_config):
    """Create a RunsDashboard with mocked daemon for testing."""
    from orch_monitor.app import RunsDashboard

    def _create_app(auto_refresh: bool = False):
        app = RunsDashboard(
            vault_path=mock_config.vault_path, auto_refresh=auto_refresh
        )
        app.daemon = mock_daemon
        app.config = mock_config
        return app

    return _create_app


@pytest.fixture
def issues_dashboard_with_mock(mock_daemon, mock_config):
    """Create an IssuesDashboard with mocked daemon for testing."""
    from orch_monitor.app import IssuesDashboard

    def _create_app(auto_refresh: bool = False):
        app = IssuesDashboard(
            vault_path=mock_config.vault_path, auto_refresh=auto_refresh
        )
        app.daemon = mock_daemon
        app.config = mock_config
        return app

    return _create_app
