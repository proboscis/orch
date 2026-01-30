;; Confirm dialog screens for orch-monitor TUI

(require orch_monitor.macros [safe-dismiss defon])

(import textual.screen [ModalScreen])
(import textual.binding [Binding])
(import textual.containers [Vertical Horizontal])
(import textual.widgets [Button Label Static])
(import textual [on])

(import orch_monitor.models [Run])
(import orch_monitor.multiplexer [get_multiplexer_for_run get_session_name])

;; ============================================================================
;; KillConfirmScreen - Confirm killing a run's terminal session
;; ============================================================================

(defclass KillConfirmScreen [(get ModalScreen bool)]
  "Confirmation dialog for killing terminal session."
  
  (setv CSS "
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
    #kill-info {
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
  ")
  
  (setv BINDINGS [(Binding "y" "confirm" "Yes, kill")
                  (Binding "n" "cancel" "No, cancel")
                  (Binding "escape" "cancel" "Cancel")])
  
  (defn __init__ [self run]
    (.__init__ (super))
    (setv self.run run)
    (setv self.multiplexer (get_multiplexer_for_run run)))
  
  (defn compose [self]
    (setv mux-name self.multiplexer.name)
    (setv session-name (or (get_session_name self.run) "N/A"))
    (with [(Vertical :id "kill-dialog")]
      (yield (Label f"Kill {mux-name} session?" :id "kill-title"))
      (with [(Vertical :id "kill-details")]
        (yield (Static f"Run: {(self.run.ref)}"))
        (yield (Static f"Session: {session-name}")))
      (with [(Vertical :id "kill-info")]
        (yield (Static "This will:"))
        (yield (Static f"  - Kill the {mux-name} session"))
        (yield (Static "  - Mark the run as canceled"))
        (yield (Static "  - Stop any running agent")))
      (with [(Horizontal :id "kill-buttons")]
        (yield (Button "Yes, kill" :variant "error" :id "confirm-btn"))
        (yield (Button "No, cancel" :id "cancel-btn")))))
  
  (defon (Button.Pressed "#confirm-btn") confirm [self]
    (safe-dismiss self True))
  
  (defon (Button.Pressed "#cancel-btn") cancel [self]
    (safe-dismiss self False))
  
  (defn action_confirm [self]
    (safe-dismiss self True))
  
  (defn action_cancel [self]
    (safe-dismiss self False)))

;; ============================================================================
;; CloseIssueConfirmScreen - Confirm closing an issue
;; ============================================================================

(defclass CloseIssueConfirmScreen [(get ModalScreen bool)]
  "Confirmation dialog for closing an issue."
  
  (setv CSS "
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
  ")
  
  (setv BINDINGS [(Binding "y" "confirm" "Yes, close")
                  (Binding "n" "cancel" "No, cancel")
                  (Binding "escape" "cancel" "Cancel")])
  
  (defn __init__ [self issue-id [issue-title None]]
    (.__init__ (super))
    (setv self.issue-id issue-id)
    (setv self.issue-title issue-title))
  
  (defn compose [self]
    (with [(Vertical :id "close-dialog")]
      (yield (Label "Close issue?" :id "close-title"))
      (with [(Vertical :id "close-details")]
        (yield (Static f"Issue: {self.issue-id}"))
        (when self.issue-title
          (setv truncated (if (> (len (or self.issue-title "")) 50)
                              (+ (cut self.issue-title 0 50) "...")
                              self.issue-title))
          (yield (Static f"Title: {truncated}"))))
      (with [(Vertical :id "close-info")]
        (yield (Static "This will:"))
        (yield (Static "  - For GitHub issues: close on GitHub"))
        (yield (Static "  - For local issues: set status to 'closed'")))
      (with [(Horizontal :id "close-buttons")]
        (yield (Button "Yes, close" :variant "warning" :id "confirm-btn"))
        (yield (Button "No, cancel" :id "cancel-btn")))))
  
  (defon (Button.Pressed "#confirm-btn") confirm [self]
    (safe-dismiss self True))
  
  (defon (Button.Pressed "#cancel-btn") cancel [self]
    (safe-dismiss self False))
  
  (defn action_confirm [self]
    (safe-dismiss self True))
  
  (defn action_cancel [self]
    (safe-dismiss self False)))
