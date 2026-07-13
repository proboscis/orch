package daemon

import (
	"fmt"
	"strings"
	"time"

	"github.com/proboscis/orch/internal/agent"
	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/multiplexer"
	"github.com/proboscis/orch/internal/sessionlifecycle"
	"github.com/proboscis/orch/internal/store"
)

// ADR-0005 R5: revive is in-place and entered only via send/attach. The
// master decides revivability from STORED FACTS (LS5), the execution host
// does the physical re-boot (native resume in the same worktree, same
// session name), and the master alone writes the ledger: session_revived
// note, the new-generation agent_session artifact (which dissolves the L-S3
// latch), and the user-sourced status re-entry (stageSessionRevived, L4').
//
// Measured CLI physics (docs/design/revive-physics.md): claude --resume <id>
// appends to the SAME transcript (identity stable; --session-id alongside
// --resume is rejected by the CLI); codex resume <id> continues the existing
// rollout. The conversation identity is durable across generations — the
// generation counts incarnations.

// reviveLeaseTimeout bounds the worker-side revive: session boot plus the
// codex rollout re-resolution (codexAgentSessionResolveTimeout, 60s).
var reviveLeaseTimeout = 120 * time.Second

// reviveResolveCodexSessionID is a test seam over the post-boot codex
// identity re-resolution (T1b arm, reused by revive with generation+1).
var reviveResolveCodexSessionID = agent.ResolveCodexSessionIDWithRetry

// reviveMultiplexer is the slice of multiplexer.Multiplexer revive needs;
// narrowed for tests.
type reviveMultiplexer interface {
	Type() multiplexer.Type
	HasSession(name string) bool
	NewSession(cfg *multiplexer.SessionConfig) error
}

// getReviveMultiplexer is a test seam resolving the run's recorded
// multiplexer. Production resolves the concrete type recorded on the run —
// revive never guesses (same rule as the reaper).
var getReviveMultiplexer = func(run *model.Run) (reviveMultiplexer, error) {
	if err := validateReaperMultiplexer(run); err != nil {
		return nil, err
	}
	muxType, _ := multiplexer.ParseType(strings.TrimSpace(run.Multiplexer))
	mux, err := multiplexer.GetMultiplexer(muxType)
	if err != nil {
		return nil, fmt.Errorf("resolve multiplexer %q for run %s: %w", run.Multiplexer, run.Ref().String(), err)
	}
	if mux == nil {
		return nil, fmt.Errorf("resolve multiplexer %q for run %s: no multiplexer returned", run.Multiplexer, run.Ref().String())
	}
	return mux, nil
}

// checkRevivePreconditions decides revivability from stored facts alone
// (ADR-0005 LS5): agent kind, recorded identity, recorded worktree, no
// worktree_removed note. A missing fact fails fast NAMING the fact and
// pointing at restart-from --branch — never a probe, never a silent fresh
// session. Filesystem/liveness checks belong to the execution host
// (revivePhysical), not here.
func checkRevivePreconditions(run *model.Run) error {
	return sessionlifecycle.CheckRevivePreconditions(run)
}

