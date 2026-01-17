"""Main Textual app for orch monitor."""

import os
import shlex
import shutil
import subprocess
from dataclasses import dataclass
from datetime import datetime, timedelta
from pathlib import Path
from typing import Optional, Tuple

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
    Input,
    RadioButton,
    RadioSet,
    SelectionList,
)

from .config import Config, FilterState, RunFilterState, IssueFilterState
from .daemon import DaemonClient, DaemonError, DaemonNotRunningError, RunFilters
from .models import Issue, IssueStatus, Run, Status
from .multiplexer import (
    Multiplexer,
    MultiplexerType,
    detect_current_multiplexer,
    get_multiplexer,
    get_multiplexer_for_run,
    get_multiplexer_type_from_run,
    get_session_name,
)
from .widgets import DetailPanel, IssueTable, RunTable


AUTO_REFRESH_INTERVAL = 5.0
ELAPSED_UPDATE_INTERVAL = 1.0
MESSAGE_REFRESH_INTERVAL = 2.5

AGENTS = ["claude", "codex", "opencode", "gemini"]
TIME_RANGES = [
    ("hour", "Last hour"),
    ("today", "Today"),
    ("week", "This week"),
    ("all", "All time"),
]


class KillConfirmScreen(ModalScreen[bool]):
    """Confirmation dialog for killing terminal session."""

    CSS = """
    KillConfirmScreen {
        align: center middle;
    }

    #kill-dialog {
        width: 50;
        height: auto;
        padding: 1 2;
        background: $surface;
        border: thick $error;
    }

    #kill-title {
        text-align: center;
        width: 100%;
        padding-bottom: 1;
        color: $error;
    }

    #kill-details {
        height: auto;
        padding: 1;
    }

    #kill-consequences {
        height: auto;
        padding: 1;
        color: $warning;
    }

    #kill-buttons {
        height: 3;
        align: center middle;
        padding-top: 1;
    }

    #kill-buttons Button {
        margin: 0 1;
    }
    """

    BINDINGS = [
        Binding("y", "confirm", "Yes, kill"),
        Binding("n", "cancel", "No, cancel"),
        Binding("escape", "cancel", "Cancel"),
    ]

    def __init__(self, run: Run):
        super().__init__()
        self.run = run
        self.multiplexer = get_multiplexer_for_run(run)

    def compose(self) -> ComposeResult:
        mux_name = self.multiplexer.name
        session_name = get_session_name(self.run) or "N/A"
        with Vertical(id="kill-dialog"):
            yield Label(f"Kill {mux_name} session?", id="kill-title")
            with Vertical(id="kill-details"):
                yield Static(f"Run: {self.run.ref()}")
                yield Static(f"Session: {session_name}")
            with Vertical(id="kill-consequences"):
                yield Static("This will:")
                yield Static(f"  - Kill the {mux_name} session")
                yield Static("  - Mark the run as canceled")
                yield Static("  - Stop any running agent")
            with Horizontal(id="kill-buttons"):
                yield Button("Yes, kill", variant="error", id="confirm-btn")
                yield Button("No, cancel", id="cancel-btn")

    @on(Button.Pressed, "#confirm-btn")
    def confirm(self) -> None:
        self.dismiss(True)

    @on(Button.Pressed, "#cancel-btn")
    def cancel(self) -> None:
        self.dismiss(False)

    def action_confirm(self) -> None:
        self.dismiss(True)

    def action_cancel(self) -> None:
        self.dismiss(False)


def _get_editor_command(file_path: Path) -> Tuple[Optional[list[str]], Optional[str]]:
    """Get editor command for opening a file.

    Returns:
        Tuple of (command_list, error_message).
        If successful, command_list is set and error_message is None.
        If failed, command_list is None and error_message describes the issue.
    """
    # Validate path - Path() creates "." which is truthy but invalid
    if not file_path or str(file_path) == "." or not file_path.exists():
        return None, "Issue file path not found"

    if file_path.is_dir():
        return None, "Issue path is a directory, not a file"

    # Get editor: prefer VISUAL, then EDITOR, fallback to vim
    editor_env = os.environ.get("VISUAL") or os.environ.get("EDITOR") or "vim"

    # Parse editor command (supports "code --wait", "vim -p", etc.)
    try:
        editor_parts = shlex.split(editor_env)
    except ValueError as e:
        return None, f"Invalid editor command: {e}"

    if not editor_parts:
        return None, "Empty editor command"

    # Validate executable exists
    editor_executable = editor_parts[0]
    if not shutil.which(editor_executable):
        return None, f"Editor not found: {editor_executable}"

    # Build full command
    return editor_parts + [str(file_path)], None


@dataclass
class RunFilterResult:
    """Result from run filter screen."""

    statuses: set[Status]
    agents: set[str]
    text_search: str
    time_range: str


@dataclass
class IssueFilterResult:
    """Result from issue filter screen."""

    statuses: set[IssueStatus]
    priorities: set[str]
    text_search: str


FILTER_SCREEN_CSS = """
    #filter-dialog {
        width: 50;
        height: auto;
        max-height: 80%;
        padding: 1 2;
        background: $surface;
        border: thick $primary;
    }
    
    #filter-title {
        text-align: center;
        width: 100%;
        text-style: bold;
    }
    
    #filter-buttons {
        height: 3;
        align: center middle;
    }
    
    #filter-buttons Button {
        margin: 0 1;
    }
    
    .filter-section-title {
        text-style: bold;
        margin-top: 1;
    }
    
    SelectionList {
        height: 6;
        margin: 0 1;
    }
    
    #status-list {
        height: 5;
    }
    
    #agent-list {
        height: 4;
    }
    
    #time-range {
        height: auto;
        padding: 0 1;
    }
    
    #text-search-input {
        width: 100%;
        margin: 0 1;
    }
"""


