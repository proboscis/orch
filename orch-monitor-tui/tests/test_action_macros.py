"""Tests for defaction guard semantics."""

import types

import hy
import pytest


def eval_hy(code: str):
    full_code = f"""
    (require orch_monitor.macros [defaction])
    {code}
    """
    mod = types.ModuleType("test_module")
    return hy.eval(hy.read_many(full_code), mod.__dict__)


class TestDefactionGuards:
    def test_require_run_binds_run_from_sync_lookup(self):
        result = eval_hy(
            """
            (defclass Run []
              (defn __init__ [self path]
                (setv self.worktree_path path)))
            (defclass App []
              (defn __init__ [self]
                (setv self._highlighted_run_ref "orch-1#r1")
                (setv self._runs_by_ref {"orch-1#r1" (Run "/tmp/worktree")})
                (setv self._highlighted_issue_id None)
                (setv self._issues_by_id {})
                (setv self.calls []))
              (defn notify [self msg #** kwargs]
                (.append self.calls msg))
              (defaction action_run_path [self] [:require-run]
                run.worktree_path))
            (setv app (App))
            (.action_run_path app)
            """
        )
        assert result == "/tmp/worktree"

    def test_require_run_missing_highlight_notifies(self):
        result = eval_hy(
            """
            (defclass App []
              (defn __init__ [self]
                (setv self._highlighted_run_ref None)
                (setv self._runs_by_ref {})
                (setv self._highlighted_issue_id None)
                (setv self._issues_by_id {})
                (setv self.calls []))
              (defn notify [self msg #** kwargs]
                (.append self.calls msg))
              (defaction action_check [self] [:require-run]
                "ok"))
            (setv app (App))
            (.action_check app)
            app.calls
            """
        )
        assert result == ["No run selected"]

    def test_require_run_missing_entry_notifies(self):
        result = eval_hy(
            """
            (defclass App []
              (defn __init__ [self]
                (setv self._highlighted_run_ref "orch-1#gone")
                (setv self._runs_by_ref {})
                (setv self._highlighted_issue_id None)
                (setv self._issues_by_id {})
                (setv self.calls []))
              (defn notify [self msg #** kwargs]
                (.append self.calls msg))
              (defaction action_check [self] [:require-run]
                "ok"))
            (setv app (App))
            (.action_check app)
            app.calls
            """
        )
        assert result == ["Run no longer available"]

    def test_require_issue_binds_issue_from_sync_lookup(self):
        result = eval_hy(
            """
            (defclass Issue []
              (defn __init__ [self issue-id]
                (setv self.id issue-id)))
            (defclass App []
              (defn __init__ [self]
                (setv self._highlighted_run_ref None)
                (setv self._runs_by_ref {})
                (setv self._highlighted_issue_id "orch-9")
                (setv self._issues_by_id {"orch-9" (Issue "orch-9")})
                (setv self.calls []))
              (defn notify [self msg #** kwargs]
                (.append self.calls msg))
              (defaction action_issue_id [self] [:require-issue]
                issue.id))
            (setv app (App))
            (.action_issue_id app)
            """
        )
        assert result == "orch-9"

    def test_defaction_rejects_selected_run_access(self):
        with pytest.raises(Exception, match="self.selected_run"):
            eval_hy(
                """
                (defclass App []
                  (defaction action_bad [self] [:require-run]
                    self.selected_run))
                """
            )

    def test_defaction_rejects_selected_issue_access(self):
        with pytest.raises(Exception, match="self.selected_issue"):
            eval_hy(
                """
                (defclass App []
                  (defaction action_bad [self] [:require-issue]
                    self.selected_issue))
                """
            )