// revivePhysical performs the execution-host part of a revive: verify the
// host-local facts (worktree present, session not already alive), build the
// native resume command, boot the session, and re-resolve the codex identity
// (claude identity is stable and pre-known). It writes NO ledger events —
// the master owns the store of record (ADR-0004).
func (s *SocketServer) revivePhysical(run *model.Run, projectRoot string) (*ReviveRunResult, error) {
	if run == nil {
		return nil, fmt.Errorf("run required")
	}
	ref := run.Ref().String()

	exists, err := worktreeDirectoryExists(run.WorktreePath)
	if err != nil {
		return nil, fmt.Errorf("revive %s: check worktree %s: %w", ref, run.WorktreePath, err)
	}
	if !exists {
		return nil, fmt.Errorf("revive %s: worktree %s does not exist on execution host — use `orch restart-from --branch`", ref, run.WorktreePath)
	}

	mux, err := getReviveMultiplexer(run)
	if err != nil {
		return nil, err
	}
	sessionName := model.GenerateSessionName(run.IssueID, run.RunID)
	if mux.HasSession(sessionName) {
		// SessionReaped latch set but the session still exists = a reap kill
		// failed and its retry is pending (LS6). Racing a boot against that
		// kill would destroy the freshly revived session; fail clearly.
		return nil, fmt.Errorf("revive %s: session %s is still alive with a pending reap-kill retry; resend after the reaper completes", ref, sessionName)
	}

	cfg, err := loadConfigForProjectRoot(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("revive %s: failed to load config: %w", ref, err)
	}
	agentType := agent.AgentType(run.Agent)
	adapter, err := agent.GetAdapter(agentType)
	if err != nil {
		return nil, fmt.Errorf("revive %s: %w", ref, err)
	}

	launchCfg := &agent.LaunchConfig{
		Type:           agentType,
		WorkDir:        run.WorktreePath,
		IssueID:        string(run.IssueID),
		RunID:          string(run.RunID),
		RunPath:        run.Path,
		Branch:         run.Branch,
		SessionName:    sessionName,
		Resume:         true,
		AgentSessionID: run.AgentSessionID,
		Model:          run.Model,
		ModelVariant:   run.ModelVariant,
		ExtraArgs:      cfg.GetExtraArgs(run.Agent),
	}

	// Re-resolve the run's recorded execution profile to its auth dir with
	// the same authoritative decision point the launch paths use, so the
	// revived agent authenticates as the same account.
	switch agentType {
	case agent.AgentCodex:
		decision, err := resolveCodexProfile(cfg, run.Agent, run.Profile, "")
		if err != nil {
			return nil, fmt.Errorf("revive %s: %w", ref, err)
		}
		launchCfg.CodexHome = decision.AuthDir
	case agent.AgentClaude:
		decision, err := resolveClaudeProfile(cfg, run.Agent, run.Profile, "")
		if err != nil {
			return nil, fmt.Errorf("revive %s: %w", ref, err)
		}
		launchCfg.ClaudeConfigDir = decision.AuthDir
	}

	if err := agent.AuthPreflight(launchCfg); err != nil {
		return nil, fmt.Errorf("revive %s: %w", ref, err)
	}

	agentCmd, err := adapter.LaunchCommand(launchCfg)
	if err != nil {
		return nil, fmt.Errorf("revive %s: build resume command: %w", ref, err)
	}

	env := launchCfg.Env()
	env = append(env, adapter.ExtraEnv()...)
	if err := mux.NewSession(&multiplexer.SessionConfig{
		SessionName: sessionName,
		WorkDir:     run.WorktreePath,
		Command:     agentCmd,
		Env:         env,
	}); err != nil {
		return nil, fmt.Errorf("revive %s: failed to create session: %w", ref, err)
	}

	result := &ReviveRunResult{
		SessionName: sessionName,
		Multiplexer: string(mux.Type()),
		Generation:  run.AgentSessionGeneration + 1,
	}
	switch agentType {
	case agent.AgentClaude:
		// Measured physics: claude --resume appends to the same transcript;
		// the identity is the stored fact, no discovery needed.
		result.AgentSessionID = run.AgentSessionID
	case agent.AgentCodex:
		// Re-run the T1b discovery arm: resume continues the existing
		// rollout, so the newest cwd-matching rollout re-yields the same id;
		// resolving rather than assuming keeps the fact observation-backed.
		id, err := reviveResolveCodexSessionID(launchCfg.CodexSessionsHome(), launchCfg.WorkDir, codexAgentSessionResolveTimeout)
		if err != nil {
			return nil, fmt.Errorf("revive %s: session booted but codex identity re-resolution failed (run stays latched-unreapable until resolved): %w", ref, err)
		}
		result.AgentSessionID = id
	}
	return result, nil
}

