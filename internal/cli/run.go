package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/tmux"
	"github.com/spf13/cobra"
)

type runOptions struct {
	New          bool
	Reuse        bool
	RunID        string
	Agent        string
	AgentCmd     string
	BaseBranch   string
	Branch       string
	WorktreeRoot string
	RepoRoot     string
	Tmux         bool
	TmuxSession  string
	DryRun       bool
}

func newRunCmd() *cobra.Command {
	opts := &runOptions{}

	cmd := &cobra.Command{
		Use:   "run ISSUE_ID",
		Short: "Create and start a new run",
		Long: `Create a new run for an issue, set up a git worktree, and launch an agent.

The run will be started in a tmux session by default.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRun(args[0], opts)
		},
	}

	cmd.Flags().BoolVar(&opts.New, "new", true, "Always create a new run (default)")
	cmd.Flags().BoolVar(&opts.Reuse, "reuse", false, "Reuse the latest run if blocked")
	cmd.Flags().StringVar(&opts.RunID, "run-id", "", "Manually specify run ID")
	cmd.Flags().StringVar(&opts.Agent, "agent", "claude", "Agent type (claude|codex|gemini|custom)")
	cmd.Flags().StringVar(&opts.AgentCmd, "agent-cmd", "", "Custom agent command (when --agent=custom)")
	cmd.Flags().StringVar(&opts.BaseBranch, "base-branch", "main", "Base branch for worktree")
	cmd.Flags().StringVar(&opts.Branch, "branch", "", "Branch name (default: issue/<ID>/run-<RUN_ID>)")
	cmd.Flags().StringVar(&opts.WorktreeRoot, "worktree-root", ".git-worktrees", "Root directory for worktrees")
	cmd.Flags().StringVar(&opts.RepoRoot, "repo-root", "", "Git repository root (default: auto-detect)")
	cmd.Flags().BoolVar(&opts.Tmux, "tmux", true, "Run in tmux session")
	cmd.Flags().StringVar(&opts.TmuxSession, "tmux-session", "", "Tmux session name (default: run-<ISSUE>-<RUN>)")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would be done without doing it")

	return cmd
}

type runResult struct {
	OK           bool   `json:"ok"`
	IssueID      string `json:"issue_id"`
	RunID        string `json:"run_id"`
	RunPath      string `json:"run_path"`
	Branch       string `json:"branch"`
	WorktreePath string `json:"worktree_path"`
	TmuxSession  string `json:"tmux_session"`
	Status       string `json:"status"`
	Error        string `json:"error,omitempty"`
}

func runRun(issueID string, opts *runOptions) error {
	st, err := getStore()
	if err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	// Resolve issue first
	issue, err := st.ResolveIssue(issueID)
	if err != nil {
		return exitWithCode(fmt.Errorf("issue not found: %s", issueID), ExitIssueNotFound)
	}

	// Determine run ID
	runID := opts.RunID
	if runID == "" {
		if opts.Reuse {
			// Try to get latest run
			latestRun, err := st.GetLatestRun(issueID)
			if err == nil && latestRun.Status == model.StatusBlocked {
				runID = latestRun.RunID
			}
		}
		if runID == "" {
			runID = model.GenerateRunID()
		}
	}

	// Determine branch name
	branch := opts.Branch
	if branch == "" {
		branch = model.GenerateBranchName(issueID, runID)
	}

	// Determine tmux session name
	tmuxSession := opts.TmuxSession
	if tmuxSession == "" {
		tmuxSession = model.GenerateTmuxSession(issueID, runID)
	}

	// Find repo root
	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		repoRoot, err = git.FindRepoRoot("")
		if err != nil {
			return exitWithCode(fmt.Errorf("could not find git repository: %w", err), ExitWorktreeError)
		}
	}

	// Compute worktree path
	worktreePath := fmt.Sprintf("%s/%s/%s", opts.WorktreeRoot, issueID, runID)

	result := &runResult{
		OK:           true,
		IssueID:      issueID,
		RunID:        runID,
		Branch:       branch,
		WorktreePath: worktreePath,
		TmuxSession:  tmuxSession,
		Status:       string(model.StatusQueued),
	}

	// Dry run - just output what would happen
	if opts.DryRun {
		if globalOpts.JSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		}
		fmt.Printf("Would create run:\n")
		fmt.Printf("  Issue:     %s\n", issueID)
		fmt.Printf("  Run ID:    %s\n", runID)
		fmt.Printf("  Branch:    %s\n", branch)
		fmt.Printf("  Worktree:  %s\n", worktreePath)
		fmt.Printf("  Session:   %s\n", tmuxSession)
		return nil
	}

	// Create run document
	run, err := st.CreateRun(issueID, runID, map[string]string{
		"agent": opts.Agent,
	})
	if err != nil {
		return exitWithCode(fmt.Errorf("failed to create run: %w", err), ExitInternalError)
	}
	result.RunPath = run.Path

	// Append initial status event
	if err := st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusQueued)); err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	// Create worktree
	worktreeResult, err := git.CreateWorktree(&git.WorktreeConfig{
		RepoRoot:     repoRoot,
		WorktreeRoot: opts.WorktreeRoot,
		IssueID:      issueID,
		RunID:        runID,
		BaseBranch:   opts.BaseBranch,
		Branch:       branch,
	})
	if err != nil {
		st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusFailed))
		return exitWithCode(fmt.Errorf("failed to create worktree: %w", err), ExitWorktreeError)
	}

	result.WorktreePath = worktreeResult.WorktreePath
	result.Branch = worktreeResult.Branch

	// Record artifacts
	st.AppendEvent(run.Ref(), model.NewArtifactEvent("worktree", map[string]string{
		"path": worktreeResult.WorktreePath,
	}))
	st.AppendEvent(run.Ref(), model.NewArtifactEvent("branch", map[string]string{
		"name": worktreeResult.Branch,
	}))

	// Get agent adapter
	agentType, err := agent.ParseAgentType(opts.Agent)
	if err != nil {
		return exitWithCode(err, ExitAgentError)
	}

	adapter, err := agent.GetAdapter(agentType)
	if err != nil {
		return exitWithCode(err, ExitAgentError)
	}

	if !adapter.IsAvailable() {
		return exitWithCode(fmt.Errorf("agent %s is not available", opts.Agent), ExitAgentError)
	}

	// Build agent launch config
	launchCfg := &agent.LaunchConfig{
		Type:      agentType,
		CustomCmd: opts.AgentCmd,
		WorkDir:   worktreeResult.WorktreePath,
		IssueID:   issueID,
		RunID:     runID,
		RunPath:   run.Path,
		VaultPath: st.VaultPath(),
		Branch:    worktreeResult.Branch,
		Prompt:    buildAgentPrompt(issue),
	}

	agentCmd, err := adapter.LaunchCommand(launchCfg)
	if err != nil {
		return exitWithCode(err, ExitAgentError)
	}

	// Update status to booting
	st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusBooting))

	if opts.Tmux {
		// Check if tmux is available
		if !tmux.IsTmuxAvailable() {
			return exitWithCode(fmt.Errorf("tmux is not available"), ExitTmuxError)
		}

		// Create tmux session
		err = tmux.NewSession(&tmux.SessionConfig{
			SessionName: tmuxSession,
			WorkDir:     worktreeResult.WorktreePath,
			Command:     agentCmd,
			Env:         launchCfg.Env(),
		})
		if err != nil {
			st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusFailed))
			return exitWithCode(fmt.Errorf("failed to create tmux session: %w", err), ExitTmuxError)
		}

		// Record session artifact
		st.AppendEvent(run.Ref(), model.NewArtifactEvent("session", map[string]string{
			"name": tmuxSession,
		}))
	}

	// Update status to running
	st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusRunning))
	result.Status = string(model.StatusRunning)

	// Output result
	if globalOpts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	if !globalOpts.Quiet {
		fmt.Printf("Run started: %s#%s\n", issueID, runID)
		fmt.Printf("  Branch:   %s\n", result.Branch)
		fmt.Printf("  Worktree: %s\n", result.WorktreePath)
		if opts.Tmux {
			fmt.Printf("  Session:  %s\n", result.TmuxSession)
			fmt.Printf("\nAttach with: orch attach %s#%s\n", issueID, runID)
		}
	}

	return nil
}

func buildAgentPrompt(issue *model.Issue) string {
	prompt := fmt.Sprintf("You are working on issue: %s\n\n", issue.ID)
	if issue.Title != "" {
		prompt += fmt.Sprintf("Title: %s\n\n", issue.Title)
	}
	if issue.Body != "" {
		prompt += fmt.Sprintf("Description:\n%s\n", issue.Body)
	}
	return prompt
}

func exitWithCode(err error, code int) error {
	if globalOpts.JSON {
		result := &runResult{
			OK:    false,
			Error: err.Error(),
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(result)
	}
	os.Exit(code)
	return err
}
