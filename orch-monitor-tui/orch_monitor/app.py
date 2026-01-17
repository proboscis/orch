"""Main Textual app for orch monitor."""

import subprocess
from pathlib import Path
from typing import Optional

from textual import on, work
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Container, Vertical, Horizontal, Grid
from textual.screen import ModalScreen
from textual.widgets import (
    Footer,
    Header,
    TabbedContent,
    TabPane,
    Button,
    Checkbox,
    Label,
    Static,
)

from .config import Config
from .daemon import DaemonClient, DaemonError, DaemonNotRunningError, RunFilters
from .models import Issue, IssueStatus, Run, Status
from .widgets import DetailPanel, IssueTable, RunTable


AUTO_REFRESH_INTERVAL = 5.0


class StatusFilterScreen(ModalScreen[set[Status] | None]):
    """Modal screen for filtering runs by status."""

    CSS = """
    StatusFilterScreen {
        align: center middle;
    }
    
    #filter-dialog {
        width: 50;
        height: auto;
        padding: 1 2;
        background: $surface;
        border: thick $primary;
    }
    
    #filter-title {
        text-align: center;
        width: 100%;
        padding-bottom: 1;
    }
    
    #filter-checkboxes {
        height: auto;
        padding: 1;
    }
    
    #filter-buttons {
        height: 3;
        align: center middle;
        padding-top: 1;
    }
    
    #filter-buttons Button {
        margin: 0 1;
    }
    """

    BINDINGS = [
        Binding("escape", "cancel", "Cancel"),
    ]

    def __init__(self, current_filter: set[Status]):
        super().__init__()
        self.current_filter = current_filter

    def compose(self) -> ComposeResult:
        with Vertical(id="filter-dialog"):
            yield Label("Filter by Status", id="filter-title")
            with Vertical(id="filter-checkboxes"):
                for status in Status:
                    if status != Status.UNKNOWN:
                        checked = (
                            status in self.current_filter or not self.current_filter
                        )
                        yield Checkbox(
                            status.value, value=checked, id=f"status-{status.value}"
                        )
            with Horizontal(id="filter-buttons"):
                yield Button("Apply", variant="primary", id="apply-btn")
                yield Button("Clear All", id="clear-btn")
                yield Button("Cancel", id="cancel-btn")

    @on(Button.Pressed, "#apply-btn")
    def apply_filter(self) -> None:
        selected: set[Status] = set()
        for status in Status:
            if status != Status.UNKNOWN:
                checkbox = self.query_one(f"#status-{status.value}", Checkbox)
                if checkbox.value:
                    selected.add(status)
        self.dismiss(selected)

    @on(Button.Pressed, "#clear-btn")
    def clear_filter(self) -> None:
        for status in Status:
            if status != Status.UNKNOWN:
                checkbox = self.query_one(f"#status-{status.value}", Checkbox)
                checkbox.value = True

    @on(Button.Pressed, "#cancel-btn")
    def cancel(self) -> None:
        self.dismiss(None)

    def action_cancel(self) -> None:
        self.dismiss(None)


COMMON_CSS = """
Screen {
    layout: vertical;
}

#main-container {
    height: 1fr;
}

DataTable {
    height: 1fr;
}

#detail-container {
    height: 40%;
    border-top: solid $accent;
}

#detail-content {
    padding: 1;
    height: 1fr;
    overflow-y: auto;
}
"""