class RunFilterScreen(ModalScreen[RunFilterResult | None]):
    """Modal screen for filtering runs."""

    CSS = (
        """
    RunFilterScreen {
        align: center middle;
    }
    """
        + FILTER_SCREEN_CSS
    )

    BINDINGS = [
        Binding("escape", "cancel", "Cancel"),
        Binding("enter", "apply", "Apply"),
    ]

    def __init__(self, current_filter: RunFilterState):
        super().__init__()
        self.current_filter = current_filter

    def compose(self) -> ComposeResult:
        with Vertical(id="filter-dialog"):
            yield Label("Filter Runs", id="filter-title")

            with Horizontal(id="filter-buttons"):
                yield Button("Apply", variant="primary", id="apply-btn")
                yield Button("Clear", id="clear-btn")
                yield Button("Cancel", id="cancel-btn")

            yield Label("Status", classes="filter-section-title")
            status_items = [
                (
                    status.value,
                    status.value,
                    status.value in self.current_filter.statuses
                    or not self.current_filter.statuses,
                )
                for status in Status
                if status != Status.UNKNOWN
            ]
            yield SelectionList[str](*status_items, id="status-list")

            yield Label("Agent", classes="filter-section-title")
            agent_items = [
                (
                    agent,
                    agent,
                    agent in self.current_filter.agents
                    or not self.current_filter.agents,
                )
                for agent in AGENTS
            ]
            yield SelectionList[str](*agent_items, id="agent-list")

            yield Label("Time Range", classes="filter-section-title")
            with RadioSet(id="time-range"):
                for value, label in TIME_RANGES:
                    yield RadioButton(
                        label,
                        value=(self.current_filter.time_range == value),
                        id=f"time-{value}",
                    )

            yield Label("Search", classes="filter-section-title")
            yield Input(
                value=self.current_filter.text_search,
                placeholder="ID, branch, issue...",
                id="text-search-input",
            )

    @on(Button.Pressed, "#apply-btn")
    def apply_filter(self) -> None:
        status_list = self.query_one("#status-list", SelectionList)
        statuses = {Status(v) for v in status_list.selected}

        agent_list = self.query_one("#agent-list", SelectionList)
        agents = set(agent_list.selected)

        time_range = "all"
        for value, _ in TIME_RANGES:
            radio = self.query_one(f"#time-{value}", RadioButton)
            if radio.value:
                time_range = value
                break

        text_search = self.query_one("#text-search-input", Input).value

        self.dismiss(
            RunFilterResult(
                statuses=statuses,
                agents=agents,
                text_search=text_search,
                time_range=time_range,
            )
        )

    @on(Button.Pressed, "#clear-btn")
    def clear_filter(self) -> None:
        status_list = self.query_one("#status-list", SelectionList)
        status_list.select_all()

        agent_list = self.query_one("#agent-list", SelectionList)
        agent_list.select_all()

        all_time_radio = self.query_one("#time-all", RadioButton)
        all_time_radio.value = True

        self.query_one("#text-search-input", Input).value = ""

    @on(Button.Pressed, "#cancel-btn")
    def cancel(self) -> None:
        self.dismiss(None)

    def action_cancel(self) -> None:
        self.dismiss(None)

    def action_apply(self) -> None:
        self.apply_filter()


class IssueFilterScreen(ModalScreen[IssueFilterResult | None]):
    """Modal screen for filtering issues."""

    CSS = (
        """
    IssueFilterScreen {
        align: center middle;
    }
    """
        + FILTER_SCREEN_CSS
    )

    BINDINGS = [
        Binding("escape", "cancel", "Cancel"),
        Binding("enter", "apply", "Apply"),
    ]

    def __init__(self, current_filter: IssueFilterState):
        super().__init__()
        self.current_filter = current_filter

    def compose(self) -> ComposeResult:
        with Vertical(id="filter-dialog"):
            yield Label("Filter Issues", id="filter-title")

            with Horizontal(id="filter-buttons"):
                yield Button("Apply", variant="primary", id="apply-btn")
                yield Button("Clear", id="clear-btn")
                yield Button("Cancel", id="cancel-btn")

            yield Label("Status", classes="filter-section-title")
            status_items = [
                (
                    status.value,
                    status.value,
                    status.value in self.current_filter.statuses
                    or not self.current_filter.statuses,
                )
                for status in IssueStatus
            ]
            yield SelectionList[str](*status_items, id="issue-status-list")

            yield Label("Search", classes="filter-section-title")
            yield Input(
                value=self.current_filter.text_search,
                placeholder="ID, title, summary...",
                id="text-search-input",
            )

    @on(Button.Pressed, "#apply-btn")
    def apply_filter(self) -> None:
        status_list = self.query_one("#issue-status-list", SelectionList)
        statuses = {IssueStatus(v) for v in status_list.selected}
        text_search = self.query_one("#text-search-input", Input).value

        self.dismiss(
            IssueFilterResult(
                statuses=statuses,
                priorities=set(),
                text_search=text_search,
            )
        )

    @on(Button.Pressed, "#clear-btn")
    def clear_filter(self) -> None:
        status_list = self.query_one("#issue-status-list", SelectionList)
        status_list.select_all()
        self.query_one("#text-search-input", Input).value = ""

    @on(Button.Pressed, "#cancel-btn")
    def cancel(self) -> None:
        self.dismiss(None)

    def action_cancel(self) -> None:
        self.dismiss(None)

    def action_apply(self) -> None:
        self.apply_filter()


