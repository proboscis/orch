;; ADR-0004: every mutation of store data goes through the owning daemon.
;;
;; Executable ADR (doeff-adr defadr). Run: uv run pytest docs/adr -q
;; Decided 2026-07-12 after three same-day incidents (see :problem) showed
;; the issue/run store being written around the daemon — by the CLI, by
;; operators over ssh, and by agents committing store files through git.

(require doeff-adr.macros [defadr defsemgrep rule law])
(import doeff-adr.macros [fact interpretation counterexample])

(defadr ADR-0004-STORE-WRITES-THROUGH-DAEMON
  :title "store mutations flow through the owning daemon (single write path)"
  :status "accepted"
  :scope ["internal/store/file" "internal/cli/issue.go" "internal/daemon"]
  :problem
    [(fact "agents committed store files via git PRs while orch wrote the same files directly; the proboscis-ema store checkout drifted 340 commits from main and served a resolved issue as open for a month"
           :evidence "ISSUE-TRD-162 re-dispatched 2026-07-11 although PR #534 merged 2026-06-12")
     (fact "an operator ssh-append to an issue file stayed invisible to every daemon read until a master restart: FileStore marks its cache dirty only on its OWN writes"
           :evidence "internal/store/file/file.go markCacheDirty call sites; incident 2026-07-12")
     (fact "the CLI itself writes around the daemon: runIssueEdit opens the store file directly in $EDITOR for local issues, and the --title error message instructs the user to 'edit the file directly'"
           :evidence "internal/cli/issue.go runIssueEdit")]
  :context
    [(interpretation "the store is the store of record (run-state-machine.md D-C1); a store of record with more than one writer has no single source of truth")
     (interpretation "cache coherence, event-fold correctness, and the pr-attach/issue-resolve automations all assume the daemon saw every write; a bypassed write invalidates each of them silently")
     (interpretation "this extends the single-writer discipline of run-status-write-surface (one sanctioned status-event constructor) from one event kind to the whole store surface, at process level")]
  :decision
    [(rule R1 "every store mutation - issue create/edit/close/resolve, run event append - is a daemon verb over the API; there is no sanctioned out-of-band write")
     (rule R2 "the CLI never writes store files directly: interactive edit round-trips through a daemon verb on a temp copy, and non-interactive body edits (--body/--append-body/stdin) exist so agents and operators never need ssh file edits")
     (rule R3 "the daemon DETECTS out-of-band modification of store files and fails fast as an ADR-0004 violation: the affected operation returns an explicit drift error naming the file and this ADR; adopting the external change requires an explicit operator action (daemon restart). No silent stale serve, no silent convergence")
     (rule R4 "store data never lives inside a git worktree that agents or PRs mutate: default XDG location or a dedicated directory outside any agent-writable checkout")]
  :laws
    [(law single-writer
       :statement "mutation(store_file) => author(mutation) == owning_daemon"
       :counterexamples
         [(counterexample "operator appends a section to an issue .md over ssh; daemon serves the pre-edit body indefinitely (2026-07-12)")
          (counterexample "CLI opens the store file in $EDITOR and saves (runIssueEdit local path)")
          (counterexample "agent commits VAULT/Issues/*.md in a PR; merge rewrites store files behind the daemon (proboscis-ema, 2026-06..07)")])
     (law drift-fail-fast
       :statement "observed_file_state != cache_state => operation fails with an ADR-0004 violation error naming the file (never silent-stale, never silent-adopt)"
       :counterexamples
         [(counterexample "issue file changed on disk; orch issue show silently returns the cached pre-change body (2026-07-12, required a master restart to surface)")])]
  :enforcement
    [(defsemgrep adr0004-cli-no-editor-on-store-path
       "adr0004-cli-no-editor-on-store-path"
       [{"relative-path" "internal/cli/bad/run_issue_edit.go"
         "source" "package cli\n\nfunc runIssueEdit(issue *Issue) error {\n\treturn openInEditor(issue.Path)\n}\n"}]
       [{"relative-path" "internal/cli/good/edit_via_daemon.go"
         "source" "package cli\n\nfunc editIssueViaDaemon(issue *Issue) error {\n\ttmp := writeTempCopy(issue)\n\tif err := openInEditor(tmp); err != nil {\n\t\treturn err\n\t}\n\treturn submitIssueUpdate(issue.ID, tmp)\n}\n"}])
     (defsemgrep adr0004-no-edit-the-file-directly-advice
       :languages ["generic"]
       :message "ADR-0004 R1: never instruct users to edit store files directly; point them at the daemon verb instead"
       :pattern "edit the file directly"
       :bad ["return fmt.Errorf(\"--title update not yet implemented for local issues; edit the file directly: %s\", issue.Path)"]
       :good ["return fmt.Errorf(\"--title update not yet implemented for local issues; use 'orch issue edit' once the daemon verb lands (ADR-0004)\")"])]
  :plans ["orch issue store-write-daemon-path (implementation: daemon update verb, CLI reroute + non-interactive flags, drift detection with fail-fast error, live-tree semgrep rule installation once the CLI shape is fixed)"])
