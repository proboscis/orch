package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/s22625/orch/internal/daemon"
	"github.com/s22625/orch/internal/model"
	"github.com/s22625/orch/internal/store"
	"github.com/s22625/orch/internal/tmux"
	"github.com/spf13/cobra"
)

type repairOptions struct {
	DryRun bool
	Force  bool
}

func newRepairCmd() *cobra.Command {
	opts := &repairOptions{}

	cmd := &cobra.Command{
		Use:   "repair",
		Short: "Repair system state",
		Long: `Repair system state by fixing inconsistencies.

This command will:
- Restart the daemon if it's not running or unhealthy
- Mark "running" runs with no tmux session as failed
- Report orphaned sessions and worktrees`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepair(opts)
		},
	}

	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Report problems without fixing them")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Fix without confirmation")

	return cmd
}

func runRepair(opts *repairOptions) error {
	st, err := getStore()
	if err != nil {
		return err
	}

	vaultPath := st.VaultPath()
	problemsFound := 0
	problemsFixed := 0

	// 1. Check and repair daemon
	fmt.Println("Checking daemon...")
	daemonFixed, err := repairDaemon(vaultPath, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  error: %v\n", err)
	}
	if daemonFixed {
		problemsFound++
		if !opts.DryRun {
			problemsFixed++
		}
	}

	// 2. Check and repair stale runs
	fmt.Println("Checking runs...")
	staleFixed, err := repairStaleRuns(st, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  error: %v\n", err)
	}
	problemsFound += staleFixed
	if !opts.DryRun {
		problemsFixed += staleFixed
	}

	// 3. Report orphaned sessions
	fmt.Println("Checking for orphaned sessions...")
	orphanedSessions := findOrphanedSessions(st)
	if len(orphanedSessions) > 0 {
		problemsFound += len(orphanedSessions)
		fmt.Printf("  found %d orphaned tmux sessions:\n", len(orphanedSessions))
		for _, s := range orphanedSessions {
			fmt.Printf("    - %s\n", s)
		}
		if !opts.DryRun && opts.Force {
			for _, s := range orphanedSessions {
				if err := tmux.KillSession(s); err != nil {
					fmt.Fprintf(os.Stderr, "    failed to kill %s: %v\n", s, err)
				} else {
					problemsFixed++
					fmt.Printf("    killed: %s\n", s)
				}
			}
		} else if !opts.DryRun {
			fmt.Println("  use --force to kill orphaned sessions")
		}
	}

	// Summary
	fmt.Println()
	if problemsFound == 0 {
		fmt.Println("No problems found.")
	} else if opts.DryRun {
		fmt.Printf("Found %d problems (dry-run, no changes made)\n", problemsFound)
	} else {
		fmt.Printf("Fixed %d/%d problems\n", problemsFixed, problemsFound)
	}

	if problemsFound > 0 && !opts.DryRun {
		os.Exit(1) // Exit code 1 = repairs were made
	}

	return nil
}

// repairDaemon checks if daemon is running and starts it if not
func repairDaemon(vaultPath string, opts *repairOptions) (bool, error) {
	if daemon.IsRunning(vaultPath) {
		pid := daemon.GetRunningPID(vaultPath)
		fmt.Printf("  daemon running (pid=%d)\n", pid)
		return false, nil
	}

	fmt.Println("  daemon not running")

	if opts.DryRun {
		fmt.Println("  would start daemon")
		return true, nil
	}

	// Kill any stale PID file
	daemon.RemovePID(vaultPath)

	// Start daemon
	pid, err := daemon.StartInBackground(vaultPath)
	if err != nil {
		return true, fmt.Errorf("failed to start daemon: %w", err)
	}

	// Wait a moment for it to actually start
	time.Sleep(200 * time.Millisecond)

	if daemon.IsRunning(vaultPath) {
		fmt.Printf("  started daemon (pid=%d)\n", pid)
		return true, nil
	}

	return true, fmt.Errorf("daemon failed to start")
}

// repairStaleRuns finds runs marked as "running" but with no tmux session
func repairStaleRuns(st store.Store, opts *repairOptions) (int, error) {
	runs, err := st.ListRuns(&store.ListRunsFilter{
		Status: []model.Status{model.StatusRunning, model.StatusBooting},
	})
	if err != nil {
		return 0, err
	}

	fixed := 0
	for _, run := range runs {
		sessionName := run.TmuxSession
		if sessionName == "" {
			sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
		}

		if tmux.HasSession(sessionName) {
			continue // Session exists, run is fine
		}

		fmt.Printf("  %s#%s: marked running but no session\n", run.IssueID, run.RunID)
		fixed++

		if opts.DryRun {
			fmt.Printf("    would mark as failed\n")
			continue
		}

		// Mark as failed
		ref := &model.RunRef{IssueID: run.IssueID, RunID: run.RunID}
		event := model.NewStatusEvent(model.StatusFailed)
		if err := st.AppendEvent(ref, event); err != nil {
			fmt.Fprintf(os.Stderr, "    failed to update status: %v\n", err)
		} else {
			fmt.Printf("    marked as failed\n")
		}
	}

	if fixed == 0 {
		fmt.Println("  all running runs have active sessions")
	}

	return fixed, nil
}

// findOrphanedSessions finds tmux sessions that don't correspond to any run
func findOrphanedSessions(st store.Store) []string {
	// Get all tmux sessions
	sessions, err := tmux.ListSessions()
	if err != nil || len(sessions) == 0 {
		return nil
	}

	// Get all runs
	runs, err := st.ListRuns(&store.ListRunsFilter{})
	if err != nil {
		return nil
	}

	// Build set of expected session names
	expectedSessions := make(map[string]bool)
	for _, run := range runs {
		sessionName := run.TmuxSession
		if sessionName == "" {
			sessionName = model.GenerateTmuxSession(run.IssueID, run.RunID)
		}
		expectedSessions[sessionName] = true
	}

	// Find orphaned sessions (orch sessions that don't match any run)
	var orphaned []string
	for _, s := range sessions {
		// Only consider sessions that look like orch sessions
		if len(s) > 4 && s[:4] == "run-" {
			if !expectedSessions[s] {
				orphaned = append(orphaned, s)
			}
		}
	}

	return orphaned
}
