package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/s22625/orch/internal/config"
	"github.com/s22625/orch/internal/daemon"
	"github.com/s22625/orch/internal/model"
	"github.com/spf13/cobra"
)

type diffOptions struct {
	Stat       bool   // Show only summary (--stat)
	BaseBranch string // Override base branch
}

func newDiffCmd() *cobra.Command {
	opts := &diffOptions{}

	cmd := &cobra.Command{
		Use:   "diff RUN_REF",
		Short: "Show diff for a run's worktree changes",
		Long: `Show the git diff for a run's worktree compared to the base branch.

RUN_REF can be a short ID (2-6 hex chars), ISSUE_ID#RUN_ID, or just ISSUE_ID (for latest run).

The diff tool is selected in priority order:
1. ORCH_DIFFTOOL environment variable
2. diff_tool in .orch/config.yaml
3. delta (if installed)
4. PAGER environment variable (usually less)
5. less (fallback)`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(args[0], opts)
		},
	}

	cmd.Flags().BoolVar(&opts.Stat, "stat", false, "Show diffstat summary only")
	cmd.Flags().StringVar(&opts.BaseBranch, "base", "", "Base branch to compare against (default: from config)")

	return cmd
}

func runDiff(refStr string, opts *diffOptions) error {
	client, err := requireDaemon()
	if err != nil {
		return err
	}

	// Get run info from daemon
	var resp *daemon.GetAttachInfoResponse

	if shortIDRegex.MatchString(refStr) {
		resp, err = client.GetAttachInfo("", "", refStr)
	} else {
		ref, parseErr := model.ParseRunRef(refStr)
		if parseErr != nil {
			return parseErr
		}
		resp, err = client.GetAttachInfo(ref.IssueID, ref.RunID, "")
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "run not found: %s\n", refStr)
		os.Exit(ExitRunNotFound)
		return err
	}

	if resp.WorktreePath == "" {
		fmt.Fprintf(os.Stderr, "run has no worktree: %s\n", refStr)
		os.Exit(ExitWorktreeError)
		return fmt.Errorf("run has no worktree")
	}

	// Load config for base branch and diff tool
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Determine base branch
	baseBranch := opts.BaseBranch
	if baseBranch == "" {
		baseBranch = cfg.GetBaseBranch()
	}

	// Get the run's branch for comparison
	branch := resp.Branch
	if branch == "" {
		branch = "HEAD"
	}

	// Build git diff command
	diffArgs := buildDiffArgs(baseBranch, branch, opts.Stat)

	// Get diff tool
	diffTool := getDiffTool(cfg)

	// Execute diff
	return executeDiff(resp.WorktreePath, diffArgs, diffTool)
}

func buildDiffArgs(baseBranch, branch string, stat bool) []string {
	args := []string{"diff"}
	if stat {
		args = append(args, "--stat")
	}
	// Use merge-base syntax for cleaner diff
	args = append(args, fmt.Sprintf("%s...%s", baseBranch, branch))
	return args
}

func getDiffTool(cfg *config.Config) string {
	// Priority order:
	// 1. ORCH_DIFFTOOL env var
	if tool := os.Getenv("ORCH_DIFFTOOL"); tool != "" {
		return tool
	}

	// 2. diff_tool in config
	if cfg.DiffTool != "" {
		return cfg.DiffTool
	}

	// 3. delta (if installed)
	if _, err := exec.LookPath("delta"); err == nil {
		return "delta"
	}

	// 4. PAGER env var
	if pager := os.Getenv("PAGER"); pager != "" {
		return pager
	}

	// 5. Fallback to less
	return "less"
}

func executeDiff(worktreePath string, gitArgs []string, diffTool string) error {
	// Create git command
	gitCmd := exec.Command("git", gitArgs...)
	gitCmd.Dir = worktreePath
	gitCmd.Stderr = os.Stderr

	// If diffTool is "cat" or empty, just output directly
	if diffTool == "cat" || diffTool == "" {
		gitCmd.Stdout = os.Stdout
		return gitCmd.Run()
	}

	// Pipe git output through diff tool
	pagerCmd := exec.Command(diffTool)
	pagerCmd.Stdin, _ = gitCmd.StdoutPipe()
	pagerCmd.Stdout = os.Stdout
	pagerCmd.Stderr = os.Stderr

	if err := pagerCmd.Start(); err != nil {
		// Fall back to direct output if pager fails
		gitCmd.Stdout = os.Stdout
		return gitCmd.Run()
	}

	if err := gitCmd.Start(); err != nil {
		return err
	}

	// Wait for git to finish first
	gitErr := gitCmd.Wait()

	// Close stdin to signal EOF to pager
	pagerCmd.Wait()

	return gitErr
}
