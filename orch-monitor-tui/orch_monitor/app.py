"""Main Textual app for orch monitor."""

import logging
import os
import shlex
import shutil
import subprocess
from dataclasses import dataclass
from datetime import datetime, timedelta
from pathlib import Path
from typing import Optional, Tuple

import yaml

_logger: Optional[logging.Logger] = None


def get_logger() -> logging.Logger:
    global _logger
    if _logger is None:
        _logger = logging.getLogger("orch_monitor")
    return _logger


def setup_logging(log_path: Path) -> None:
    log_path.parent.mkdir(parents=True, exist_ok=True)
    handler = logging.FileHandler(log_path, encoding="utf-8")
    handler.setFormatter(
        logging.Formatter(
            "%(asctime)s %(levelname)s %(message)s", datefmt="%Y-%m-%d %H:%M:%S"
        )
    )
    logger = get_logger()
    logger.addHandler(handler)
    logger.setLevel(logging.DEBUG)


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

from .config import (
    Config,
    ConfigurationState,
    FilterState,
    IssueFilterState,
    RunFilterState,
)
from .daemon import (
    DaemonClient,
    DaemonError,
    DaemonNotRunningError,
    MonitorRegistration,
    RunFilters,
)
from .models import DiffStats, FileChange, Issue, IssueStatus, Run, Status
from .multiplexer import (
    Multiplexer,
    MultiplexerType,
    detect_current_multiplexer,
    get_multiplexer,
    get_multiplexer_for_run,
    get_multiplexer_type_from_run,
    get_session_name,
)
from .widgets import DetailPanel, IssueTable, RunTable, TabbedStatsPanel


AUTO_REFRESH_INTERVAL = 5.0
ELAPSED_UPDATE_INTERVAL = 2.0
MESSAGE_REFRESH_INTERVAL = 2.5

AGENTS = ["claude", "codex", "opencode", "gemini"]
TIME_RANGES = [
    ("hour", "Last hour"),
    ("today", "Today"),
    ("week", "This week"),
    ("all", "All time"),
]


def _log_error(operation: str, error: str, project_root: Path) -> None:
    log_path = project_root / ".orch" / "monitor-tui.log"
    try:
        log_path.parent.mkdir(parents=True, exist_ok=True)
        timestamp = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
        with open(log_path, "a") as f:
            f.write(f"{timestamp} [{operation}] {error}\n")
    except OSError:
        pass


from functools import lru_cache
from time import time as _time


# TTL cache wrapper for git diff stats (30 second TTL)
_diff_stats_cache_time: dict[str, float] = {}
_DIFF_STATS_TTL = 30.0  # seconds


@lru_cache(maxsize=256)
def _get_git_diff_stats_cached(
    worktree_path: str, branch: str, base_branch: str
) -> Optional[DiffStats]:
    """Internal cached implementation of git diff stats retrieval."""
    try:
        # Use git diff --numstat for machine-readable output
        result = subprocess.run(
            ["git", "diff", "--numstat", f"{base_branch}...{branch}"],
            capture_output=True,
            text=True,
            timeout=5,
            cwd=worktree_path,
            encoding="utf-8",
            errors="replace",  # Handle non-UTF8 filenames
        )

        if result.returncode != 0:
            # Try without the merge-base (three dots) syntax
            result = subprocess.run(
                ["git", "diff", "--numstat", f"{base_branch}..{branch}"],
                capture_output=True,
                text=True,
                timeout=5,
                cwd=worktree_path,
                encoding="utf-8",
                errors="replace",
            )

        if result.returncode != 0:
            # Log failure for debugging (only once due to cache)
            get_logger().debug(
                f"git diff failed for {branch} vs {base_branch}: {result.stderr}"
            )
            return None

        files: list[FileChange] = []
        total_additions = 0
        total_deletions = 0

        for line in result.stdout.strip().split("\n"):
            if not line:
                continue
            parts = line.split("\t")
            if len(parts) != 3:
                continue

            add_str, del_str, path = parts
            # Handle binary files (shown as - -)
            additions = int(add_str) if add_str != "-" else 0
            deletions = int(del_str) if del_str != "-" else 0

            files.append(
                FileChange(path=path, additions=additions, deletions=deletions)
            )
            total_additions += additions
            total_deletions += deletions

        return DiffStats(
            files=files,
            total_additions=total_additions,
            total_deletions=total_deletions,
        )

    except (
        subprocess.TimeoutExpired,
        subprocess.SubprocessError,
        OSError,
        ValueError,
    ) as e:
        get_logger().debug(f"git diff exception for {branch}: {e}")
        return None


def _get_git_diff_stats(
    worktree_path: str, branch: str, base_branch: str = "main"
) -> Optional[DiffStats]:
    """Get git diff statistics for a branch compared to base branch.

    Uses LRU cache with TTL to avoid repeated git calls while allowing
    updates when branches change.

    Args:
        worktree_path: Path to the git worktree
        branch: The branch to compare
        base_branch: The base branch to compare against (default: main)

    Returns:
        DiffStats object or None if unable to get stats
    """
    if not worktree_path or not branch:
        return None

    cache_key = f"{worktree_path}:{branch}:{base_branch}"
    now = _time()

    # Check if cached entry is stale
    if cache_key in _diff_stats_cache_time:
        if now - _diff_stats_cache_time[cache_key] > _DIFF_STATS_TTL:
            # Clear stale entry from LRU cache
            _get_git_diff_stats_cached.cache_clear()
            _diff_stats_cache_time.clear()

    result = _get_git_diff_stats_cached(worktree_path, branch, base_branch)
    _diff_stats_cache_time[cache_key] = now
    return result


def _format_changed_files_lines(
    diff_stats: Optional[DiffStats],
    max_files: int = 10,
    path_width: int = 35,
) -> list[str]:
    """Format changed files for display in detail panel.

    Args:
        diff_stats: DiffStats object or None
        max_files: Maximum number of files to show
        path_width: Width for path column

    Returns:
        List of formatted lines with Rich markup
    """
    if not diff_stats or not diff_stats.files:
        return []

    lines = []
    lines.append("")
    lines.append(f"[bold]Changed Files ({diff_stats.file_count}):[/bold]")

    for fc in diff_stats.files[:max_files]:
        # Truncate long paths
        path = fc.path
        if len(path) > path_width:
            path = "..." + path[-(path_width - 3) :]

        # Pad plain strings FIRST, then add markup (fixes alignment issue)
        add_plain = f"+{fc.additions}" if fc.additions else ""
        del_plain = f"-{fc.deletions}" if fc.deletions else ""

        # Apply color after padding
        add_str = f"[green]{add_plain:>6}[/green]" if add_plain else " " * 6
        del_str = f"[red]{del_plain:>6}[/red]" if del_plain else " " * 6

        lines.append(f"  {path:<{path_width}}  {add_str}  {del_str}")

    if len(diff_stats.files) > max_files:
        remaining = len(diff_stats.files) - max_files
        lines.append(f"  [dim]... and {remaining} more file(s)[/dim]")

    lines.append(f"  [bold]{'─' * path_width}[/bold]")
    lines.append(
        f"  [bold]Total: [green]+{diff_stats.total_additions}[/green] "
        f"[red]-{diff_stats.total_deletions}[/red][/bold]"
    )

    return lines


def _build_orch_cmd(config: "Config") -> list[str]:
    cmd = ["orch"]
    if config.project_root:
        cmd.extend(["--project-root", str(config.project_root)])
    if config.issues_root:
        cmd.extend(["--issues-root", str(config.issues_root)])
    return cmd


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


