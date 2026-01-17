"""Custom widgets for orch monitor TUI."""

from typing import Optional

from textual.app import ComposeResult
from textual.widgets import DataTable, Static
from textual.widgets.data_table import RowDoesNotExist
from textual.containers import Container

from .models import Issue, Run, Status


class CursorPreservingTable(DataTable):
    """DataTable that preserves cursor position across repopulation."""

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.cursor_type = "row"
        self.zebra_stripes = True

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


class RunTable(CursorPreservingTable):
    """Table widget for displaying runs."""

    def populate(self, runs: list[Run]) -> None:
        saved_key, saved_index = self._save_cursor_state()

        self.clear(columns=True)

        self.add_column("ID", width=8)
        self.add_column("Issue", width=15)
        self.add_column("Status", width=12)
        self.add_column("Agent", width=10)
        self.add_column("Model", width=18)
        self.add_column("Elapsed", width=10, key="elapsed")
        self.add_column("Branch", width=25)

        for run in runs:
            status_str = run.status.value
            if run.status == Status.RUNNING:
                status_str = f"[green]{status_str}[/green]"
            elif run.status == Status.BLOCKED:
                status_str = f"[yellow]{status_str}[/yellow]"
            elif run.status == Status.FAILED:
                status_str = f"[red]{status_str}[/red]"
            elif run.status == Status.DONE:
                status_str = f"[blue]{status_str}[/blue]"
            elif run.status == Status.PR_OPEN:
                status_str = f"[cyan]{status_str}[/cyan]"

            model_str = run.model or "-"
            if run.model and run.model_variant:
                model_str = f"{run.model}/{run.model_variant}"

            self.add_row(
                run.short_id(),
                run.issue_id,
                status_str,
                run.agent or "-",
                model_str,
                run.elapsed_time(),
                run.branch or "-",
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
        self.add_column("Title", width=60)

        for issue in issues:
            status_str = issue.status.value
            if issue.status.value == "open":
                status_str = f"[green]{status_str}[/green]"
            elif issue.status.value == "resolved":
                status_str = f"[blue]{status_str}[/blue]"
            elif issue.status.value == "closed":
                status_str = f"[dim]{status_str}[/dim]"

            self.add_row(
                issue.id,
                status_str,
                issue.title or issue.summary,
                key=issue.id,
            )

        self._restore_cursor(saved_key, saved_index, len(issues))


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
