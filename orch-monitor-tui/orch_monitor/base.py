"""Base classes to reduce duplication in the TUI."""

from typing import Generic, TypeVar, Optional, Literal
from textual.binding import Binding
from textual.screen import ModalScreen
from textual.app import ComposeResult
from textual.containers import Container, Vertical, Horizontal
from textual.widgets import Button, Label, Static
from textual import on

T = TypeVar("T")

ButtonVariant = Literal["default", "primary", "success", "warning", "error"]


class ConfirmDialogScreen(ModalScreen[bool]):
    """Flexible confirmation dialog base class.

    Subclass and override:
    - compose_details() - yield widgets for the details section
    - compose_info() - yield widgets for the info/consequences section

    Set class variables for configuration:
    - DIALOG_ID: CSS ID prefix (e.g., "kill" -> #kill-dialog, etc.)
    - DIALOG_COLOR: Border/title color (error, warning, etc.)
    - CONFIRM_LABEL: Text for confirm button
    - CONFIRM_VARIANT: Button variant for confirm button
    """

    # Override these in subclasses
    DIALOG_ID: str = "confirm"
    DIALOG_COLOR: str = "warning"
    CONFIRM_LABEL: str = "Yes"
    CONFIRM_VARIANT: ButtonVariant = "warning"

    BINDINGS = [
        Binding("y", "confirm", "Yes"),
        Binding("n", "cancel", "No"),
        Binding("escape", "cancel", "Cancel"),
    ]

    def __init__(self, title: str):
        super().__init__()
        self._title = title

    def compose_details(self) -> ComposeResult:
        """Override to yield widgets for the details section."""
        yield from ()

    def compose_info(self) -> ComposeResult:
        """Override to yield widgets for the info/consequences section."""
        yield from ()

    def compose(self) -> ComposeResult:
        d = self.DIALOG_ID
        with Vertical(id=f"{d}-dialog"):
            yield Label(self._title, id=f"{d}-title")
            with Vertical(id=f"{d}-details"):
                yield from self.compose_details()
            with Vertical(id=f"{d}-info"):
                yield from self.compose_info()
            with Horizontal(id=f"{d}-buttons"):
                yield Button(
                    self.CONFIRM_LABEL,
                    variant=self.CONFIRM_VARIANT,
                    id="confirm-btn",
                )
                yield Button("No, cancel", id="cancel-btn")

    @on(Button.Pressed, "#confirm-btn")
    def _on_confirm(self) -> None:
        self.dismiss(True)

    @on(Button.Pressed, "#cancel-btn")
    def _on_cancel(self) -> None:
        self.dismiss(False)

    def action_confirm(self) -> None:
        self.dismiss(True)

    def action_cancel(self) -> None:
        self.dismiss(False)


# Legacy simple ConfirmScreen for backwards compatibility
class ConfirmScreen(ModalScreen[bool], Generic[T]):
    """Simple confirmation modal with just title and message."""

    BINDINGS = [
        ("escape", "cancel", "Cancel"),
        ("enter", "confirm", "Confirm"),
    ]

    CSS = """
    ConfirmScreen {
        align: center middle;
    }
    ConfirmScreen > Container {
        width: 60;
        height: auto;
        border: thick $background 80%;
        background: $surface;
        padding: 1 2;
    }
    ConfirmScreen .title {
        text-style: bold;
        margin-bottom: 1;
    }
    ConfirmScreen .buttons {
        margin-top: 1;
        height: 3;
    }
    ConfirmScreen Button {
        margin: 0 1;
    }
    """

    def __init__(self, title: str, message: str):
        super().__init__()
        self._title = title
        self._message = message

    def compose(self) -> ComposeResult:
        with Container():
            yield Label(self._title, classes="title")
            yield Static(self._message)
            with Horizontal(classes="buttons"):
                yield Button("Cancel", id="cancel", variant="default")
                yield Button("Confirm", id="confirm", variant="error")

    def on_button_pressed(self, event: Button.Pressed) -> None:
        self.dismiss(event.button.id == "confirm")

    def action_confirm(self) -> None:
        self.dismiss(True)

    def action_cancel(self) -> None:
        self.dismiss(False)
