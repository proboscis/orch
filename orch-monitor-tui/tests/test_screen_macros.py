"""Tests for screen-related macros: safe-dismiss and defon.

safe-dismiss defers dismiss() via call_later to avoid Textual's
"Can't await screen.dismiss() from the screen's message handler" error.

defon is a compile-time enforced @on handler that rejects raw .dismiss calls,
forcing use of safe-dismiss instead.
"""

import types

import hy
import pytest


def eval_hy(code: str):
    full_code = f"""
    (require orch_monitor.macros [safe-dismiss defon])
    {code}
    """
    mod = types.ModuleType("test_module")
    return hy.eval(hy.read_many(full_code), mod.__dict__)


class TestSafeDismiss:
    """safe-dismiss defers dismiss and discards dismiss()'s AwaitComplete."""

    def test_call_later_callback_invokes_dismiss(self):
        """Verify safe-dismiss schedules a callback that calls dismiss."""
        result = eval_hy("""
        (setv calls [])
        (defclass FakeScreen []
          (defn dismiss [self val]
            (.append calls #("dismiss" val)))
          (defn call_later [self callback]
            (callback)))
        (setv screen (FakeScreen))
        (safe-dismiss screen 42)
        calls
        """)
        assert result == [("dismiss", 42)]

    def test_deferred_not_immediate(self):
        """Verify call_later is invoked, not dismiss directly."""
        result = eval_hy("""
        (setv order [])
        (defclass FakeScreen []
          (defn dismiss [self val]
            (.append order "dismiss"))
          (defn call_later [self callback #* args]
            (.append order "scheduled")))
        (setv screen (FakeScreen))
        (safe-dismiss screen True)
        order
        """)
        assert result == ["scheduled"]


class TestDefon:
    """defon generates @on-decorated handlers with compile-time .dismiss check."""

    def test_rejects_raw_dismiss(self):
        with pytest.raises(Exception, match="raw .dismiss"):
            eval_hy("""
            (import textual [on])
            (import textual.widgets [Button])
            (defon (Button.Pressed "#btn") handler [self]
              (.dismiss self True))
            """)

    def test_rejects_nested_raw_dismiss(self):
        with pytest.raises(Exception, match="raw .dismiss"):
            eval_hy("""
            (import textual [on])
            (import textual.widgets [Button])
            (defon (Button.Pressed "#btn") handler [self]
              (when True
                (do
                  (.dismiss self False))))
            """)

    def test_accepts_safe_dismiss(self):
        eval_hy("""
        (import textual [on])
        (import textual.widgets [Button])
        (defon (Button.Pressed "#btn") handler [self]
          (safe-dismiss self True))
        """)

    def test_accepts_body_without_dismiss(self):
        eval_hy("""
        (import textual [on])
        (import textual.widgets [Button])
        (defon (Button.Pressed "#btn") handler [self]
          (.notify self "hello"))
        """)


class TestDefonErrorMessages:
    """defon error messages include the handler name for debuggability."""

    def test_error_includes_handler_name(self):
        with pytest.raises(Exception, match="apply_filter"):
            eval_hy("""
            (import textual [on])
            (import textual.widgets [Button])
            (defon (Button.Pressed "#apply-btn") apply_filter [self]
              (.dismiss self True))
            """)

    def test_error_includes_different_handler_name(self):
        with pytest.raises(Exception, match="cancel"):
            eval_hy("""
            (import textual [on])
            (import textual.widgets [Button])
            (defon (Button.Pressed "#cancel-btn") cancel [self]
              (.dismiss self None))
            """)
