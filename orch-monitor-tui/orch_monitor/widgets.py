"""Custom widgets for orch monitor TUI."""

from typing import Optional

from rich.markup import escape
from textual.app import ComposeResult
from textual.binding import Binding
from textual.message import Message
from textual.widgets import DataTable, Static, TabbedContent, TabPane
from textual.widgets.data_table import RowDoesNotExist
from textual.containers import Container, VerticalScroll

from .models import DiffStats, Issue, Run, Status

MAX_MODEL_DISPLAY_WIDTH = 18


def shorten_model_name(model: str) -> str:
    if not model:
        return ""

    if "/" in model:
        model = model.rsplit("/", 1)[-1]

    model = model.lower()

    if model.startswith("claude-"):
        name = model[7:]
        return _format_claude_model(name)

    if model.startswith("gpt-"):
        name = model[4:]
        return "gpt" + _format_version(name)

    if model.startswith("o") and len(model) >= 2 and model[1].isdigit():
        return model

    if model.startswith("gemini-"):
        name = model[7:]
        return "gemini" + _format_version(name)

    return model[:12] if len(model) > 12 else model


def _format_claude_model(name: str) -> str:
    parts = name.split("-")
    if len(parts) < 2:
        return name

    if parts[0].isdigit():
        numeric_parts = [parts[0]]
        model_idx = 1
        for i in range(1, len(parts)):
            if parts[i].isdigit():
                numeric_parts.append(parts[i])
                model_idx = i + 1
            else:
                break
        if model_idx < len(parts):
            model_type = parts[model_idx]
            version = ".".join(numeric_parts)
            return model_type + version
        return ".".join(numeric_parts)

    model_type = parts[0]
    version = _format_version("-".join(parts[1:]))
    return model_type + version


def _format_version(version: str) -> str:
    if not version:
        return ""

    parts = version.split("-")
    if len(parts) == 1:
        return version

    if all(p.isdigit() for p in parts):
        return ".".join(parts)

    return version


def model_display_name(model: str, variant: str) -> str:
    if not model:
        return "-"

    short_model = shorten_model_name(model)
    if not short_model:
        return "-"

    if not variant:
        return short_model

    result = f"{short_model}/{variant}"
    if len(result) > MAX_MODEL_DISPLAY_WIDTH:
        return result[:MAX_MODEL_DISPLAY_WIDTH]
    return result


def format_diff_number(value: int) -> str:
    """Format a diff number, abbreviating large values.

    Examples:
        0 -> "0"
        99 -> "99"
        1234 -> "1.2k"
        12345 -> "12.3k"
    """
    if value < 1000:
        return str(value)
    elif value < 10000:
        return f"{value / 1000:.1f}k"
    else:
        return f"{value // 1000}k"


class CursorPreservingTable(DataTable):
    """DataTable that preserves cursor position across repopulation."""

    BINDINGS = [
        ("j", "cursor_down", "Down"),
        ("k", "cursor_up", "Up"),
        ("g", "scroll_top", "Top"),
        ("G", "scroll_bottom", "Bottom"),
    ]

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.cursor_type = "row"
        self.zebra_stripes = True

    def action_cursor_down(self) -> None:
        if self.has_focus:
            super().action_cursor_down()

    def action_cursor_up(self) -> None:
        if self.has_focus:
            super().action_cursor_up()

    def action_scroll_top(self) -> None:
        if self.has_focus:
            super().action_scroll_top()

    def action_scroll_bottom(self) -> None:
        if self.has_focus:
            super().action_scroll_bottom()

    def _get_current_row_key(self) -> Optional[str]:
        if self.row_count == 0:
            return None
        try:
            row_key, _ = self.coordinate_to_cell_key(self.cursor_coordinate)
            if row_key is None or row_key.value is None:
                return None
            return str(row_key.value)
        except (KeyError, IndexError):
            return None

    def _restore_cursor(
        self, saved_key: Optional[str], saved_index: int, total_rows: int
    ) -> None:
        if total_rows == 0:
            return

        if saved_key:
            try:
                new_index = self.get_row_index(saved_key)
                self.move_cursor(row=new_index)
                return
            except RowDoesNotExist:
                pass

        if saved_index < total_rows:
            self.move_cursor(row=saved_index)
        elif total_rows > 0:
            self.move_cursor(row=total_rows - 1)

    def _save_cursor_state(self) -> tuple[Optional[str], int]:
        saved_key = self._get_current_row_key()
        saved_index = self.cursor_coordinate.row if self.row_count > 0 else 0
        return saved_key, saved_index


