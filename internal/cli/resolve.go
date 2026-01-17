package cli

import (
	"fmt"
	"os"

	"github.com/s22625/orch/internal/model"
	"github.com/spf13/cobra"
)

type resolveOptions struct {
	Force bool
}

func newResolveCmd() *cobra.Command {
	opts := &resolveOptions{}

	cmd := &cobra.Command{
		Use:   "resolve ISSUE_ID",
		Short: "Mark an issue as resolved",
		Long: `Mark an issue as resolved. This indicates the issue specification has been
completed and no further work is needed.

This updates the issue's status in its frontmatter from 'open' to 'resolved'.
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
	if testBypassDaemon {
		return runResolveDirect(issueID, opts)
	}

	client, err := requireDaemon()
	if err != nil {
		return err
	}

	resp, err := client.ResolveIssue(issueID, opts.Force)
	if err != nil {
		if err.Error() == "daemon error: not_found" {
			fmt.Fprintf(os.Stderr, "issue not found: %s\n", issueID)
			os.Exit(ExitRunNotFound)
		}
		if err.Error() == "daemon error: no_completed_runs" {
			return fmt.Errorf("issue %s has no completed runs; use --force to resolve anyway", issueID)
		}
		return err
	}

	if !globalOpts.Quiet {
		fmt.Printf("resolved: %s\n", resp.IssueID)
	}

	return nil
}

func runResolveDirect(issueID string, opts *resolveOptions) error {
	st, err := getStore()
	if err != nil {
		return err
	}

	issue, err := st.ResolveIssue(issueID)
	if err != nil {
		if err.Error() == "issue not found: "+issueID {
			fmt.Fprintf(os.Stderr, "issue not found: %s\n", issueID)
			os.Exit(ExitRunNotFound)
		}
		return err
	}

	if issue.Status == model.IssueStatusResolved {
		if !globalOpts.Quiet {
			fmt.Printf("resolved: %s\n", issueID)
		}
		return nil
	}

	if !opts.Force {
		runs, _ := st.ListRuns(nil)
		hasCompletedRun := false
		for _, run := range runs {
			if run.IssueID == issueID && (run.Status == model.StatusDone || run.Status == model.StatusPROpen) {
				hasCompletedRun = true
				break
			}
		}
		if !hasCompletedRun {
			return fmt.Errorf("issue %s has no completed runs; use --force to resolve anyway", issueID)
		}
	}

	if err := st.SetIssueStatus(issueID, model.IssueStatusResolved); err != nil {
		return err
	}

	if !globalOpts.Quiet {
		fmt.Printf("resolved: %s\n", issueID)
	}

	return nil
}