def filter_runs_client_side(runs: list[Run], filter_state: RunFilterState) -> list[Run]:
    """Apply client-side filtering for fields the daemon doesn't support."""
    result = runs

    if filter_state.agents:
        result = [r for r in result if r.agent in filter_state.agents]

    if filter_state.text_search:
        search = filter_state.text_search.lower()
        result = [
            r
            for r in result
            if search in r.run_id.lower()
            or search in r.issue_id.lower()
            or search in (r.branch or "").lower()
        ]

    if filter_state.time_range != "all":
        now = datetime.now()
        if filter_state.time_range == "hour":
            cutoff = now - timedelta(hours=1)
        elif filter_state.time_range == "today":
            cutoff = now.replace(hour=0, minute=0, second=0, microsecond=0)
        elif filter_state.time_range == "week":
            cutoff = now - timedelta(days=7)
        else:
            cutoff = None

        if cutoff:

            def compare_time(started: datetime) -> bool:
                if started.tzinfo is not None:
                    started = started.replace(tzinfo=None)
                return started >= cutoff

            result = [r for r in result if r.started_at and compare_time(r.started_at)]

    return result


def filter_issues_client_side(
    issues: list[Issue], filter_state: IssueFilterState
) -> list[Issue]:
    """Apply client-side filtering for issues."""
    result = issues

    if filter_state.statuses:
        result = [i for i in result if i.status.value in filter_state.statuses]

    if filter_state.text_search:
        search = filter_state.text_search.lower()
        result = [
            i
            for i in result
            if search in i.id.lower()
            or search in (i.title or "").lower()
            or search in (i.summary or "").lower()
        ]

    return result


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

RUNS_DASHBOARD_CSS = (
    COMMON_CSS
    + """
#main-container {
    layout: horizontal;
}

#runs-table {
    width: 55%;
}

#run-detail-container {
    width: 45%;
    border-left: solid $accent;
}

#run-detail-content {
    padding: 1;
    height: 1fr;
    overflow-y: auto;
}
"""
)


