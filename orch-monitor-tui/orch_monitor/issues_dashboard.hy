;; IssuesDashboard - Hy implementation with enforced error handling
;; All error handling uses macros to ensure no silent failures

(require orch_monitor.macros [with-fallback with-fallback-silent must-succeed
                              daemon-result result-unwrap-or set->
                              when-ok when-err if-ok ok-or when-some when-none
                              defaction with-issue-actions])

;; ============================================================================
;; Standard library imports
;; ============================================================================

(import logging)
(import os)
(import subprocess)

(import datetime [datetime])
(import pathlib [Path])
(import typing [Optional])

;; ============================================================================
;; Textual imports
;; ============================================================================

(import textual [on work])
(import textual.app [App ComposeResult])
(import textual.binding [Binding])
(import textual.containers [Container])
(import textual.widgets [Footer Header Input])

;; ============================================================================
;; Local imports
;; ============================================================================

(import orch_monitor.config [Config IssueFilterState])
(import orch_monitor.models [Issue IssueStatus])
(import orch_monitor.types [IssueFilterResult])
(import orch_monitor.orch_api [OrchAPI create_orch_api])
(import orch_monitor.orch_api [IssueFilters :as ApiIssueFilters
                               IssueStatus :as ApiIssueStatus])
(import orch_monitor.converters [api_issue_to_model :as _api_issue_to_model
                                 api_issues_to_model :as _api_issues_to_model_issues])
(import orch_monitor.confirm_screens [CloseIssueConfirmScreen])
(import orch_monitor.filter_screens [IssueFilterScreen])
(import orch_monitor.help_screen [HelpScreen])
(import orch_monitor.agent_screen [AgentSelectScreen])
(import orch_monitor.multiplexer [detect_current_multiplexer get_multiplexer])
(import orch_monitor.widgets [IssueTable])
(import orch_monitor.app_base [get-logger _log-error _get-editor-command
                               _get-issue-file-path _input-has-focus
                               _get-available-agents
                               AUTO_REFRESH_INTERVAL])
(import orch_monitor.runs_dashboard [COMMON_CSS])

;; ============================================================================
;; IssuesDashboard Class
;; ============================================================================

