package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/s22625/orch/internal/orchapi"
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
	ctx := context.Background()

	api, err := getAPIForListing()
	if err != nil {
		return err
	}

	ref, err := orchapi.ParseRunRef(refStr)
	if err != nil {
		return err
	}

	if opts.Stat {
		stats, err := api.GetDiffStats(ctx, ref)
		if err != nil {
			return err
		}
		fmt.Printf(" %d files changed, %d insertions(+), %d deletions(-)\n",
			stats.FilesChanged, stats.Additions, stats.Deletions)
		for _, f := range stats.Files {
			fmt.Printf(" %s\n", f)
		}
		return nil
	}

	diffContent, err := api.GetDiff(ctx, ref)
	if err != nil {
		return err
	}

	cfg, err := api.GetConfig(ctx, "")
	if err != nil {
		cfg = nil
	}

	diffTool := getDiffTool(cfg)
	return displayDiff(diffContent, diffTool)
}

func getDiffTool(cfg *orchapi.Config) string {
	if tool := os.Getenv("ORCH_DIFFTOOL"); tool != "" {
		return tool
	}

	if cfg != nil && cfg.DiffTool != "" {
		return cfg.DiffTool
	}

	if _, err := exec.LookPath("delta"); err == nil {
		return "delta"
	}

	if pager := os.Getenv("PAGER"); pager != "" {
		return pager
	}

	return "less"
}

func displayDiff(content string, diffTool string) error {
	if diffTool == "cat" || diffTool == "" || content == "" {
		fmt.Print(content)
		return nil
	}

	pagerCmd := exec.Command(diffTool)
	pagerCmd.Stdin = strings.NewReader(content)
	pagerCmd.Stdout = os.Stdout
	pagerCmd.Stderr = os.Stderr

	if err := pagerCmd.Run(); err != nil {
		fmt.Print(content)
	}
	return nil
}