class CloseIssueConfirmScreen(ModalScreen[bool]):
    """Confirmation dialog for closing an issue."""

    CSS = """
    CloseIssueConfirmScreen {
        align: center middle;
    }

    #close-dialog {
        width: 50;
        height: auto;
        padding: 1 2;
        background: $surface;
        border: thick $warning;
    }

    #close-title {
        text-align: center;
        width: 100%;
        padding-bottom: 1;
        color: $warning;
    }

    #close-details {
        height: auto;
        padding: 1;
    }

    #close-info {
        height: auto;
        padding: 1;
        color: $text-muted;
    }

    #close-buttons {
        height: 3;
        align: center middle;
        padding-top: 1;
    }

    #close-buttons Button {
        margin: 0 1;
    }
    """

    BINDINGS = [
        Binding("y", "confirm", "Yes, close"),
        Binding("n", "cancel", "No, cancel"),
        Binding("escape", "cancel", "Cancel"),
    ]

    def __init__(self, issue_id: str, issue_title: Optional[str] = None):
        super().__init__()
        self.issue_id = issue_id
        self.issue_title = issue_title

    def compose(self) -> ComposeResult:
        with Vertical(id="close-dialog"):
            yield Label("Close issue?", id="close-title")
            with Vertical(id="close-details"):
                yield Static(f"Issue: {self.issue_id}")
                if self.issue_title:
                    yield Static(
                        f"Title: {self.issue_title[:50]}{'...' if len(self.issue_title or '') > 50 else ''}"
                    )
            with Vertical(id="close-info"):
                yield Static("This will:")
                yield Static("  - For GitHub issues: close on GitHub")
                yield Static("  - For local issues: set status to 'closed'")
            with Horizontal(id="close-buttons"):
                yield Button("Yes, close", variant="warning", id="confirm-btn")
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


def _get_issue_file_path(issue: "Issue") -> Tuple[Optional[Path], Optional[str]]:
    """Get the file path for an issue, creating a temp file for GitHub issues.

    For local issues, returns the existing path.
    For GitHub issues (identified by 'gh-' or 'gh#' prefix or URL path), creates a temp file
    with the issue body.

    Returns:
        Tuple of (file_path, error_message).
        If successful, file_path is set and error_message is None.
        If failed, file_path is None and error_message describes the issue.
    """
    import tempfile

    path_str = str(issue.path) if issue.path else ""

    def is_url_path(s: str) -> bool:
        return (
            s.startswith("http://")
            or s.startswith("https://")
            or s.startswith("http:/")
            or s.startswith("https:/")
        )

    is_github_issue = (
        issue.id.startswith("gh-")
        or issue.id.startswith("gh#")
        or is_url_path(path_str)
    )

    log = get_logger()

    if is_github_issue:
        if not issue.body:
            err = f"GitHub issue {issue.id} has no body content"
            log.error(err)
            return None, err

        github_url = ""
        if issue.frontmatter.get("url"):
            github_url = issue.frontmatter["url"]
        elif is_url_path(path_str):
            github_url = path_str.replace("https:/", "https://").replace(
                "http:/", "http://"
            )
        else:
            err = f"GitHub issue {issue.id} has no URL (path={path_str!r}, frontmatter.url missing)"
            log.error(err)
            return None, err

        try:
            safe_id = "".join(c if c.isalnum() or c in "-_" else "_" for c in issue.id)
            fd, temp_path = tempfile.mkstemp(
                suffix=".md", prefix=f"orch-issue-{safe_id}-"
            )
            with os.fdopen(fd, "w") as f:
                f.write(f"# {issue.title or issue.id}\n\n")
                f.write(f"<!-- GitHub Issue: {github_url} -->\n")
                f.write(f"<!-- Note: Changes here are NOT synced to GitHub -->\n\n")
                f.write(issue.body)
            log.debug(f"Created temp file for {issue.id}: {temp_path}")
            return Path(temp_path), None
        except OSError as e:
            err = f"Failed to create temp file for {issue.id}: {e}"
            log.error(err)
            return None, err

    if not path_str or path_str == ".":
        err = f"Local issue {issue.id} has no file path"
        log.error(err)
        return None, err

    file_path = issue.path
    if not file_path.exists():
        err = f"Local issue {issue.id} file not found: {file_path}"
        log.error(err)
        return None, err

    if file_path.is_dir():
        err = f"Local issue {issue.id} path is a directory: {file_path}"
        log.error(err)
        return None, err

    return file_path, None


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
    tags: set[str]
    tag_mode: str  # "all" (AND) or "any" (OR)
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


def _get_available_agents(config: "Config") -> list[str]:
    """Get list of available agents and presets from config."""
    agents = list(AGENTS)
    seen = set(agents)

    config_path = config.project_root / ".orch" / "config.yaml"
    if config_path.exists():
        try:
            with open(config_path) as f:
                data = yaml.safe_load(f)

            if not isinstance(data, dict):
                return agents

            presets = data.get("presets", [])
            for preset in presets:
                if isinstance(preset, dict):
                    name = preset.get("name", "")
                    if name:
                        backend = preset.get("backend") or "opencode"
                        preset_str = f"{backend}:{name}"
                        if preset_str not in seen:
                            agents.append(preset_str)
                            seen.add(preset_str)
        except yaml.YAMLError as e:
            get_logger().debug(f"Failed to parse config for agents: {e}")
        except OSError as e:
            get_logger().debug(f"Failed to read config for agents: {e}")

    return agents


