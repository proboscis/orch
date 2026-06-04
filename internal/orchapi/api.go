// Package orchapi defines the unified API interface for orch CLI commands.
//
// This package is the single entry point for CLI to access orch data.
// CLI commands MUST use OrchAPI, never access Store or daemon directly.
//
// Architecture:
//
//	CLI Commands
//	     |
//	     v
//	OrchAPI (this interface)
//	     |
//	     v
//	DaemonClient (implements OrchAPI via proto)
//	     |
//	     v
//	Daemon (owns Store, handles all data access)
package orchapi

import (
	"context"

	"github.com/s22625/orch/internal/model"
)

// OrchAPI is the unified interface for all orch CLI operations.
//
// All methods accept a context for cancellation and timeout control.
// All run-related methods accept RunRef which supports multiple formats:
//   - Short ID: 2-6 char hex prefix (e.g., "a3b4c5")
//   - Full ref: ISSUE_ID#RUN_ID (e.g., "my-task#20231220-100000")
//   - Issue only: ISSUE_ID (resolves to latest run)
//
// This interface is implemented by:
//   - DaemonClient: production implementation via daemon proto
//   - MockOrchAPI: test implementation for unit tests
type OrchAPI interface {
	// =========================================================================
	// Issues
	// =========================================================================

	// GetIssue retrieves an issue by ID.
	// Returns ErrNotFound if the issue does not exist.
	GetIssue(ctx context.Context, issueID model.IssueID) (*Issue, error)

	// ListIssues returns issues matching the filter.
	// If filter is nil, returns all issues.
	ListIssues(ctx context.Context, filter *ListIssuesFilter) (*ListIssuesResult, error)

	// CreateIssue creates a new issue.
	// Returns ErrAlreadyExists if an issue with the same ID exists.
	CreateIssue(ctx context.Context, req *CreateIssueRequest) (*Issue, error)

	// SetIssueStatus updates an issue's status.
	// Returns ErrNotFound if the issue does not exist.
	SetIssueStatus(ctx context.Context, issueID model.IssueID, status IssueStatus) error

	// CloseIssue closes an issue (convenience wrapper for SetIssueStatus).
	CloseIssue(ctx context.Context, issueID model.IssueID) error

	// =========================================================================
	// Runs - Resolution
	// =========================================================================

	// ResolveRun resolves a RunRef to a full Run.
	// This is the unified entry point for all run lookups.
	// Handles short_id, full ref, and issue-only (latest) formats.
	// Returns ErrNotFound if no matching run exists.
	// Returns ErrAmbiguousRef if short_id matches multiple runs.
	ResolveRun(ctx context.Context, ref RunRef) (*Run, error)

	// =========================================================================
	// Runs - CRUD
	// =========================================================================

	// GetRun retrieves a run by full issue_id and run_id.
	// Prefer ResolveRun for user-facing lookups.
	GetRun(ctx context.Context, issueID model.IssueID, runID model.RunID) (*Run, error)

	// GetLatestRun retrieves the most recent run for an issue.
	// Returns ErrNotFound if the issue has no runs.
	GetLatestRun(ctx context.Context, issueID model.IssueID) (*Run, error)

	// ListRuns returns runs matching the filter.
	// If filter is nil, returns all runs.
	ListRuns(ctx context.Context, filter *ListRunsFilter) (*ListRunsResult, error)

	// =========================================================================
	// Runs - Lifecycle
	// =========================================================================

	// StartRun creates and starts a new run for an issue.
	// Creates worktree, branch, and starts agent in tmux session.
	StartRun(ctx context.Context, req *StartRunRequest) (*StartRunResult, error)

	// CreateRun creates a new run record without starting the agent.
	// Used by restart-from command to create runs that reuse existing worktrees.
	CreateRun(ctx context.Context, req *CreateRunRequest) (*CreateRunResult, error)

	// StopRun stops a running run.
	// Sends interrupt to the agent and records cancel status.
	StopRun(ctx context.Context, ref RunRef) error

	// AppendEvent appends an event to a run's event log.
	AppendEvent(ctx context.Context, ref RunRef, event *Event) (*AppendEventResult, error)

	// WaitForRuns blocks until any specified run leaves its active execution state.
	WaitForRuns(ctx context.Context, refs []string, timeoutSeconds int) (*WaitForRunsResult, error)

	// StreamRunEvents subscribes to run state transition events emitted by
	// the daemon. The returned stream emits frames until Close is called or
	// the daemon disconnects. Filter fields narrow the subscription; empty
	// fields match any value.
	StreamRunEvents(ctx context.Context, filter *RunEventFilter) (RunEventStream, error)

	// =========================================================================
	// Session Operations
	// =========================================================================

	// GetAttachInfo returns information needed to attach to a run's session.
	// Includes session name, multiplexer type, and session existence check.
	// Returns ErrNotFound if the run does not exist.
	// Returns ErrSessionNotFound if the run exists but has no active session.
	GetAttachInfo(ctx context.Context, ref RunRef) (*AttachInfo, error)

	// CaptureSession captures output from a run's session.
	// Supports both tmux capture and OpenCode session capture.
	CaptureSession(ctx context.Context, ref RunRef, lines int) (*CaptureResult, error)

	// SendMessage sends a message/input to a run's session.
	SendMessage(ctx context.Context, ref RunRef, message string, noEnter bool) error

	// InjectInitialPrompt injects the initial prompt into a run's session.
	// Handles both tmux and OpenCode agents through the daemon.
	InjectInitialPrompt(ctx context.Context, ref RunRef, prompt string) error

	// =========================================================================
	// Git Operations
	// =========================================================================

	// GetDiffStats returns diff statistics for a run's branch vs main.
	GetDiffStats(ctx context.Context, ref RunRef) (*DiffStats, error)

	// GetBranchState returns the merge state of a run's branch.
	GetBranchState(ctx context.Context, ref RunRef) (BranchState, error)

	// GetDiff returns the full diff for a run's branch vs main.
	GetDiff(ctx context.Context, ref RunRef) (string, error)

	// =========================================================================
	// Issue Resolution (merge to main)
	// =========================================================================

	// ResolveIssue marks an issue as resolved and merges the latest run's branch.
	// If force is true, skips confirmation checks.
	ResolveIssue(ctx context.Context, issueID model.IssueID, force bool) error

	// =========================================================================
	// Run Deletion
	// =========================================================================

	// DeleteRun deletes a run and its associated resources.
	DeleteRun(ctx context.Context, ref RunRef, opts *DeleteRunOptions) (*DeleteRunResult, error)

	// CleanRunWorktree removes a run's worktree while preserving the run record.
	CleanRunWorktree(ctx context.Context, ref RunRef) (*CleanRunWorktreeResult, error)

	// =========================================================================
	// Issue File Operations
	// =========================================================================

	// UpdateIssue updates an existing issue's content.
	UpdateIssue(ctx context.Context, issueID model.IssueID, req *UpdateIssueRequest) (*Issue, error)

	// ValidateIssueFiles validates issue files for proper formatting.
	ValidateIssueFiles(ctx context.Context, issueID model.IssueID) (*ValidateIssueFilesResult, error)

	// =========================================================================
	// Agent Prompt Operations
	// =========================================================================

	// WriteAgentPrompt writes the agent control prompt for a run.
	WriteAgentPrompt(ctx context.Context, ref RunRef, content string) error

	// ReadAgentPrompt reads the agent control prompt for a run.
	ReadAgentPrompt(ctx context.Context, ref RunRef) (string, error)

	// =========================================================================
	// System Operations
	// =========================================================================

	// RepairState repairs system state inconsistencies.
	RepairState(ctx context.Context, opts *RepairOptions) (*RepairResult, error)

	// GetDaemonLog returns the daemon log content.
	GetDaemonLog(ctx context.Context, lines int) (string, error)

	// ReadFile reads a file's content via daemon.
	ReadFile(ctx context.Context, path string) ([]byte, error)

	// WriteFile writes content to a file via daemon.
	WriteFile(ctx context.Context, path string, content []byte, perm uint32) error

	// =========================================================================
	// Daemon/Server
	// =========================================================================

	// Ping checks if the daemon is running and healthy.
	Ping(ctx context.Context) error

	// EnsureOpenCodeServer ensures an OpenCode server is running for the current project scope.
	EnsureOpenCodeServer(ctx context.Context) (*OpenCodeServerInfo, error)

	// QueryOpenCodeServer queries an OpenCode server for available providers and models.
	QueryOpenCodeServer(ctx context.Context, port int) (*QueryOpenCodeServerResult, error)

	// GetConfig retrieves the orch configuration for the current project scope.
	GetConfig(ctx context.Context) (*Config, error)

	// GetDaemonStatus returns the daemon's running status.
	GetDaemonStatus(ctx context.Context) (*DaemonStatus, error)

	// ContinueRun continues a run from a previous run or branch.
	ContinueRun(ctx context.Context, req *ContinueRunRequest) (*ContinueRunResult, error)
}