(defclass IssuesDashboard [App]
  "Dashboard for viewing and managing issues."
  
  (setv CSS COMMON_CSS)
  
  (setv BINDINGS [(Binding "question_mark" "help" "Help" :key_display "?")
                  (Binding "q" "quit" "Quit")
                  (Binding "r" "refresh" "Refresh")
                  (Binding "enter" "open_issue" "Open in Editor")
                  (Binding "n" "new_run" "New Run")
                  (Binding "o" "open_issue" "Open in Editor" :show False)
                  (Binding "x" "close_issue" "Close Issue")
                  (Binding "f" "filter" "Filter")
                  (Binding "ctrl+f" "clear_filters" "Clear Filters")])
  
  ;; =========================================================================
  ;; Initialization
  ;; =========================================================================
  
  (defn __init__ [self [issues-root None] [auto-refresh True] [api None]]
    (.__init__ (super))
    (setv self.config (if issues-root
                          (Config.from_issues_root issues-root)
                          (Config.load)))
    (setv self.api (or api (create_orch_api self.config.socket_path
                                            self.config.issues_root)))
    (setv self.issues [])
    (setv self.selected_issue None)
    (setv self._highlighted_issue_id None)
    (setv self.filter_state (.load_filters self.config))
    (setv self._auto_refresh_enabled auto-refresh)
    (setv self._base_title f"Issues [{self.config.project_root.name}]")
    (setv self.title self._base_title)
    (setv self._daemon_error None)
    (setv self._last_update None)
    (setv self._monitor_id None)
    (setv self._is_loading False))
  
  ;; =========================================================================
  ;; Compose UI
  ;; =========================================================================
  
  (defn compose [self]
    (yield (Header :show_clock False))
    (with [(Container :id "main-container")]
      (yield (IssueTable :id "issues-table")))
    (yield (Footer)))
  
  ;; =========================================================================
  ;; Lifecycle
  ;; =========================================================================
  
  (defn on_mount [self]
    (when-ok [monitor-id (.register_monitor self.api
                          :pid (os.getpid)
                          :monitor_type "python"
                          :view "issues"
                          :project (str self.config.project_root))]
      (setv self._monitor_id monitor-id))
    (._update_title self)
    (.refresh_data self)
    (when self._auto_refresh_enabled
      (.set_interval self AUTO_REFRESH_INTERVAL self._do_auto_refresh)))
  
  (defn on_unmount [self]
    (when self._monitor_id
      (.unregister_monitor self.api self._monitor_id)))
  
  ;; =========================================================================
  ;; Helper methods
  ;; =========================================================================
  
  (defn _update_title [self]
    (setv count (.issue_filter_count self.filter_state))
    (setv self.title
      (if (> count 0)
          f"Issues [{self.config.project_root.name}] ({count} filters)"
          f"Issues [{self.config.project_root.name}]")))
  
  (defn _do_auto_refresh [self]
    (.refresh_data self))
  
  ;; =========================================================================
  ;; Injected actions — new-run, open-issue, close-issue (shared with OrchMonitorApp)
  ;; =========================================================================
  
  (with-issue-actions)
  
  ;; =========================================================================
  ;; Dashboard-specific actions
  ;; =========================================================================
  
  (defaction action_refresh [self] [:guard-input]
    (.refresh_data self))
  
  (defn action_help [self]
    (.push_screen self (HelpScreen)))
  
  (defaction action_filter [self] [:guard-input]
    (.push_screen self
      (IssueFilterScreen self.filter_state.issue_filters)
      self.on_filter_result))
  
  (defaction action_clear_filters [self] [:guard-input]
    (.clear_issue_filters self.filter_state)
    (.save_filters self.config self.filter_state)
    (._update_title self)
    (.refresh_data self)
    (.notify self "Filters cleared"))
  
  (defn on_filter_result [self result]
    (when (is-not result None)
      (setv all-statuses (set IssueStatus))
      (setv selected-statuses
        (if (= result.statuses all-statuses)
            []
            (lfor s result.statuses s.value)))
      (setv self.filter_state.issue_filters
        (IssueFilterState :statuses selected-statuses
                          :priorities []
                          :tags (sorted result.tags)
                          :tag_mode result.tag_mode
                          :text_search result.text_search))
      (.save_filters self.config self.filter_state)
      (._update_title self)
      (.refresh_data self)))
  
  ;; =========================================================================
  ;; Data fetching
  ;; =========================================================================
  
  (defn refresh_data [self]
    (setv self._is_loading True)
    (cond
      self._daemon_error
        (setv self.title f"{self._base_title} | Reconnecting...")
      (not self.issues)
        (setv self.title f"{self._base_title} | Loading..."))
    (._fetch_issues self))
  
  (defn [(work :thread True :exclusive True)] _fetch_issues [self]
    (setv issue-filters self.filter_state.issue_filters)
    (setv status-filter [])
    (for [s issue-filters.statuses]
      ;; Use with-fallback-silent for enum parsing
      (with-fallback-silent "parse_issue_status" None
        (.append status-filter (ApiIssueStatus s))))
    (setv filters (ApiIssueFilters
                    :status status-filter
                    :tags (if issue-filters.tags (list issue-filters.tags) [])
                    :tags_mode (or issue-filters.tag_mode "or")
                    :text_search (or issue-filters.text_search None)))
    (if-ok [response (.list_issues self.api filters)]
      (do
        (setv issues (_api_issues_to_model_issues response.issues))
        (.sort issues :key (fn [i] i.id) :reverse True)
        (.call_from_thread self self._update_issues_table issues None))
      (.call_from_thread self self._update_issues_table None (str response))))
  
  (defn _update_issues_table [self issues error]
    (setv self._is_loading False)
    (setv self._last_update (.now datetime))
    (when error
      (setv self._daemon_error error)
      (setv self.title f"{self._base_title} | Disconnected")
      (setv log-path (_log-error "list_issues" error self.config.project_root
                       :socket-path self.config.socket_path))
      (.notify self f"Daemon unavailable: {error}\nSee {log-path}"
               :severity "warning" :timeout 5)
      (return))
    (setv self._daemon_error None)
    (when (is-not issues None)
      (setv self.issues issues))
    (setv time-str (.strftime self._last_update "%H:%M:%S"))
    (setv self.title f"{self._base_title} | {time-str}")
    (setv issue-table (.query_one self "#issues-table" IssueTable))
    (.populate issue-table self.issues))
  
  ;; =========================================================================
  ;; Row selection events
  ;; =========================================================================
  
  (defn [(on IssueTable.RowHighlighted)] on_issue_highlighted [self event]
    "Track highlighted issue for Enter key open functionality."
    (setv issue-id (if event.row_key event.row_key.value None))
    (when (not issue-id)
      (setv self._highlighted_issue_id None)
      (return))
    ;; Skip if already highlighted
    (when (= (getattr self "_highlighted_issue_id" None) issue-id)
      (return))
    (setv self._highlighted_issue_id issue-id)
    (._fetch_issue_detail self issue-id))
  
  (defn [(on IssueTable.RowSelected)] on_issue_selected [self event]
    "Handle Enter key on issue - open in editor."
    (.action_open_issue self))
  
  (defn [(work :thread True :exclusive True)] _fetch_issue_detail [self issue-id]
    (setv raw (ok-or (.get_issue self.api issue-id) None))
    (setv issue (when-some [i raw] (_api_issue_to_model i)))
    (.call_from_thread self self._set_selected_issue issue issue-id))
  
  (defn _set_selected_issue [self issue issue-id]
    ;; Only apply if this is still the highlighted issue
    (when (= (getattr self "_highlighted_issue_id" None) issue-id)
      (setv self.selected_issue issue)))
  
  ;; NOTE: open_issue, new_run, close_issue actions are injected by (with-issue-actions) above
  )


;; ============================================================================
;; Exports
;; ============================================================================

(setv __all__ ["IssuesDashboard"])