class AgentSelectScreen(ModalScreen[str | None]):
    """Modal screen for selecting agent/preset before starting a run."""

    CSS = """
    AgentSelectScreen {
        align: center middle;
    }

    #agent-dialog {
        width: 50;
        height: auto;
        max-height: 80%;
        padding: 1 2;
        background: $surface;
        border: thick $primary;
    }

    #agent-title {
        text-align: center;
        width: 100%;
        text-style: bold;
        padding-bottom: 1;
    }

    #agent-issue {
        text-align: center;
        width: 100%;
        color: $text-muted;
        padding-bottom: 1;
    }

    #agent-selection-list {
        height: auto;
        max-height: 12;
        margin: 0 1;
    }

    #agent-empty {
        text-align: center;
        width: 100%;
        color: $text-muted;
        padding: 1;
    }

    #agent-footer {
        text-align: center;
        width: 100%;
        color: $text-muted;
        padding-top: 1;
    }
    """

    BINDINGS = [
        Binding("escape", "cancel", "Cancel"),
        Binding("enter", "confirm", "Start", priority=True),
        Binding("k", "cursor_up", "Up", show=False),
        Binding("j", "cursor_down", "Down", show=False),
        Binding("1", "quick_select_1", "1", show=False),
        Binding("2", "quick_select_2", "2", show=False),
        Binding("3", "quick_select_3", "3", show=False),
        Binding("4", "quick_select_4", "4", show=False),
        Binding("5", "quick_select_5", "5", show=False),
        Binding("6", "quick_select_6", "6", show=False),
        Binding("7", "quick_select_7", "7", show=False),
        Binding("8", "quick_select_8", "8", show=False),
        Binding("9", "quick_select_9", "9", show=False),
    ]

    def __init__(self, issue_id: str, agents: list[str]):
        super().__init__()
        self.issue_id = issue_id
        self.agents = agents

    def compose(self) -> ComposeResult:
        with Vertical(id="agent-dialog"):
            yield Label("Select Agent", id="agent-title")
            yield Label(f"Issue: {self.issue_id}", id="agent-issue")
            if self.agents:
                items = [
                    (f"[{i + 1}] {agent}", agent, i == 0)
                    for i, agent in enumerate(self.agents)
                ]
                yield SelectionList[str](*items, id="agent-selection-list")
                yield Label(
                    "[Enter] Start  [1-9] Quick select  [Esc] Cancel", id="agent-footer"
                )
            else:
                yield Label("No agents available", id="agent-empty")
                yield Label("[Esc] cancel", id="agent-footer")

    def on_mount(self) -> None:
        if self.agents:
            self.query_one("#agent-selection-list", SelectionList).focus()

    def action_cursor_up(self) -> None:
        if self.agents:
            sel = self.query_one("#agent-selection-list", SelectionList)
            sel.action_cursor_up()

    def action_cursor_down(self) -> None:
        if self.agents:
            sel = self.query_one("#agent-selection-list", SelectionList)
            sel.action_cursor_down()

    def action_confirm(self) -> None:
        if not self.agents:
            self.dismiss(None)
            return
        sel = self.query_one("#agent-selection-list", SelectionList)
        # Prioritize highlighted item (cursor position) over checkbox selections
        if sel.highlighted is not None:
            self.dismiss(self.agents[sel.highlighted])
        else:
            # Fallback: find first agent in list order that is selected (deterministic)
            selected = sel.selected
            for agent in self.agents:
                if agent in selected:
                    self.dismiss(agent)
                    return
            self.dismiss(None)

    def action_cancel(self) -> None:
        self.dismiss(None)

    def _quick_select(self, index: int) -> None:
        if 0 <= index < len(self.agents):
            self.dismiss(self.agents[index])

    def action_quick_select_1(self) -> None:
        self._quick_select(0)

    def action_quick_select_2(self) -> None:
        self._quick_select(1)

    def action_quick_select_3(self) -> None:
        self._quick_select(2)

    def action_quick_select_4(self) -> None:
        self._quick_select(3)

    def action_quick_select_5(self) -> None:
        self._quick_select(4)

    def action_quick_select_6(self) -> None:
        self._quick_select(5)

    def action_quick_select_7(self) -> None:
        self._quick_select(6)

    def action_quick_select_8(self) -> None:
        self._quick_select(7)

    def action_quick_select_9(self) -> None:
        self._quick_select(8)


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

    def on_mount(self) -> None:
        self.query_one("#text-search-input", Input).focus()

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

    def on_mount(self) -> None:
        self.query_one("#text-search-input", Input).focus()

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

            yield Label("Tags (comma-separated)", classes="filter-section-title")
            yield Input(
                value=", ".join(self.current_filter.tags)
                if self.current_filter.tags
                else "",
                placeholder="bug, urgent, feature...",
                id="tag-filter-input",
            )

            yield Label("Tag Mode", classes="filter-section-title")
            with RadioSet(id="tag-mode-set"):
                yield RadioButton(
                    "Any (OR)",
                    value=self.current_filter.tag_mode != "all",
                    id="tag-mode-any",
                )
                yield RadioButton(
                    "All (AND)",
                    value=self.current_filter.tag_mode == "all",
                    id="tag-mode-all",
                )

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

        # Parse tags from input
        tag_input = self.query_one("#tag-filter-input", Input).value
        tags = {t.strip() for t in tag_input.split(",") if t.strip()}

        # Get tag mode from radio set
        tag_mode_all = self.query_one("#tag-mode-all", RadioButton)
        tag_mode = "all" if tag_mode_all.value else "any"

        self.dismiss(
            IssueFilterResult(
                statuses=statuses,
                priorities=set(),
                tags=tags,
                tag_mode=tag_mode,
                text_search=text_search,
            )
        )

    @on(Button.Pressed, "#clear-btn")
    def clear_filter(self) -> None:
        status_list = self.query_one("#issue-status-list", SelectionList)
        status_list.select_all()
        self.query_one("#tag-filter-input", Input).value = ""
        self.query_one("#tag-mode-any", RadioButton).value = True
        self.query_one("#text-search-input", Input).value = ""

    @on(Button.Pressed, "#cancel-btn")
    def cancel(self) -> None:
        self.dismiss(None)

    def action_cancel(self) -> None:
        self.dismiss(None)

    def action_apply(self) -> None:
        self.apply_filter()


class HelpScreen(ModalScreen[None]):
    """Modal screen showing keybindings and workflow tips."""

    CSS = """
    HelpScreen {
        align: center middle;
    }

    #help-dialog {
        width: 70;
        height: auto;
        max-height: 85%;
        padding: 1 2;
        background: $surface;
        border: thick $primary;
    }

    #help-title {
        text-align: center;
        width: 100%;
        text-style: bold;
        margin-bottom: 1;
    }

    .help-section-title {
        text-style: bold;
        margin-top: 1;
        color: $primary;
    }

    #help-content {
        height: auto;
        overflow-y: auto;
    }

    .help-key {
        color: $accent;
    }

    #close-btn {
        margin-top: 1;
        width: 100%;
    }
    """

    BINDINGS = [
        Binding("escape", "close", "Close"),
        Binding("q", "close", "Close"),
        Binding("?", "close", "Close"),
    ]

    def compose(self) -> ComposeResult:
        with Vertical(id="help-dialog"):
            yield Label("Orch Monitor Help", id="help-title")

            with Vertical(id="help-content"):
                yield Label("Keybindings", classes="help-section-title")
                yield Label("  ?         Show this help screen")
                yield Label("  q         Quit")
                yield Label("  r         Refresh data")
                yield Label("  Tab       Switch between Runs/Issues tabs")
                yield Label("  f         Filter runs/issues")
                yield Label("  Ctrl+f    Clear all filters")
                yield Label("  Enter     Attach to run / Open issue")
                yield Label("  a         Attach to selected run")
                yield Label("  s         Stop selected run")
                yield Label("  X         Kill session (force)")
                yield Label("  n         New run for selected issue")
                yield Label("  o         Open issue in editor")
                yield Label("  x         Close issue")
                yield Label("  d         View diff for selected run")

                yield Label("Quick Workflow", classes="help-section-title")
                yield Label("  1. Select issue in Issues tab")
                yield Label("  2. Press n to start a new run")
                yield Label("  3. Select agent (claude/opencode/codex)")
                yield Label("  4. Monitor progress in Runs tab")
                yield Label("  5. Press a or Enter to attach")
                yield Label("  6. Review PR when status is pr_open")

                yield Label("Status Legend", classes="help-section-title")
                yield Label("  queued    -> Run waiting to start")
                yield Label("  booting   -> Agent starting up")
                yield Label("  running   -> Agent actively working")
                yield Label("  blocked   -> Agent needs input (attach!)")
                yield Label("  pr_open   -> PR created, review it")
                yield Label("  done      -> Work completed")

            yield Button("Close (Esc/?/q)", id="close-btn", variant="primary")

    @on(Button.Pressed, "#close-btn")
    def close_help(self) -> None:
        self.dismiss(None)

    def action_close(self) -> None:
        self.dismiss(None)


