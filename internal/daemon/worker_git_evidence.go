package daemon

import (
	"fmt"
	"strings"

	"github.com/proboscis/orch/internal/model"
)

// getRunGitStateViaWorker gathers the worker-local facts used by the O5
// dead-session evidence ladder. It deliberately reuses the existing
// get_diff_stats and get_branch_state capabilities instead of introducing a
// monitor-specific worker effect.
func (s *SocketServer) getRunGitStateViaWorker(run *model.Run, projectID, projectRoot string) (*GetDiffStatsResult, *GetBranchStateResult, error) {
	if run == nil {
		return nil, nil, fmt.Errorf("run required")
	}
	if strings.TrimSpace(projectID) == "" {
		return nil, nil, fmt.Errorf("no project context available for remote run %s#%s", run.IssueID, run.RunID)
	}

	target, err := resolveWorkerTargetForRunFields(run, projectRoot)
	if err != nil {
		return nil, nil, err
	}
	snapshot := newRunSnapshot(run)

	diffPayload := &WorkerEffectPayload{
		GetDiffStats: &GetDiffStatsPayload{
			Target:         strings.TrimSpace(run.Target),
			TargetHost:     target.Host,
			TargetWorkerID: target.WorkerID,
			RunSnapshot:    snapshot,
		},
	}
	diffResult, err := s.runGitEvidenceLease(run, projectID, "get_diff_stats", diffPayload)
	if err != nil {
		return nil, nil, err
	}
	if diffResult.DiffStatsResult == nil {
		return nil, nil, fmt.Errorf("worker get_diff_stats lease for run %s#%s completed without diff_stats_result", run.IssueID, run.RunID)
	}

	branchPayload := &WorkerEffectPayload{
		GetBranchState: &GetBranchStatePayload{
			Target:         strings.TrimSpace(run.Target),
			TargetHost:     target.Host,
			TargetWorkerID: target.WorkerID,
			RunSnapshot:    snapshot,
		},
	}
	branchResult, err := s.runGitEvidenceLease(run, projectID, "get_branch_state", branchPayload)
	if err != nil {
		return nil, nil, err
	}
	if branchResult.BranchStateResult == nil {
		return nil, nil, fmt.Errorf("worker get_branch_state lease for run %s#%s completed without branch_state_result", run.IssueID, run.RunID)
	}

	return diffResult.DiffStatsResult, branchResult.BranchStateResult, nil
}

func (s *SocketServer) runGitEvidenceLease(run *model.Run, projectID, effect string, payload *WorkerEffectPayload) (*WorkerEffectResult, error) {
	lease, err := s.acquireWorkerLease(projectID, effect, string(run.IssueID), string(run.RunID), payload)
	if err != nil {
		return nil, fmt.Errorf("worker %s lease for run %s#%s: %w", effect, run.IssueID, run.RunID, err)
	}
	completedLease, err := s.waitForWorkerLeaseCompletion(lease.LeaseID, remoteGitEvidenceTimeout)
	if err != nil {
		return nil, fmt.Errorf("worker %s lease for run %s#%s: %w", effect, run.IssueID, run.RunID, err)
	}
	result, err := decodeWorkerEffectResult(completedLease.ResultJSON)
	if err != nil {
		return nil, fmt.Errorf("decode worker %s result for run %s#%s: %w", effect, run.IssueID, run.RunID, err)
	}
	return result, nil
}

// sendMessageViaWorker delivers a message to a worker-hosted run's agent
// session through the existing send_message lease capability — the same
// route `orch send` takes for remote runs. Used by the monitor plane to
// deliver daemon notices (run-state-machine.md §11 L-N1..L-N3).
func (s *SocketServer) sendMessageViaWorker(run *model.Run, projectID, projectRoot, message string) error {
	if run == nil {
		return fmt.Errorf("run required")
	}
	if strings.TrimSpace(projectID) == "" {
		return fmt.Errorf("no project context available for remote run %s#%s", run.IssueID, run.RunID)
	}
	target, err := resolveWorkerTargetForRunFields(run, projectRoot)
	if err != nil {
		return err
	}
	payload := &WorkerEffectPayload{
		SendMessage: &SendMessagePayload{
			Message:        message,
			Target:         strings.TrimSpace(run.Target),
			TargetHost:     target.Host,
			TargetWorkerID: target.WorkerID,
			RunSnapshot:    newRunSnapshot(run),
		},
	}
	if _, err := s.runGitEvidenceLease(run, projectID, "send_message", payload); err != nil {
		return err
	}
	return nil
}
