import subprocess
from datetime import datetime

from textual import on, work
from textual.app import App, ComposeResult
from textual.containers import Container, Horizontal, Vertical
from textual.widgets import DataTable, Footer, Header, Label, Static
from .config import OrchConfig, load_config
from .models import Issue, IssueStatus, Run, Status
from .vault import VaultStore


class StatusBar(Static):
    def __init__(self) -> None:
        super().__init__()
        self.update_text("")

    def update_text(self, text: str) -> None:
        self.update(text or "Ready")


class IssuesPanel(Vertical):
    BINDINGS = [
        ("enter", "select_issue", "Select Issue"),
    ]

    def __init__(self, vault: VaultStore) -> None:
        super().__init__()
        self.vault = vault
        self.issues: list[Issue] = []

    def compose(self) -> ComposeResult:
        yield Label("Issues", classes="panel-title")
        table = DataTable()
        table.cursor_type = "row"
        table.add_column("ID", key="id")
        table.add_column("Status", key="status")
        table.add_column("Title", key="title")
        yield table

    def on_mount(self) -> None:
        self.refresh_issues()

    def refresh_issues(self) -> None:
        table = self.query_one(DataTable)
        table.clear()

        self.issues = self.vault.list_issues()
        for issue in self.issues:
            table.add_row(
                issue.id,
                issue.status.value,
                issue.title[:50],
                key=issue.id,
            )

    def action_select_issue(self) -> None:
        table = self.query_one(DataTable)
        if table.cursor_row is not None and self.issues:
            issue = self.issues[table.cursor_row]
            app = self.app
            if isinstance(app, OrchMonitorApp):
                app.show_issue_detail(issue)


class RunsPanel(Vertical):
    BINDINGS = [
        ("enter", "select_run", "Select Run"),
        ("a", "attach_run", "Attach"),
        ("s", "stop_run", "Stop"),
    ]

    def __init__(self, vault: VaultStore, config: OrchConfig) -> None:
        super().__init__()
        self.vault = vault
        self.config = config
        self.runs: list[Run] = []
        self.status_filter: list[Status] = []

    def compose(self) -> ComposeResult:
        yield Label("Runs", classes="panel-title")
        table = DataTable()
        table.cursor_type = "row"
        table.add_column("Issue", key="issue")
        table.add_column("Run ID", key="run_id")
        table.add_column("Status", key="status")
        table.add_column("Agent", key="agent")
        table.add_column("Elapsed", key="elapsed")
        table.add_column("Branch", key="branch")
        yield table

    def on_mount(self) -> None:
        self.refresh_runs()

    def refresh_runs(self) -> None:
        table = self.query_one(DataTable)
        table.clear()

        self.runs = self.vault.list_runs()

        if self.status_filter:
            self.runs = [r for r in self.runs if r.status in self.status_filter]

        for run in self.runs:
            table.add_row(
                run.issue_id,
                run.run_id,
                run.status.value,
                run.agent,
                run.elapsed,
                run.branch[:20] if run.branch else "",
                key=run.ref,
            )

    def action_select_run(self) -> None:
        table = self.query_one(DataTable)
        if table.cursor_row is not None and self.runs:
            run = self.runs[table.cursor_row]
            app = self.app
            if isinstance(app, OrchMonitorApp):
                app.show_run_detail(run)

    def action_attach_run(self) -> None:
        table = self.query_one(DataTable)
        if table.cursor_row is not None and self.runs:
            run = self.runs[table.cursor_row]
            self.attach_to_run(run)

    def action_stop_run(self) -> None:
        table = self.query_one(DataTable)
        if table.cursor_row is not None and self.runs:
            run = self.runs[table.cursor_row]
            self.stop_run(run)

    def attach_to_run(self, run: Run) -> None:
        if not run.tmux_session:
            app = self.app
            if isinstance(app, OrchMonitorApp):
                app.update_status(f"No tmux session for {run.ref}")
            return

        self.app.exit()

        vault_flag = f"--vault={self.config.vault}"
        subprocess.run(["orch", vault_flag, "attach", run.ref])

    def stop_run(self, run: Run) -> None:
        vault_flag = f"--vault={self.config.vault}"
        result = subprocess.run(
            ["orch", vault_flag, "stop", run.ref],
            capture_output=True,
            text=True,
        )

        app = self.app
        if isinstance(app, OrchMonitorApp):
            if result.returncode == 0:
                app.update_status(f"Stopped {run.ref}")
                self.refresh_runs()
            else:
                app.update_status(f"Failed to stop {run.ref}: {result.stderr}")

    def set_status_filter(self, statuses: list[Status]) -> None:
        self.status_filter = statuses
        self.refresh_runs()


