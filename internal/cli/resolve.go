package cli

import (
	"fmt"
	"os"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
	"github.com/spf13/cobra"
)

type resolveOptions struct {
	Force bool
}

func newResolveCmd() *cobra.Command {
	opts := &resolveOptions{}

	cmd := &cobra.Command{
		Use:   "resolve ISSUE_ID",
		Short: "Mark an issue as completed",
		Long: `Mark an issue as completed. This indicates the issue specification has been
finished and no further work is needed.

This updates the issue's status in its frontmatter from 'open' to 'completed'.
Note: This does not change run statuses - runs have their own lifecycle states.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runResolve(args[0], opts)
		},
	}

	cmd.Flags().BoolVar(&opts.Force, "force", false, "Resolve even if no completed runs exist")

	return cmd
}

func runResolve(issueID string, opts *resolveOptions) error {
	st, err := getStore()
	if err != nil {
		return err
	}

	issue, err := st.ResolveIssue(issueID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "issue not found: %s\n", issueID)
		os.Exit(ExitRunNotFound)
		return err
	}

	if issue.Status == model.IssueStatusCompleted {
		if !globalOpts.Quiet {
			fmt.Printf("issue %s already completed\n", issueID)
		}
		return nil
	}

	// Check if there are any completed runs (done or pr_open) unless --force is used
	if !opts.Force {
		filter := store.ListRunsFilter{IssueID: issueID}
		runs, err := st.ListRuns(&filter)
		if err != nil {
			return fmt.Errorf("failed to list runs: %w", err)
		}

		hasCompletedRun := false
		for _, run := range runs {
			if run.Status == model.StatusDone || run.Status == model.StatusPROpen {
				hasCompletedRun = true
				break
			}
		}

		if !hasCompletedRun {
			return fmt.Errorf("issue %s has no completed runs; use --force to resolve anyway", issueID)
		}
	}

	if err := st.SetIssueStatus(issueID, model.IssueStatusCompleted); err != nil {
		return fmt.Errorf("failed to mark issue as completed: %w", err)
	}

	if !globalOpts.Quiet {
		fmt.Printf("completed: %s\n", issueID)
	}

	return nil
}
