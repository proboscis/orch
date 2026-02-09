package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/daemon"
	"github.com/s22625/orch/internal/git"
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
	cmd.Flags().StringVar(&opts.Multiplexer, "multiplexer", "", "Terminal multiplexer (tmux|zellij)")
	cmd.Flags().BoolVar(&opts.NoPR, "no-pr", false, "Skip PR creation instructions in agent prompt")
	cmd.Flags().StringVar(&opts.PromptTemplate, "prompt-template", "", "Custom prompt template file")
	cmd.Flags().StringVar(&opts.Branch, "branch", "", "Existing branch to continue from")
	cmd.Flags().StringVar(&opts.IssueID, "issue", "", "Issue ID (required with --branch when no RUN_REF)")
	cmd.Flags().StringVar(&opts.WorktreeDir, "worktree-dir", "", "Directory for worktrees (default: ~/.orch/worktrees)")
	cmd.Flags().StringVar(&opts.RepoRoot, "repo-root", "", "Git repository root (default: auto-detect)")

	return cmd
}

func runContinue(refStr string, opts *continueOptions) error {
	if err := applyPromptConfigDefaultsForContinue(opts); err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	var issueID, runID, shortID string
	if opts.Branch != "" {
		var err error
		issueID, err = resolveContinueIssueID(refStr, opts)
		if err != nil {
			return exitWithCode(err, ExitInternalError)
		}
	} else {
		if opts.IssueID != "" {
			return exitWithCode(fmt.Errorf("--issue requires --branch"), ExitInternalError)
		}
		if refStr == "" {
			return exitWithCode(fmt.Errorf("RUN_REF required (or use --branch with --issue)"), ExitInternalError)
		}
		if strings.Contains(refStr, "#") {
			parts := strings.SplitN(refStr, "#", 2)
			issueID = parts[0]
			runID = parts[1]
		} else if shortIDRegex.MatchString(refStr) {
			shortID = refStr
		} else {
			issueID = refStr
		}
	}

	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		var err error
		repoRoot, err = git.FindMainRepoRoot("")
		if err != nil {
			return exitWithCode(fmt.Errorf("could not find git repository: %w", err), ExitWorktreeError)
		}
	}

	issuesRoot, err := getIssuesRoot()
	if err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	daemonClient := daemon.NewProtoClientWithIssuesRoot(repoRoot, issuesRoot)
	if !daemonClient.IsAvailable() {
		if _, err := daemon.StartInBackground(); err != nil {
			return exitWithCode(fmt.Errorf("daemon not running and failed to start: %w\nRun 'orch repair' to fix daemon issues", err), ExitInternalError)
		}
		for i := 0; i < 20; i++ {
			if daemonClient.IsAvailable() {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
		if !daemonClient.IsAvailable() {
			return exitWithCode(fmt.Errorf("daemon started but not responding"), ExitInternalError)
		}
	}

	resp, err := daemonClient.ContinueRun(&daemon.ContinueRunOptions{
		IssueID:        issueID,
		RunID:          runID,
		ShortID:        shortID,
		Branch:         normalizeBranchName(opts.Branch),
		Agent:          opts.Agent,
		AgentCmd:       opts.AgentCmd,
		AgentProfile:   opts.AgentProfile,
		WorktreeDir:    opts.WorktreeDir,
		NoPR:           opts.NoPR,
		PromptTemplate: opts.PromptTemplate,
		PRTargetBranch: opts.PRTargetBranch,
		Multiplexer:    opts.Multiplexer,
		TmuxSession:    opts.TmuxSession,
		ProjectRoot:    repoRoot,
		RepoRoot:       opts.RepoRoot,
	})
	if err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	result := &continueResult{
		OK:            true,
		IssueID:       resp.IssueID,
		RunID:         resp.RunID,
		Branch:        resp.Branch,
		WorktreePath:  resp.WorktreePath,
		TmuxSession:   resp.TmuxSession,
		Status:        resp.Status,
		ContinuedFrom: resp.ContinuedFrom,
	}

	if globalOpts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	if !globalOpts.Quiet {
		fmt.Printf("Run continued: %s#%s\n", resp.IssueID, resp.RunID)
		fmt.Printf("  Continued from: %s\n", resp.ContinuedFrom)
		fmt.Printf("  Branch:         %s\n", resp.Branch)
		fmt.Printf("  Worktree:       %s\n", resp.WorktreePath)
		if resp.TmuxSession != "" {
			fmt.Printf("  Session:        %s\n", resp.TmuxSession)
			fmt.Printf("\nAttach with: orch attach %s#%s\n", resp.IssueID, resp.RunID)
		}
	}

	return nil
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

	if opts.WorktreeDir == "" {
		if cfg.WorktreeDir != "" {
			opts.WorktreeDir = cfg.WorktreeDir
		} else {
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