class RunsDashboard(App):
    """Runs-only dashboard for tmux pane."""

    CSS = COMMON_CSS

    BINDINGS = [
        Binding("q", "quit", "Quit"),
        Binding("r", "refresh", "Refresh"),
        Binding("enter", "attach", "Attach"),
        Binding("s", "stop", "Stop"),
        Binding("f", "filter", "Filter"),
    ]

    def __init__(self, vault_path: Optional[Path] = None, auto_refresh: bool = True):
        super().__init__()
        if vault_path:
            self.config = Config.from_vault(vault_path)
        else:
            self.config = Config.load()
        self.daemon = DaemonClient(self.config.socket_path)
        self.runs: list[Run] = []
        self.selected_run: Optional[Run] = None
        self.status_filter: set[Status] = set()
        self._auto_refresh_enabled = auto_refresh
        self.title = f"Runs [{self.config.vault_path.name}]"
        self._daemon_error: Optional[str] = None

    def compose(self) -> ComposeResult:
        yield Header(show_clock=False)
        with Container(id="main-container"):
            yield RunTable(id="runs-table")
        yield Footer()

    def on_mount(self) -> None:
        if not self.daemon.is_available():
            self.notify(
                "Daemon not running. Start with: orch ps",
                severity="error",
                timeout=10,
            )
        self.refresh_data()
        if self._auto_refresh_enabled:
            self.set_interval(AUTO_REFRESH_INTERVAL, self._do_auto_refresh)

    def _do_auto_refresh(self) -> None:
        self.refresh_data()

    def action_refresh(self) -> None:
        self.refresh_data()

    def action_filter(self) -> None:
        self.push_screen(StatusFilterScreen(self.status_filter), self.on_filter_result)

    def on_filter_result(self, result: set[Status] | None) -> None:
        if result is not None:
            self.status_filter = result
            self.refresh_data()

    def refresh_data(self) -> None:
        self._fetch_runs()

    @work(thread=True, exclusive=True)
    def _fetch_runs(self) -> None:
        from datetime import datetime

        try:
            filters = (
                RunFilters(status=list(self.status_filter))
                if self.status_filter
                else None
            )
            response = self.daemon.list_runs(filters)
            runs = response.runs
            error = None
        except DaemonNotRunningError:
            runs = []
            error = "Daemon not running"
        except DaemonError as e:
            runs = []
            error = str(e)

        runs.sort(
            key=lambda r: r.updated_at or r.started_at or datetime.min, reverse=True
        )
        self.call_from_thread(self._update_runs_table, runs, error)

    def _update_runs_table(self, runs: list[Run], error: Optional[str]) -> None:
        self.runs = runs
        self._daemon_error = error
        run_table = self.query_one("#runs-table", RunTable)
        run_table.populate(self.runs)

    @on(RunTable.RowSelected)
    def on_run_selected(self, event: RunTable.RowSelected) -> None:
        run_ref = event.row_key.value
        if not run_ref:
            return
        issue_id, run_id = run_ref.split("#")
        self._fetch_run_detail(issue_id, run_id)

    @work(thread=True)
    def _fetch_run_detail(self, issue_id: str, run_id: str) -> None:
        try:
            run = self.daemon.get_run(issue_id, run_id)
        except DaemonError:
            run = None
        self.call_from_thread(self._set_selected_run, run)

    def _set_selected_run(self, run: Optional[Run]) -> None:
        self.selected_run = run

    def action_attach(self) -> None:
        if not self.selected_run or not self.selected_run.tmux_session:
            return
        self.exit()
        subprocess.run(["tmux", "attach-session", "-t", self.selected_run.tmux_session])

    def action_stop(self) -> None:
        if not self.selected_run:
            return
        self._do_stop(self.selected_run.ref())

    @work(thread=True)
    def _do_stop(self, run_ref: str) -> None:
        try:
            subprocess.run(
                [
                    "orch",
                    "--vault",
                    str(self.config.vault_path),
                    "stop",
                    run_ref,
                ],
                check=True,
            )
            self.call_from_thread(self.refresh_data)
        except subprocess.CalledProcessError:
            pass


class IssuesDashboard(App):
    """Issues-only dashboard for tmux pane."""

    CSS = COMMON_CSS

    BINDINGS = [
        Binding("q", "quit", "Quit"),
        Binding("r", "refresh", "Refresh"),
        Binding("enter", "new_run", "New Run"),
    ]

    def __init__(self, vault_path: Optional[Path] = None, auto_refresh: bool = True):
        super().__init__()
        if vault_path:
            self.config = Config.from_vault(vault_path)
        else:
            self.config = Config.load()
        self.daemon = DaemonClient(self.config.socket_path)
        self.issues: list[Issue] = []
        self.selected_issue: Optional[Issue] = None
        self._auto_refresh_enabled = auto_refresh
        self.title = f"Issues [{self.config.vault_path.name}]"
        self._daemon_error: Optional[str] = None

    def compose(self) -> ComposeResult:
        yield Header(show_clock=False)
        with Container(id="main-container"):
            yield IssueTable(id="issues-table")
        yield Footer()

    def on_mount(self) -> None:
        if not self.daemon.is_available():
            self.notify(
                "Daemon not running. Start with: orch ps",
                severity="error",
                timeout=10,
            )
        self.refresh_data()
        if self._auto_refresh_enabled:
            self.set_interval(AUTO_REFRESH_INTERVAL, self._do_auto_refresh)

    def _do_auto_refresh(self) -> None:
        self.refresh_data()

    def action_refresh(self) -> None:
        self.refresh_data()

    def refresh_data(self) -> None:
        self._fetch_issues()

    @work(thread=True, exclusive=True)
    def _fetch_issues(self) -> None:
        try:
            response = self.daemon.list_issues()
            issues = response.issues
            error = None
        except DaemonNotRunningError:
            issues = []
            error = "Daemon not running"
        except DaemonError as e:
            issues = []
            error = str(e)

        issues.sort(key=lambda i: i.id)
        self.call_from_thread(self._update_issues_table, issues, error)

    def _update_issues_table(self, issues: list[Issue], error: Optional[str]) -> None:
        self.issues = issues
        self._daemon_error = error
        issue_table = self.query_one("#issues-table", IssueTable)
        issue_table.populate(self.issues)

    @on(IssueTable.RowSelected)
    def on_issue_selected(self, event: IssueTable.RowSelected) -> None:
        issue_id = event.row_key.value
        if issue_id:
            self._fetch_issue_detail(issue_id)

    @work(thread=True)
    def _fetch_issue_detail(self, issue_id: str) -> None:
        try:
            issue = self.daemon.get_issue(issue_id)
        except DaemonError:
            issue = None
        self.call_from_thread(self._set_selected_issue, issue)

    def _set_selected_issue(self, issue: Optional[Issue]) -> None:
        self.selected_issue = issue

    def action_new_run(self) -> None:
        if not self.selected_issue:
            return
        self.exit()
        subprocess.run(
            [
                "orch",
                "--vault",
                str(self.config.vault_path),
                "run",
                self.selected_issue.id,
            ]
        )


