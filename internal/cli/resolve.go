package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/proboscis/orch/internal/model"
	"github.com/proboscis/orch/internal/orchapi"
	"github.com/spf13/cobra"
)

type resolveOptions struct {
	Force bool
}

type resolveDeps struct {
	getAPI func() (orchapi.OrchAPI, error)
}

func defaultResolveDeps() *resolveDeps {
	return &resolveDeps{getAPI: getAPI}
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
	return runResolveWithDeps(context.Background(), issueID, opts, defaultResolveDeps())
}

func runResolveWithDeps(ctx context.Context, issueID string, opts *resolveOptions, deps *resolveDeps) error {
	api, err := deps.getAPI()
	if err != nil {
		return err
	}

	err = api.ResolveIssue(ctx, model.IssueID(issueID), opts.Force)
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
