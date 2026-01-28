;; Filter screens for runs and issues

(require orch_monitor.macros [with-fallback-silent safe-dismiss defon])

(import textual [on])
(import textual.screen [ModalScreen])
(import textual.binding [Binding])
(import textual.containers [Vertical Horizontal])
(import textual.widgets [Label Button Input RadioButton RadioSet SelectionList])
(import textual.app [ComposeResult])

(import orch_monitor.models [Status IssueStatus])
(import orch_monitor.config [RunFilterState IssueFilterState])
(import orch_monitor.types [RunFilterResult IssueFilterResult])
(import orch_monitor.app_base [FILTER_SCREEN_CSS AGENTS TIME_RANGES])

;; ============================================================================
;; RunFilterScreen
;; ============================================================================

(defclass RunFilterScreen [(get ModalScreen (| RunFilterResult None))]
  
  (setv CSS (+ "
    RunFilterScreen {
        align: center middle;
    }
    " FILTER_SCREEN_CSS))
  
  (setv BINDINGS
    [(Binding "escape" "cancel" "Cancel")
     (Binding "enter" "apply" "Apply")])
  
  (defn __init__ [self current-filter]
    (.__init__ (super))
    (setv self.current_filter current-filter))
  
  (defn on_mount [self]
    (with-fallback-silent "focus_search" None
      (.focus (.query_one self "#text-search-input" Input))))
  
  (defn compose [self]
    (with [(Vertical :id "filter-dialog")]
      (yield (Label "Filter Runs" :id "filter-title"))
      
      (with [(Horizontal :id "filter-buttons")]
        (yield (Button "Apply" :variant "primary" :id "apply-btn"))
        (yield (Button "Clear" :id "clear-btn"))
        (yield (Button "Cancel" :id "cancel-btn")))
      
      (yield (Label "Status" :classes "filter-section-title"))
      (setv status-items
        (lfor status Status
          :if (!= status Status.UNKNOWN)
          #(status.value
            status.value
            (or (in status.value self.current_filter.statuses)
                (not self.current_filter.statuses)))))
      (yield ((get SelectionList str) #* status-items :id "status-list"))
      
      (yield (Label "Agent" :classes "filter-section-title"))
      (setv agent-items
        (lfor agent AGENTS
          #(agent
            agent
            (or (in agent self.current_filter.agents)
                (not self.current_filter.agents)))))
      (yield ((get SelectionList str) #* agent-items :id "agent-list"))
      
      (yield (Label "Time Range" :classes "filter-section-title"))
      (with [(RadioSet :id "time-range")]
        (for [#(value label) TIME_RANGES]
          (yield (RadioButton label
                   :value (= self.current_filter.time_range value)
                   :id f"time-{value}"))))
      
      (yield (Label "Search" :classes "filter-section-title"))
      (yield (Input :value self.current_filter.text_search
                    :placeholder "ID, branch, issue..."
                    :id "text-search-input"))))
  
  (defon (Button.Pressed "#apply-btn") apply_filter [self]
    (setv status-list (.query_one self "#status-list" SelectionList))
    (setv statuses (sfor v status-list.selected (Status v)))
    
    (setv agent-list (.query_one self "#agent-list" SelectionList))
    (setv agents (set agent-list.selected))
    
    (setv time-range "all")
    (for [#(value _) TIME_RANGES]
      (setv radio (.query_one self f"#time-{value}" RadioButton))
      (when radio.value
        (setv time-range value)
        (break)))
    
    (setv text-search (. (.query_one self "#text-search-input" Input) value))
    
    (safe-dismiss self (RunFilterResult :statuses statuses
                                       :agents agents
                                       :text_search text-search
                                       :time_range time-range)))
  
  (defon (Button.Pressed "#clear-btn") clear_filter [self]
    (setv status-list (.query_one self "#status-list" SelectionList))
    (.select_all status-list)
    
    (setv agent-list (.query_one self "#agent-list" SelectionList))
    (.select_all agent-list)
    
    (setv all-time-radio (.query_one self "#time-all" RadioButton))
    (setv all-time-radio.value True)
    
    (setv (. (.query_one self "#text-search-input" Input) value) ""))
  
  (defon (Button.Pressed "#cancel-btn") cancel [self]
    (safe-dismiss self None))
  
  (defn action_cancel [self]
    (.dismiss self None))
  
  (defn action_apply [self]
    (.apply_filter self)))

;; ============================================================================
;; IssueFilterScreen
;; ============================================================================

(defclass IssueFilterScreen [(get ModalScreen (| IssueFilterResult None))]
  
  (setv CSS (+ "
    IssueFilterScreen {
        align: center middle;
    }
    " FILTER_SCREEN_CSS))
  
  (setv BINDINGS
    [(Binding "escape" "cancel" "Cancel")
     (Binding "enter" "apply" "Apply")])
  
  (defn __init__ [self current-filter]
    (.__init__ (super))
    (setv self.current_filter current-filter))
  
  (defn on_mount [self]
    (with-fallback-silent "focus_search" None
      (.focus (.query_one self "#text-search-input" Input))))
  
  (defn compose [self]
    (with [(Vertical :id "filter-dialog")]
      (yield (Label "Filter Issues" :id "filter-title"))
      
      (with [(Horizontal :id "filter-buttons")]
        (yield (Button "Apply" :variant "primary" :id "apply-btn"))
        (yield (Button "Clear" :id "clear-btn"))
        (yield (Button "Cancel" :id "cancel-btn")))
      
      (yield (Label "Status" :classes "filter-section-title"))
      (setv status-items
        (lfor status IssueStatus
          #(status.value
            status.value
            (or (in status.value self.current_filter.statuses)
                (not self.current_filter.statuses)))))
      (yield ((get SelectionList str) #* status-items :id "issue-status-list"))
      
      (yield (Label "Tags (comma-separated)" :classes "filter-section-title"))
      (setv tag-value (if self.current_filter.tags
                          (.join ", " self.current_filter.tags)
                          ""))
      (yield (Input :value tag-value
                    :placeholder "bug, urgent, feature..."
                    :id "tag-filter-input"))
      
      (yield (Label "Tag Mode" :classes "filter-section-title"))
      (with [(RadioSet :id "tag-mode-set")]
        (yield (RadioButton "Any (OR)"
                 :value (!= self.current_filter.tag_mode "all")
                 :id "tag-mode-any"))
        (yield (RadioButton "All (AND)"
                 :value (= self.current_filter.tag_mode "all")
                 :id "tag-mode-all")))
      
      (yield (Label "Search" :classes "filter-section-title"))
      (yield (Input :value self.current_filter.text_search
                    :placeholder "ID, title, summary..."
                    :id "text-search-input"))))
  
  (defon (Button.Pressed "#apply-btn") apply_filter [self]
    (setv status-list (.query_one self "#issue-status-list" SelectionList))
    (setv statuses (sfor v status-list.selected (IssueStatus v)))
    (setv text-search (. (.query_one self "#text-search-input" Input) value))
    
    (setv tag-input (. (.query_one self "#tag-filter-input" Input) value))
    (setv tags (sfor t (.split tag-input ",") :if (.strip t) (.strip t)))
    
    (setv tag-mode-all (.query_one self "#tag-mode-all" RadioButton))
    (setv tag-mode (if tag-mode-all.value "all" "any"))
    
    (safe-dismiss self (IssueFilterResult :statuses statuses
                                         :priorities (set)
                                         :tags tags
                                         :tag_mode tag-mode
                                         :text_search text-search)))
  
  (defon (Button.Pressed "#clear-btn") clear_filter [self]
    (setv status-list (.query_one self "#issue-status-list" SelectionList))
    (.select_all status-list)
    (setv (. (.query_one self "#tag-filter-input" Input) value) "")
    (setv (. (.query_one self "#tag-mode-any" RadioButton) value) True)
    (setv (. (.query_one self "#text-search-input" Input) value) ""))
  
  (defon (Button.Pressed "#cancel-btn") cancel [self]
    (safe-dismiss self None))
  
  (defn action_cancel [self]
    (.dismiss self None))
  
  (defn action_apply [self]
    (.apply_filter self)))

(setv __all__ ["RunFilterScreen" "IssueFilterScreen"])
