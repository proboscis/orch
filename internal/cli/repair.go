package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/s22625/orch/internal/agent"
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

	projectRoot, err := getProjectRoot()
	if err != nil {
		return fmt.Errorf("project root required for repair: %w", err)
	}
	problemsFound := 0
	problemsFixed := 0

	fmt.Println("Checking daemon registry...")
	registryFixed, err := repairDaemonRegistry(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  error: %v\n", err)
	}
	if registryFixed > 0 {
		problemsFound += registryFixed
		if !opts.DryRun {
			problemsFixed += registryFixed
		}
	}

	fmt.Println("Checking daemon...")
	daemonFixed, err := repairDaemon(projectRoot, opts)
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

func repairDaemon(projectRoot string, opts *repairOptions) (bool, error) {
	if daemon.IsRunning(projectRoot) {
		pid := daemon.GetRunningPID(projectRoot)
		fmt.Printf("  daemon running (pid=%d)\n", pid)

		stale, err := daemon.IsStaleBinary(projectRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  warning: could not check binary staleness: %v\n", err)
		} else if stale {
			fmt.Println("  WARNING: daemon is running stale binary (code updated since start)")
			if opts.DryRun {
				fmt.Println("  would restart daemon with new binary")
				return true, nil
			}

			oldMeta, _ := daemon.ReadMetadata(projectRoot)
			if err := daemon.RestartDaemon(projectRoot); err != nil {
				return true, fmt.Errorf("failed to restart daemon: %w", err)
			}

			if waitForDaemonRestart(projectRoot, oldMeta, 2*time.Second) {
				newPid := daemon.GetRunningPID(projectRoot)
				fmt.Printf("  restarted daemon with new binary (pid=%d)\n", newPid)
				return true, nil
			}
			return true, fmt.Errorf("daemon restart failed")
		}
		return false, nil
	}

	fmt.Println("  daemon not running")

	if opts.DryRun {
		fmt.Println("  would start daemon")
		return true, nil
	}

	daemon.RemovePID(projectRoot)

	pid, err := daemon.StartInBackground(projectRoot)
	if err != nil {
		return true, fmt.Errorf("failed to start daemon: %w", err)
	}

	time.Sleep(200 * time.Millisecond)

	if daemon.IsRunning(projectRoot) {
		fmt.Printf("  started daemon (pid=%d)\n", pid)
		return true, nil
	}

	return true, fmt.Errorf("daemon failed to start")
}

// repairStaleRuns finds runs marked as "running" but with no active agent session
func repairStaleRuns(st store.Store, opts *repairOptions) (int, error) {
	runs, err := st.ListRuns(&store.ListRunsFilter{
		Status: []model.Status{model.StatusRunning, model.StatusBooting},
	})
	if err != nil {
		return 0, err
	}

	fixed := 0
	for _, run := range runs {
		// Use AgentManager to check if agent is alive (works for both tmux and opencode)
		mgr := agent.GetManager(run)
		if mgr.IsAlive(run) {
			continue // Agent is alive, run is fine
		}

		fmt.Printf("  %s#%s: marked %s but agent not alive\n", run.IssueID, run.RunID, run.Status)
		fixed++

		newStatus := model.StatusFailed
		if run.Agent == "opencode" {
			newStatus = model.StatusUnknown
		}

		if opts.DryRun {
			fmt.Printf("    would mark as %s\n", newStatus)
			continue
		}

		ref := &model.RunRef{IssueID: run.IssueID, RunID: run.RunID}
		event := model.NewStatusEvent(newStatus)
		if err := st.AppendEvent(ref, event); err != nil {
			fmt.Fprintf(os.Stderr, "    failed to update status: %v\n", err)
		} else {
			fmt.Printf("    marked as %s\n", newStatus)
		}
	}

	if fixed == 0 {
		fmt.Println("  all running runs have active agents")
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

func waitForDaemonRestart(projectRoot string, oldMeta *daemon.DaemonMetadata, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !daemon.IsRunning(projectRoot) {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		newMeta, err := daemon.ReadMetadata(projectRoot)
		if err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		if oldMeta == nil || newMeta.StartedAt.After(oldMeta.StartedAt) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func repairDaemonRegistry(opts *repairOptions) (int, error) {
	if err := daemon.CleanupStaleRegistrations(); err != nil {
		return 0, err
	}

	infos, err := daemon.ListAllDaemons()
	if err != nil {
		return 0, err
	}

	fixed := 0
	var unhealthy []*daemon.DaemonInfo

	for _, info := range infos {
		if !info.IsHealthy {
			unhealthy = append(unhealthy, info)
		}
	}

	if len(unhealthy) > 0 {
		fmt.Printf("  found %d unhealthy daemon(s) (running but socket unavailable):\n", len(unhealthy))
		for _, info := range unhealthy {
			fmt.Printf("    - pid=%d project=%s\n", info.PID, info.ProjectRoot)
			fixed++
			if !opts.DryRun && opts.Force {
				if err := daemon.KillDaemon(info.ProjectRoot); err != nil {
					fmt.Fprintf(os.Stderr, "      failed to kill: %v\n", err)
				} else {
					fmt.Printf("      killed unhealthy daemon\n")
				}
			}
		}
		if !opts.DryRun && !opts.Force && len(unhealthy) > 0 {
			fmt.Println("  use --force to kill unhealthy daemons")
		}
	}

	if len(infos) > 0 && len(unhealthy) == 0 {
		fmt.Printf("  %d daemon(s) registered, all healthy\n", len(infos))
	} else if len(infos) == 0 {
		fmt.Println("  no daemons registered")
	}

	return fixed, nil
}
