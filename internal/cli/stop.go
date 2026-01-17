package cli

import (
	"fmt"
	"os"

	"github.com/s22625/orch/internal/model"
	"github.com/spf13/cobra"
)

type stopOptions struct {
	All   bool
	Force bool
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
			if opts.All {
				return runStopAll(opts)
			}
			if len(args) == 0 {
				return fmt.Errorf("RUN_REF required (or use --all)")
			}
			return runStop(args[0], opts)
		},
	}

	cmd.Flags().BoolVar(&opts.All, "all", false, "Stop all running runs")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Force stop even if session doesn't exist")

	return cmd
}

func runStop(refStr string, opts *stopOptions) error {
	client, err := requireDaemon()
	if err != nil {
		return err
	}

	var issueID, runID string

	if shortIDRegex.MatchString(refStr) {
		resp, err := client.GetRunByShortID(refStr)
		if err == nil && resp.Run != nil {
			issueID = resp.Run.IssueID
			runID = resp.Run.RunID
		} else if len(refStr) == 6 {
			fmt.Fprintf(os.Stderr, "run not found: %s\n", refStr)
			os.Exit(ExitRunNotFound)
			return fmt.Errorf("run not found: %s", refStr)
		}
	}

	if issueID == "" {
		ref, err := model.ParseRunRef(refStr)
		if err != nil {
			return err
		}
		issueID = ref.IssueID
		if !ref.IsLatest() {
			runID = ref.RunID
		}
	}

	resp, err := client.StopRun(issueID, runID, opts.Force)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(ExitInternalError)
		return err
	}

	if !globalOpts.Quiet {
		if len(resp.StoppedRuns) == 0 {
			if runID != "" {
				fmt.Printf("no active run to stop: %s#%s\n", issueID, runID)
			} else {
				fmt.Printf("no active runs for issue: %s\n", issueID)
			}
		} else if len(resp.StoppedRuns) == 1 {
			fmt.Printf("stopped: %s#%s\n", issueID, resp.StoppedRuns[0])
		} else {
			fmt.Printf("stopped %d runs for %s\n", resp.StoppedCount, issueID)
		}
	}

	return nil
}

func runStopAll(opts *stopOptions) error {
	client, err := requireDaemon()
	if err != nil {
		return err
	}

	resp, err := client.ListRuns("", []string{"running", "booting", "blocked", "blocked_api", "queued"}, 0, "")
	if err != nil {
		return err
	}

	if len(resp.Runs) == 0 {
		if !globalOpts.Quiet {
			fmt.Println("No running runs to stop")
		}
		return nil
	}

	stoppedCount := 0
	for _, run := range resp.Runs {
		_, err := client.StopRun(run.IssueID, run.RunID, opts.Force)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to stop %s#%s: %v\n", run.IssueID, run.RunID, err)
		} else {
			stoppedCount++
			if !globalOpts.Quiet {
				fmt.Printf("stopped: %s#%s\n", run.IssueID, run.RunID)
			}
		}
	}

	return nil
}