class OrchMonitorApp(App):
    """Orch monitor TUI application (tabbed view)."""

    CSS = (
        COMMON_CSS
        + """
    #tables-container {
        height: 60%;
    }
    """
    )

    BINDINGS = [
        Binding("q", "quit", "Quit"),
        Binding("r", "refresh", "Refresh"),
        Binding("enter", "select", "Select"),
        Binding("a", "attach", "Attach"),
        Binding("s", "stop", "Stop"),
        Binding("n", "new_run", "New Run"),
        Binding("f", "filter", "Filter"),
        Binding("tab", "switch_focus", "Switch Focus"),
    ]

    def __init__(self, vault_path: Optional[Path] = None, auto_refresh: bool = True):
        super().__init__()

        if vault_path:
            self.config = Config.from_vault(vault_path)
        else:
            self.config = Config.load()

        self.daemon = DaemonClient(self.config.socket_path)
        self.runs: list[Run] = []
        self.issues: list[Issue] = []
        self.selected_run: Optional[Run] = None
        self.selected_issue: Optional[Issue] = None
        self.current_focus = "runs"
        self.status_filter: set[Status] = set()
        self._auto_refresh_enabled = auto_refresh
        self.title = f"Orch Monitor [{self.config.vault_path.name}]"
        self._daemon_error: Optional[str] = None

    def compose(self) -> ComposeResult:
        yield Header()

        with Container(id="main-container"):
            with TabbedContent(id="tables-container"):
                with TabPane("Runs", id="runs-pane"):
                    yield RunTable(id="runs-table")

                with TabPane("Issues", id="issues-pane"):
                    yield IssueTable(id="issues-table")

            with Vertical(id="detail-container"):
                yield DetailPanel(id="detail-panel")

        yield Footer()

    def on_mount(self) -> None:
        if not self.daemon.is_available():
            self.notify(
                "Daemon not running. Start with: orch ps",
                severity="error",
                timeout=10,
            )
        self.refresh_data()
        if self._auto_refresh_enabled:
            self.set_interval(AUTO_REFRESH_INTERVAL, self._do_auto_refresh)

    def _do_auto_refresh(self) -> None:
        self.refresh_data()

    def action_refresh(self) -> None:
        self.refresh_data()

    def action_filter(self) -> None:
        self.push_screen(StatusFilterScreen(self.status_filter), self.on_filter_result)

    def on_filter_result(self, result: set[Status] | None) -> None:
        if result is not None:
            self.status_filter = result
            self.refresh_data()

    def refresh_data(self) -> None:
        self._fetch_all_data()

    @work(thread=True, exclusive=True)
    def _fetch_all_data(self) -> None:
        from datetime import datetime

        try:
            filters = (
                RunFilters(status=list(self.status_filter))
                if self.status_filter
                else None
            )
            runs_response = self.daemon.list_runs(filters)
            runs = runs_response.runs
            error = None
        except DaemonNotRunningError:
            runs = []
            error = "Daemon not running"
        except DaemonError as e:
            runs = []
            error = str(e)

        runs.sort(
            key=lambda r: r.updated_at or r.started_at or datetime.min, reverse=True
        )

        try:
            issues_response = self.daemon.list_issues()
            issues = issues_response.issues
        except DaemonError:
            issues = []

        issues.sort(key=lambda i: i.id)

        self.call_from_thread(self._update_all_tables, runs, issues, error)

    def _update_all_tables(
        self, runs: list[Run], issues: list[Issue], error: Optional[str]
    ) -> None:
        self.runs = runs
        self.issues = issues
        self._daemon_error = error

        run_table = self.query_one("#runs-table", RunTable)
        run_table.populate(self.runs)

        issue_table = self.query_one("#issues-table", IssueTable)
        issue_table.populate(self.issues)

    def action_switch_focus(self) -> None:
        tabbed = self.query_one(TabbedContent)
        if tabbed.active == "runs-pane":
            tabbed.active = "issues-pane"
            self.current_focus = "issues"
        else:
            tabbed.active = "runs-pane"
            self.current_focus = "runs"

    @on(RunTable.RowSelected)
    def on_run_selected(self, event: RunTable.RowSelected) -> None:
        run_ref = event.row_key.value
        if not run_ref:
            return
        issue_id, run_id = run_ref.split("#")
        self._fetch_run_for_detail(issue_id, run_id)

    @work(thread=True)
    def _fetch_run_for_detail(self, issue_id: str, run_id: str) -> None:
        try:
            run = self.daemon.get_run(issue_id, run_id)
        except DaemonError:
            run = None
        self.call_from_thread(self._show_run_detail_callback, run)

    def _show_run_detail_callback(self, run: Optional[Run]) -> None:
        if run:
            self.selected_run = run
            self.show_run_detail(run)

    @on(IssueTable.RowSelected)
    def on_issue_selected(self, event: IssueTable.RowSelected) -> None:
        issue_id = event.row_key.value
        if not issue_id:
            return
        self._fetch_issue_for_detail(issue_id)

    @work(thread=True)
    def _fetch_issue_for_detail(self, issue_id: str) -> None:
        try:
            issue = self.daemon.get_issue(issue_id)
        except DaemonError:
            issue = None
        self.call_from_thread(self._show_issue_detail_callback, issue)

    def _show_issue_detail_callback(self, issue: Optional[Issue]) -> None:
        if issue:
            self.selected_issue = issue
            self.show_issue_detail(issue)

    def show_run_detail(self, run: Run) -> None:
        detail_panel = self.query_one("#detail-panel", DetailPanel)

        content_lines = [
            f"Run: {run.ref()}",
            f"Status: {run.status.value}",
            f"Agent: {run.agent or '-'}",
            f"Started: {run.started_at.strftime('%Y-%m-%d %H:%M:%S') if run.started_at else '-'}",
            f"Updated: {run.updated_at.strftime('%Y-%m-%d %H:%M:%S') if run.updated_at else '-'}",
            f"Elapsed: {run.elapsed_time()}",
            f"Branch: {run.branch or '-'}",
            f"Worktree: {run.worktree_path or '-'}",
            f"Tmux Session: {run.tmux_session or '-'}",
            "",
            "Recent Events:",
            "",
        ]

        for event in run.events[-10:]:
            timestamp = event.timestamp.strftime("%H:%M:%S")
            content_lines.append(f"  {timestamp} | {event.type.value} | {event.name}")

        detail_panel.update_content(
            "\n".join(content_lines), f"Run Details: {run.ref()}"
        )

    def show_issue_detail(self, issue: Issue) -> None:
        detail_panel = self.query_one("#detail-panel", DetailPanel)

        content_lines = [
            f"ID: {issue.id}",
            f"Status: {issue.status.value}",
            f"Title: {issue.title}",
            "",
            "Content:",
            "",
            issue.body[:1000],
        ]

        detail_panel.update_content("\n".join(content_lines), f"Issue: {issue.id}")

    def action_attach(self) -> None:
        if not self.selected_run or not self.selected_run.tmux_session:
            return

        self.exit()
        subprocess.run(["tmux", "attach-session", "-t", self.selected_run.tmux_session])

    def action_stop(self) -> None:
        if not self.selected_run:
            return
        self._do_stop(self.selected_run.ref())

    @work(thread=True)
    def _do_stop(self, run_ref: str) -> None:
        try:
            subprocess.run(
                [
                    "orch",
                    "--vault",
                    str(self.config.vault_path),
                    "stop",
                    run_ref,
                ],
                check=True,
            )
            self.call_from_thread(self.refresh_data)
        except subprocess.CalledProcessError:
            pass

    def action_new_run(self) -> None:
        if not self.selected_issue:
            return

        self.exit()
        subprocess.run(
            [
                "orch",
                "--vault",
                str(self.config.vault_path),
                "run",
                self.selected_issue.id,
            ]
        )

    def action_select(self) -> None:
        if self.current_focus == "runs" and self.selected_run:
            self.action_attach()
        elif self.current_focus == "issues" and self.selected_issue:
            self.action_new_run()
