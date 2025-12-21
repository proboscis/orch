package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
	"github.com/s22625/orch/internal/tmux"
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
	st, err := getStore()
	if err != nil {
		return err
	}

	// Try as short ID first
	if shortIDRegex.MatchString(refStr) {
		run, err := st.GetRunByShortID(refStr)
		if err == nil {
			return stopRun(st, run, opts)
		}
		// Fall through to try as regular ref
	}

	ref, err := model.ParseRunRef(refStr)
	if err != nil {
		return err
	}

	// If no specific run ID, stop ALL active runs for this issue
	if ref.IsLatest() {
		return stopIssueRuns(st, ref.IssueID, opts)
	}

	// Stop specific run
	run, err := st.GetRun(ref)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run not found: %s\n", refStr)
		os.Exit(ExitRunNotFound)
		return err
	}

	return stopRun(st, run, opts)
}

func stopIssueRuns(st store.Store, issueID string, opts *stopOptions) error {
	runs, err := st.ListRuns(&store.ListRunsFilter{
		IssueID: issueID,
		Status:  []model.Status{model.StatusRunning, model.StatusBooting, model.StatusBlocked, model.StatusQueued},
	})
	if err != nil {
		return err
	}

	if len(runs) == 0 {
		if !globalOpts.Quiet {
			fmt.Printf("No active runs for issue: %s\n", issueID)
		}
		return nil
	}

	var stopped int
	for _, run := range runs {
		if err := stopRun(st, run, opts); err != nil {
			fmt.Fprintf(os.Stderr, "failed to stop %s#%s: %v\n", run.IssueID, run.RunID, err)
		} else {
			stopped++
		}
	}

	if !globalOpts.Quiet && stopped > 1 {
		fmt.Printf("stopped %d runs for %s\n", stopped, issueID)
	}

	return nil
}

func runStopAll(opts *stopOptions) error {
	st, err := getStore()
	if err != nil {
		return err
	}

	runs, err := st.ListRuns(&store.ListRunsFilter{
		Status: []model.Status{model.StatusRunning, model.StatusBooting},
	})
	if err != nil {
		return err
	}

	if len(runs) == 0 {
		if !globalOpts.Quiet {
			fmt.Println("No running runs to stop")
		}
		return nil
	}

	for _, run := range runs {
		if err := stopRun(st, run, opts); err != nil {
			fmt.Fprintf(os.Stderr, "failed to stop %s#%s: %v\n", run.IssueID, run.RunID, err)
		}
	}

	return nil
}

func stopRun(st interface{ AppendEvent(ref *model.RunRef, event *model.Event) error }, run *model.Run, opts *stopOptions) error {
	// Skip if already terminal
	if run.Status == model.StatusDone || run.Status == model.StatusFailed || run.Status == model.StatusCanceled {
		if !globalOpts.Quiet {
			fmt.Printf("%s#%s already %s\n", run.IssueID, run.RunID, run.Status)
		}
		return nil
	}

	sessionName := run.TmuxSession
	if sessionName == "" {
		sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
	}

	// Kill tmux session if it exists
	if tmux.HasSession(sessionName) {
		if err := tmux.KillSession(sessionName); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to kill session %s: %v\n", sessionName, err)
		} else if !globalOpts.Quiet {
			fmt.Printf("killed session: %s\n", sessionName)
		}
	}

	// Append canceled event
	ref := &model.RunRef{IssueID: run.IssueID, RunID: run.RunID}
	event := &model.Event{
		Timestamp: time.Now(),
		Type:      "status",
		Name:      string(model.StatusCanceled),
	}

	if err := st.AppendEvent(ref, event); err != nil {
		return fmt.Errorf("failed to append canceled event: %w", err)
	}

	if !globalOpts.Quiet {
		fmt.Printf("stopped: %s#%s\n", run.IssueID, run.RunID)
	}

	return nil
}
