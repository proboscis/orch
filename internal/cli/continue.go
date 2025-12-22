package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/s22625/orch/internal/agent"
	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/git"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/tmux"
	"github.com/spf13/cobra"
)

type continueOptions struct {
	Agent          string
	AgentCmd       string
	AgentProfile   string
	Tmux           bool
	TmuxSession    string
	NoPR           bool
	PromptTemplate string
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
		Use:   "continue RUN_REF",
		Short: "Continue work from an existing run",
		Long: `Continue work from an existing run by reusing its worktree and branch.

This creates a new run record that references the original run.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runContinue(args[0], opts)
		},
	}

	cmd.Flags().StringVar(&opts.Agent, "agent", "", "Agent type (claude|codex|gemini|custom)")
	cmd.Flags().StringVar(&opts.AgentCmd, "agent-cmd", "", "Custom agent command (when --agent=custom)")
	cmd.Flags().StringVar(&opts.AgentProfile, "profile", "", "Agent profile (e.g., claude --profile)")
	cmd.Flags().BoolVar(&opts.Tmux, "tmux", true, "Run in tmux session")
	cmd.Flags().StringVar(&opts.TmuxSession, "tmux-session", "", "Tmux session name (default: run-<ISSUE>-<RUN>)")
	cmd.Flags().BoolVar(&opts.NoPR, "no-pr", false, "Skip PR creation instructions in agent prompt")
	cmd.Flags().StringVar(&opts.PromptTemplate, "prompt-template", "", "Custom prompt template file")

	return cmd
}

func runContinue(refStr string, opts *continueOptions) error {
	st, err := getStore()
	if err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	if err := applyPromptConfigDefaultsForContinue(opts); err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	fromRun, err := resolveRun(st, refStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run not found: %s\n", refStr)
		os.Exit(ExitRunNotFound)
		return err
	}

	if isActiveStatusForContinue(fromRun.Status) {
		return exitWithCode(fmt.Errorf("run %s is %s; stop it before continuing", fromRun.Ref().String(), fromRun.Status), ExitInternalError)
	}

	if fromRun.WorktreePath == "" {
		return exitWithCode(fmt.Errorf("run %s has no worktree path", fromRun.Ref().String()), ExitWorktreeError)
	}
	if fromRun.Branch == "" {
		return exitWithCode(fmt.Errorf("run %s has no branch", fromRun.Ref().String()), ExitWorktreeError)
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

	issue, err := st.ResolveIssue(fromRun.IssueID)
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

	continuedFrom := fromRun.Ref().String()
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

	run, err := st.CreateRun(fromRun.IssueID, runID, map[string]string{
		"agent":          agentName,
		"continued_from": continuedFrom,
	})
	if err != nil {
		return exitWithCode(fmt.Errorf("failed to create run: %w", err), ExitInternalError)
	}
	result.RunPath = run.Path

	if err := st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusQueued)); err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	st.AppendEvent(run.Ref(), model.NewArtifactEvent("worktree", map[string]string{
		"path": fromRun.WorktreePath,
	}))
	st.AppendEvent(run.Ref(), model.NewArtifactEvent("branch", map[string]string{
		"name": fromRun.Branch,
	}))

	promptOpts := &promptOptions{
		NoPR:           opts.NoPR,
		PromptTemplate: opts.PromptTemplate,
	}
	if err := ensurePromptFile(fromRun.WorktreePath, issue, promptOpts); err != nil {
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
		Type:      agentType,
		CustomCmd: opts.AgentCmd,
		WorkDir:   fromRun.WorktreePath,
		IssueID:   fromRun.IssueID,
		RunID:     runID,
		RunPath:   run.Path,
		VaultPath: st.VaultPath(),
		Branch:    fromRun.Branch,
		Prompt:    buildContinuePrompt(fromRun),
		Profile:   opts.AgentProfile,
	}

	agentCmd, err := adapter.LaunchCommand(launchCfg)
	if err != nil {
		return exitWithCode(err, ExitAgentError)
	}

	st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusBooting))

	if opts.Tmux {
		if !tmux.IsTmuxAvailable() {
			return exitWithCode(fmt.Errorf("tmux is not available"), ExitTmuxError)
		}

		err = tmux.NewSession(&tmux.SessionConfig{
			SessionName: tmuxSession,
			WorkDir:     fromRun.WorktreePath,
			Command:     agentCmd,
			Env:         launchCfg.Env(),
		})
		if err != nil {
			st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusFailed))
			return exitWithCode(fmt.Errorf("failed to create tmux session: %w", err), ExitTmuxError)
		}

		if adapter.PromptInjection() == agent.InjectionTmux && launchCfg.Prompt != "" {
			if pattern := adapter.ReadyPattern(); pattern != "" {
				if err := tmux.WaitForReady(tmuxSession, pattern, 30*time.Second); err != nil {
					st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusFailed))
					return exitWithCode(fmt.Errorf("agent did not become ready: %w", err), ExitAgentError)
				}
			}
			if err := tmux.SendKeys(tmuxSession, launchCfg.Prompt); err != nil {
				st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusFailed))
				return exitWithCode(fmt.Errorf("failed to send prompt to session: %w", err), ExitTmuxError)
			}
		}

		st.AppendEvent(run.Ref(), model.NewArtifactEvent("session", map[string]string{
			"name": tmuxSession,
		}))

		windowID := ""
		if windows, err := tmux.ListWindows(tmuxSession); err == nil {
			for _, window := range windows {
				if window.Index == 0 {
					windowID = window.ID
					break
				}
			}
		}
		if windowID != "" {
			st.AppendEvent(run.Ref(), model.NewArtifactEvent("window", map[string]string{
				"id": windowID,
			}))
		}
	}

	st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusRunning))
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

func buildContinuePrompt(fromRun *model.Run) string {
	return fmt.Sprintf("%s\nThis run continues from %s. Use the existing worktree and branch and resume from the current state.\n", promptFileInstruction, fromRun.Ref().String())
}

func ensurePromptFile(worktreePath string, issue *model.Issue, opts *promptOptions) error {
	promptPath := filepath.Join(worktreePath, promptFileName)
	if _, err := os.Stat(promptPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}

	agentPrompt := buildAgentPrompt(issue, opts)
	return os.WriteFile(promptPath, []byte(agentPrompt), 0644)
}

func applyPromptConfigDefaultsForContinue(opts *continueOptions) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if opts.PromptTemplate == "" && cfg.PromptTemplate != "" {
		opts.PromptTemplate = cfg.PromptTemplate
	}

	if cfg.NoPR && !opts.NoPR {
		opts.NoPR = cfg.NoPR
	}

	return nil
}

func isActiveStatusForContinue(status model.Status) bool {
	switch status {
	case model.StatusRunning, model.StatusBlocked, model.StatusBlockedAPI, model.StatusBooting, model.StatusQueued, model.StatusPROpen:
		return true
	default:
		return false
	}
}
