;; OrchMonitorApp - Combined runs and issues dashboard
;; Hy implementation with enforced error handling via macros

(require orch_monitor.macros [with-fallback with-fallback-silent must-succeed
                              daemon-result result-unwrap-or set->
                              when-ok when-err if-ok ok-or when-some when-none
                              defaction with-run-actions with-issue-actions])

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
(import textual.containers [Container Vertical])
(import textual.widgets [Footer Header TabbedContent TabPane Input])

;; ============================================================================
;; Local imports
;; ============================================================================

(import orch_monitor.config [Config RunFilterState IssueFilterState])
(import orch_monitor.models [DiffStats Run Issue Status IssueStatus])
(import orch_monitor.types [RunFilterResult IssueFilterResult])
(import orch_monitor.orch_api [OrchAPI create_orch_api])
(import orch_monitor.orch_api [RunFilters :as ApiRunFilters
                               RunStatus :as ApiRunStatus
                               IssueFilters :as ApiIssueFilters
                               IssueStatus :as ApiIssueStatus])
(import orch_monitor.converters [api_run_to_model :as _api_run_to_model
                                 api_runs_to_model :as _api_runs_to_model_runs
                                 api_issue_to_model :as _api_issue_to_model
                                 api_issues_to_model :as _api_issues_to_model_issues])
(import orch_monitor.confirm_screens [KillConfirmScreen CloseIssueConfirmScreen])
(import orch_monitor.filter_screens [RunFilterScreen IssueFilterScreen])
(import orch_monitor.help_screen [HelpScreen])
(import orch_monitor.agent_screen [AgentSelectScreen])
(import orch_monitor.multiplexer [Multiplexer MultiplexerType
                                  detect_current_multiplexer get_multiplexer
                                  get_multiplexer_for_run get_multiplexer_type_from_run
                                  get_session_name])
(import orch_monitor.widgets [DetailPanel IssueTable RunTable])
(import orch_monitor.app_base [get-logger _log-error _build-orch-cmd
                               _get-editor-command _get-issue-file-path
                               _input-has-focus _get-available-agents
                               AUTO_REFRESH_INTERVAL ELAPSED_UPDATE_INTERVAL
                               MESSAGE_REFRESH_INTERVAL AGENTS])
(import orch_monitor.runs_dashboard [COMMON_CSS])

;; ============================================================================
;; CSS
;; ============================================================================

