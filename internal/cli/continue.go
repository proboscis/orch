package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/multiplexer"
	"github.com/s22625/orch/internal/orchapi"
	"github.com/spf13/cobra"
)

type continueOptions struct {
	Agent          string
	AgentCmd       string
	AgentProfile   string
	Tmux           bool
	TmuxSession    string
	Multiplexer    string
	NoPR           bool
	PromptTemplate string
	PRTargetBranch string
	Branch         string
	IssueID        string
	WorktreeDir    string
	RepoRoot       string
}

type continueResult struct {
	OK            bool   `json:"ok"`
	IssueID       string `json:"issue_id"`
	RunID         string `json:"run_id"`
	RunPath       string `json:"run_path"`
	Branch        string `json:"branch"`
	WorktreePath  string `json:"worktree_path"`
	TmuxSession   string `json:"tmux_session"`
	Status        string `json:"status"`
	ContinuedFrom string `json:"continued_from"`
	Error         string `json:"error,omitempty"`
}

func newContinueCmd() *cobra.Command {
	opts := &continueOptions{}

	cmd := &cobra.Command{
		Use:   "continue [RUN_REF|ISSUE_ID]",
		Short: "Continue work from an existing run",
		Long: `Continue work from an existing run by reusing its worktree and branch.

This creates a new run record that references the original run.

Use --branch with an issue ID to continue from an untracked branch.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref := ""
			if len(args) > 0 {
				ref = args[0]
			}
			return runContinue(ref, opts)
		},
	}

	cmd.Flags().StringVar(&opts.Agent, "agent", "", "Agent type (claude|codex|gemini|custom)")
	cmd.Flags().StringVar(&opts.AgentCmd, "agent-cmd", "", "Custom agent command (when --agent=custom)")
	cmd.Flags().StringVar(&opts.AgentProfile, "profile", "", "Agent profile (e.g., claude --profile)")
	cmd.Flags().BoolVar(&opts.Tmux, "tmux", true, "Run in tmux session")
	cmd.Flags().StringVar(&opts.TmuxSession, "tmux-session", "", "Tmux session name (default: run-<ISSUE>-<RUN>)")
	cmd.Flags().BoolVar(&opts.NoPR, "no-pr", false, "Skip PR creation instructions in agent prompt")
	cmd.Flags().StringVar(&opts.PromptTemplate, "prompt-template", "", "Custom prompt template file")
	cmd.Flags().StringVar(&opts.Branch, "branch", "", "Existing branch to continue from")
	cmd.Flags().StringVar(&opts.IssueID, "issue", "", "Issue ID (required with --branch when no RUN_REF)")
	cmd.Flags().StringVar(&opts.WorktreeDir, "worktree-dir", "", "Directory for worktrees (default: ~/.orch/worktrees)")
	cmd.Flags().StringVar(&opts.RepoRoot, "repo-root", "", "Git repository root (default: auto-detect)")

	return cmd
}

func runContinue(refStr string, opts *continueOptions) error {
	ctx := context.Background()
	api, err := getAPI()
	if err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	if err := applyPromptConfigDefaultsForContinue(opts); err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	if opts.Branch != "" {
		return continueFromBranch(ctx, api, refStr, opts)
	}
	if opts.IssueID != "" {
		return exitWithCode(fmt.Errorf("--issue requires --branch"), ExitInternalError)
	}
	if refStr == "" {
		return exitWithCode(fmt.Errorf("RUN_REF required (or use --branch with --issue)"), ExitInternalError)
	}

	return continueFromRun(ctx, api, refStr, opts)
}

func appendStatusViaAPI(ctx context.Context, api orchapi.OrchAPI, issueID, runID string, status model.Status) error {
	ref := orchapi.RunRef{IssueID: issueID, RunID: runID}
	event := &orchapi.Event{
		Type: "status",
		Name: string(status),
	}
	_, err := api.AppendEvent(ctx, ref, event)
	return err
}

func continueFromRun(ctx context.Context, api orchapi.OrchAPI, refStr string, opts *continueOptions) error {
	ref, err := orchapi.ParseRunRef(refStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid run ref: %s\n", refStr)
		os.Exit(ExitRunNotFound)
		return err
	}

	fromRun, err := api.ResolveRun(ctx, ref)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run not found: %s\n", refStr)
		os.Exit(ExitRunNotFound)
		return err
	}

	if isActiveStatusForContinueAPI(fromRun.Status) {
		return exitWithCode(fmt.Errorf("run %s#%s is %s; stop it before continuing", fromRun.IssueID, fromRun.RunID, fromRun.Status), ExitInternalError)
	}

	if fromRun.WorktreePath == "" {
		return exitWithCode(fmt.Errorf("run %s#%s has no worktree path", fromRun.IssueID, fromRun.RunID), ExitWorktreeError)
	}
	if fromRun.Branch == "" {
		return exitWithCode(fmt.Errorf("run %s#%s has no branch", fromRun.IssueID, fromRun.RunID), ExitWorktreeError)
	}

	worktreeInfo, err := os.Stat(fromRun.WorktreePath)
	if err != nil {
		return exitWithCode(fmt.Errorf("worktree not found: %w", err), ExitWorktreeError)
	}
	if !worktreeInfo.IsDir() {
		return exitWithCode(fmt.Errorf("worktree path is not a directory: %s", fromRun.WorktreePath), ExitWorktreeError)
	}

	currentBranch, err := git.GetCurrentBranch(fromRun.WorktreePath)
	if err != nil {
		return exitWithCode(fmt.Errorf("failed to read worktree branch: %w", err), ExitWorktreeError)
	}
	if currentBranch != fromRun.Branch {
		return exitWithCode(fmt.Errorf("worktree %s is on branch %s; expected %s", fromRun.WorktreePath, currentBranch, fromRun.Branch), ExitWorktreeError)
	}

	issue, err := api.GetIssue(ctx, fromRun.IssueID)
	if err != nil {
		return exitWithCode(fmt.Errorf("issue not found: %s", fromRun.IssueID), ExitIssueNotFound)
	}

	agentName := opts.Agent
	if agentName == "" {
		if fromRun.Agent != "" {
			agentName = fromRun.Agent
		} else {
			agentName = "claude"
		}
	}

	runID := model.GenerateRunID()
	tmuxSession := opts.TmuxSession
	if tmuxSession == "" {
		tmuxSession = model.GenerateTmuxSession(fromRun.IssueID, runID)
	}

	continuedFrom := fmt.Sprintf("%s#%s", fromRun.IssueID, fromRun.RunID)
	result := &continueResult{
		OK:            true,
		IssueID:       fromRun.IssueID,
		RunID:         runID,
		Branch:        fromRun.Branch,
		WorktreePath:  fromRun.WorktreePath,
		TmuxSession:   tmuxSession,
		Status:        string(model.StatusQueued),
		ContinuedFrom: continuedFrom,
	}

	runResult, err := api.CreateRun(ctx, &orchapi.CreateRunRequest{
		IssueID: fromRun.IssueID,
		RunID:   runID,
		Metadata: map[string]string{
			"agent":          agentName,
			"continued_from": continuedFrom,
		},
	})
	if err != nil {
		return exitWithCode(fmt.Errorf("failed to create run: %w", err), ExitInternalError)
	}
	result.RunPath = runResult.Path

	if err := appendStatusViaAPI(ctx, api, fromRun.IssueID, runID, model.StatusQueued); err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	runRef := orchapi.RunRef{IssueID: fromRun.IssueID, RunID: runID}
	api.AppendEvent(ctx, runRef, &orchapi.Event{Type: "artifact", Name: "worktree", Attrs: map[string]string{"path": fromRun.WorktreePath}})
	api.AppendEvent(ctx, runRef, &orchapi.Event{Type: "artifact", Name: "branch", Attrs: map[string]string{"name": fromRun.Branch}})

	issuesRoot, _ := getIssuesRoot()
	promptOpts := &promptOptions{
		NoPR:           opts.NoPR,
		PromptTemplate: opts.PromptTemplate,
		PRTargetBranch: opts.PRTargetBranch,
		IssuesRoot:     issuesRoot,
		IssuePath:      issue.Path,
	}
	issueModel := apiIssueToModelIssue(issue)
	if err := ensurePromptFileAPI(ctx, api, fromRun.WorktreePath, issueModel, promptOpts); err != nil {
		return exitWithCode(fmt.Errorf("failed to write prompt file: %w", err), ExitInternalError)
	}

	agentType, err := agent.ParseAgentType(agentName)
	if err != nil {
		return exitWithCode(err, ExitAgentError)
	}

	adapter, err := agent.GetAdapter(agentType)
	if err != nil {
		return exitWithCode(err, ExitAgentError)
	}

	if !adapter.IsAvailable() {
		return exitWithCode(fmt.Errorf("agent %s is not available", agentName), ExitAgentError)
	}

	launchCfg := &agent.LaunchConfig{
		Type:       agentType,
		CustomCmd:  opts.AgentCmd,
		WorkDir:    fromRun.WorktreePath,
		IssueID:    fromRun.IssueID,
		RunID:      runID,
		RunPath:    runResult.Path,
		IssuesRoot: issuesRoot,
		Branch:     fromRun.Branch,
		Prompt:     buildContinuePrompt(continuedFrom),
		Profile:    opts.AgentProfile,
	}

	agentCmd, err := adapter.LaunchCommand(launchCfg)
	if err != nil {
		return exitWithCode(err, ExitAgentError)
	}

	appendStatusViaAPI(ctx, api, fromRun.IssueID, runID, model.StatusBooting)

	if opts.Tmux {
		muxType, _ := multiplexer.ParseType(opts.Multiplexer)
		var mux multiplexer.Multiplexer
		if muxType == multiplexer.TypeAuto {
			mux, err = multiplexer.GetAuto()
		} else {
			var warning string
			mux, warning, err = multiplexer.GetWithFallback(muxType)
			if warning != "" {
				fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
			}
		}
		if err != nil {
			return exitWithCode(err, ExitTmuxError)
		}

		err = mux.NewSession(&multiplexer.SessionConfig{
			SessionName: tmuxSession,
			WorkDir:     fromRun.WorktreePath,
			Command:     agentCmd,
			Env:         launchCfg.Env(),
		})
		if err != nil {
			appendStatusViaAPI(ctx, api, fromRun.IssueID, runID, model.StatusFailed)
			return exitWithCode(fmt.Errorf("failed to create %s session: %w", mux.Type(), err), ExitTmuxError)
		}

		if adapter.PromptInjection() == agent.InjectionTmux && launchCfg.Prompt != "" {
			if pattern := adapter.ReadyPattern(); pattern != "" {
				if err := mux.WaitForReady(tmuxSession, pattern, 30*time.Second); err != nil {
					appendStatusViaAPI(ctx, api, fromRun.IssueID, runID, model.StatusFailed)
					return exitWithCode(fmt.Errorf("agent did not become ready: %w", err), ExitAgentError)
				}
			}
			if err := mux.SendKeys(tmuxSession, launchCfg.Prompt); err != nil {
				appendStatusViaAPI(ctx, api, fromRun.IssueID, runID, model.StatusFailed)
				return exitWithCode(fmt.Errorf("failed to send prompt to session: %w", err), ExitTmuxError)
			}
		}

		// Only create session artifact for non-HTTP agents (opencode uses HTTP, no tmux session)
		if adapter.PromptInjection() != agent.InjectionHTTP {
			api.AppendEvent(ctx, runRef, &orchapi.Event{Type: "artifact", Name: "session", Attrs: map[string]string{"name": tmuxSession, "multiplexer": string(mux.Type())}})

			windowID := ""
			if windows, err := mux.ListWindows(tmuxSession); err == nil {
				for _, window := range windows {
					if window.Index == 0 {
						windowID = window.ID
						break
					}
				}
			}
			if windowID != "" {
				api.AppendEvent(ctx, runRef, &orchapi.Event{Type: "artifact", Name: "window", Attrs: map[string]string{"id": windowID}})
			}
		}
	}

	appendStatusViaAPI(ctx, api, fromRun.IssueID, runID, model.StatusRunning)
	result.Status = string(model.StatusRunning)

	if globalOpts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	if !globalOpts.Quiet {
		fmt.Printf("Run continued: %s#%s\n", fromRun.IssueID, runID)
		fmt.Printf("  Continued from: %s\n", continuedFrom)
		fmt.Printf("  Branch:         %s\n", result.Branch)
		fmt.Printf("  Worktree:       %s\n", result.WorktreePath)
		if opts.Tmux {
			fmt.Printf("  Session:        %s\n", result.TmuxSession)
			fmt.Printf("\nAttach with: orch attach %s#%s\n", fromRun.IssueID, runID)
		}
	}

	return nil
}

func continueFromBranch(ctx context.Context, api orchapi.OrchAPI, refStr string, opts *continueOptions) error {
	issueID, err := resolveContinueIssueID(refStr, opts)
	if err != nil {
		return exitWithCode(err, ExitInternalError)
	}
	branch := normalizeBranchName(opts.Branch)
	if branch == "" {
		return exitWithCode(fmt.Errorf("branch is required"), ExitInternalError)
	}

	issue, err := api.GetIssue(ctx, issueID)
	if err != nil {
		return exitWithCode(fmt.Errorf("issue not found: %s", issueID), ExitIssueNotFound)
	}

	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		repoRoot, err = git.FindMainRepoRoot("")
		if err != nil {
			return exitWithCode(fmt.Errorf("could not find git repository: %w", err), ExitWorktreeError)
		}
	}

	runID := model.GenerateRunID()
	tmuxSession := opts.TmuxSession
	if tmuxSession == "" {
		tmuxSession = model.GenerateTmuxSession(issueID, runID)
	}

	agentName := opts.Agent
	if agentName == "" {
		agentName = "claude"
	}

	worktreePath, err := resolveWorktreeForBranch(repoRoot, branch, opts.WorktreeDir, issueID, runID, agentName)
	if err != nil {
		return exitWithCode(err, ExitWorktreeError)
	}

	continuedFrom := fmt.Sprintf("branch:%s", branch)
	result := &continueResult{
		OK:            true,
		IssueID:       issueID,
		RunID:         runID,
		Branch:        branch,
		WorktreePath:  worktreePath,
		TmuxSession:   tmuxSession,
		Status:        string(model.StatusQueued),
		ContinuedFrom: continuedFrom,
	}

	runResult, err := api.CreateRun(ctx, &orchapi.CreateRunRequest{
		IssueID: issueID,
		RunID:   runID,
		Metadata: map[string]string{
			"agent":          agentName,
			"continued_from": continuedFrom,
		},
	})
	if err != nil {
		return exitWithCode(fmt.Errorf("failed to create run: %w", err), ExitInternalError)
	}
	result.RunPath = runResult.Path

	if err := appendStatusViaAPI(ctx, api, issueID, runID, model.StatusQueued); err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	runRef := orchapi.RunRef{IssueID: issueID, RunID: runID}
	api.AppendEvent(ctx, runRef, &orchapi.Event{Type: "artifact", Name: "worktree", Attrs: map[string]string{"path": worktreePath}})
	api.AppendEvent(ctx, runRef, &orchapi.Event{Type: "artifact", Name: "branch", Attrs: map[string]string{"name": branch}})

	issuesRoot, _ := getIssuesRoot()
	promptOpts := &promptOptions{
		NoPR:           opts.NoPR,
		PromptTemplate: opts.PromptTemplate,
		PRTargetBranch: opts.PRTargetBranch,
		IssuesRoot:     issuesRoot,
		IssuePath:      issue.Path,
	}
	issueModel := apiIssueToModelIssue(issue)
	if err := ensurePromptFileAPI(ctx, api, worktreePath, issueModel, promptOpts); err != nil {
		return exitWithCode(fmt.Errorf("failed to write prompt file: %w", err), ExitInternalError)
	}

	agentType, err := agent.ParseAgentType(agentName)
	if err != nil {
		return exitWithCode(err, ExitAgentError)
	}
	adapter, err := agent.GetAdapter(agentType)
	if err != nil {
		return exitWithCode(err, ExitAgentError)
	}
	if !adapter.IsAvailable() {
		return exitWithCode(fmt.Errorf("agent %s is not available", agentName), ExitAgentError)
	}

	launchCfg := &agent.LaunchConfig{
		Type:       agentType,
		CustomCmd:  opts.AgentCmd,
		WorkDir:    worktreePath,
		IssueID:    issueID,
		RunID:      runID,
		RunPath:    runResult.Path,
		IssuesRoot: issuesRoot,
		Branch:     branch,
		Prompt:     buildContinuePrompt(continuedFrom),
		Profile:    opts.AgentProfile,
	}

	agentCmd, err := adapter.LaunchCommand(launchCfg)
	if err != nil {
		return exitWithCode(err, ExitAgentError)
	}

	appendStatusViaAPI(ctx, api, issueID, runID, model.StatusBooting)

	if opts.Tmux {
		muxType, _ := multiplexer.ParseType(opts.Multiplexer)
		var mux multiplexer.Multiplexer
		if muxType == multiplexer.TypeAuto {
			mux, err = multiplexer.GetAuto()
		} else {
			var warning string
			mux, warning, err = multiplexer.GetWithFallback(muxType)
			if warning != "" {
				fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
			}
		}
		if err != nil {
			return exitWithCode(err, ExitTmuxError)
		}

		err = mux.NewSession(&multiplexer.SessionConfig{
			SessionName: tmuxSession,
			WorkDir:     worktreePath,
			Command:     agentCmd,
			Env:         launchCfg.Env(),
		})
		if err != nil {
			appendStatusViaAPI(ctx, api, issueID, runID, model.StatusFailed)
			return exitWithCode(fmt.Errorf("failed to create %s session: %w", mux.Type(), err), ExitTmuxError)
		}

		if adapter.PromptInjection() == agent.InjectionTmux && launchCfg.Prompt != "" {
			if pattern := adapter.ReadyPattern(); pattern != "" {
				if err := mux.WaitForReady(tmuxSession, pattern, 30*time.Second); err != nil {
					appendStatusViaAPI(ctx, api, issueID, runID, model.StatusFailed)
					return exitWithCode(fmt.Errorf("agent did not become ready: %w", err), ExitAgentError)
				}
			}
			if err := mux.SendKeys(tmuxSession, launchCfg.Prompt); err != nil {
				appendStatusViaAPI(ctx, api, issueID, runID, model.StatusFailed)
				return exitWithCode(fmt.Errorf("failed to send prompt to session: %w", err), ExitTmuxError)
			}
		}

		// Only create session artifact for non-HTTP agents (opencode uses HTTP, no tmux session)
		if adapter.PromptInjection() != agent.InjectionHTTP {
			api.AppendEvent(ctx, runRef, &orchapi.Event{Type: "artifact", Name: "session", Attrs: map[string]string{"name": tmuxSession, "multiplexer": string(mux.Type())}})

			windowID := ""
			if windows, err := mux.ListWindows(tmuxSession); err == nil {
				for _, window := range windows {
					if window.Index == 0 {
						windowID = window.ID
						break
					}
				}
			}
			if windowID != "" {
				api.AppendEvent(ctx, runRef, &orchapi.Event{Type: "artifact", Name: "window", Attrs: map[string]string{"id": windowID}})
			}
		}
	}

	appendStatusViaAPI(ctx, api, issueID, runID, model.StatusRunning)
	result.Status = string(model.StatusRunning)

	if globalOpts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	if !globalOpts.Quiet {
		fmt.Printf("Run continued: %s#%s\n", issueID, runID)
		fmt.Printf("  Continued from: %s\n", continuedFrom)
		fmt.Printf("  Branch:         %s\n", result.Branch)
		fmt.Printf("  Worktree:       %s\n", result.WorktreePath)
		if opts.Tmux {
			fmt.Printf("  Session:        %s\n", result.TmuxSession)
			fmt.Printf("\nAttach with: orch attach %s#%s\n", issueID, runID)
		}
	}

	return nil
}

func buildContinuePrompt(continuedFrom string) string {
	return fmt.Sprintf("%s\nThis run continues from %s. Use the existing worktree and branch and resume from the current state.\n", promptFileInstruction, continuedFrom)
}

func applyPromptConfigDefaultsForContinue(opts *continueOptions) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if opts.PromptTemplate == "" && cfg.PromptTemplate != "" {
		opts.PromptTemplate = cfg.PromptTemplate
	}

	if opts.PRTargetBranch == "" && cfg.PRTargetBranch != "" {
		opts.PRTargetBranch = cfg.PRTargetBranch
	}

	if cfg.NoPR && !opts.NoPR {
		opts.NoPR = cfg.NoPR
	}

	// WorktreeDir: use config value if flag not provided, fallback to "~/.orch/worktrees"
	if opts.WorktreeDir == "" {
		if cfg.WorktreeDir != "" {
			opts.WorktreeDir = cfg.WorktreeDir
		} else {
			// Default to ~/.orch/worktrees (outside repo, keeps repo clean)
			home, _ := os.UserHomeDir()
			opts.WorktreeDir = filepath.Join(home, ".orch", "worktrees")
		}
	}

	return nil
}

func resolveContinueIssueID(refStr string, opts *continueOptions) (string, error) {
	if opts.IssueID != "" && refStr != "" {
		return "", fmt.Errorf("issue ID specified twice")
	}

	if opts.IssueID != "" {
		return opts.IssueID, nil
	}

	if refStr == "" {
		return "", fmt.Errorf("issue ID required when using --branch")
	}
	if strings.Contains(refStr, "#") {
		return "", fmt.Errorf("RUN_REF is not allowed with --branch; use an issue ID")
	}
	if shortIDRegex.MatchString(refStr) {
		return "", fmt.Errorf("short run IDs are not allowed with --branch; use an issue ID")
	}

	return refStr, nil
}

func normalizeBranchName(branch string) string {
	branch = strings.TrimSpace(branch)
	return strings.TrimPrefix(branch, "refs/heads/")
}

func resolveWorktreeForBranch(repoRoot, branch, worktreeDir, issueID, runID, agent string) (string, error) {
	matches, err := git.FindWorktreesByBranch(repoRoot, branch)
	if err != nil {
		return "", fmt.Errorf("failed to list worktrees: %w", err)
	}
	if len(matches) > 1 {
		var paths []string
		for _, match := range matches {
			paths = append(paths, match.Path)
		}
		return "", fmt.Errorf("branch %s is checked out in multiple worktrees: %s", branch, strings.Join(paths, ", "))
	}
	if len(matches) == 1 {
		path := matches[0].Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(repoRoot, path)
		}
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("worktree not found: %w", err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("worktree path is not a directory: %s", path)
		}

		currentBranch, err := git.GetCurrentBranch(path)
		if err != nil {
			return "", fmt.Errorf("failed to read worktree branch: %w", err)
		}
		if currentBranch != branch {
			return "", fmt.Errorf("worktree %s is on branch %s; expected %s", path, currentBranch, branch)
		}

		return path, nil
	}

	result, err := git.CreateWorktreeFromBranch(&git.WorktreeConfig{
		RepoRoot:    repoRoot,
		WorktreeDir: worktreeDir,
		IssueID:     issueID,
		RunID:       runID,
		Agent:       agent,
		Branch:      branch,
	})
	if err != nil {
		return "", fmt.Errorf("failed to create worktree: %w", err)
	}
	return result.WorktreePath, nil
}

func isActiveStatusForContinue(status model.Status) bool {
	switch status {
	case model.StatusRunning, model.StatusBlocked, model.StatusBlockedAPI, model.StatusBooting, model.StatusQueued, model.StatusPROpen:
		return true
	default:
		return false
	}
}

func isActiveStatusForContinueAPI(status orchapi.RunStatus) bool {
	switch status {
	case orchapi.RunStatusRunning, orchapi.RunStatusBlocked, orchapi.RunStatusBlockedAPI, orchapi.RunStatusBooting, orchapi.RunStatusQueued, orchapi.RunStatusPROpen:
		return true
	default:
		return false
	}
}

func apiIssueToModelIssue(issue *orchapi.Issue) *model.Issue {
	if issue == nil {
		return nil
	}
	return &model.Issue{
		ID:      issue.ID,
		Title:   issue.Title,
		Summary: issue.Summary,
		Status:  model.IssueStatus(issue.Status),
		Body:    issue.Body,
		Path:    issue.Path,
	}
}

func ensurePromptFileAPI(ctx context.Context, api orchapi.OrchAPI, worktreePath string, issue *model.Issue, opts *promptOptions) error {
	promptPath := filepath.Join(worktreePath, promptFileName)
	if _, err := os.Stat(promptPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	agentPrompt := buildAgentPrompt(issue, opts)
	return api.WriteFile(ctx, promptPath, []byte(agentPrompt), 0644)
}