// reviveRunForVerb is the master-side revive orchestration used by the entry
// verbs send and attach (R6: observation verbs never boot). It checks the
// stored-fact preconditions against the authoritative run, routes the
// physical re-boot to the execution host (worker lease) or runs it locally,
// and then writes the ledger on the master store: session_revived note →
// agent_session generation+1 (dissolves the L-S3 latch) → user-sourced
// status re-entry via the launch plane (stageSessionRevived).
func (s *SocketServer) reviveRunForVerb(st store.Store, projectID, projectRoot string, run *model.Run) error {
	if st == nil || run == nil {
		return fmt.Errorf("store and run required")
	}
	if err := checkRevivePreconditions(run); err != nil {
		return err
	}

	var result *ReviveRunResult
	if s.runRequiresWorkerDelegation(run, "") {
		if strings.TrimSpace(projectID) == "" {
			return fmt.Errorf("no project context available to revive remote run %s#%s", run.IssueID, run.RunID)
		}
		target, err := resolveWorkerTargetForRunFields(run, projectRoot)
		if err != nil {
			return err
		}
		payload := &WorkerEffectPayload{
			ReviveRun: &ReviveRunPayload{
				Target:         strings.TrimSpace(run.Target),
				TargetHost:     target.Host,
				TargetWorkerID: target.WorkerID,
				RunSnapshot:    newRunSnapshot(run),
			},
		}
		lease, err := s.acquireWorkerLease(projectID, "revive_run", string(run.IssueID), string(run.RunID), payload)
		if err != nil {
			return err
		}
		completed, err := s.waitForWorkerLeaseCompletion(lease.LeaseID, reviveLeaseTimeout)
		if err != nil {
			return fmt.Errorf("revive on execution host %s for run %s#%s: %w", target.Host, run.IssueID, run.RunID, err)
		}
		effectResult, err := decodeWorkerEffectResult(completed.ResultJSON)
		if err != nil {
			return fmt.Errorf("decode revive result from execution host %s for run %s#%s: %w", target.Host, run.IssueID, run.RunID, err)
		}
		if effectResult.ReviveRunResult == nil {
			return fmt.Errorf("revive on execution host %s for run %s#%s completed without revive_run_result", target.Host, run.IssueID, run.RunID)
		}
		result = effectResult.ReviveRunResult
	} else {
		local, err := s.revivePhysical(run, projectRoot)
		if err != nil {
			return err
		}
		result = local
	}

	// Ledger, on the master store only (ADR-0004). Order matters: the note
	// is the narrative marker the gatherer folds (T2c), the agent_session
	// artifact is what dissolves the L-S3 latch, and the status re-entry
	// goes last so a run never counts as running before its latch state and
	// lineage are durable.
	note := model.NewDaemonNoticeEvent("session_revived", map[string]string{
		"generation":   fmt.Sprintf("%d", result.Generation),
		"session_name": result.SessionName,
	})
	if err := st.AppendEvent(run.Ref(), note); err != nil {
		return fmt.Errorf("record session_revived note for %s#%s: %w", run.IssueID, run.RunID, err)
	}
	s.appendAgentSessionArtifact(st, run, run.Agent, result.AgentSessionID, result.Generation)
	s.reportLaunchProgress(st, run, launchReached(stageSessionRevived))
	s.logger.Printf("%s#%s: revived session %s (agent=%s generation=%d)", run.IssueID, run.RunID, result.SessionName, run.Agent, result.Generation)
	return nil
}

// reviveIfReaped is the send/attach entry hook: a run whose CURRENT
// generation is recorded as reaped gets revived in place before the verb
// proceeds. Non-reaped runs pass through untouched. It re-reads the
// authoritative run afterward so the caller continues against post-revive
// state.
func (s *SocketServer) reviveIfReaped(st store.Store, projectID, projectRoot string, run *model.Run) (*model.Run, error) {
	if run == nil || !run.SessionReaped() {
		return run, nil
	}
	if err := s.reviveRunForVerb(st, projectID, projectRoot, run); err != nil {
		return run, err
	}
	fresh, err := st.GetRun(run.Ref())
	if err != nil {
		return run, fmt.Errorf("reload run %s#%s after revive: %w", run.IssueID, run.RunID, err)
	}
	return fresh, nil
}
