"""Main Textual app for orch monitor."""

import subprocess
from pathlib import Path
from typing import Optional

from textual import on
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Container, Vertical
from textual.widgets import Footer, Header, TabbedContent, TabPane

from .config import Config
from .models import Issue, Run
from .vault import VaultReader
from .widgets import DetailPanel, IssueTable, RunTable


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
    ]

    def __init__(self, vault_path: Optional[Path] = None):
        super().__init__()
        if vault_path:
            self.config = Config.from_vault(vault_path)
        else:
            self.config = Config.load()
        self.vault = VaultReader(self.config.vault_path)
        self.runs: list[Run] = []
        self.selected_run: Optional[Run] = None

    def compose(self) -> ComposeResult:
        yield Header(show_clock=False)
        with Container(id="main-container"):
            yield RunTable(id="runs-table")
        yield Footer()

    def on_mount(self) -> None:
        self.refresh_data()

    def action_refresh(self) -> None:
        self.refresh_data()

    def refresh_data(self) -> None:
        from datetime import datetime

        self.runs = self.vault.list_runs()
        self.runs.sort(
            key=lambda r: r.updated_at or r.started_at or datetime.min, reverse=True
        )
        run_table = self.query_one("#runs-table", RunTable)
        run_table.populate(self.runs)

    @on(RunTable.RowSelected)
    def on_run_selected(self, event: RunTable.RowSelected) -> None:
        run_ref = event.row_key.value
        if not run_ref:
            return
        issue_id, run_id = run_ref.split("#")
        self.selected_run = self.vault.get_run(issue_id, run_id)

    def action_attach(self) -> None:
        if not self.selected_run or not self.selected_run.tmux_session:
            return
        self.exit()
        subprocess.run(["tmux", "attach-session", "-t", self.selected_run.tmux_session])

    def action_stop(self) -> None:
        if not self.selected_run:
            return
        try:
            subprocess.run(
                [
                    "orch",
                    "--vault",
                    str(self.config.vault_path),
                    "stop",
                    self.selected_run.ref(),
                ],
                check=True,
            )
            self.refresh_data()
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

    def __init__(self, vault_path: Optional[Path] = None):
        super().__init__()
        if vault_path:
            self.config = Config.from_vault(vault_path)
        else:
            self.config = Config.load()
        self.vault = VaultReader(self.config.vault_path)
        self.issues: list[Issue] = []
        self.selected_issue: Optional[Issue] = None

    def compose(self) -> ComposeResult:
        yield Header(show_clock=False)
        with Container(id="main-container"):
            yield IssueTable(id="issues-table")
        yield Footer()

    def on_mount(self) -> None:
        self.refresh_data()

    def action_refresh(self) -> None:
        self.refresh_data()

    def refresh_data(self) -> None:
        self.issues = self.vault.list_issues()
        self.issues.sort(key=lambda i: i.id)
        issue_table = self.query_one("#issues-table", IssueTable)
        issue_table.populate(self.issues)

    @on(IssueTable.RowSelected)
    def on_issue_selected(self, event: IssueTable.RowSelected) -> None:
        issue_id = event.row_key.value
        if issue_id:
            self.selected_issue = self.vault.get_issue(issue_id)

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
        Binding("tab", "switch_focus", "Switch Focus"),
    ]

    def __init__(self, vault_path: Optional[Path] = None):
        super().__init__()

        if vault_path:
            self.config = Config.from_vault(vault_path)
        else:
            self.config = Config.load()

        self.vault = VaultReader(self.config.vault_path)
        self.runs: list[Run] = []
        self.issues: list[Issue] = []
        self.selected_run: Optional[Run] = None
        self.selected_issue: Optional[Issue] = None
        self.current_focus = "runs"

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
        self.refresh_data()

    def action_refresh(self) -> None:
        self.refresh_data()

    def refresh_data(self) -> None:
        from datetime import datetime

        self.runs = self.vault.list_runs()
        self.runs.sort(
            key=lambda r: r.updated_at or r.started_at or datetime.min, reverse=True
        )

        self.issues = self.vault.list_issues()
        self.issues.sort(key=lambda i: i.id)

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
        run = self.vault.get_run(issue_id, run_id)

        if run:
            self.selected_run = run
            self.show_run_detail(run)

    @on(IssueTable.RowSelected)
    def on_issue_selected(self, event: IssueTable.RowSelected) -> None:
        issue_id = event.row_key.value
        if not issue_id:
            return

        issue = self.vault.get_issue(issue_id)

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

        try:
            subprocess.run(
                [
                    "orch",
                    "--vault",
                    str(self.config.vault_path),
                    "stop",
                    self.selected_run.ref(),
                ],
                check=True,
            )
            self.refresh_data()
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
