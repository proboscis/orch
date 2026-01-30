;; AgentSelectScreen - Modal for selecting agent/preset before starting a run

(require orch_monitor.macros [with-fallback-silent safe-dismiss])

(import textual.screen [ModalScreen])
(import textual.binding [Binding])
(import textual.containers [Vertical])
(import textual.widgets [Label SelectionList])
(import textual.app [ComposeResult])

(setv AGENT_SELECT_CSS "
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
")

(defclass AgentSelectScreen [(get ModalScreen (| str None))]
  (setv CSS AGENT_SELECT_CSS)
  
  (setv BINDINGS
    [(Binding "escape" "cancel" "Cancel")
     (Binding "enter" "confirm" "Start" :priority True)
     (Binding "k" "cursor_up" "Up" :show False)
     (Binding "j" "cursor_down" "Down" :show False)
     (Binding "1" "quick_select_1" "1" :show False)
     (Binding "2" "quick_select_2" "2" :show False)
     (Binding "3" "quick_select_3" "3" :show False)
     (Binding "4" "quick_select_4" "4" :show False)
     (Binding "5" "quick_select_5" "5" :show False)
     (Binding "6" "quick_select_6" "6" :show False)
     (Binding "7" "quick_select_7" "7" :show False)
     (Binding "8" "quick_select_8" "8" :show False)
     (Binding "9" "quick_select_9" "9" :show False)])
  
  (defn __init__ [self issue-id agents]
    (.__init__ (super))
    (setv self.issue_id issue-id)
    (setv self.agents agents))
  
  (defn compose [self]
    (with [(Vertical :id "agent-dialog")]
      (yield (Label "Select Agent" :id "agent-title"))
      (yield (Label f"Issue: {self.issue_id}" :id "agent-issue"))
      (if self.agents
          (do
            (setv items (lfor #(i agent) (enumerate self.agents)
                          #(f"[{(+ i 1)}] {agent}" agent (= i 0))))
            (yield ((get SelectionList str) #* items :id "agent-selection-list"))
            (yield (Label "[Enter] Start  [1-9] Quick select  [Esc] Cancel" :id "agent-footer")))
          (do
            (yield (Label "No agents available" :id "agent-empty"))
            (yield (Label "[Esc] cancel" :id "agent-footer"))))))
  
  (defn on_mount [self]
    (when self.agents
      (with-fallback-silent "focus_agent_list" None
        (.focus (.query_one self "#agent-selection-list" SelectionList)))))
  
  (defn action_cursor_up [self]
    (when self.agents
      (with-fallback-silent "cursor_up" None
        (setv sel (.query_one self "#agent-selection-list" SelectionList))
        (.action_cursor_up sel))))
  
  (defn action_cursor_down [self]
    (when self.agents
      (with-fallback-silent "cursor_down" None
        (setv sel (.query_one self "#agent-selection-list" SelectionList))
        (.action_cursor_down sel))))
  
  (defn action_confirm [self]
    (when (not self.agents)
      (safe-dismiss self None)
      (return))
    (with-fallback-silent "confirm_agent" None
      (setv sel (.query_one self "#agent-selection-list" SelectionList))
      (if (is-not sel.highlighted None)
          (safe-dismiss self (get self.agents sel.highlighted))
          (do
            (setv selected sel.selected)
            (for [agent self.agents]
              (when (in agent selected)
                (safe-dismiss self agent)
                (return)))
            (safe-dismiss self None)))))
  
  (defn action_cancel [self]
    (safe-dismiss self None))
  
  (defn _quick_select [self index]
    (when (and (>= index 0) (< index (len self.agents)))
      (safe-dismiss self (get self.agents index))))
  
  ;; Quick select actions (1-9)
  (defn action_quick_select_1 [self] (._quick_select self 0))
  (defn action_quick_select_2 [self] (._quick_select self 1))
  (defn action_quick_select_3 [self] (._quick_select self 2))
  (defn action_quick_select_4 [self] (._quick_select self 3))
  (defn action_quick_select_5 [self] (._quick_select self 4))
  (defn action_quick_select_6 [self] (._quick_select self 5))
  (defn action_quick_select_7 [self] (._quick_select self 6))
  (defn action_quick_select_8 [self] (._quick_select self 7))
  (defn action_quick_select_9 [self] (._quick_select self 8)))

(setv __all__ ["AgentSelectScreen"])