class RunsDashboard(App):
    """Runs-only dashboard for tmux pane."""

    CSS = RUNS_DASHBOARD_CSS

    BINDINGS = [
        Binding("q", "quit", "Quit"),
        Binding("r", "refresh", "Refresh"),
        Binding("enter", "attach", "Attach", priority=True),
        Binding("s", "stop", "Stop"),
        Binding("X", "kill_session", "Kill"),
        Binding("f", "filter", "Filter"),
        Binding("ctrl+f", "clear_filters", "Clear Filters"),
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
        self.filter_state = self.config.load_filters()
        self._auto_refresh_enabled = auto_refresh
        self._base_title = f"Runs [{self.config.vault_path.name}]"
        self.title = self._base_title
        self._daemon_error: Optional[str] = None
        self._last_update: Optional[datetime] = None
        self._highlighted_run_ref: Optional[str] = None

    def compose(self) -> ComposeResult:
        yield Header(show_clock=False)
        with Container(id="main-container"):
            yield RunTable(id="runs-table")
            with Vertical(id="run-detail-container"):
                yield Static("", id="run-detail-content")
        yield Footer()

    def on_mount(self) -> None:
        if not self.daemon.is_available():
            self.notify(
                "Daemon not running. Start with: orch ps",
                severity="error",
                timeout=10,
            )
        self._update_title()
        self.refresh_data()
        if self._auto_refresh_enabled:
            self.set_interval(AUTO_REFRESH_INTERVAL, self._do_auto_refresh)
        self.set_interval(ELAPSED_UPDATE_INTERVAL, self._update_elapsed_times)

    def _update_elapsed_times(self) -> None:
        if not self.runs:
            return
        try:
            run_table = self.query_one("#runs-table", RunTable)
        except Exception:
            return
        for run in self.runs:
            if run.is_active():
                try:
                    run_table.update_cell(run.ref(), "elapsed", run.elapsed_time())
                except KeyError:
                    pass

    def _update_title(self) -> None:
        count = self.filter_state.run_filter_count()
        if count > 0:
            self.title = f"Runs [{self.config.vault_path.name}] ({count} filters)"
        else:
            self.title = f"Runs [{self.config.vault_path.name}]"

    def _do_auto_refresh(self) -> None:
        self.refresh_data()

    def action_refresh(self) -> None:
        self.refresh_data()

    def action_filter(self) -> None:
        self.push_screen(
            RunFilterScreen(self.filter_state.run_filters), self.on_filter_result
        )

    def action_clear_filters(self) -> None:
        self.filter_state.clear_run_filters()
        self.config.save_filters(self.filter_state)
        self._update_title()
        self.refresh_data()
        self.notify("Filters cleared")

    def on_filter_result(self, result: RunFilterResult | None) -> None:
        if result is not None:
            all_statuses = {s for s in Status if s != Status.UNKNOWN}
            selected_statuses = (
                []
                if result.statuses == all_statuses
                else [s.value for s in result.statuses]
            )
            all_agents = set(AGENTS)
            selected_agents = [] if result.agents == all_agents else list(result.agents)

            self.filter_state.run_filters = RunFilterState(
                statuses=selected_statuses,
                agents=selected_agents,
                text_search=result.text_search,
                time_range=result.time_range,
            )
            self.config.save_filters(self.filter_state)
            self._update_title()
            self.refresh_data()

    def refresh_data(self) -> None:
        self._fetch_runs()

    @work(thread=True, exclusive=True)
    def _fetch_runs(self) -> None:
        try:
            status_filter = []
            for s in self.filter_state.run_filters.statuses:
                try:
                    status_filter.append(Status(s))
                except ValueError:
                    pass
            filters = RunFilters(status=status_filter) if status_filter else None
            response = self.daemon.list_runs(filters)
            runs = response.runs
            error = None
        except DaemonNotRunningError:
            runs = []
            error = "Daemon not running"
        except DaemonError as e:
            runs = []
            error = str(e)

        runs = filter_runs_client_side(runs, self.filter_state.run_filters)
        runs.sort(
            key=lambda r: r.updated_at or r.started_at or datetime.min, reverse=True
        )
        self.call_from_thread(self._update_runs_table, runs, error)

    def _update_runs_table(self, runs: list[Run], error: Optional[str]) -> None:
        self.runs = runs
        self._daemon_error = error
        self._last_update = datetime.now()
        self.title = f"{self._base_title} | Last updated: {self._last_update.strftime('%H:%M:%S')}"
        run_table = self.query_one("#runs-table", RunTable)
        run_table.populate(self.runs)

    @on(RunTable.RowSelected)
    def on_run_selected(self, event: RunTable.RowSelected) -> None:
        """Handle Enter key on run - trigger attach."""
        self.action_attach()

    @on(RunTable.RowHighlighted)
    def on_run_highlighted(self, event: RunTable.RowHighlighted) -> None:
        """Track highlighted run for Enter key attach functionality."""
        run_ref = event.row_key.value if event.row_key else None
        if not run_ref or "#" not in run_ref:
            self._highlighted_run_ref = None
            return
        # Skip if already highlighted (avoids redundant fetches on rapid navigation)
        if getattr(self, "_highlighted_run_ref", None) == run_ref:
            return
        self._highlighted_run_ref = run_ref
        issue_id, run_id = run_ref.split("#", 1)
        self._fetch_run_detail(issue_id, run_id, run_ref)

    @work(thread=True, exclusive=True)
    def _fetch_run_detail(self, issue_id: str, run_id: str, run_ref: str) -> None:
        try:
            run = self.daemon.get_run(issue_id, run_id)
        except DaemonError:
            run = None
        self.call_from_thread(self._set_selected_run, run, run_ref)

    def _set_selected_run(self, run: Optional[Run], run_ref: str) -> None:
        if getattr(self, "_highlighted_run_ref", None) == run_ref:
            self.selected_run = run
            self._update_run_detail_panel(run)

    def _update_run_detail_panel(self, run: Optional[Run]) -> None:
        try:
            detail = self.query_one("#run-detail-content", Static)
        except Exception:
            return

        if not run:
            detail.update("")
            return

        lines = [f"[bold]{run.ref()}[/bold]"]

        if run.pr_url:
            lines.append(f"PR: {run.pr_url}")

        issue = self._get_issue_for_run(run)
        if issue:
            lines.append("")
            lines.append(f"[bold]{issue.title or issue.id}[/bold]")
            if issue.body:
                summary = issue.body[:300].replace("\n", " ")
                lines.append(f"[dim]{summary}[/dim]")

        if run.agent == "opencode" and run.server_port and run.opencode_session_id:
            lines.append("")
            lines.append("[bold]Chat Messages:[/bold]")
            messages = self._fetch_opencode_messages(run)
            if messages:
                for msg in messages[-8:]:
                    role = msg.get("role", "?")
                    text = msg.get("text", "")[:150]
                    if text:
                        color = "cyan" if role == "assistant" else "green"
                        lines.append(f"[{color}]{role}:[/{color}] {text}")
            else:
                lines.append("[dim]No messages yet[/dim]")
        elif run.tmux_session:
            lines.append("")
            lines.append("[bold]Session Output:[/bold]")
            output = self._capture_session_output(run)
            if output:
                for line in output[-12:]:
                    lines.append(f"[dim]{line}[/dim]")
            else:
                lines.append("[dim]No output captured[/dim]")

        detail.update("\n".join(lines))

    def _fetch_opencode_messages(self, run: Run) -> list[dict]:
        if not run.server_port or not run.opencode_session_id:
            return []
        try:
            import urllib.request
            import json

            url = f"http://127.0.0.1:{run.server_port}/session/{run.opencode_session_id}/message"
            req = urllib.request.Request(
                url, headers={"X-OpenCode-Directory": run.worktree_path or ""}
            )
            with urllib.request.urlopen(req, timeout=2) as resp:
                data = json.loads(resp.read().decode())
                result = []
                for msg in data:
                    role = msg.get("info", {}).get("role", "")
                    parts = msg.get("parts", [])
                    text = " ".join(
                        p.get("text", "") for p in parts if p.get("type") == "text"
                    )
                    if text:
                        result.append({"role": role, "text": text})
                return result
        except Exception:
            return []

    def _get_issue_for_run(self, run: Run) -> Optional[Issue]:
        try:
            return self.daemon.get_issue(run.issue_id)
        except Exception:
            return None

    def _capture_session_output(self, run: Run) -> list[str]:
        if not run.tmux_session:
            return []
        try:
            mux_type = get_multiplexer_type_from_run(run)
            if mux_type == MultiplexerType.ZELLIJ:
                result = subprocess.run(
                    ["zellij", "action", "dump-screen", "/dev/stdout"],
                    capture_output=True,
                    text=True,
                    timeout=2,
                    env={**os.environ, "ZELLIJ_SESSION_NAME": run.tmux_session},
                )
            else:
                result = subprocess.run(
                    ["tmux", "capture-pane", "-t", run.tmux_session, "-p", "-S", "-30"],
                    capture_output=True,
                    text=True,
                    timeout=2,
                )
            if result.returncode != 0:
                return []
            lines = [line.rstrip() for line in result.stdout.split("\n")]
            return [line for line in lines if line.strip()]
        except Exception:
            return []

    def action_attach(self) -> None:
        if not self.selected_run:
            self.notify("No run selected", severity="warning")
            return

        run = self.selected_run
        current_mux_type = detect_current_multiplexer()

        attach_cmd = [
            "orch",
            "--vault",
            str(self.config.vault_path),
            "attach",
            run.ref(),
        ]

        if current_mux_type:
            current_mux = get_multiplexer(current_mux_type)
            tab_name = f"attach-{run.short_id()}"
            if current_mux.new_tab_with_command(tab_name, attach_cmd):
                self.notify(f"Opened tab: {tab_name}")
                return
            self.notify(
                "Failed to create tab, falling back to exit", severity="warning"
            )

        self.exit()
        subprocess.run(attach_cmd)

    def action_stop(self) -> None:
        run_ref = getattr(self, "_highlighted_run_ref", None)
        if not run_ref:
            self.notify("No run selected", severity="warning")
            return
        self._do_stop(run_ref)
        self.notify(f"Stopping {run_ref}")

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

    def action_kill_session(self) -> None:
        """Show kill confirmation dialog for selected run."""
        if not self.selected_run:
            self.notify("No run selected", severity="warning")
            return
        session_name = get_session_name(self.selected_run)
        if not session_name:
            self.notify("Run has no session", severity="warning")
            return
        run = self.selected_run
        multiplexer = get_multiplexer_for_run(run)
        run_ref = run.ref()

        def on_confirm(confirmed: bool) -> None:
            if confirmed:
                self._do_kill_session(session_name, multiplexer, run_ref)

        self.push_screen(KillConfirmScreen(run), on_confirm)

    @work(thread=True)
    def _do_kill_session(
        self, session_name: str, multiplexer: Multiplexer, run_ref: str
    ) -> None:
        """Kill terminal session and mark run as canceled."""
        try:
            session_existed = multiplexer.kill_session(session_name)

            stop_result = subprocess.run(
                [
                    "orch",
                    "--vault",
                    str(self.config.vault_path),
                    "stop",
                    run_ref,
                ],
                capture_output=True,
            )

            if stop_result.returncode != 0:
                stderr = stop_result.stderr.decode().strip()
                self.call_from_thread(
                    self.notify,
                    f"Failed to stop run: {stderr or 'unknown error'}",
                    severity="error",
                )
                return

            if session_existed:
                msg = f"Killed session for {run_ref}"
            else:
                msg = f"Session already dead; run {run_ref} marked canceled"
            self.call_from_thread(self.notify, msg, severity="information")
            self.call_from_thread(self.refresh_data)
        except Exception as e:
            self.call_from_thread(
                self.notify,
                f"Failed to kill session: {e}",
                severity="error",
            )


class IssuesDashboard(App):
    """Issues-only dashboard for tmux pane."""

    CSS = COMMON_CSS

    BINDINGS = [
        Binding("q", "quit", "Quit"),
        Binding("r", "refresh", "Refresh"),
        Binding("enter", "open_issue", "Open in Editor"),
        Binding("n", "new_run", "New Run"),
        Binding("o", "open_issue", "Open in Editor", show=False),
        Binding("f", "filter", "Filter"),
        Binding("ctrl+f", "clear_filters", "Clear Filters"),
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
        self.filter_state = self.config.load_filters()
        self._auto_refresh_enabled = auto_refresh
        self._base_title = f"Issues [{self.config.vault_path.name}]"
        self.title = self._base_title
        self._daemon_error: Optional[str] = None
        self._last_update: Optional[datetime] = None

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
        self._update_title()
        self.refresh_data()
        if self._auto_refresh_enabled:
            self.set_interval(AUTO_REFRESH_INTERVAL, self._do_auto_refresh)

    def _update_title(self) -> None:
        count = self.filter_state.issue_filter_count()
        if count > 0:
            self.title = f"Issues [{self.config.vault_path.name}] ({count} filters)"
        else:
            self.title = f"Issues [{self.config.vault_path.name}]"

    def _do_auto_refresh(self) -> None:
        self.refresh_data()

    def action_refresh(self) -> None:
        self.refresh_data()

    def action_filter(self) -> None:
        self.push_screen(
            IssueFilterScreen(self.filter_state.issue_filters), self.on_filter_result
        )

    def action_clear_filters(self) -> None:
        self.filter_state.clear_issue_filters()
        self.config.save_filters(self.filter_state)
        self._update_title()
        self.refresh_data()
        self.notify("Filters cleared")

    def on_filter_result(self, result: IssueFilterResult | None) -> None:
        if result is not None:
            all_statuses = set(IssueStatus)
            selected_statuses = (
                []
                if result.statuses == all_statuses
                else [s.value for s in result.statuses]
            )

            self.filter_state.issue_filters = IssueFilterState(
                statuses=selected_statuses,
                priorities=[],
                text_search=result.text_search,
            )
            self.config.save_filters(self.filter_state)
            self._update_title()
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

        issues = filter_issues_client_side(issues, self.filter_state.issue_filters)
        issues.sort(key=lambda i: i.id)
        self.call_from_thread(self._update_issues_table, issues, error)

    def _update_issues_table(self, issues: list[Issue], error: Optional[str]) -> None:
        self.issues = issues
        self._daemon_error = error
        self._last_update = datetime.now()
        self.title = f"{self._base_title} | Last updated: {self._last_update.strftime('%H:%M:%S')}"
        issue_table = self.query_one("#issues-table", IssueTable)
        issue_table.populate(self.issues)

    @on(IssueTable.RowHighlighted)
    def on_issue_highlighted(self, event: IssueTable.RowHighlighted) -> None:
        """Track highlighted issue for Enter key open functionality."""
        issue_id = event.row_key.value if event.row_key else None
        if not issue_id:
            self._highlighted_issue_id = None
            return
        # Skip if already highlighted (avoids redundant fetches on rapid navigation)
        if getattr(self, "_highlighted_issue_id", None) == issue_id:
            return
        self._highlighted_issue_id = issue_id
        self._fetch_issue_detail(issue_id)

    @on(IssueTable.RowSelected)
    def on_issue_selected(self, event: IssueTable.RowSelected) -> None:
        """Handle Enter key on issue - open in editor."""
        self.action_open_issue()

    @work(thread=True, exclusive=True)
    def _fetch_issue_detail(self, issue_id: str) -> None:
        try:
            issue = self.daemon.get_issue(issue_id)
        except DaemonError:
            issue = None
        self.call_from_thread(self._set_selected_issue, issue, issue_id)

    def _set_selected_issue(self, issue: Optional[Issue], issue_id: str) -> None:
        # Only apply if this is still the highlighted issue (prevents stale updates)
        if getattr(self, "_highlighted_issue_id", None) == issue_id:
            self.selected_issue = issue

    def action_open_issue(self) -> None:
        if not self.selected_issue:
            self.notify("No issue selected", severity="warning")
            return

        cmd, error = _get_editor_command(self.selected_issue.path)
        if error or cmd is None:
            self.notify(error or "Unknown error", severity="error")
            return

        # Suspend TUI, open editor, resume on exit
        with self.suspend():
            subprocess.run(cmd)
        self.refresh_data()

    def action_new_run(self) -> None:
        if not self.selected_issue:
            self.notify("No issue selected", severity="warning")
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
        Binding("X", "kill_session", "Kill"),
        Binding("n", "new_run", "New Run"),
        Binding("o", "open_issue", "Open in Editor"),
        Binding("f", "filter", "Filter"),
        Binding("ctrl+f", "clear_filters", "Clear Filters"),
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
        self.filter_state = self.config.load_filters()
        self._auto_refresh_enabled = auto_refresh
        self._base_title = f"Orch Monitor [{self.config.vault_path.name}]"
        self.title = self._base_title
        self._daemon_error: Optional[str] = None
        self._last_update: Optional[datetime] = None

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
        self._update_tab_titles()
        self.refresh_data()
        if self._auto_refresh_enabled:
            self.set_interval(AUTO_REFRESH_INTERVAL, self._do_auto_refresh)
            # Separate faster refresh for messages only (for active runs)
            self.set_interval(MESSAGE_REFRESH_INTERVAL, self._do_message_refresh)
        self.set_interval(ELAPSED_UPDATE_INTERVAL, self._update_elapsed_times)

    def _update_elapsed_times(self) -> None:
        if not self.runs:
            return
        try:
            run_table = self.query_one("#runs-table", RunTable)
        except Exception:
            return
        for run in self.runs:
            if run.is_active():
                try:
                    run_table.update_cell(run.ref(), "elapsed", run.elapsed_time())
                except KeyError:
                    pass

    def _update_tab_titles(self) -> None:
        try:
            run_count = self.filter_state.run_filter_count()
            issue_count = self.filter_state.issue_filter_count()

            runs_pane = self.query_one("#runs-pane", TabPane)
            issues_pane = self.query_one("#issues-pane", TabPane)

            if run_count > 0:
                runs_pane.update(f"Runs ({run_count} filters)")
            else:
                runs_pane.update("Runs")

            if issue_count > 0:
                issues_pane.update(f"Issues ({issue_count} filters)")
            else:
                issues_pane.update("Issues")
        except Exception:
            pass

    def _do_auto_refresh(self) -> None:
        self.refresh_data()

    def _do_message_refresh(self) -> None:
        """Refresh only the messages for the selected run (lightweight update)."""
        if self.selected_run and self.selected_run.status in (
            Status.RUNNING,
            Status.BOOTING,
            Status.BLOCKED,
        ):
            self.show_run_detail(self.selected_run)

    def action_refresh(self) -> None:
        self.refresh_data()

    def action_filter(self) -> None:
        if self.current_focus == "runs":
            self.push_screen(
                RunFilterScreen(self.filter_state.run_filters),
                self.on_run_filter_result,
            )
        else:
            self.push_screen(
                IssueFilterScreen(self.filter_state.issue_filters),
                self.on_issue_filter_result,
            )

    def action_clear_filters(self) -> None:
        if self.current_focus == "runs":
            self.filter_state.clear_run_filters()
        else:
            self.filter_state.clear_issue_filters()
        self.config.save_filters(self.filter_state)
        self._update_tab_titles()
        self.refresh_data()
        self.notify("Filters cleared")

    def on_run_filter_result(self, result: RunFilterResult | None) -> None:
        if result is not None:
            all_statuses = {s for s in Status if s != Status.UNKNOWN}
            selected_statuses = (
                []
                if result.statuses == all_statuses
                else [s.value for s in result.statuses]
            )
            all_agents = set(AGENTS)
            selected_agents = [] if result.agents == all_agents else list(result.agents)

            self.filter_state.run_filters = RunFilterState(
                statuses=selected_statuses,
                agents=selected_agents,
                text_search=result.text_search,
                time_range=result.time_range,
            )
            self.config.save_filters(self.filter_state)
            self._update_tab_titles()
            self.refresh_data()

    def on_issue_filter_result(self, result: IssueFilterResult | None) -> None:
        if result is not None:
            all_statuses = set(IssueStatus)
            selected_statuses = (
                []
                if result.statuses == all_statuses
                else [s.value for s in result.statuses]
            )

            self.filter_state.issue_filters = IssueFilterState(
                statuses=selected_statuses,
                priorities=[],
                text_search=result.text_search,
            )
            self.config.save_filters(self.filter_state)
            self._update_tab_titles()
            self.refresh_data()

    def refresh_data(self) -> None:
        self._fetch_all_data()

    @work(thread=True, exclusive=True)
    def _fetch_all_data(self) -> None:
        try:
            status_filter = []
            for s in self.filter_state.run_filters.statuses:
                try:
                    status_filter.append(Status(s))
                except ValueError:
                    pass
            filters = RunFilters(status=status_filter) if status_filter else None
            runs_response = self.daemon.list_runs(filters)
            runs = runs_response.runs
            error = None
        except DaemonNotRunningError:
            runs = []
            error = "Daemon not running"
        except DaemonError as e:
            runs = []
            error = str(e)

        runs = filter_runs_client_side(runs, self.filter_state.run_filters)
        runs.sort(
            key=lambda r: r.updated_at or r.started_at or datetime.min, reverse=True
        )

        try:
            issues_response = self.daemon.list_issues()
            issues = issues_response.issues
        except DaemonError:
            issues = []

        issues = filter_issues_client_side(issues, self.filter_state.issue_filters)
        issues.sort(key=lambda i: i.id)

        self.call_from_thread(self._update_all_tables, runs, issues, error)

    def _update_all_tables(
        self, runs: list[Run], issues: list[Issue], error: Optional[str]
    ) -> None:
        self.runs = runs
        self.issues = issues
        self._daemon_error = error
        self._last_update = datetime.now()
        self.title = f"{self._base_title} | Last updated: {self._last_update.strftime('%H:%M:%S')}"

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

    @on(RunTable.RowHighlighted)
    def on_run_highlighted(self, event: RunTable.RowHighlighted) -> None:
        """Track highlighted run for Enter key attach functionality."""
        run_ref = event.row_key.value if event.row_key else None
        if not run_ref or "#" not in run_ref:
            self._highlighted_run_ref = None
            return
        # Skip if already highlighted (avoids redundant fetches on rapid navigation)
        if getattr(self, "_highlighted_run_ref", None) == run_ref:
            return
        self._highlighted_run_ref = run_ref
        issue_id, run_id = run_ref.split("#", 1)
        self._fetch_run_for_detail(issue_id, run_id, run_ref)

    @work(thread=True, exclusive=True)
    def _fetch_run_for_detail(self, issue_id: str, run_id: str, run_ref: str) -> None:
        try:
            run = self.daemon.get_run(issue_id, run_id)
        except DaemonError:
            run = None
        self.call_from_thread(self._show_run_detail_callback, run, run_ref)

    def _show_run_detail_callback(self, run: Optional[Run], run_ref: str) -> None:
        # Only apply if this is still the highlighted run (prevents stale updates)
        if getattr(self, "_highlighted_run_ref", None) != run_ref:
            return
        if run:
            self.selected_run = run
            self.show_run_detail(run)

    @on(IssueTable.RowHighlighted)
    def on_issue_highlighted(self, event: IssueTable.RowHighlighted) -> None:
        """Track highlighted issue for Enter key open functionality."""
        issue_id = event.row_key.value if event.row_key else None
        if not issue_id:
            self._highlighted_issue_id = None
            return
        # Skip if already highlighted (avoids redundant fetches on rapid navigation)
        if getattr(self, "_highlighted_issue_id", None) == issue_id:
            return
        self._highlighted_issue_id = issue_id
        self._fetch_issue_for_detail(issue_id)

    @on(IssueTable.RowSelected)
    def on_issue_selected(self, event: IssueTable.RowSelected) -> None:
        """Handle Enter key on issue - show detail and open in editor."""
        # Show detail in panel first
        if self.selected_issue:
            self.show_issue_detail(self.selected_issue)

    @work(thread=True, exclusive=True)
    def _fetch_issue_for_detail(self, issue_id: str) -> None:
        try:
            issue = self.daemon.get_issue(issue_id)
        except DaemonError:
            issue = None
        self.call_from_thread(self._show_issue_detail_callback, issue, issue_id)

    def _show_issue_detail_callback(
        self, issue: Optional[Issue], issue_id: str
    ) -> None:
        # Only apply if this is still the highlighted issue (prevents stale updates)
        if getattr(self, "_highlighted_issue_id", None) != issue_id:
            return
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
            f"Session: {run.tmux_session or '-'}",
            f"Multiplexer: {run.multiplexer or 'tmux'}",
        ]

        # Add recent messages section
        content_lines.append("")
        content_lines.append("[bold]" + "-" * 50 + "[/bold]")
        content_lines.append("[bold]Recent Messages:[/bold]")
        content_lines.append("")

        if run.tmux_session:
            messages = self._fetch_tmux_pane_output(run.tmux_session)
            if messages:
                for line in messages[-15:]:  # Last 15 lines
                    # Truncate long lines for display
                    display_line = line[:100] + "..." if len(line) > 100 else line
                    content_lines.append(f"  {display_line}")
                if run.status in (Status.RUNNING, Status.BOOTING):
                    content_lines.append("")
                    content_lines.append("[dim]--- Streaming... ---[/dim]")
            else:
                content_lines.append("  [dim](No output captured)[/dim]")
        else:
            content_lines.append("  [dim](No tmux session available)[/dim]")

        detail_panel.update_content(
            "\n".join(content_lines), f"Run Details: {run.ref()}"
        )

    def _fetch_tmux_pane_output(self, tmux_session: str) -> list[str]:
        """Capture recent output from a tmux pane.

        Returns list of non-empty lines from the pane.
        """
        try:
            result = subprocess.run(
                ["tmux", "capture-pane", "-t", tmux_session, "-p", "-S", "-50"],
                capture_output=True,
                text=True,
                timeout=2,
            )
            if result.returncode != 0:
                return []

            # Filter empty lines and clean up output
            lines = [line.rstrip() for line in result.stdout.split("\n")]
            # Remove trailing empty lines but keep internal structure
            while lines and not lines[-1]:
                lines.pop()
            # Filter out mostly empty lines
            lines = [line for line in lines if line.strip()]
            return lines
        except (subprocess.TimeoutExpired, subprocess.SubprocessError):
            return []

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
        if not self.selected_run:
            self.notify("No run selected", severity="warning")
            return

        run = self.selected_run
        current_mux_type = detect_current_multiplexer()

        attach_cmd = [
            "orch",
            "--vault",
            str(self.config.vault_path),
            "attach",
            run.ref(),
        ]

        if current_mux_type:
            current_mux = get_multiplexer(current_mux_type)
            tab_name = f"attach-{run.short_id()}"
            if current_mux.new_tab_with_command(tab_name, attach_cmd):
                self.notify(f"Opened tab: {tab_name}")
                return
            self.notify(
                "Failed to create tab, falling back to exit", severity="warning"
            )

        self.exit()
        subprocess.run(attach_cmd)

    def action_stop(self) -> None:
        run_ref = getattr(self, "_highlighted_run_ref", None)
        if not run_ref:
            self.notify("No run selected", severity="warning")
            return
        self._do_stop(run_ref)
        self.notify(f"Stopping {run_ref}")

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

    def action_kill_session(self) -> None:
        """Show kill confirmation dialog for selected run."""
        if not self.selected_run:
            self.notify("No run selected", severity="warning")
            return
        session_name = get_session_name(self.selected_run)
        if not session_name:
            self.notify("Run has no session", severity="warning")
            return
        run = self.selected_run
        multiplexer = get_multiplexer_for_run(run)
        run_ref = run.ref()

        def on_confirm(confirmed: bool) -> None:
            if confirmed:
                self._do_kill_session(session_name, multiplexer, run_ref)

        self.push_screen(KillConfirmScreen(run), on_confirm)

    @work(thread=True)
    def _do_kill_session(
        self, session_name: str, multiplexer: Multiplexer, run_ref: str
    ) -> None:
        """Kill terminal session and mark run as canceled."""
        try:
            session_existed = multiplexer.kill_session(session_name)

            stop_result = subprocess.run(
                [
                    "orch",
                    "--vault",
                    str(self.config.vault_path),
                    "stop",
                    run_ref,
                ],
                capture_output=True,
            )

            if stop_result.returncode != 0:
                stderr = stop_result.stderr.decode().strip()
                self.call_from_thread(
                    self.notify,
                    f"Failed to stop run: {stderr or 'unknown error'}",
                    severity="error",
                )
                return

            if session_existed:
                msg = f"Killed session for {run_ref}"
            else:
                msg = f"Session already dead; run {run_ref} marked canceled"
            self.call_from_thread(self.notify, msg, severity="information")
            self.call_from_thread(self.refresh_data)
        except Exception as e:
            self.call_from_thread(
                self.notify,
                f"Failed to kill session: {e}",
                severity="error",
            )

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

    def action_open_issue(self) -> None:
        if not self.selected_issue:
            self.notify("No issue selected", severity="warning")
            return

        cmd, error = _get_editor_command(self.selected_issue.path)
        if error or cmd is None:
            self.notify(error or "Unknown error", severity="error")
            return

        # Suspend TUI, open editor, resume on exit
        with self.suspend():
            subprocess.run(cmd)
        self.refresh_data()

    def action_select(self) -> None:
        if self.current_focus == "runs" and self.selected_run:
            self.action_attach()
        elif self.current_focus == "issues" and self.selected_issue:
            self.action_open_issue()
