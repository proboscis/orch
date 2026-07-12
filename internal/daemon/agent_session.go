package daemon

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/proboscis/orch/internal/agent"
	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/store"
)

// codexAgentSessionResolveTimeout bounds the post-boot wait for codex to
// write its rollout file (ADR-0005 R1): codex records the session at boot,
// typically within seconds; the ladder allows up to ~60s before recording
// the miss. Variable for tests.
var codexAgentSessionResolveTimeout = 60 * time.Second

// recordAgentSessionIdentity implements ADR-0005 R1 on the execution host:
// once the agent session has started, record the agent-native session
// identity as an agent_session artifact so reap (R4) can decide safety from
// a stored fact. claude ids are minted by the ladder and pinned via
// --session-id; codex ids are resolved post-boot from the rollout whose
// session_meta cwd matches this run's worktree (worktree paths are unique
// per run). gemini and opencode record nothing here: opencode has its own
// opencode_session seam, and an identity-less run is simply never reaped.
// A codex resolution failure is recorded loudly as an error artifact — the
// id is never guessed. The returned result travels in the worker result so
// the master projects the same fact onto its store.
func (s *SocketServer) recordAgentSessionIdentity(st store.Store, run *model.Run, launchCfg *agent.LaunchConfig) *AgentSessionResult {
	if st == nil || run == nil || launchCfg == nil {
		return nil
	}
	switch launchCfg.Type {
	case agent.AgentClaude:
		id := strings.TrimSpace(launchCfg.AgentSessionID)
		if id == "" {
			return nil
		}
		s.appendAgentSessionArtifact(st, run, "claude", id, 1)
		return &AgentSessionResult{Backend: "claude", ID: id, Generation: 1}
	case agent.AgentCodex:
		id, err := agent.ResolveCodexSessionIDWithRetry(launchCfg.CodexSessionsHome(), launchCfg.WorkDir, codexAgentSessionResolveTimeout)
		if err != nil {
			msg := fmt.Sprintf("agent_session_unresolved: %v", err)
			s.appendArtifactEventBestEffort(st, run.Ref(), "error", map[string]string{"message": msg})
			s.logger.Printf("%s#%s: %s (run stays unreapable per ADR-0005 R4)", run.IssueID, run.RunID, msg)
			return &AgentSessionResult{Backend: "codex", Unresolved: msg}
		}
		s.appendAgentSessionArtifact(st, run, "codex", id, 1)
		return &AgentSessionResult{Backend: "codex", ID: id, Generation: 1}
	}
	return nil
}

func (s *SocketServer) appendAgentSessionArtifact(st store.Store, run *model.Run, backend, id string, generation int) {
	s.appendArtifactEventBestEffort(st, run.Ref(), "agent_session", map[string]string{
		"backend":    backend,
		"id":         id,
		"generation": strconv.Itoa(generation),
	})
}

// projectAgentSessionToMasterStore mirrors the execution host's ADR-0005 R1
// identity fact (or its recorded miss) onto the master store, the same way
// opencode_session is projected from a delegated worker result.
func (s *SocketServer) projectAgentSessionToMasterStore(st store.Store, run *model.Run, result *AgentSessionResult) {
	if result == nil {
		return
	}
	if id := strings.TrimSpace(result.ID); id != "" {
		s.appendAgentSessionArtifact(st, run, result.Backend, id, result.Generation)
		return
	}
	if msg := strings.TrimSpace(result.Unresolved); msg != "" {
		s.appendArtifactEventBestEffort(st, run.Ref(), "error", map[string]string{"message": msg})
	}
}