def short_agent_status(status: Status) -> str:
    mapping = {
        Status.QUEUED: "queue",
        Status.BOOTING: "boot",
        Status.RUNNING: "run",
        Status.BLOCKED: "block",
        Status.BLOCKED_API: "block",
        Status.PR_OPEN: "pr",
        Status.DONE: "done",
        Status.FAILED: "fail",
        Status.CANCELED: "cancel",
        Status.UNKNOWN: "?",
    }
    return mapping.get(status, "?")


def color_agent_status(status: Status) -> str:
    short = short_agent_status(status)
    colors = {
        Status.RUNNING: "green",
        Status.BOOTING: "green",
        Status.BLOCKED: "yellow",
        Status.BLOCKED_API: "yellow",
        Status.FAILED: "red",
        Status.DONE: "blue",
        Status.PR_OPEN: "cyan",
        Status.QUEUED: "white",
        Status.CANCELED: "dim",
        Status.UNKNOWN: "magenta",
    }
    color = colors.get(status, "")
    if color:
        return f"[{color}]{short}[/{color}]"
    return short


def color_branch_status(branch_status: str) -> str:
    colors = {
        "clean": "green",
        "dirty": "yellow",
        "merged": "blue",
        "conflict": "red",
    }
    color = colors.get(branch_status, "")
    if color:
        return f"[{color}]{branch_status}[/{color}]"
    return branch_status


def color_pr_status(pr_status: str) -> str:
    colors = {
        "open": "cyan",
        "merged": "green",
        "closed": "dim",
    }
    color = colors.get(pr_status, "")
    if color:
        return f"[{color}]{pr_status}[/{color}]"
    return pr_status


def derive_pr_status(run: Run, branch_status: str = "") -> str:
    if run.status == Status.DONE and run.pr_url:
        return "merged"
    if branch_status == "merged" and run.pr_url:
        return "merged"
    if run.pr_url or run.status == Status.PR_OPEN:
        return "open"
    return "-"


class RunTable(CursorPreservingTable):
    def populate(
        self,
        runs: list[Run],
        diff_stats: Optional[dict[str, DiffStats]] = None,
        branch_states: Optional[dict[str, str]] = None,
    ) -> None:
        saved_key, saved_index = self._save_cursor_state()

        self.clear(columns=True)

        self.add_column("ID", width=8)
        self.add_column("Issue", width=15)
        self.add_column("Agent", width=7)
        self.add_column("Branch", width=8)
        self.add_column("PR", width=7)
        self.add_column("CLI", width=10)
        self.add_column("Model", width=18)
        self.add_column("+", width=6)
        self.add_column("-", width=6)
        self.add_column("Mux", width=4)
        self.add_column("Elapsed", width=10, key="elapsed")

        diff_stats = diff_stats or {}
        branch_states = branch_states or {}

        for run in runs:
            agent_str = color_agent_status(run.status)

            branch_status = branch_states.get(run.ref(), "-")
            branch_str = color_branch_status(branch_status)

            pr_status = derive_pr_status(run, branch_status)
            pr_str = color_pr_status(pr_status)

            model_str = model_display_name(run.model, run.model_variant)
            mux = run.multiplexer
            if mux == "zellij":
                mux_short = "zj"
            elif mux == "tmux":
                mux_short = "tx"
            else:
                mux_short = "-"

            stats = diff_stats.get(run.ref())
            if stats:
                add_str = f"[green]+{format_diff_number(stats.total_additions)}[/green]"
                del_str = f"[red]-{format_diff_number(stats.total_deletions)}[/red]"
            else:
                add_str = "-"
                del_str = "-"

            self.add_row(
                run.short_id(),
                run.issue_id,
                agent_str,
                branch_str,
                pr_str,
                run.agent or "-",
                model_str,
                add_str,
                del_str,
                mux_short,
                run.elapsed_time(),
                key=run.ref(),
            )

        self._restore_cursor(saved_key, saved_index, len(runs))


