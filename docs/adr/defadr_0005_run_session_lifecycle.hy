;; ADR-0005: run sessions are disposable cache — reap requires recorded revivability.
;;
;; Executable ADR (doeff-adr defadr). Run: uv run pytest docs/adr -q
;; Decided 2026-07-13 after the 2026-07-11 incident: 25 live `run-*` tmux
;; sessions belonged to done/failed/canceled runs, several burning 80-100%
;; CPU for 5.5 days (battery drained on a 100W charger); a follow-up sweep
;; found ~40 idle agent processes. `orch repair --force` could not clean
;; them (see :problem). Design discussion: user + frontier, 2026-07-12..13.

(require doeff-adr.macros [defadr defsemgrep rule law])
(import doeff-adr.macros [fact interpretation counterexample])

(defadr ADR-0005-RUN-SESSION-LIFECYCLE
  :title "run sessions are disposable cache; reap needs recorded revivability; revive is in-place via send/attach"
  :status "proposed"
  :scope ["internal/daemon" "internal/agent" "internal/multiplexer" "internal/cli" "internal/model"]
  :problem
    [(fact "the step() effect vocabulary has no session-kill effect, so every autonomously detected terminal transition (PR merged→done, PR closed→canceled, output-inferred done/failed) leaves the interactive agent session alive; session lifetime is coupled to run status only on the explicit user-stop path"
           :evidence "internal/daemon/step.go:281-310 (exhaustive effect enum), step.go:454-468 (PR verdicts), internal/agent/manager.go:197-205 (text verdicts), internal/daemon/socket.go:5096-5135 (the one kill path)")
     (fact "a run that reaches a terminal status is never visited again by the monitor, so a missed kill is never retried"
           :evidence "internal/daemon/daemon.go:376-378 (non-terminal listing), internal/daemon/monitor.go:69 (terminal early-return)")
     (fact "repair's orphan detection is status-blind: expectedSessions is built from ListRuns without a status filter, so a terminal run's live session is 'expected' forever and never reported or killed"
           :evidence "internal/daemon/proto_handler.go:3743-3780, :3757; incident 2026-07-11: 25 sessions survived repair")
     (fact "claude/codex runs have no recorded agent-native session identity — claude is launched without --session-id and 'rollout' appears nowhere in the Go tree — so killing a session forfeits the agent's conversational context and nothing dares reap; opencode already records identity as the opencode_session artifact event"
           :evidence "internal/agent/claude.go:20-57, internal/agent/codex.go:21-54; precedent internal/daemon/socket.go:2001-2058, internal/model/run.go:248-250")
     (fact "orch tick appends a 'resume' event that no code consumes (not in any fold, not in monitor/step); it re-drives nothing and reports optimistic success"
           :evidence "internal/cli/tick.go:160-171, internal/daemon/socket.go:5723-5772 (plain append), zero consumers in internal/model + internal/daemon")
     (fact "session-kill failures are silent: KillSession errors are warning-logged and nil-returned, stop writes canceled even when the kill failed, and an empty Multiplexer field skips the kill entirely; delete never kills sessions at all"
           :evidence "internal/daemon/socket.go:5123-5132, internal/daemon/proto_handler.go:1906-1911, proto_handler.go:3356-3394 (SessionKilled always false)")
     (fact "clean removes the worktree but records no event, so 'this run can no longer be revived' is not a stored fact anywhere; restart-from by run-ref fails on a cleaned worktree (no recreation), while the --branch path can recreate one"
           :evidence "internal/daemon/proto_handler.go:3406-3422, socket.go:4257-4260, socket.go:4206-4217")]
  :context
    [(interpretation "the mux session is derived, disposable state: the agent's conversational state is persisted outside orch (claude transcript jsonl / codex rollout) on every turn, and the work product lives in the worktree/branch; the durable run record is the event log. Sessions leaked by absence of decision — no layer owned their lifetime (coupling-core: ownership/lifecycle of long-lived entities)")
     (interpretation "reap safety IS revive capability: killing a session is safe exactly when the agent-native session identity is recorded and the worktree still exists; therefore revivability must be a precondition checked from stored facts, not probed at kill time")
     (interpretation "the store-of-record seam for agent identity already exists: the opencode_session artifact event, folded by DeriveState onto model.Run — agent_session for claude/codex copies that seam (D-C1 compliant: identity travels as an event, no new mutable field)")
     (interpretation "a reaped session must be fold-visible to the transition core: killing a non-terminal run's session otherwise feeds attested O3 session-gone evidence into step() and manufactures false failed verdicts; the gate_ack / daemon_notice note-event-as-ledger pattern (run-state-machine.md §9.7/§11.5) is the sanctioned mechanism")
     (interpretation "the reaper writes no status events — it kills sessions and appends note/artifact events — so it lives outside the frozen status write surface (commitRunStatus); the only step() change is absorbing post-reap gone observations, which is a policy amendment shipped with this ADR (frontier + human, per AGENTS.md routing)")]
  :decision
    [(rule R1 "identity at launch: orch mints the claude session UUID and passes --session-id at run launch; codex rollout id is resolved at boot by matching $CODEX_HOME/sessions rollout session_meta payload.cwd == worktree_path; both are recorded as an agent_session artifact event (attrs: backend, id, generation) and folded onto model.Run like opencode_session. Initial backend scope: claude + codex; gemini records nothing and is therefore never reaped (R4)")
     (rule R2 "daemon reaper reconciler: a self-throttled sibling pass in the daemon tick loop enumerates ALL runs (including terminal) per repo context and kills run-* sessions whose run is (a) terminal for > terminal grace (default 10min), (b) of a resolved issue and idle > resolved grace (default 1h), or (c) idle > TTL (default 7d, idle = run.UpdatedAt age). Policy keys live in a reaper: config section (pointer-merge, KnownFields)")
     (rule R3 "reap protocol, in order: persist a final pane capture (sidecar file under the run log dir + artifact event session_snapshot) → append note session_reaped (attrs: reason, session_name, generation) → KillSession. A failed kill is an error artifact and a retry on the next pass — never a warning-and-forget")
     (rule R4 "reap interlock: the reaper kills only sessions whose run has a recorded agent_session (or backend opencode) AND an existing worktree (no worktree_removed note); anything else is kept alive and REPORTED (repair output + reaper log). Unrevivable garbage is a human decision, not a default")
     (rule R5 "revive is in-place and entered only via send/attach: on a reaped run, orch send / orch attach re-boot the same run — same run record, same session name, agent resumed natively (claude --resume <chain tip> / codex resume <id>) in the same worktree — then append note session_revived plus a new-generation agent_session artifact. Terminal runs re-enter via a user-sourced status event (CanTransitionStatus already permits user-sourced terminal exit). Preconditions are stored facts (LS5); a missing one fails fast naming the fact and pointing at restart-from --branch")
     (rule R6 "observers never boot: capture/capture-all/ps/show/wait/diff/events/query/debug perform no launch; capture on a reaped run serves the persisted session_snapshot with an explicit 'reaped at <ts>' notice. ps/show/debug surface the session state (live / reaped(revivable) / reaped(unrevivable))")
     (rule R7 "orch tick is removed — command, registration, docs, and the dead 'resume' event vocabulary; the unused proto ResumeRun RPC goes with it. No compat shim: agents are controlled via orch send (its own help already says so)")
     (rule R8 "clean appends a worktree_removed note event so unrevivability becomes a stored fact; delete kills the session (via the R3 protocol) before removing the run record; an empty Multiplexer field on a kill path is an error, not a skip")]
  :laws
    [(law session-single-owner
       :statement "after grace, live_session(run-*) => ¬terminal(run) ∧ ¬resolved(issue(run)) ∧ age(run.UpdatedAt) < idle_ttl"
       :counterexamples
         [(counterexample "2026-07-11: 25 run-* sessions of done/failed/canceled runs alive up to 5.5 days, 4 CPU cores burned, repair unable to clean (status-blind expected set)")
          (counterexample "waiting run parked 7+ days with a live session because no TTL dimension exists")])
     (law reap-preserves-revivability
       :statement "reap(run) => recorded(agent_session(run)) ∧ worktree_exists(run); reap touches no worktree, no branch, no agent transcript"
       :counterexamples
         [(counterexample "reaper kills a gemini run's session with no recorded identity — context unreachable, revive impossible (must be kept + reported instead)")
          (counterexample "reaper removes a worktree to 'finish the cleanup' — destroys the revive precondition it was built to preserve")])
     (law reap-fold-visible
       :statement "kill happens only after session_reaped is appended; a session-gone observation on a reaped generation advances no dead-check and produces no verdict"
       :counterexamples
         [(counterexample "reaper kills a waiting run's session silently; the attested observer accumulates 3 gone checks and step() writes failed — a false death verdict manufactured by our own janitor")])
     (law observers-never-boot
       :statement "capture/ps/show/wait/diff/events/query/debug never create a session or launch an agent; revive entry is exactly {send, attach}"
       :counterexamples
         [(counterexample "orch capture on a reaped run boots the agent to look at its screen — a read now costs a launch and races the reaper")])
     (law revivability-is-stored
       :statement "revive preconditions are decided from the event log (agent_session present, no worktree_removed); a missing precondition yields an explicit error naming the fact, never a probe, never a silent fresh session"
       :counterexamples
         [(counterexample "send to a cleaned run silently falls back to a fresh context session — the user believes the agent remembers its work; it does not (no-silent-fallback, fail-fast)")])
     (law kill-failures-are-loud
       :statement "a failed KillSession yields an error artifact and a retry on the next reaper pass; canceled/deleted is never recorded as achieved when the kill failed silently"
       :counterexamples
         [(counterexample "socket.go:5126 warning-lognil-return: stop reports canceled, session lives on (observed pre-ADR behavior)")])]
  :enforcement
    [(defsemgrep adr0005-no-warning-only-kill-failure
       :languages ["generic"]
       :message "ADR-0005 R3/LS6: a failed session kill must produce an error artifact and retry, never a warning-only log"
       :pattern "warning: failed to kill session"
       :bad ["s.logger.Printf(\"warning: failed to kill session %s: %v\", sessionName, err)\nreturn nil"]
       :good ["return fmt.Errorf(\"kill session %s: %w (ADR-0005: reaper will retry; recorded as error artifact)\", sessionName, err)"])
     (defsemgrep adr0005-tick-stays-dead
       :languages ["generic"]
       :message "ADR-0005 R7: orch tick was removed (its resume event had zero consumers); agents are controlled via orch send — do not reintroduce"
       :pattern "orch tick"
       :bad ["Use `orch tick --all` to resume waiting runs."]
       :good ["Use `orch send <RUN_REF> <message>` to answer waiting runs."])]
  :plans ["docs/orch-2026-07-13-run-session-lifecycle-architecture-plan.md"])