class OnboardingScreen(ModalScreen[bool]):
    """Screen shown when orch is not configured for this project."""

    CSS = """
    OnboardingScreen {
        align: center middle;
    }
    #onboarding-dialog {
        width: 75;
        height: auto;
        max-height: 90%;
        padding: 1 2;
        background: $surface;
        border: thick $accent;
    }
    #onboarding-title {
        text-align: center;
        width: 100%;
        text-style: bold;
        color: $warning;
        margin-bottom: 1;
    }
    .setup-section {
        margin-top: 1;
        padding: 1;
        background: $surface-darken-1;
    }
    .code-line {
        color: $success;
        margin-left: 4;
    }
    #status-line {
        margin-top: 1;
        text-align: center;
        color: $text-muted;
    }
    """

    BINDINGS = [
        Binding("r", "retry", "Retry"),
        Binding("q", "quit_app", "Quit"),
        Binding("escape", "quit_app", "Quit"),
    ]

    def __init__(self, config_state: "ConfigurationState"):
        super().__init__()
        self.config_state = config_state
        self._polling = True

    def compose(self) -> ComposeResult:
        with Vertical(id="onboarding-dialog"):
            yield Label("Orch Not Configured", id="onboarding-title")
            yield Static(f"Directory: {self.config_state.project_root}")
            yield Static("")

            # Show what's missing
            if not self.config_state.has_orch_dir:
                yield Static("[yellow]Missing:[/] .orch/ directory")
            if not self.config_state.has_issues_path:
                yield Static("[yellow]Missing:[/] Issues path not set")

            with Vertical(classes="setup-section"):
                yield Static("[bold]Quick Setup[/]")
                yield Static("")
                yield Static("1. Create config directory:")
                yield Static("mkdir -p .orch", classes="code-line")
                yield Static("")
                yield Static("2. Set issues path (one of):")
                yield Static("export ORCH_ISSUES_ROOT=~/my-issues", classes="code-line")
                yield Static("# Or in .orch/config.yaml:", classes="code-line")
                yield Static("#   issues:", classes="code-line")
                yield Static("#     path: ~/my-issues", classes="code-line")
                yield Static("")
                yield Static("3. For full guided setup:")
                yield Static("orch tutorial", classes="code-line")

            yield Static("")
            yield Static(
                "[dim]Watching for setup... (auto-continues when ready)[/]",
                id="status-line",
            )
            yield Static("")
            yield Button("[R]etry", id="retry-btn", variant="primary")
            yield Button("[Q]uit", id="quit-btn")

    def on_mount(self) -> None:
        self.set_interval(2.0, self._check_configuration)

    def _check_configuration(self) -> None:
        if not self._polling:
            return
        from .config import detect_configuration_state

        state = detect_configuration_state()
        if state.has_orch_dir and state.has_issues_path:
            self._polling = False
            self.notify("Configuration detected!", severity="information")
            self.dismiss(True)

    def on_button_pressed(self, event: Button.Pressed) -> None:
        if event.button.id == "retry-btn":
            self._check_configuration()
        elif event.button.id == "quit-btn":
            self.action_quit_app()

    def action_retry(self) -> None:
        self._check_configuration()

    def action_quit_app(self) -> None:
        self._polling = False
        self.dismiss(False)


class OnboardingApp(App):
    """Minimal app for onboarding when orch is not configured."""

    CSS = """
    Screen {
        align: center middle;
        background: $surface;
    }
    """

    def __init__(self, config_state: "ConfigurationState"):
        super().__init__()
        self.config_state = config_state
        self.result = False

    def on_mount(self) -> None:
        self.push_screen(OnboardingScreen(self.config_state), self._on_result)

    def _on_result(self, result: bool) -> None:
        self.result = result
        self.exit(result)


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

    # Tag filtering
    if filter_state.tags:
        filter_tags = {t.lower() for t in filter_state.tags}
        if filter_state.tag_mode == "all":
            # AND mode: issue must have all filter tags
            result = [
                i for i in result if filter_tags.issubset({t.lower() for t in i.tags})
            ]
        else:
            # OR mode (any): issue must have at least one filter tag
            result = [
                i
                for i in result
                if filter_tags.intersection({t.lower() for t in i.tags})
            ]

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

#run-tabs {
    height: 1fr;
}

#run-tabs > ContentSwitcher {
    height: 1fr;
}

#stats-scroll, #issue-scroll, #changes-scroll {
    height: 1fr;
    padding: 1;
}

