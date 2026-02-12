package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/s22625/orch/internal/orchapi"
	"github.com/spf13/cobra"
)

type stopOptions struct {
	All   bool
	Force bool
}

type stopDeps struct {
	getAPI      func() (orchapi.OrchAPI, error)
	parseRunRef func(string) (orchapi.RunRef, error)
	stdout      io.Writer
	stderr      io.Writer
	exit        func(int)
}

func defaultStopDeps() *stopDeps {
	return &stopDeps{
		getAPI:      getAPI,
		parseRunRef: orchapi.ParseRunRef,
		stdout:      os.Stdout,
		stderr:      os.Stderr,
		exit:        os.Exit,
	}
}

func newStopCmd() *cobra.Command {
	opts := &stopOptions{}

	cmd := &cobra.Command{
		Use:   "stop [ISSUE_ID | ISSUE_ID#RUN_ID]",
		Short: "Stop running runs",
		Long: `Stop runs by killing their tmux sessions and marking them as canceled.

If given an ISSUE_ID (without #RUN_ID), stops ALL active runs for that issue.
If given a specific ISSUE_ID#RUN_ID, stops only that run.

If the run is already stopped (done/failed/canceled), this is a no-op.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			deps := defaultStopDeps()
			if opts.All {
				return runStopAllWithDeps(opts, deps)
			}
			if len(args) == 0 {
				return fmt.Errorf("RUN_REF required (or use --all)")
			}
			return runStopWithDeps(args[0], opts, deps)
		},
	}

	cmd.Flags().BoolVar(&opts.All, "all", false, "Stop all running runs")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Force stop even if session doesn't exist")

	return cmd
}

func runStop(refStr string, opts *stopOptions) error {
	return runStopWithDeps(refStr, opts, defaultStopDeps())
}

func runStopWithDeps(refStr string, opts *stopOptions, deps *stopDeps) error {
	ctx := context.Background()
	api, err := deps.getAPI()
	if err != nil {
		return err
	}

	ref, err := deps.parseRunRef(refStr)
	if err != nil {
		return err
	}

	run, err := api.ResolveRun(ctx, ref)
	if err != nil {
		if errors.Is(err, orchapi.ErrNotFound) {
			fmt.Fprintf(deps.stderr, "run not found: %s\n", refStr)
			deps.exit(ExitRunNotFound)
			return err
		}
		return err
	}

	if ref.IsLatest() {
		return stopAllForIssue(ctx, api, run.IssueID, opts, deps)
	}

	if err := api.StopRun(ctx, orchapi.RunRef{IssueID: run.IssueID, RunID: run.RunID}); err != nil {
		fmt.Fprintf(deps.stderr, "error: %v\n", err)
		deps.exit(ExitInternalError)
		return err
	}

	if !globalOpts.Quiet {
		fmt.Fprintf(deps.stdout, "stopped: %s#%s\n", run.IssueID, run.RunID)
	}

	return nil
}

func stopAllForIssue(ctx context.Context, api orchapi.OrchAPI, issueID string, opts *stopOptions, deps *stopDeps) error {
	resp, err := api.ListRuns(ctx, &orchapi.ListRunsFilter{
		IssueID: issueID,
		Status: []orchapi.RunStatus{
			orchapi.RunStatusRunning,
			orchapi.RunStatusBooting,
			orchapi.RunStatusBlocked,
			orchapi.RunStatusBlockedAPI,
			orchapi.RunStatusQueued,
		},
	})
	if err != nil {
		return err
	}

	if len(resp.Runs) == 0 {
		if !globalOpts.Quiet {
			fmt.Fprintf(deps.stdout, "no active runs for issue: %s\n", issueID)
		}
		return nil
	}

	stoppedCount := 0
	for _, run := range resp.Runs {
		if err := api.StopRun(ctx, orchapi.RunRef{IssueID: run.IssueID, RunID: run.RunID}); err != nil {
			fmt.Fprintf(deps.stderr, "failed to stop %s#%s: %v\n", run.IssueID, run.RunID, err)
		} else {
			stoppedCount++
			if !globalOpts.Quiet {
				fmt.Fprintf(deps.stdout, "stopped: %s#%s\n", run.IssueID, run.RunID)
			}
		}
	}

	if !globalOpts.Quiet && stoppedCount > 1 {
		fmt.Fprintf(deps.stdout, "stopped %d runs for %s\n", stoppedCount, issueID)
	}

	return nil
}

func runStopAll(opts *stopOptions) error {
	return runStopAllWithDeps(opts, defaultStopDeps())
}

func runStopAllWithDeps(opts *stopOptions, deps *stopDeps) error {
	ctx := context.Background()
	api, err := deps.getAPI()
	if err != nil {
		return err
	}

	resp, err := api.ListRuns(ctx, &orchapi.ListRunsFilter{
		Status: []orchapi.RunStatus{
			orchapi.RunStatusRunning,
			orchapi.RunStatusBooting,
			orchapi.RunStatusBlocked,
			orchapi.RunStatusBlockedAPI,
			orchapi.RunStatusQueued,
		},
	})
	if err != nil {
		return err
	}

	if len(resp.Runs) == 0 {
		if !globalOpts.Quiet {
			fmt.Fprintln(deps.stdout, "No running runs to stop")
		}
		return nil
	}

	stoppedCount := 0
	for _, run := range resp.Runs {
		if err := api.StopRun(ctx, orchapi.RunRef{IssueID: run.IssueID, RunID: run.RunID}); err != nil {
			fmt.Fprintf(deps.stderr, "failed to stop %s#%s: %v\n", run.IssueID, run.RunID, err)
		} else {
			stoppedCount++
			if !globalOpts.Quiet {
				fmt.Fprintf(deps.stdout, "stopped: %s#%s\n", run.IssueID, run.RunID)
			}
		}
	}

	return nil
}