class DetailPanel(Vertical):
    def __init__(self) -> None:
        super().__init__()
        self.content = ""

    def compose(self) -> ComposeResult:
        yield Label("Detail", classes="panel-title")
        yield Static("", id="detail-content")

    def show_issue(self, issue: Issue) -> None:
        content = f"[bold]{issue.title}[/bold]\n\n"
        content += f"ID: {issue.id}\n"
        content += f"Status: {issue.status.value}\n"
        content += f"Path: {issue.path}\n\n"
        content += issue.body[:500]

        detail = self.query_one("#detail-content", Static)
        detail.update(content)

    def show_run(self, run: Run) -> None:
        content = f"[bold]{run.ref}[/bold]\n\n"
        content += f"Status: {run.status.value}\n"
        content += f"Agent: {run.agent}\n"
        content += f"Branch: {run.branch}\n"
        content += f"Elapsed: {run.elapsed}\n"
        content += f"Started: {run.started_at}\n\n"
        content += f"Events ({len(run.events)}):\n"

        for event in run.events[-10:]:
            content += f"  {event.timestamp.strftime('%H:%M:%S')} | {event.type.value} | {event.name}\n"

        detail = self.query_one("#detail-content", Static)
        detail.update(content)

    def clear(self) -> None:
        detail = self.query_one("#detail-content", Static)
        detail.update("")


class OrchMonitorApp(App):
    CSS = """
    Screen {
        layout: vertical;
    }
    
    .panel-title {
        background: $accent;
        color: $text;
        padding: 0 1;
        text-style: bold;
    }
    
    #main-container {
        height: 100%;
    }
    
    #left-panel {
        width: 30%;
    }
    
    #right-panel {
        width: 70%;
    }
    
    #top-panels {
        height: 70%;
    }
    
    #detail-panel {
        height: 30%;
        border-top: solid $accent;
    }
    
    DataTable {
        height: 100%;
    }
    
    #detail-content {
        padding: 1;
        height: 100%;
    }
    
    StatusBar {
        background: $accent;
        color: $text;
        padding: 0 1;
    }
    """

    BINDINGS = [
        ("q", "quit", "Quit"),
        ("r", "refresh", "Refresh"),
        ("f", "filter", "Filter"),
        ("tab", "switch_focus", "Switch Panel"),
    ]

    def __init__(self, config: OrchConfig) -> None:
        super().__init__()
        self.config = config
        self.vault = VaultStore(config.vault)

    def compose(self) -> ComposeResult:
        yield Header()

        with Container(id="main-container"):
            with Horizontal(id="top-panels"):
                with Vertical(id="left-panel"):
                    yield IssuesPanel(self.vault)

                with Vertical(id="right-panel"):
                    yield RunsPanel(self.vault, self.config)

            yield DetailPanel()

        yield StatusBar()
        yield Footer()

    def on_mount(self) -> None:
        self.start_auto_refresh()

    def start_auto_refresh(self) -> None:
        self.set_interval(5.0, self.action_refresh)

    def action_refresh(self) -> None:
        try:
            issues_panel = self.query_one(IssuesPanel)
            runs_panel = self.query_one(RunsPanel)

            issues_panel.refresh_issues()
            runs_panel.refresh_runs()

            self.update_status(f"Refreshed at {datetime.now().strftime('%H:%M:%S')}")
        except Exception as e:
            self.update_status(f"Refresh failed: {e}")

    def action_filter(self) -> None:
        self.update_status("Filter dialog not yet implemented")

    def action_switch_focus(self) -> None:
        focused = self.focused

        if isinstance(focused, DataTable):
            parent = focused.parent
            if isinstance(parent, IssuesPanel):
                runs_panel = self.query_one(RunsPanel)
                table = runs_panel.query_one(DataTable)
                table.focus()
            else:
                issues_panel = self.query_one(IssuesPanel)
                table = issues_panel.query_one(DataTable)
                table.focus()

    def show_issue_detail(self, issue: Issue) -> None:
        detail_panel = self.query_one(DetailPanel)
        detail_panel.show_issue(issue)

    def show_run_detail(self, run: Run) -> None:
        detail_panel = self.query_one(DetailPanel)
        detail_panel.show_run(run)

    def update_status(self, text: str) -> None:
        status_bar = self.query_one(StatusBar)
        status_bar.update_text(text)


def main() -> None:
    try:
        config = load_config()
    except ValueError as e:
        print(f"Error: {e}")
        return

    app = OrchMonitorApp(config)
    app.run()