class IssueTable(CursorPreservingTable):
    """Table widget for displaying issues."""

    def populate(self, issues: list[Issue]) -> None:
        saved_key, saved_index = self._save_cursor_state()

        self.clear(columns=True)

        self.add_column("ID", width=15)
        self.add_column("Status", width=10)
        self.add_column("Tags", width=20)
        self.add_column("Title", width=50)

        for issue in issues:
            status_str = issue.status.value
            if issue.status.value == "open":
                status_str = f"[green]{status_str}[/green]"
            elif issue.status.value == "resolved":
                status_str = f"[blue]{status_str}[/blue]"
            elif issue.status.value == "closed":
                status_str = f"[dim]{status_str}[/dim]"

            # Format tags with color
            tags_str = self._format_tags_display(issue.tags)

            self.add_row(
                issue.id,
                status_str,
                tags_str,
                issue.title or issue.summary,
                key=issue.id,
            )

        self._restore_cursor(saved_key, saved_index, len(issues))

    def _format_tags_display(self, tags: list[str]) -> str:
        """Format tags for display with color coding."""
        if not tags:
            return "-"

        formatted = []
        for tag in tags:
            safe_tag = escape(tag)
            tag_lower = tag.lower()
            if tag_lower in ("bug", "bugfix", "fix"):
                formatted.append(f"[red]\\[{safe_tag}][/red]")
            elif tag_lower in ("urgent", "critical", "high"):
                formatted.append(f"[bold red]\\[{safe_tag}][/bold red]")
            elif tag_lower in ("enhancement", "feature", "new"):
                formatted.append(f"[green]\\[{safe_tag}][/green]")
            elif tag_lower in ("refactor", "cleanup", "chore"):
                formatted.append(f"[cyan]\\[{safe_tag}][/cyan]")
            elif tag_lower in ("docs", "documentation"):
                formatted.append(f"[yellow]\\[{safe_tag}][/yellow]")
            elif tag_lower in ("test", "testing"):
                formatted.append(f"[magenta]\\[{safe_tag}][/magenta]")
            else:
                formatted.append(f"[dim]\\[{safe_tag}][/dim]")

        if len(tags) > 2:
            return " ".join(formatted[:2]) + "..."
        return " ".join(formatted)


class DetailPanel(Container):
    """Panel for displaying detailed content."""

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.content_widget = Static("", id="detail-content")

    def compose(self) -> ComposeResult:
        yield self.content_widget

    def update_content(self, content: str, title: str = "") -> None:
        if title:
            markup = f"[bold]{title}[/bold]\n\n{content}"
        else:
            markup = content
        self.content_widget.update(markup)

    def clear(self) -> None:
        self.content_widget.update("")