#stats-content, #issue-content, #changes-content {
    width: 100%;
}
"""
)


def _input_has_focus(app: App) -> bool:
    focused = app.focused
    return focused is not None and isinstance(focused, Input)


class RunsDashboard(App):
    CSS = RUNS_DASHBOARD_CSS

    BINDINGS = [
        Binding("question_mark", "help", "Help", key_display="?"),
        Binding("q", "quit", "Quit"),
        Binding("r", "refresh", "Refresh"),
        Binding("enter", "attach", "Attach", priority=True),
        Binding("s", "stop", "Stop"),
        Binding("X", "kill_session", "Kill"),
        Binding("f", "filter", "Filter"),
        Binding("ctrl+f", "clear_filters", "Clear Filters"),
        Binding("d", "diff", "Diff"),
    ]

    def __init__(self, issues_root: Optional[Path] = None, auto_refresh: bool = True):
        super().__init__()
        if issues_root:
            self.config = Config.from_issues_root(issues_root)
        else:
            self.config = Config.load()
        self.daemon = DaemonClient(self.config.socket_path, self.config.issues_root)
        self.runs: list[Run] = []
        self.selected_run: Optional[Run] = None
        self.filter_state = self.config.load_filters()
        self._auto_refresh_enabled = auto_refresh
        self._base_title = f"Runs [{self.config.project_root.name}]"
        self.title = self._base_title
        self._daemon_error: Optional[str] = None
        self._last_update: Optional[datetime] = None
        self._highlighted_run_ref: Optional[str] = None
        self._monitor_registration = MonitorRegistration(
            self.daemon, str(self.config.project_root), "runs"
        )

    def compose(self) -> ComposeResult:
        yield Header(show_clock=False)
        with Container(id="main-container"):
            yield RunTable(id="runs-table")
            with Vertical(id="run-detail-container"):
                yield TabbedStatsPanel(id="run-detail-tabs")
        yield Footer()

    def on_mount(self) -> None:
        self._monitor_registration.register()
        self._update_title()
        self.refresh_data()
        if self._auto_refresh_enabled:
            self.set_interval(AUTO_REFRESH_INTERVAL, self._do_auto_refresh)
        self.set_interval(ELAPSED_UPDATE_INTERVAL, self._update_elapsed_times)

    def on_unmount(self) -> None:
        self._monitor_registration.unregister()

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
            self.title = f"Runs [{self.config.project_root.name}] ({count} filters)"
        else:
            self.title = f"Runs [{self.config.project_root.name}]"

    def _do_auto_refresh(self) -> None:
        self.refresh_data()

    def action_refresh(self) -> None:
        if _input_has_focus(self):
            return
        self.refresh_data()

    def action_help(self) -> None:
        """Show help screen with keybindings and workflow tips."""
        self.push_screen(HelpScreen())

    def action_filter(self) -> None:
        if _input_has_focus(self):
            return
        self.push_screen(
            RunFilterScreen(self.filter_state.run_filters), self.on_filter_result
        )

    def action_clear_filters(self) -> None:
        if _input_has_focus(self):
            return
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
            runs = None
            error = "Daemon not running"
        except DaemonError as e:
            runs = None
            error = str(e)

        if runs is not None:
            runs = filter_runs_client_side(runs, self.filter_state.run_filters)
            runs.sort(
                key=lambda r: r.updated_at or r.started_at or datetime.min, reverse=True
            )
        self.call_from_thread(self._update_runs_table, runs, error)

    def _update_runs_table(
        self, runs: Optional[list[Run]], error: Optional[str]
    ) -> None:
        self._last_update = datetime.now()

        if error:
            self._daemon_error = error
            self.title = f"{self._base_title} | ERROR: {error}"
            self.notify(f"Refresh failed: {error}", severity="error", timeout=5)
            _log_error("list_runs", error, self.config.project_root)
            return

        self._daemon_error = None
        if runs is not None:
            self.runs = runs
        self.title = f"{self._base_title} | Last updated: {self._last_update.strftime('%H:%M:%S')}"

        # Collect diff stats for all runs with worktrees
        diff_stats: dict[str, DiffStats] = {}
        for run in self.runs:
            if run.worktree_path and run.branch:
                stats = _get_git_diff_stats(
                    run.worktree_path, run.branch, self.config.base_branch
                )
                if stats:
                    diff_stats[run.ref()] = stats

        run_table = self.query_one("#runs-table", RunTable)
        run_table.populate(self.runs, diff_stats=diff_stats)

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
        issue_id, run_id = run_ref.rsplit("#", 1)
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
            tabs_panel = self.query_one("#run-detail-tabs", TabbedStatsPanel)
        except Exception:
            return

        if not run:
            tabs_panel.clear_all()
            return

        # === Stats Tab ===
        stats_lines = [f"[bold]{run.ref()}[/bold]"]
        stats_lines.append("")
        stats_lines.append(f"[bold]Status:[/bold] {run.status.value}")
        stats_lines.append(f"[bold]Elapsed:[/bold] {run.elapsed_time()}")
        stats_lines.append(f"[bold]Agent:[/bold] {run.agent or '-'}")
        if run.model:
            from .widgets import model_display_name

            model_str = model_display_name(run.model, run.model_variant)
            stats_lines.append(f"[bold]Model:[/bold] {model_str}")
        if run.branch:
            stats_lines.append(f"[bold]Branch:[/bold] {run.branch}")
        if run.pr_url:
            stats_lines.append(f"[bold]PR:[/bold] {run.pr_url}")

        # Add chat messages or session output to stats
        if run.agent == "opencode" and run.server_port and run.opencode_session_id:
            stats_lines.append("")
            stats_lines.append("[bold]Chat Messages:[/bold]")
            messages = self._fetch_opencode_messages(run)
            if messages:
                for msg in messages[-8:]:
                    role = msg.get("role", "?")
                    text = msg.get("text", "")[:150]
                    if text:
                        color = "cyan" if role == "assistant" else "green"
                        stats_lines.append(f"[{color}]{role}:[/{color}] {text}")
            else:
                stats_lines.append("[dim]No messages yet[/dim]")
        elif run.tmux_session:
            stats_lines.append("")
            stats_lines.append("[bold]Session Output:[/bold]")
            output = self._capture_session_output(run)
            if output:
                for line in output[-12:]:
                    stats_lines.append(f"[dim]{line}[/dim]")
            else:
                stats_lines.append("[dim]No output captured[/dim]")

        tabs_panel.update_stats("\n".join(stats_lines))

        # === Issue Tab ===
        issue = self._get_issue_for_run(run)
        if issue:
            issue_lines = [f"[bold]{issue.title or issue.id}[/bold]"]
            if issue.tags:
                issue_lines.append(f"[dim]Tags: {', '.join(issue.tags)}[/dim]")
            if issue.status:
                issue_lines.append(f"[dim]Status: {issue.status.value}[/dim]")
            issue_lines.append("")
            if issue.body:
                issue_lines.append(issue.body)
            elif issue.summary:
                issue_lines.append(issue.summary)
            tabs_panel.update_issue("\n".join(issue_lines))
        else:
            tabs_panel.update_issue(f"[dim]Issue not found: {run.issue_id}[/dim]")

        # === Changes Tab ===
        if run.worktree_path and run.branch:
            diff_stats = _get_git_diff_stats(
                run.worktree_path, run.branch, self.config.base_branch
            )
            if diff_stats and diff_stats.files:
                changes_lines = [
                    f"[bold]Changed Files ({diff_stats.file_count}):[/bold]"
                ]
                changes_lines.append(
                    f"[bold]Total: [green]+{diff_stats.total_additions}[/green] "
                    f"[red]-{diff_stats.total_deletions}[/red][/bold]"
                )
                changes_lines.append("")
                for fc in diff_stats.files:
                    add_str = f"[green]+{fc.additions}[/green]" if fc.additions else ""
                    del_str = f"[red]-{fc.deletions}[/red]" if fc.deletions else ""
                    changes_lines.append(f"  {fc.path}  {add_str} {del_str}")
                tabs_panel.update_changes("\n".join(changes_lines))
            else:
                tabs_panel.update_changes("[dim]No changes detected[/dim]")
        else:
            tabs_panel.update_changes("[dim]No worktree or branch information[/dim]")

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
        self._do_attach(self.selected_run)

    @work(thread=True)
    def _do_attach(self, run: Run) -> None:
        """Attach to run in background thread to avoid blocking TUI."""
        current_mux_type = detect_current_multiplexer()

        attach_cmd = _build_orch_cmd(self.config) + ["attach", run.ref()]

        if current_mux_type:
            current_mux = get_multiplexer(current_mux_type)

            # Check for cross-session Zellij attach (which doesn't work)
            if current_mux_type == MultiplexerType.ZELLIJ:
                run_mux_type = get_multiplexer_type_from_run(run)
                if run_mux_type == MultiplexerType.ZELLIJ:
                    current_session = current_mux.get_current_session()
                    run_session = get_session_name(run)
                    if current_session and run_session and current_session != run_session:
                        # Can't attach to different Zellij session from inside Zellij
                        cmd_str = " ".join(attach_cmd)
                        self.call_from_thread(
                            self.notify,
                            f"Cannot attach to different Zellij session from inside Zellij.\n"
                            f"Run in a separate terminal: {cmd_str}",
                            severity="warning",
                            timeout=15,
                        )
                        return

            tab_name = f"{run.issue_id}[{run.short_id()}]"
            if current_mux.new_tab_with_command(tab_name, attach_cmd):
                self.call_from_thread(self.notify, f"Opened tab: {tab_name}")
                return
            self.call_from_thread(
                self.notify,
                "Failed to create tab, falling back to exit",
                severity="warning",
            )

        self.call_from_thread(self._exit_and_attach, attach_cmd)

    def _exit_and_attach(self, attach_cmd: list[str]) -> None:
        """Exit TUI and run attach command (must be called from main thread)."""
        self.exit()
        subprocess.run(attach_cmd)

    def action_stop(self) -> None:
        if _input_has_focus(self):
            return
        run_ref = getattr(self, "_highlighted_run_ref", None)
        if not run_ref:
            self.notify("No run selected", severity="warning")
            return
        self._do_stop(run_ref)
        self.notify(f"Stopping {run_ref}")

    @work(thread=True)
    def _do_stop(self, run_ref: str) -> None:
        try:
            cmd = _build_orch_cmd(self.config) + ["stop", run_ref]
            subprocess.run(cmd, check=True)
            self.call_from_thread(self.refresh_data)
        except subprocess.CalledProcessError:
            pass

    def action_diff(self) -> None:
        """Show git diff for selected run."""
        if _input_has_focus(self):
            return
        if not self.selected_run:
            self.notify("No run selected", severity="warning")
            return
        if not self.selected_run.worktree_path:
            self.notify("Run has no worktree", severity="warning")
            return
        self._do_diff_runs(self.selected_run)

    @work(thread=True)
    def _do_diff_runs(self, run: Run) -> None:
        """Open diff in a new terminal tab."""
        current_mux_type = detect_current_multiplexer()

        diff_cmd = _build_orch_cmd(self.config) + ["diff", run.ref()]

        if current_mux_type:
            current_mux = get_multiplexer(current_mux_type)
            tab_name = f"diff:{run.short_id()}"
            if current_mux.new_tab_with_command(tab_name, diff_cmd):
                self.call_from_thread(self.notify, f"Opened diff: {tab_name}")
                return
            self.call_from_thread(
                self.notify,
                "Failed to create tab, falling back to exit",
                severity="warning",
            )

        self.call_from_thread(self._exit_and_diff_runs, diff_cmd)

    def _exit_and_diff_runs(self, diff_cmd: list[str]) -> None:
        """Exit TUI and run diff command."""
        self.exit()
        subprocess.run(diff_cmd)

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

            stop_cmd = _build_orch_cmd(self.config) + ["stop", run_ref]
            stop_result = subprocess.run(stop_cmd, capture_output=True)

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
    CSS = COMMON_CSS

    BINDINGS = [
        Binding("question_mark", "help", "Help", key_display="?"),
        Binding("q", "quit", "Quit"),
        Binding("r", "refresh", "Refresh"),
        Binding("enter", "open_issue", "Open in Editor"),
        Binding("n", "new_run", "New Run"),
        Binding("o", "open_issue", "Open in Editor", show=False),
        Binding("x", "close_issue", "Close Issue"),
        Binding("f", "filter", "Filter"),
        Binding("ctrl+f", "clear_filters", "Clear Filters"),
    ]

    def __init__(self, issues_root: Optional[Path] = None, auto_refresh: bool = True):
        super().__init__()
        if issues_root:
            self.config = Config.from_issues_root(issues_root)
        else:
            self.config = Config.load()
        self.daemon = DaemonClient(self.config.socket_path, self.config.issues_root)
        self.issues: list[Issue] = []
        self.selected_issue: Optional[Issue] = None
        self._highlighted_issue_id: Optional[str] = None
        self.filter_state = self.config.load_filters()
        self._auto_refresh_enabled = auto_refresh
        self._base_title = f"Issues [{self.config.project_root.name}]"
        self.title = self._base_title
        self._daemon_error: Optional[str] = None
        self._last_update: Optional[datetime] = None
        self._monitor_registration = MonitorRegistration(
            self.daemon, str(self.config.project_root), "issues"
        )

    def compose(self) -> ComposeResult:
        yield Header(show_clock=False)
        with Container(id="main-container"):
            yield IssueTable(id="issues-table")
        yield Footer()

    def on_mount(self) -> None:
        self._monitor_registration.register()
        self._update_title()
        self.refresh_data()
        if self._auto_refresh_enabled:
            self.set_interval(AUTO_REFRESH_INTERVAL, self._do_auto_refresh)

    def on_unmount(self) -> None:
        self._monitor_registration.unregister()

    def _update_title(self) -> None:
        count = self.filter_state.issue_filter_count()
        if count > 0:
            self.title = f"Issues [{self.config.project_root.name}] ({count} filters)"
        else:
            self.title = f"Issues [{self.config.project_root.name}]"

    def _do_auto_refresh(self) -> None:
        self.refresh_data()

    def action_refresh(self) -> None:
        if _input_has_focus(self):
            return
        self.refresh_data()

    def action_help(self) -> None:
        """Show help screen with keybindings and workflow tips."""
        self.push_screen(HelpScreen())

    def action_filter(self) -> None:
        if _input_has_focus(self):
            return
        self.push_screen(
            IssueFilterScreen(self.filter_state.issue_filters), self.on_filter_result
        )

    def action_clear_filters(self) -> None:
        if _input_has_focus(self):
            return
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
                tags=sorted(result.tags),
                tag_mode=result.tag_mode,
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
            issues = None
            error = "Daemon not running"
        except DaemonError as e:
            issues = None
            error = str(e)

        if issues is not None:
            issues = filter_issues_client_side(issues, self.filter_state.issue_filters)
            issues.sort(key=lambda i: i.id)
        self.call_from_thread(self._update_issues_table, issues, error)

    def _update_issues_table(
        self, issues: Optional[list[Issue]], error: Optional[str]
    ) -> None:
        self._last_update = datetime.now()

        if error:
            self._daemon_error = error
            self.title = f"{self._base_title} | ERROR: {error}"
            self.notify(f"Refresh failed: {error}", severity="error", timeout=5)
            _log_error("list_issues", error, self.config.project_root)
            return

        self._daemon_error = None
        if issues is not None:
            self.issues = issues
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
        if _input_has_focus(self):
            return
        if not self.selected_issue:
            self.notify("No issue selected", severity="warning")
            return

        # Get file path (creates temp file for GitHub issues)
        file_path, error = _get_issue_file_path(self.selected_issue)
        if error or file_path is None:
            self.notify(error or "Unknown error", severity="error")
            return

        cmd, error = _get_editor_command(file_path)
        if error or cmd is None:
            self.notify(error or "Unknown error", severity="error")
            return

        current_mux_type = detect_current_multiplexer()

        if current_mux_type:
            # Open in new multiplexer tab/window
            current_mux = get_multiplexer(current_mux_type)
            tab_name = f"edit-{self.selected_issue.id}"
            if current_mux.new_tab_with_command(tab_name, cmd):
                self.notify(f"Opened tab: {tab_name}")
                return
            self.notify(
                "Failed to create tab, falling back to suspend", severity="warning"
            )

        # Fallback: Suspend TUI, open editor, resume on exit
        with self.suspend():
            subprocess.run(cmd)
        self.refresh_data()

    def action_new_run(self) -> None:
        if _input_has_focus(self):
            return
        if not self.selected_issue:
            self.notify("No issue selected", severity="warning")
            return
        agents = _get_available_agents(self.config)
        self.push_screen(
            AgentSelectScreen(self.selected_issue.id, agents),
            self._on_agent_selected,
        )

    def _on_agent_selected(self, agent: str | None) -> None:
        if agent and self.selected_issue:
            issue_id = self.selected_issue.id
            self.notify(f"Starting run for {issue_id} with {agent}...")
            self._do_new_run(issue_id, agent)

    @work(thread=True, exclusive=True)
    def _do_new_run(self, issue_id: str, agent: str) -> None:
        log = get_logger()
        try:
            cmd = _build_orch_cmd(self.config) + ["run", issue_id, "--agent", agent]

            result = subprocess.run(cmd, capture_output=True, text=True, timeout=60)
            if result.returncode == 0:
                self.call_from_thread(
                    self.notify, f"Run started for {issue_id}", severity="information"
                )
            else:
                error_msg = (
                    result.stderr.strip() or result.stdout.strip() or "Unknown error"
                )
                if len(error_msg) > 200:
                    error_msg = error_msg[:200] + "..."
                log.error(
                    f"Failed to start run for {issue_id}: exit={result.returncode}, "
                    f"stderr={result.stderr!r}, stdout={result.stdout!r}"
                )
                self.call_from_thread(
                    self.notify,
                    f"Failed to start run (exit {result.returncode}): {error_msg}",
                    severity="error",
                )
            self.call_from_thread(self.refresh_data)
        except subprocess.TimeoutExpired:
            log.error(f"Timeout starting run for {issue_id}")
            self.call_from_thread(self.notify, "Timeout starting run", severity="error")
        except Exception as e:
            log.exception(f"Exception starting run for {issue_id}")
            self.call_from_thread(
                self.notify, f"Failed to start run: {e}", severity="error"
            )

    def action_close_issue(self) -> None:
        if _input_has_focus(self):
            return
        if not self.selected_issue:
            self.notify("No issue selected", severity="warning")
            return

        issue_id = self.selected_issue.id
        issue_title = self.selected_issue.title

        def on_confirm(confirmed: bool | None) -> None:
            if confirmed:
                self._do_close_issue(issue_id)

        self.push_screen(
            CloseIssueConfirmScreen(issue_id, issue_title),
            on_confirm,
        )

    @work(thread=True, exclusive=True)
    def _do_close_issue(self, issue_id: str) -> None:
        log = get_logger()
        try:
            cmd = _build_orch_cmd(self.config) + ["issue", "close", issue_id]

            result = subprocess.run(cmd, capture_output=True, text=True, timeout=60)
            if result.returncode == 0:
                self.call_from_thread(
                    self.notify, f"Closed issue {issue_id}", severity="information"
                )
            else:
                error_msg = (
                    result.stderr.strip() or result.stdout.strip() or "Unknown error"
                )
                if len(error_msg) > 200:
                    error_msg = error_msg[:200] + "..."
                log.error(
                    f"Failed to close issue {issue_id}: exit={result.returncode}, "
                    f"stderr={result.stderr!r}, stdout={result.stdout!r}"
                )
                self.call_from_thread(
                    self.notify,
                    f"Failed to close issue: {error_msg}",
                    severity="error",
                )
            self.call_from_thread(self.refresh_data)
        except subprocess.TimeoutExpired:
            log.error(f"Timeout closing issue {issue_id}")
            self.call_from_thread(
                self.notify, "Timeout closing issue", severity="error"
            )
        except Exception as e:
            log.exception(f"Exception closing issue {issue_id}")
            self.call_from_thread(
                self.notify, f"Failed to close issue: {e}", severity="error"
            )


class OrchMonitorApp(App):
    CSS = (
        COMMON_CSS
        + """
    #tables-container {
        height: 60%;
    }
    """
    )

    BINDINGS = [
        Binding("question_mark", "help", "Help", key_display="?"),
        Binding("q", "quit", "Quit"),
        Binding("r", "refresh", "Refresh"),
        Binding("enter", "select", "Select"),
        Binding("a", "attach", "Attach"),
        Binding("s", "stop", "Stop"),
        Binding("X", "kill_session", "Kill"),
        Binding("n", "new_run", "New Run"),
        Binding("o", "open_issue", "Open in Editor"),
        Binding("x", "close_issue", "Close Issue"),
        Binding("f", "filter", "Filter"),
        Binding("ctrl+f", "clear_filters", "Clear Filters"),
        Binding("d", "diff", "Diff"),
        Binding("tab", "switch_focus", "Switch Focus"),
    ]

    def __init__(self, issues_root: Optional[Path] = None, auto_refresh: bool = True):
        super().__init__()

        if issues_root:
            self.config = Config.from_issues_root(issues_root)
        else:
            self.config = Config.load()

        self.daemon = DaemonClient(self.config.socket_path, self.config.issues_root)
        self.runs: list[Run] = []
        self.issues: list[Issue] = []
        self.selected_run: Optional[Run] = None
        self.selected_issue: Optional[Issue] = None
        self.current_focus = "runs"
        self._highlighted_issue_id: Optional[str] = None
        self.filter_state = self.config.load_filters()
        self._auto_refresh_enabled = auto_refresh
        self._base_title = f"Orch Monitor [{self.config.project_root.name}]"
        self.title = self._base_title
        self._daemon_error: Optional[str] = None
        self._last_update: Optional[datetime] = None
        self._monitor_registration = MonitorRegistration(
            self.daemon, str(self.config.project_root), "combined"
        )

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
        self._monitor_registration.register()
        self._update_tab_titles()
        self.refresh_data()
        if self._auto_refresh_enabled:
            self.set_interval(AUTO_REFRESH_INTERVAL, self._do_auto_refresh)
            self.set_interval(MESSAGE_REFRESH_INTERVAL, self._do_message_refresh)
        self.set_interval(ELAPSED_UPDATE_INTERVAL, self._update_elapsed_times)

    def on_unmount(self) -> None:
        self._monitor_registration.unregister()

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
        if _input_has_focus(self):
            return
        self.refresh_data()

    def action_help(self) -> None:
        """Show help screen with keybindings and workflow tips."""
        self.push_screen(HelpScreen())

    def action_filter(self) -> None:
        if _input_has_focus(self):
            return
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
        if _input_has_focus(self):
            return
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
                tags=sorted(result.tags),
                tag_mode=result.tag_mode,
                text_search=result.text_search,
            )
            self.config.save_filters(self.filter_state)
            self._update_tab_titles()
            self.refresh_data()

    def refresh_data(self) -> None:
        self._fetch_all_data()

    @work(thread=True, exclusive=True)
    def _fetch_all_data(self) -> None:
        runs: Optional[list[Run]] = None
        issues: Optional[list[Issue]] = None
        error: Optional[str] = None

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
        except DaemonNotRunningError:
            error = "Daemon not running"
        except DaemonError as e:
            error = str(e)

        if runs is not None:
            runs = filter_runs_client_side(runs, self.filter_state.run_filters)
            runs.sort(
                key=lambda r: r.updated_at or r.started_at or datetime.min, reverse=True
            )

        try:
            issues_response = self.daemon.list_issues()
            issues = issues_response.issues
        except DaemonError as e:
            if error is None:
                error = str(e)

        if issues is not None:
            issues = filter_issues_client_side(issues, self.filter_state.issue_filters)
            issues.sort(key=lambda i: i.id)

        self.call_from_thread(self._update_all_tables, runs, issues, error)

    def _update_all_tables(
        self,
        runs: Optional[list[Run]],
        issues: Optional[list[Issue]],
        error: Optional[str],
    ) -> None:
        self._last_update = datetime.now()

        if error:
            self._daemon_error = error
            self.title = f"{self._base_title} | ERROR: {error}"
            self.notify(f"Refresh failed: {error}", severity="error", timeout=5)
            _log_error("fetch_all", error, self.config.project_root)
            return

        self._daemon_error = None
        if runs is not None:
            self.runs = runs
        if issues is not None:
            self.issues = issues
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
        issue_id, run_id = run_ref.rsplit("#", 1)
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
        # Add changed files section (uses helper for consistent formatting)
        if run.worktree_path and run.branch:
            diff_stats = _get_git_diff_stats(
                run.worktree_path, run.branch, self.config.base_branch
            )
            changed_lines = _format_changed_files_lines(
                diff_stats, max_files=15, path_width=40
            )
            if changed_lines:
                content_lines.append("")
                content_lines.append("[bold]" + "-" * 50 + "[/bold]")
                content_lines.extend(changed_lines)

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
        if _input_has_focus(self):
            return
        if not self.selected_run:
            self.notify("No run selected", severity="warning")
            return
        self._do_attach(self.selected_run)

    @work(thread=True)
    def _do_attach(self, run: Run) -> None:
        current_mux_type = detect_current_multiplexer()

        attach_cmd = _build_orch_cmd(self.config) + ["attach", run.ref()]

        if current_mux_type:
            current_mux = get_multiplexer(current_mux_type)

            # Check for cross-session Zellij attach (which doesn't work)
            if current_mux_type == MultiplexerType.ZELLIJ:
                run_mux_type = get_multiplexer_type_from_run(run)
                if run_mux_type == MultiplexerType.ZELLIJ:
                    current_session = current_mux.get_current_session()
                    run_session = get_session_name(run)
                    if current_session and run_session and current_session != run_session:
                        # Can't attach to different Zellij session from inside Zellij
                        cmd_str = " ".join(attach_cmd)
                        self.call_from_thread(
                            self.notify,
                            f"Cannot attach to different Zellij session from inside Zellij.\n"
                            f"Run in a separate terminal: {cmd_str}",
                            severity="warning",
                            timeout=15,
                        )
                        return

            tab_name = f"{run.issue_id}[{run.short_id()}]"
            if current_mux.new_tab_with_command(tab_name, attach_cmd):
                self.call_from_thread(self.notify, f"Opened tab: {tab_name}")
                return
            self.call_from_thread(
                self.notify,
                "Failed to create tab, falling back to exit",
                severity="warning",
            )

        self.call_from_thread(self._exit_and_attach, attach_cmd)

    def _exit_and_attach(self, attach_cmd: list[str]) -> None:
        self.exit()
        subprocess.run(attach_cmd)

    def action_stop(self) -> None:
        if _input_has_focus(self):
            return
        run_ref = getattr(self, "_highlighted_run_ref", None)
        if not run_ref:
            self.notify("No run selected", severity="warning")
            return
        self._do_stop(run_ref)
        self.notify(f"Stopping {run_ref}")

    @work(thread=True)
    def _do_stop(self, run_ref: str) -> None:
        try:
            cmd = _build_orch_cmd(self.config) + ["stop", run_ref]
            subprocess.run(cmd, check=True)
            self.call_from_thread(self.refresh_data)
        except subprocess.CalledProcessError:
            pass

    def action_diff(self) -> None:
        """Show git diff for selected run."""
        if _input_has_focus(self):
            return
        if not self.selected_run:
            self.notify("No run selected", severity="warning")
            return
        if not self.selected_run.worktree_path:
            self.notify("Run has no worktree", severity="warning")
            return
        self._do_diff(self.selected_run)

    @work(thread=True)
    def _do_diff(self, run: Run) -> None:
        """Open diff in a new terminal tab."""
        current_mux_type = detect_current_multiplexer()

        diff_cmd = _build_orch_cmd(self.config) + ["diff", run.ref()]

        if current_mux_type:
            current_mux = get_multiplexer(current_mux_type)
            tab_name = f"diff:{run.short_id()}"
            if current_mux.new_tab_with_command(tab_name, diff_cmd):
                self.call_from_thread(self.notify, f"Opened diff: {tab_name}")
                return
            self.call_from_thread(
                self.notify,
                "Failed to create tab, falling back to exit",
                severity="warning",
            )

        self.call_from_thread(self._exit_and_diff, diff_cmd)

    def _exit_and_diff(self, diff_cmd: list[str]) -> None:
        """Exit TUI and run diff command."""
        self.exit()
        subprocess.run(diff_cmd)

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

            stop_cmd = _build_orch_cmd(self.config) + ["stop", run_ref]
            stop_result = subprocess.run(stop_cmd, capture_output=True)

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
        if _input_has_focus(self):
            return
        if not self.selected_issue:
            self.notify("No issue selected", severity="warning")
            return
        agents = _get_available_agents(self.config)
        self.push_screen(
            AgentSelectScreen(self.selected_issue.id, agents),
            self._on_agent_selected,
        )

    def _on_agent_selected(self, agent: str | None) -> None:
        if agent and self.selected_issue:
            issue_id = self.selected_issue.id
            self.notify(f"Starting run for {issue_id} with {agent}...")
            self._do_new_run(issue_id, agent)

    @work(thread=True, exclusive=True)
    def _do_new_run(self, issue_id: str, agent: str) -> None:
        log = get_logger()
        try:
            cmd = _build_orch_cmd(self.config) + ["run", issue_id, "--agent", agent]

            result = subprocess.run(cmd, capture_output=True, text=True, timeout=60)
            if result.returncode == 0:
                self.call_from_thread(
                    self.notify, f"Run started for {issue_id}", severity="information"
                )
            else:
                error_msg = (
                    result.stderr.strip() or result.stdout.strip() or "Unknown error"
                )
                if len(error_msg) > 200:
                    error_msg = error_msg[:200] + "..."
                log.error(
                    f"Failed to start run for {issue_id}: exit={result.returncode}, "
                    f"stderr={result.stderr!r}, stdout={result.stdout!r}"
                )
                self.call_from_thread(
                    self.notify,
                    f"Failed to start run (exit {result.returncode}): {error_msg}",
                    severity="error",
                )
            self.call_from_thread(self.refresh_data)
        except subprocess.TimeoutExpired:
            log.error(f"Timeout starting run for {issue_id}")
            self.call_from_thread(self.notify, "Timeout starting run", severity="error")
        except Exception as e:
            log.exception(f"Exception starting run for {issue_id}")
            self.call_from_thread(
                self.notify, f"Failed to start run: {e}", severity="error"
            )

    def action_open_issue(self) -> None:
        if _input_has_focus(self):
            return
        if not self.selected_issue:
            self.notify("No issue selected", severity="warning")
            return

        # Get file path (creates temp file for GitHub issues)
        file_path, error = _get_issue_file_path(self.selected_issue)
        if error or file_path is None:
            self.notify(error or "Unknown error", severity="error")
            return

        cmd, error = _get_editor_command(file_path)
        if error or cmd is None:
            self.notify(error or "Unknown error", severity="error")
            return

        current_mux_type = detect_current_multiplexer()

        if current_mux_type:
            # Open in new multiplexer tab/window
            current_mux = get_multiplexer(current_mux_type)
            tab_name = f"edit-{self.selected_issue.id}"
            if current_mux.new_tab_with_command(tab_name, cmd):
                self.notify(f"Opened tab: {tab_name}")
                return
            self.notify(
                "Failed to create tab, falling back to suspend", severity="warning"
            )

        # Fallback: Suspend TUI, open editor, resume on exit
        with self.suspend():
            subprocess.run(cmd)
        self.refresh_data()

    def action_select(self) -> None:
        if self.current_focus == "runs" and self.selected_run:
            self.action_attach()
        elif self.current_focus == "issues" and self.selected_issue:
            self.action_open_issue()

    def action_close_issue(self) -> None:
        if _input_has_focus(self):
            return
        if not self.selected_issue:
            self.notify("No issue selected", severity="warning")
            return

        # Capture issue_id before modal to prevent race condition if user navigates
        issue_id = self.selected_issue.id
        issue_title = self.selected_issue.title

        def on_confirm(confirmed: bool | None) -> None:
            if confirmed:
                self._do_close_issue(issue_id)

        self.push_screen(
            CloseIssueConfirmScreen(issue_id, issue_title),
            on_confirm,
        )

    @work(thread=True, exclusive=True)
    def _do_close_issue(self, issue_id: str) -> None:
        log = get_logger()
        try:
            cmd = _build_orch_cmd(self.config) + ["issue", "close", issue_id]

            result = subprocess.run(cmd, capture_output=True, text=True, timeout=60)
            if result.returncode == 0:
                self.call_from_thread(
                    self.notify, f"Closed issue {issue_id}", severity="information"
                )
            else:
                error_msg = (
                    result.stderr.strip() or result.stdout.strip() or "Unknown error"
                )
                if len(error_msg) > 200:
                    error_msg = error_msg[:200] + "..."
                log.error(
                    f"Failed to close issue {issue_id}: exit={result.returncode}, "
                    f"stderr={result.stderr!r}, stdout={result.stdout!r}"
                )
                self.call_from_thread(
                    self.notify,
                    f"Failed to close issue: {error_msg}",
                    severity="error",
                )
            self.call_from_thread(self.refresh_data)
        except subprocess.TimeoutExpired:
            log.error(f"Timeout closing issue {issue_id}")
            self.call_from_thread(
                self.notify, "Timeout closing issue", severity="error"
            )
        except Exception as e:
            log.exception(f"Exception closing issue {issue_id}")
            self.call_from_thread(
                self.notify, f"Failed to close issue: {e}", severity="error"
            )
