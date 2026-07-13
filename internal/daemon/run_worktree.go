package daemon

import (
	"fmt"
	"os"
	"strings"

	"github.com/proboscis/orch/api/orchpb"
	"github.com/proboscis/orch/internal/git"
	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/store"
)

const (
	runWorktreeInspect = "inspect"
	runWorktreeRemove  = "remove"
)

func (s *SocketServer) worktreeRequestContext(projectID string, st store.Store) *orchpb.RequestContext {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		if repo := s.repoContextForStore(st); repo != nil {
			projectID = strings.TrimSpace(repo.RepoID)
		}
	}
	return &orchpb.RequestContext{ProjectId: projectID}
}

func executeRunWorktreeOperation(projectRoot string, run *model.Run, operation string) (*RunWorktreeResult, error) {
	if run == nil {
		return nil, fmt.Errorf("run required")
	}

	worktreePath := strings.TrimSpace(run.WorktreePath)
	switch strings.TrimSpace(operation) {
	case runWorktreeInspect:
		if worktreePath == "" {
			return &RunWorktreeResult{}, nil
		}
		_, err := os.Stat(worktreePath)
		if err == nil {
			return &RunWorktreeResult{Exists: true}, nil
		}
		if os.IsNotExist(err) {
			return &RunWorktreeResult{}, nil
		}
		return nil, fmt.Errorf("failed to stat worktree %s: %w", worktreePath, err)

	case runWorktreeRemove:
		if worktreePath == "" {
			return &RunWorktreeResult{Skipped: true, Reason: "run has no recorded worktree"}, nil
		}
		repoRoot, err := resolveExecutionRepoRoot(projectRoot, worktreePath)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve repo root for worktree cleanup: %w", err)
		}
		return removeRegisteredRunWorktree(repoRoot, run)

	default:
		return nil, fmt.Errorf("unsupported run_worktree operation %q", operation)
	}
}

func resolveExecutionRepoRoot(projectRoot, worktreePath string) (string, error) {
	for _, candidate := range []string{projectRoot, worktreePath} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		if repoRoot, err := git.FindMainRepoRoot(candidate); err == nil {
			return repoRoot, nil
		}
		if repoRoot, err := git.FindRepoRoot(candidate); err == nil {
			return repoRoot, nil
		}
	}
	return "", fmt.Errorf("repo root not found from project %q or worktree %q", projectRoot, worktreePath)
}

func removeRegisteredRunWorktree(repoRoot string, run *model.Run) (*RunWorktreeResult, error) {
	worktreePath := strings.TrimSpace(run.WorktreePath)
	infos, err := git.ListWorktreeInfos(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to list worktrees for %s: %w", repoRoot, err)
	}

	registered := false
	normalizedWorktreePath := normalizeWorktreePathForComparison(worktreePath)
	for _, info := range infos {
		if normalizeWorktreePathForComparison(info.Path) == normalizedWorktreePath {
			registered = true
			break
		}
		if strings.TrimSpace(run.Branch) != "" && strings.TrimSpace(info.Branch) == strings.TrimSpace(run.Branch) {
			registered = true
			break
		}
	}

	result := &RunWorktreeResult{Registered: registered}
	if _, statErr := os.Stat(worktreePath); statErr == nil {
		result.Exists = true
	} else if !os.IsNotExist(statErr) {
		return nil, fmt.Errorf("failed to stat worktree %s: %w", worktreePath, statErr)
	}

	if !registered {
		if !result.Exists {
			result.Skipped = true
			result.Reason = "worktree already absent"
			return result, nil
		}
		return nil, fmt.Errorf("worktree path %s exists but is not registered in repo %s", worktreePath, repoRoot)
	}

	if err := git.RemoveWorktree(repoRoot, worktreePath); err != nil {
		return nil, fmt.Errorf("failed to remove worktree %s: %w", worktreePath, err)
	}
	result.Removed = true
	result.Exists = false
	return result, nil
}

func (s *SocketServer) runWorktreeOperation(ctx *orchpb.RequestContext, run *model.Run, operation string) (*RunWorktreeResult, error) {
	if run == nil {
		return nil, fmt.Errorf("run required")
	}
	if strings.TrimSpace(run.WorktreePath) == "" {
		return executeRunWorktreeOperation("", run, operation)
	}

	projectRoot := s.resolveProjectRootFromContextOrProto(ctx, "")
	if !s.runRequiresWorkerDelegation(run, "") {
		return executeRunWorktreeOperation(projectRoot, run, operation)
	}

	projectID := projectIDFromContext(ctx)
	if strings.TrimSpace(projectID) == "" {
		return nil, fmt.Errorf("no project context available for worktree %s on remote run %s#%s", operation, run.IssueID, run.RunID)
	}
	target, err := resolveWorkerTargetForRunFields(run, projectRoot)
	if err != nil {
		return nil, err
	}
	payload := &WorkerEffectPayload{
		RunWorktree: &RunWorktreePayload{
			Operation:      operation,
			Target:         strings.TrimSpace(run.Target),
			TargetHost:     target.Host,
			TargetWorkerID: target.WorkerID,
			RunSnapshot:    newRunSnapshot(run),
		},
	}
	completedLease, err := s.withWorkerLease(projectID, "run_worktree", string(run.IssueID), string(run.RunID), payload)
	if err != nil {
		return nil, fmt.Errorf("worktree %s on execution host %s for run %s#%s: %w", operation, target.Host, run.IssueID, run.RunID, err)
	}
	effectResult, err := decodeWorkerEffectResult(completedLease.ResultJSON)
	if err != nil {
		return nil, fmt.Errorf("decode worktree %s result from execution host %s for run %s#%s: %w", operation, target.Host, run.IssueID, run.RunID, err)
	}
	if effectResult.RunWorktreeResult == nil {
		return nil, fmt.Errorf("worktree %s on execution host %s for run %s#%s completed without run_worktree_result", operation, target.Host, run.IssueID, run.RunID)
	}
	return effectResult.RunWorktreeResult, nil
}