class ScrollableTabContent(VerticalScroll):
    """A scrollable content area for a tab with scroll position preservation."""

    SCROLL_AMOUNT = 3  # Lines to scroll per key press

    def __init__(self, tab_id: str, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.tab_id = tab_id
        self._content = Static("", id=f"{tab_id}-content")
        self._saved_scroll_y: float = 0.0

    def compose(self) -> ComposeResult:
        yield self._content

    def update_content(self, content: str) -> None:
        """Update the content and restore scroll position."""
        self._content.update(content)

    def save_scroll_position(self) -> None:
        """Save current scroll position."""
        self._saved_scroll_y = self.scroll_y

    def restore_scroll_position(self) -> None:
        """Restore saved scroll position."""
        self.scroll_y = self._saved_scroll_y

    def scroll_down_lines(self, lines: int = 1) -> None:
        """Scroll down by specified number of lines."""
        self.scroll_y += lines * self.SCROLL_AMOUNT

    def scroll_up_lines(self, lines: int = 1) -> None:
        """Scroll up by specified number of lines."""
        self.scroll_y = max(0, self.scroll_y - lines * self.SCROLL_AMOUNT)

    def scroll_to_top(self) -> None:
        """Scroll to the top of the content."""
        self.scroll_y = 0

    def scroll_to_bottom(self) -> None:
        """Scroll to the bottom of the content."""
        self.scroll_end(animate=False)

    @property
    def can_scroll(self) -> bool:
        """Check if content overflows and can be scrolled."""
        return self.max_scroll_y > 0

    @property
    def scroll_indicator(self) -> str:
        """Return scroll indicator string if scrollable."""
        if not self.can_scroll:
            return ""

        at_top = self.scroll_y <= 0
        at_bottom = self.scroll_y >= self.max_scroll_y

        if at_top and not at_bottom:
            return "[scroll ↓]"
        elif at_bottom and not at_top:
            return "[scroll ↑]"
        elif not at_top and not at_bottom:
            return "[scroll ↕]"
        return ""


class TabbedStatsPanel(Container):
    """Tabbed panel for run details with Stats, Issue, and Changes tabs.

    Supports keyboard navigation:
    - Tab or 1/2/3: Switch between tabs
    - j/k or arrows: Scroll within current tab
    - g/G: Jump to top/bottom of current tab
    """

    BINDINGS = [
        Binding("tab", "next_tab", "Next Tab", show=False),
        Binding("1", "switch_tab('stats')", "Stats", show=False),
        Binding("2", "switch_tab('issue')", "Issue", show=False),
        Binding("3", "switch_tab('changes')", "Changes", show=False),
        Binding("j", "scroll_down", "Down", show=False),
        Binding("k", "scroll_up", "Up", show=False),
        Binding("down", "scroll_down", "Down", show=False),
        Binding("up", "scroll_up", "Up", show=False),
        Binding("g", "scroll_top", "Top", show=False),
        Binding("G", "scroll_bottom", "Bottom", show=False),
    ]

    class TabChanged(Message):
        """Posted when the active tab changes."""

        def __init__(self, tab_id: str) -> None:
            super().__init__()
            self.tab_id = tab_id

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self._stats_content = ScrollableTabContent("stats", id="stats-scroll")
        self._issue_content = ScrollableTabContent("issue", id="issue-scroll")
        self._changes_content = ScrollableTabContent("changes", id="changes-scroll")
        self._scroll_positions: dict[str, float] = {
            "stats": 0.0,
            "issue": 0.0,
            "changes": 0.0,
        }
        self._current_tab: str = "stats"

    def compose(self) -> ComposeResult:
        with TabbedContent(id="run-tabs"):
            with TabPane("Stats", id="stats"):
                yield self._stats_content
            with TabPane("Issue", id="issue"):
                yield self._issue_content
            with TabPane("Changes", id="changes"):
                yield self._changes_content

    def on_tabbed_content_tab_activated(
        self, event: TabbedContent.TabActivated
    ) -> None:
        """Handle tab activation - save old position, restore new."""
        old_tab = self._current_tab
        new_tab = event.tab.id or "stats"

        # Save scroll position for old tab
        old_content = self._get_content_for_tab(old_tab)
        if old_content:
            self._scroll_positions[old_tab] = old_content.scroll_y

        # Update current tab
        self._current_tab = new_tab

        # Restore scroll position for new tab
        new_content = self._get_content_for_tab(new_tab)
        if new_content:
            new_content.scroll_y = self._scroll_positions.get(new_tab, 0.0)

        self.post_message(self.TabChanged(new_tab))

    def _get_content_for_tab(self, tab_id: str) -> Optional[ScrollableTabContent]:
        """Get the ScrollableTabContent for a given tab ID."""
        if tab_id == "stats":
            return self._stats_content
        elif tab_id == "issue":
            return self._issue_content
        elif tab_id == "changes":
            return self._changes_content
        return None

    def _get_current_content(self) -> Optional[ScrollableTabContent]:
        """Get the current tab's scrollable content."""
        return self._get_content_for_tab(self._current_tab)

    def action_next_tab(self) -> None:
        """Cycle to the next tab."""
        tabs = ["stats", "issue", "changes"]
        current_idx = tabs.index(self._current_tab) if self._current_tab in tabs else 0
        next_idx = (current_idx + 1) % len(tabs)
        self.action_switch_tab(tabs[next_idx])

    def action_switch_tab(self, tab_id: str) -> None:
        """Switch to a specific tab."""
        try:
            tabbed_content = self.query_one("#run-tabs", TabbedContent)
            tabbed_content.active = tab_id
        except Exception:
            pass

    def action_scroll_down(self) -> None:
        """Scroll current tab content down."""
        content = self._get_current_content()
        if content:
            content.scroll_down_lines()

    def action_scroll_up(self) -> None:
        """Scroll current tab content up."""
        content = self._get_current_content()
        if content:
            content.scroll_up_lines()

    def action_scroll_top(self) -> None:
        """Scroll current tab content to top."""
        content = self._get_current_content()
        if content:
            content.scroll_to_top()

    def action_scroll_bottom(self) -> None:
        """Scroll current tab content to bottom."""
        content = self._get_current_content()
        if content:
            content.scroll_to_bottom()

    def update_stats(self, content: str) -> None:
        """Update the Stats tab content."""
        self._stats_content.update_content(content)

    def update_issue(self, content: str) -> None:
        """Update the Issue tab content."""
        self._issue_content.update_content(content)

    def update_changes(self, content: str) -> None:
        """Update the Changes tab content."""
        self._changes_content.update_content(content)

    def clear_all(self) -> None:
        """Clear all tab contents."""
        self._stats_content.update_content("")
        self._issue_content.update_content("")
        self._changes_content.update_content("")
        # Reset scroll positions
        self._scroll_positions = {"stats": 0.0, "issue": 0.0, "changes": 0.0}

    @property
    def current_tab(self) -> str:
        """Get the current active tab ID."""
        return self._current_tab
