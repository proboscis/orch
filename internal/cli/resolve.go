package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

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
	api, err := getAPI()
	if err != nil {
		return err
	}

	ctx := context.Background()
	err = api.ResolveIssue(ctx, issueID, opts.Force)
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "not_found") || strings.Contains(errStr, "not found") {
			fmt.Fprintf(os.Stderr, "issue not found: %s\n", issueID)
			os.Exit(ExitRunNotFound)
		}
		if strings.Contains(errStr, "no_completed_runs") || strings.Contains(errStr, "no completed runs") {
			return fmt.Errorf("issue %s has no completed runs; use --force to resolve anyway", issueID)
		}
		return err
	}

	if !globalOpts.Quiet {
		fmt.Printf("resolved: %s\n", issueID)
	}

	return nil
}
