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
		Use:   "resolve RUN_REF",
		Short: "Resolve a run",
		Long: `Mark a run as resolved and hide it from default 'orch ps' output.

RUN_REF can be ISSUE_ID#RUN_ID, ISSUE_ID (latest), or a short ID.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runResolve(args[0], opts)
		},
	}

	cmd.Flags().BoolVar(&opts.Force, "force", false, "Resolve even if run is not done or pr_open")

	return cmd
}

func runResolve(refStr string, opts *resolveOptions) error {
	st, err := getStore()
	if err != nil {
		return err
	}

	run, err := resolveRun(st, refStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run not found: %s\n", refStr)
		os.Exit(ExitRunNotFound)
		return err
	}

	if run.Status == model.StatusCompleted {
		if !globalOpts.Quiet {
			fmt.Printf("%s#%s already completed\n", run.IssueID, run.RunID)
		}
		return nil
	}

	if !opts.Force && run.Status != model.StatusDone && run.Status != model.StatusPROpen {
		return fmt.Errorf("run %s#%s is %s; use --force to resolve anyway", run.IssueID, run.RunID, run.Status)
	}

	// Mark run as completed (operational lifecycle state)
	if err := st.AppendEvent(run.Ref(), model.NewStatusEvent(model.StatusCompleted)); err != nil {
		return fmt.Errorf("failed to append completed event: %w", err)
	}

	// Mark issue as resolved (issue resolution state)
	if err := st.SetIssueStatus(run.IssueID, model.IssueStatusResolved); err != nil {
		// Log error but don't fail the command since the run was completed
		fmt.Fprintf(os.Stderr, "warning: failed to mark issue as resolved: %v\n", err)
	}

	if !globalOpts.Quiet {
		fmt.Printf("resolved: %s#%s\n", run.IssueID, run.RunID)
	}

	return nil
}
