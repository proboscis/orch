;; ruleid: tui-no-proto-status-remap
(defn proto-status->model [status]
  status)

;; ruleid: tui-no-proto-status-remap
(setv status pb.RUN_STATUS_RUNNING)

;; ruleid: tui-no-proto-branch-mux-remap
(setv mux pb.MULTIPLEXER_TMUX)

;; ruleid: tui-no-proto-branch-mux-remap
(setv state pb.BRANCH_STATE_DIRTY)

(defn sort-runs [runs]
  ;; ruleid: tui-no-client-side-run-sort
  (.sort runs))

(defn bare []
  (try
    True
    ;; ruleid: hy-tui-no-bare-except
    (except []
      False)))

(defn thin-view [req]
  ;; ok: tui-no-proto-status-remap
  (.append req.list_runs.status_text "running")
  ;; ok: tui-no-client-side-filtering
  (.append req.list_runs.agents "codex"))