(setv ORCH_MONITOR_CSS (+ COMMON_CSS "
#tables-container {
    height: 60%;
}
"))

;; ============================================================================
;; OrchMonitorApp Class
;; ============================================================================

(defclass OrchMonitorApp [App]
  "Combined dashboard for monitoring both runs and issues."
  
  (setv CSS ORCH_MONITOR_CSS)
  
  (setv BINDINGS [(Binding "question_mark" "help" "Help" :key_display "?")
                  (Binding "q" "quit" "Quit")
                  (Binding "r" "refresh" "Refresh")
                  (Binding "enter" "select" "Select")
                  (Binding "a" "attach" "Attach")
                  (Binding "s" "stop" "Stop")
                  (Binding "X" "kill_session" "Kill")
                  (Binding "n" "new_run" "New Run")
                  (Binding "o" "open_issue" "Open in Editor")
                  (Binding "x" "close_issue" "Close Issue")
                  (Binding "f" "filter" "Filter")
                  (Binding "ctrl+f" "clear_filters" "Clear Filters")
                  (Binding "d" "diff" "Diff")
                  (Binding "tab" "switch_focus" "Switch Focus")])
  
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
    (setv self.runs [])
    (setv self.issues [])
    (setv self.selected_run None)
    (setv self.selected_issue None)
    (setv self.current_focus "runs")
    (setv self._highlighted_run_ref None)
    (setv self._highlighted_issue_id None)
    (setv self.filter_state (.load_filters self.config))
    (setv self._auto_refresh_enabled auto-refresh)
    (setv self._base_title f"Orch Monitor [{self.config.project_root.name}]")
    (setv self.title self._base_title)
    (setv self._daemon_error None)
    (setv self._last_update None)
    (setv self._monitor_id None)
    (setv self._is_loading False))
  
  ;; =========================================================================
  ;; Compose UI
  ;; =========================================================================
  
  (defn compose [self]
    (yield (Header))
    (with [(Container :id "main-container")]
      (with [(TabbedContent :id "tables-container")]
        (with [(TabPane "Runs" :id "runs-pane")]
          (yield (RunTable :id "runs-table")))
        (with [(TabPane "Issues" :id "issues-pane")]
          (yield (IssueTable :id "issues-table"))))
      (with [(Vertical :id "detail-container")]
        (yield (DetailPanel :id "detail-panel"))))
    (yield (Footer)))
  
  ;; =========================================================================
  ;; Lifecycle
  ;; =========================================================================
  
  (defn on_mount [self]
    (when-ok [monitor-id (.register_monitor self.api
                          :pid (os.getpid)
                          :monitor_type "python"
                          :view "combined"
                          :project (str self.config.project_root))]
      (setv self._monitor_id monitor-id))
    (._update_tab_titles self)
    (.refresh_data self)
    (when self._auto_refresh_enabled
      (.set_interval self AUTO_REFRESH_INTERVAL self._do_auto_refresh)
      (.set_interval self MESSAGE_REFRESH_INTERVAL self._do_message_refresh))
    (.set_interval self ELAPSED_UPDATE_INTERVAL self._update_elapsed_times))
  
  (defn on_unmount [self]
    (when self._monitor_id
      (.unregister_monitor self.api self._monitor_id)))
  
  ;; =========================================================================
  ;; Timer callbacks - All use with-fallback-silent for cosmetic updates
  ;; =========================================================================
  
  (defn _update_elapsed_times [self]
    "Update elapsed time display for active runs. Silent on errors."
    (when (not self.runs)
      (return))
    ;; Query table with silent fallback - cosmetic update
    (setv run-table (with-fallback-silent "query_runs_table" None
                      (.query_one self "#runs-table" RunTable)))
    (when (is run-table None)
      (return))
    ;; Update each active run's elapsed time
    (for [run self.runs]
      (when (.is_active run)
        ;; Silent fallback for each cell update - cosmetic only
        (with-fallback-silent "update_elapsed_cell" None
          (.update_cell run-table (.ref run) "elapsed" (.elapsed_time run))))))
  
  (defn _update_tab_titles [self]
    "Update tab titles with filter counts. Silent on errors."
    ;; Use with-fallback-silent - cosmetic update that shouldn't crash TUI
    (with-fallback-silent "update_tab_titles" None
      (setv run-count (.run_filter_count self.filter_state))
      (setv issue-count (.issue_filter_count self.filter_state))
      (setv runs-pane (.query_one self "#runs-pane" TabPane))
      (setv issues-pane (.query_one self "#issues-pane" TabPane))
      ;; Note: TabPane.update() may not exist in all Textual versions
      ;; Using label attribute if available
      (when (hasattr runs-pane "label")
        (setv runs-pane.label (if (> run-count 0)
                                  f"Runs ({run-count} filters)"
                                  "Runs")))
      (when (hasattr issues-pane "label")
        (setv issues-pane.label (if (> issue-count 0)
                                    f"Issues ({issue-count} filters)"
                                    "Issues")))))
  
  (defn _do_auto_refresh [self]
    (.refresh_data self))
  
  (defn _do_message_refresh [self]
    "Refresh only the messages for the selected run (lightweight update)."
    (when (and self.selected_run
               (in self.selected_run.status [Status.RUNNING Status.BOOTING Status.BLOCKED]))
      (.show_run_detail self self.selected_run)))
  
  ;; =========================================================================
  ;; Injected actions — shared behavior via compile-time macros
  ;; =========================================================================
  
  (with-run-actions)    ;; attach, stop, kill, diff
  (with-issue-actions)  ;; new-run, open-issue, close-issue
  
  ;; =========================================================================
  ;; Dashboard-specific actions
  ;; =========================================================================
  
  (defaction action_refresh [self] [:guard-input]
    (.refresh_data self))
  
  (defn action_help [self]
    (.push_screen self (HelpScreen)))
  
  (defaction action_filter [self] [:guard-input]
    (if (= self.current_focus "runs")
        (.push_screen self
          (RunFilterScreen self.filter_state.run_filters)
          self.on_run_filter_result)
        (.push_screen self
          (IssueFilterScreen self.filter_state.issue_filters)
          self.on_issue_filter_result)))
  
  (defaction action_clear_filters [self] [:guard-input]
    (if (= self.current_focus "runs")
        (.clear_run_filters self.filter_state)
        (.clear_issue_filters self.filter_state))
    (.save_filters self.config self.filter_state)
    (._update_tab_titles self)
    (.refresh_data self)
    (.notify self "Filters cleared"))
  
  (defn on_run_filter_result [self result]
    (when (is-not result None)
      (setv all-statuses (sfor s Status :if (!= s Status.UNKNOWN) s))
      (setv selected-statuses
        (if (= result.statuses all-statuses)
            []
            (lfor s result.statuses s.value)))
      (setv all-agents (set AGENTS))
      (setv selected-agents
        (if (= result.agents all-agents)
            []
            (list result.agents)))
      (setv self.filter_state.run_filters
        (RunFilterState :statuses selected-statuses
                        :agents selected-agents
                        :text_search result.text_search
                        :time_range result.time_range))
      (.save_filters self.config self.filter_state)
      (._update_tab_titles self)
      (.refresh_data self)))
  
  (defn on_issue_filter_result [self result]
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
      (._update_tab_titles self)
      (.refresh_data self)))
  
  ;; =========================================================================
  ;; Data fetching
  ;; =========================================================================
  
  (defn refresh_data [self]
    (setv self._is_loading True)
    (cond
      self._daemon_error
        (setv self.title f"{self._base_title} | Reconnecting...")
      (and (not self.runs) (not self.issues))
        (setv self.title f"{self._base_title} | Loading..."))
    (._fetch_all_data self))
  
  (defn [(work :thread True :exclusive True)] _fetch_all_data [self]
    (setv runs None)
    (setv issues None)
    (setv error None)
    
    ;; Fetch runs
    (setv run-filters self.filter_state.run_filters)
    (setv status-filter [])
    (for [s run-filters.statuses]
      (with-fallback-silent "parse_run_status" None
        (.append status-filter (ApiRunStatus s))))
    (setv agent-filter (if (= (len run-filters.agents) 1)
                           (get run-filters.agents 0)
                           None))
    (setv run-api-filters (ApiRunFilters
                            :status status-filter
                            :agent agent-filter
                            :text_search (or run-filters.text_search None)
                            :time_range (if (!= run-filters.time_range "all")
                                            run-filters.time_range
                                            None)))
    (if-ok [runs-response (.list_runs self.api run-api-filters)]
      (setv runs (_api_runs_to_model_runs runs-response.runs))
      (setv error (str runs-response)))
    (when-some [r runs]
      (when (> (len run-filters.agents) 1)
        (setv runs (lfor r runs :if (in r.agent run-filters.agents) r)))
      (.sort runs :key (fn [r] (or r.updated_at r.started_at datetime.min)) :reverse True))
    
    ;; Fetch issues
    (setv issue-filters self.filter_state.issue_filters)
    (setv issue-status-filter [])
    (for [s issue-filters.statuses]
      (with-fallback-silent "parse_issue_status" None
        (.append issue-status-filter (ApiIssueStatus s))))
    (setv issue-api-filters (ApiIssueFilters
                              :status issue-status-filter
                              :tags (if issue-filters.tags (list issue-filters.tags) [])
                              :tags_mode (or issue-filters.tag_mode "or")
                              :text_search (or issue-filters.text_search None)))
    (if-ok [issues-response (.list_issues self.api issue-api-filters)]
      (setv issues (_api_issues_to_model_issues issues-response.issues))
      (when-none error
        (setv error (str issues-response))))
    (when-some [i issues]
      (.sort issues :key (fn [i] i.id) :reverse True))
    
    (.call_from_thread self self._update_all_tables runs issues error))
  
  (defn _update_all_tables [self runs issues error]
    (setv self._is_loading False)
    (setv self._last_update (.now datetime))
    (when error
      (setv self._daemon_error error)
      (setv self.title f"{self._base_title} | Disconnected")
      (setv log-path (_log-error "fetch_all" error self.config.project_root
                       :socket-path self.config.socket_path))
      (.notify self f"Daemon unavailable: {error}\nSee {log-path}"
               :severity "warning" :timeout 5)
      (return))
    (setv self._daemon_error None)
    (when (is-not runs None)
      (setv self.runs runs))
    (when (is-not issues None)
      (setv self.issues issues))
    (setv time-str (.strftime self._last_update "%H:%M:%S"))
    (setv self.title f"{self._base_title} | {time-str}")
    
    ;; Build diff stats and branch states
    (setv diff-stats {})
    (setv branch-states {})
    (for [run self.runs]
      (when (or (> run.additions 0) (> run.deletions 0))
        (setv (get diff-stats (.ref run))
          (DiffStats :files []
                     :total_additions run.additions
                     :total_deletions run.deletions)))
      (when run.branch_state
        (setv (get branch-states (.ref run)) run.branch_state)))
    
    (setv run-table (.query_one self "#runs-table" RunTable))
    (.populate run-table self.runs :diff_stats diff-stats :branch_states branch-states)
    (setv issue-table (.query_one self "#issues-table" IssueTable))
    (.populate issue-table self.issues))
  
  ;; =========================================================================
  ;; Focus switching
  ;; =========================================================================
  
  (defn action_switch_focus [self]
    (setv tabbed (.query_one self TabbedContent))
    (if (= tabbed.active "runs-pane")
        (do
          (setv tabbed.active "issues-pane")
          (setv self.current_focus "issues"))
        (do
          (setv tabbed.active "runs-pane")
          (setv self.current_focus "runs"))))
  
  ;; =========================================================================
  ;; Row selection events
  ;; =========================================================================
  
  (defn [(on RunTable.RowHighlighted)] on_run_highlighted [self event]
    (setv run-ref (if event.row_key event.row_key.value None))
    (when (or (not run-ref) (not (in "#" run-ref)))
      (setv self._highlighted_run_ref None)
      (return))
    (when (= (getattr self "_highlighted_run_ref" None) run-ref)
      (return))
    (setv self._highlighted_run_ref run-ref)
    (setv #(issue-id run-id) (.rsplit run-ref "#" 1))
    (._fetch_run_for_detail self issue-id run-id run-ref))
  
  (defn [(work :thread True :exclusive True)] _fetch_run_for_detail [self issue-id run-id run-ref]
    (setv raw (ok-or (.get_run self.api issue-id run-id) None))
    (setv run (when-some [r raw] (_api_run_to_model r)))
    (.call_from_thread self self._show_run_detail_callback run run-ref))
  
  (defn _show_run_detail_callback [self run run-ref]
    (when (!= (getattr self "_highlighted_run_ref" None) run-ref)
      (return))
    (when run
      (setv self.selected_run run)
      (.show_run_detail self run)))
  
  (defn [(on IssueTable.RowHighlighted)] on_issue_highlighted [self event]
    (setv issue-id (if event.row_key event.row_key.value None))
    (when (not issue-id)
      (setv self._highlighted_issue_id None)
      (return))
    (when (= (getattr self "_highlighted_issue_id" None) issue-id)
      (return))
    (setv self._highlighted_issue_id issue-id)
    (._fetch_issue_for_detail self issue-id))
  
  (defn [(on IssueTable.RowSelected)] on_issue_selected [self event]
    (when self.selected_issue
      (.show_issue_detail self self.selected_issue)))
  
  (defn [(work :thread True :exclusive True)] _fetch_issue_for_detail [self issue-id]
    (setv raw (ok-or (.get_issue self.api issue-id) None))
    (setv issue (when-some [i raw] (_api_issue_to_model i)))
    (.call_from_thread self self._show_issue_detail_callback issue issue-id))
  
  (defn _show_issue_detail_callback [self issue issue-id]
    (when (!= (getattr self "_highlighted_issue_id" None) issue-id)
      (return))
    (when issue
      (setv self.selected_issue issue)
      (.show_issue_detail self issue)))
  
  ;; =========================================================================
  ;; Detail panel display
  ;; =========================================================================
  
  (defn show_run_detail [self run]
    (setv detail-panel (.query_one self "#detail-panel" DetailPanel))
    (setv date-fmt "%Y-%m-%d %H:%M:%S")
    (setv started-str (if run.started_at (.strftime run.started_at date-fmt) "-"))
    (setv updated-str (if run.updated_at (.strftime run.updated_at date-fmt) "-"))
    (setv agent-str (or run.agent "-"))
    (setv branch-str (or run.branch "-"))
    (setv worktree-str (or run.worktree_path "-"))
    (setv session-str (or run.tmux_session "-"))
    (setv mux-str (or run.multiplexer "-"))
    (setv content-lines
      [f"Run: {(.ref run)}"
       f"Status: {run.status.value}"
       f"Agent: {agent-str}"
       f"Started: {started-str}"
       f"Updated: {updated-str}"
       f"Elapsed: {(.elapsed_time run)}"
       f"Branch: {branch-str}"
       f"Worktree: {worktree-str}"
       f"Session: {session-str}"
       f"Multiplexer: {mux-str}"])
    (when (or (> run.additions 0) (> run.deletions 0))
      (.extend content-lines ["" (+ "[bold]" (* "-" 50) "[/bold]")
                              f"[bold]Changes: [green]+{run.additions}[/green] [red]-{run.deletions}[/red][/bold]"]))
    (.extend content-lines ["" "" (+ "[bold]" (* "-" 50) "[/bold]")
                            "[bold]Recent Messages:[/bold]" ""])
    (when run.tmux_session
      (setv messages (._fetch_session_output self run))
      (if messages
          (do
            (for [line (cut messages -15 None)]
              (setv display-line (if (> (len line) 100) (+ (cut line 0 100) "...") line))
              (.append content-lines f"  {display-line}"))
            (when (in run.status [Status.RUNNING Status.BOOTING])
              (.extend content-lines ["" "[dim]--- Streaming... ---[/dim]"])))
          (.append content-lines "  [dim](No output captured)[/dim]")))
    (when (not run.tmux_session)
      (.append content-lines "  [dim](No tmux session available)[/dim]"))
    (.update_content detail-panel (.join "\n" content-lines) f"Run Details: {(.ref run)}"))
  
  (defn _fetch_session_output [self run]
    "Capture recent output from a session using the appropriate multiplexer."
    (when (not run.tmux_session)
      (return []))
    (with-fallback-silent "fetch_session_output" []
      (setv mux (get_multiplexer_for_run run))
      (.capture_pane mux run.tmux_session 50)))
  
  (defn show_issue_detail [self issue]
    (setv detail-panel (.query_one self "#detail-panel" DetailPanel))
    (setv content-lines
      [f"ID: {issue.id}"
       f"Status: {issue.status.value}"
       f"Title: {issue.title}"
       ""
       "Content:"
       ""
       (cut issue.body 0 1000)])
    (.update_content detail-panel (.join "\n" content-lines) f"Issue: {issue.id}"))
  
  ;; NOTE: attach, stop, diff, kill, new_run, open_issue, close_issue
  ;; are all injected by (with-run-actions) and (with-issue-actions) above
  
  ;; =========================================================================
  ;; Select action (Enter key) — dispatches to injected actions
  ;; =========================================================================
  
  (defn action_select [self]
    (cond
      (and (= self.current_focus "runs") self.selected_run)
        (.action_attach self)
      (and (= self.current_focus "issues") self.selected_issue)
        (.action_open_issue self))))


;; ============================================================================
;; Exports
;; ============================================================================

(setv __all__ ["OrchMonitorApp" "ORCH_MONITOR_CSS"])
