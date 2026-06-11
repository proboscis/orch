package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/orchapi"
	"github.com/spf13/cobra"
)

type continueOptions struct {
	Agent          string
	AgentCmd       string
	AgentProfile   string
	CodexProfile   string
	Tmux           bool
	SessionName    string
	Multiplexer    string
	NoPR           bool
	PromptTemplate string
	PRTargetBranch string
	Branch         string
	IssueID        string
	WorktreeDir    string
	WorktreeSet    bool
}

type continueResult struct {
	OK            bool   `json:"ok"`
	IssueID       string `json:"issue_id"`
	RunID         string `json:"run_id"`
	RunPath       string `json:"run_path"`
	Branch        string `json:"branch"`
	WorktreePath  string `json:"worktree_path"`
	SessionName   string `json:"session_name"`
	Status        string `json:"status"`
	ContinuedFrom string `json:"continued_from"`
	Error         string `json:"error,omitempty"`
}

type continueDeps struct {
	getAPI func() (orchapi.OrchAPI, error)
}

func defaultContinueDeps() *continueDeps {
	return &continueDeps{getAPI: getAPI}
}

func newRestartFromCmd() *cobra.Command {
	opts := &continueOptions{}

	cmd := &cobra.Command{
		Use:   "restart-from [RUN_REF|ISSUE_ID]",
		Short: "Restart work from an existing run",
		Long: `Restart work from an existing run by reusing its worktree and branch.

This creates a new run record that references the original run.

Use this for failed, canceled, or unknown runs.

Use --branch with an issue ID to restart from an untracked branch.`,
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
	cmd.Flags().StringVar(&opts.CodexProfile, "codex-profile", "", "Codex execution profile from config (codex.profiles); defaults to the prior run's profile via codex.default_profile")
	cmd.Flags().BoolVar(&opts.Tmux, "tmux", true, "Run in tmux session")
	cmd.Flags().StringVar(&opts.SessionName, "session-name", "", "Session name (default: run-<ISSUE>-<RUN>)")
	cmd.Flags().StringVar(&opts.Multiplexer, "multiplexer", "", "Terminal multiplexer (tmux|zellij)")
	cmd.Flags().BoolVar(&opts.NoPR, "no-pr", false, "Skip PR creation instructions in agent prompt")
	cmd.Flags().StringVar(&opts.PromptTemplate, "prompt-template", "", "Custom prompt template file")
	cmd.Flags().StringVar(&opts.Branch, "branch", "", "Existing branch to restart from")
	cmd.Flags().StringVar(&opts.IssueID, "issue", "", "Issue ID (required with --branch when no RUN_REF)")
	return cmd
}

func runContinue(refStr string, opts *continueOptions) error {
	ctx := context.Background()
	return runContinueWithDeps(ctx, refStr, opts, defaultContinueDeps())
}

func runContinueWithDeps(ctx context.Context, refStr string, opts *continueOptions, deps *continueDeps) error {
	_, _, rootErr := getProjectRootWithSource()
	remoteMode := strings.TrimSpace(getRemoteAddr()) != ""
	if rootErr != nil {
		if !remoteMode {
			return exitWithCode(fmt.Errorf("project scope required: run from repository root or set --project/ORCH_PROJECT"), ExitWorktreeError)
		}
	}

	api, err := deps.getAPI()
	if err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	cfg, err := api.GetConfig(ctx)
	if err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	applyContinueConfigDefaults(opts, cfg, remoteMode)

	var issueID, runID, shortID string
	if opts.Branch != "" {
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

	resp, err := api.ContinueRun(ctx, &orchapi.ContinueRunRequest{
		IssueID:        model.IssueID(issueID),
		RunID:          model.RunID(runID),
		ShortID:        model.ShortID(shortID),
		Branch:         normalizeBranchName(opts.Branch),
		Agent:          opts.Agent,
		AgentCmd:       opts.AgentCmd,
		AgentProfile:   opts.AgentProfile,
		CodexProfile:   opts.CodexProfile,
		WorktreeDir:    opts.WorktreeDir,
		NoPR:           opts.NoPR,
		PromptTemplate: opts.PromptTemplate,
		PRTargetBranch: opts.PRTargetBranch,
		Multiplexer:    opts.Multiplexer,
		SessionName:    opts.SessionName,
		NoSession:      !opts.Tmux,
	})
	if err != nil {
		return exitWithCode(err, ExitInternalError)
	}

	result := &continueResult{
		OK:            true,
		IssueID:       string(resp.IssueID),
		RunID:         string(resp.RunID),
		Branch:        resp.Branch,
		WorktreePath:  resp.WorktreePath,
		SessionName:   resp.SessionName,
		Status:        resp.Status,
		ContinuedFrom: resp.ContinuedFrom,
	}

	if globalOpts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	if !globalOpts.Quiet {
		shortID := orchapi.ComputeShortID(resp.IssueID, resp.RunID)
		fmt.Printf("Run restarted: %s#%s (%s)\n", resp.IssueID, resp.RunID, shortID)
		fmt.Printf("  Restarted from: %s\n", resp.ContinuedFrom)
		fmt.Printf("  Branch:         %s\n", resp.Branch)
		fmt.Printf("  Worktree:       %s\n", resp.WorktreePath)
		if resp.SessionName != "" {
			fmt.Printf("  Session:        %s\n", resp.SessionName)
			fmt.Printf("\nAttach with: orch attach %s\n", shortID)
		}
	}

	return nil
}

func applyContinueConfigDefaults(opts *continueOptions, cfg *orchapi.Config, remoteMode bool) {
	if opts.PromptTemplate == "" && cfg.PromptTemplate != "" {
		opts.PromptTemplate = cfg.PromptTemplate
	}

	if opts.PRTargetBranch == "" && cfg.PRTargetBranch != "" {
		opts.PRTargetBranch = cfg.PRTargetBranch
	}

	if cfg.NoPR && !opts.NoPR {
		opts.NoPR = cfg.NoPR
	}

	if opts.WorktreeDir == "" && !opts.WorktreeSet {
		if cfg.WorktreeDir != "" {
			opts.WorktreeDir = cfg.WorktreeDir
		} else if !remoteMode {
			home, _ := os.UserHomeDir()
			opts.WorktreeDir = filepath.Join(home, ".orch", "worktrees")
		}
	}
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
